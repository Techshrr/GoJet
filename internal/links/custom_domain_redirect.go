package links

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
)

const AccessClaimDomainUnavailable AccessClaimState = "domain_unavailable"

// GetByHostCodeForRedirect performs the first runtime custom-domain gate before
// destination-risk resolution or target selection. The Link and domain
// authority are observed in one short MySQL transaction. Official-host Links
// remain unaffected by the additional authority.
func (s *MySQLStore) GetByHostCodeForRedirect(ctx context.Context, hostname, code string, now time.Time) (Link, error) {
	host, err := normalizeHostname(hostname)
	if err != nil {
		return Link{}, err
	}
	normalizedCode, err := normalizeCode(code)
	if err != nil || now.IsZero() {
		return Link{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback()

	link, err := scanLink(tx.QueryRowContext(ctx, linkSelect+` WHERE hostname = ? AND code = ? FOR UPDATE`, host, normalizedCode))
	if err != nil {
		return Link{}, err
	}
	canonical, err := s.authorizeCustomDomainRoutingTx(ctx, tx, link.WorkspaceID, link.Hostname, link.DomainKind, now.UTC())
	if err != nil || canonical != link.Hostname {
		return Link{}, ErrCustomDomainUnavailable
	}
	if err := tx.Commit(); err != nil {
		return Link{}, err
	}
	return link, nil
}

// ClaimRedirectAccessCurrentAuthority is retained for P05/P06 compatibility.
// P07 production wiring uses ClaimRedirectAccessCurrentAuthorityWithAnalytics,
// which adds a durable analytics outbox record in the same transaction as the
// accepted click claim.
func (s *MySQLStore) ClaimRedirectAccessCurrentAuthority(ctx context.Context, id, expectedVersion uint64, expectedFingerprint string, now time.Time) (Link, AccessClaimState, error) {
	current, state, _, err := s.claimRedirectAccessCurrentAuthority(ctx, id, expectedVersion, expectedFingerprint, now, nil)
	return current, state, err
}

// ClaimRedirectAccessCurrentAuthorityWithAnalytics performs the exact P05/P06
// final authority gate and, only after the click claim is allowed, writes one
// deterministic analytics outbox event before the transaction commits. This
// makes the accepted click and recoverable analytics intent atomic in MySQL.
func (s *MySQLStore) ClaimRedirectAccessCurrentAuthorityWithAnalytics(
	ctx context.Context,
	id, expectedVersion uint64,
	expectedFingerprint string,
	now time.Time,
	dimensions analytics.Dimensions,
) (Link, AccessClaimState, *analytics.Event, error) {
	return s.claimRedirectAccessCurrentAuthority(ctx, id, expectedVersion, expectedFingerprint, now, &dimensions)
}

func (s *MySQLStore) claimRedirectAccessCurrentAuthority(
	ctx context.Context,
	id, expectedVersion uint64,
	expectedFingerprint string,
	now time.Time,
	dimensions *analytics.Dimensions,
) (Link, AccessClaimState, *analytics.Event, error) {
	if id == 0 || expectedVersion == 0 || !validateFingerprint(expectedFingerprint) || now.IsZero() {
		return Link{}, AccessClaimConflict, nil, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, AccessClaimConflict, nil, err
	}
	defer tx.Rollback()

	current, err := scanLink(tx.QueryRowContext(ctx, linkSelect+` WHERE id = ? FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Link{}, AccessClaimDeleted, nil, nil
		}
		return Link{}, AccessClaimConflict, nil, err
	}
	if current.Version != expectedVersion || current.RiskFingerprint != expectedFingerprint {
		return current, AccessClaimConflict, nil, nil
	}
	if current.Status == "deleted" || current.DeletedAt != nil {
		return current, AccessClaimDeleted, nil, nil
	}
	if current.Status != "active" {
		return current, AccessClaimPaused, nil, nil
	}
	if _, err := s.authorizeCustomDomainRoutingTx(ctx, tx, current.WorkspaceID, current.Hostname, current.DomainKind, now.UTC()); err != nil {
		return current, AccessClaimDomainUnavailable, nil, nil
	}

	now = now.UTC()
	if current.ExpiresAt != nil && !current.ExpiresAt.After(now) {
		return current, AccessClaimExpired, nil, nil
	}
	if current.ClickLimit != nil && current.ClickCount >= *current.ClickLimit {
		return current, AccessClaimExhausted, nil, nil
	}
	if current.OneTime && current.ClickCount >= 1 {
		return current, AccessClaimExhausted, nil, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET click_count = click_count + 1
		WHERE id = ? AND version = ? AND risk_fingerprint = ?`,
		current.ID, current.Version, expectedFingerprint,
	)
	if err != nil {
		return Link{}, AccessClaimConflict, nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Link{}, AccessClaimConflict, nil, err
	}
	if affected != 1 {
		return current, AccessClaimConflict, nil, nil
	}
	current.ClickCount++

	var event *analytics.Event
	if dimensions != nil {
		measured := *dimensions
		// Until P12 owns the broader Campaign organization product, P07 uses the
		// existing Link UTM campaign field as the server-owned measurement key.
		// This is measurement association only; it does not create Campaign CRUD.
		if measured.CampaignID == "" {
			measured.CampaignID = current.UTM.Campaign
		}
		// External request metadata and user-authored UTM campaign text must not
		// turn analytics measurement into a redirect availability dependency. A
		// value outside the strict storage contract is represented as unknown.
		measured = analytics.SanitizeDimensions(measured)
		created, eventErr := analytics.NewClickEvent(current.WorkspaceID, current.ID, current.ClickCount, now, measured)
		if eventErr != nil {
			return Link{}, AccessClaimConflict, nil, eventErr
		}
		if eventErr := analytics.InsertOutboxTx(ctx, tx, created); eventErr != nil {
			return Link{}, AccessClaimConflict, nil, eventErr
		}
		event = &created
	}

	if err := tx.Commit(); err != nil {
		return Link{}, AccessClaimConflict, nil, err
	}
	return current, AccessClaimAllowed, event, nil
}

func (s *MySQLStore) MarkAnalyticsOutboxPublished(ctx context.Context, eventID, streamID string, at time.Time) error {
	return analytics.NewStore(s.db).MarkOutboxPublished(ctx, eventID, streamID, at)
}

func (s *MySQLStore) RecordAnalyticsOutboxPublishFailure(ctx context.Context, eventID string, publishErr error) error {
	return analytics.NewStore(s.db).RecordOutboxPublishFailure(ctx, eventID, publishErr)
}
