package links

import (
	"context"
	"database/sql"
	"errors"
	"time"
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

// ClaimRedirectAccessCurrentAuthority is the final authority gate immediately
// before a redirect may emit Location. It re-locks the Link, requires the exact
// version/fingerprint that received destination-risk allow, and re-checks
// current custom-domain routing authority in the same transaction as the
// click/one-time claim. A domain suspension that races the request therefore
// cannot reuse an earlier successful domain check.
func (s *MySQLStore) ClaimRedirectAccessCurrentAuthority(ctx context.Context, id, expectedVersion uint64, expectedFingerprint string, now time.Time) (Link, AccessClaimState, error) {
	if id == 0 || expectedVersion == 0 || !validateFingerprint(expectedFingerprint) || now.IsZero() {
		return Link{}, AccessClaimConflict, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	defer tx.Rollback()

	current, err := scanLink(tx.QueryRowContext(ctx, linkSelect+` WHERE id = ? FOR UPDATE`, id))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Link{}, AccessClaimDeleted, nil
		}
		return Link{}, AccessClaimConflict, err
	}
	if current.Version != expectedVersion || current.RiskFingerprint != expectedFingerprint {
		return current, AccessClaimConflict, nil
	}
	if current.Status == "deleted" || current.DeletedAt != nil {
		return current, AccessClaimDeleted, nil
	}
	if current.Status != "active" {
		return current, AccessClaimPaused, nil
	}
	if _, err := s.authorizeCustomDomainRoutingTx(ctx, tx, current.WorkspaceID, current.Hostname, current.DomainKind, now.UTC()); err != nil {
		return current, AccessClaimDomainUnavailable, nil
	}

	now = now.UTC()
	if current.ExpiresAt != nil && !current.ExpiresAt.After(now) {
		return current, AccessClaimExpired, nil
	}
	if current.ClickLimit != nil && current.ClickCount >= *current.ClickLimit {
		return current, AccessClaimExhausted, nil
	}
	if current.OneTime && current.ClickCount >= 1 {
		return current, AccessClaimExhausted, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET click_count = click_count + 1
		WHERE id = ? AND version = ? AND risk_fingerprint = ?`,
		current.ID, current.Version, expectedFingerprint,
	)
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	if affected != 1 {
		return current, AccessClaimConflict, nil
	}
	current.ClickCount++
	if err := tx.Commit(); err != nil {
		return Link{}, AccessClaimConflict, err
	}
	return current, AccessClaimAllowed, nil
}
