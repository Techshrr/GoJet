package links

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
)

// QRSourceAuthority is the P08 read-only view of the exact-current Link
// authority. It intentionally contains no customer destination. QR encoders use
// PublicURL so a QR can never become an alternate destination-risk path.
type QRSourceAuthority struct {
	Link      Link      `json:"-"`
	PublicURL string    `json:"public_url"`
	RiskState RiskState `json:"risk_state"`
	Ready     bool      `json:"ready"`
	Reason    string    `json:"reason"`
}

// ResolveQRSourceAuthority locks the source Link while evaluating its current
// lifecycle/domain state, then resolves the risk decision bound to that exact
// fingerprint. Only a ready result may be rendered/distributed by P08.
func (s *MySQLStore) ResolveQRSourceAuthority(ctx context.Context, workspaceID string, linkID uint64, risk *RedisRiskStore, now time.Time) (QRSourceAuthority, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || linkID == 0 || now.IsZero() {
		return QRSourceAuthority{}, ErrInvalidInput
	}
	if risk == nil {
		return QRSourceAuthority{}, errors.New("destination-risk dependency unavailable")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return QRSourceAuthority{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, linkSelect+` WHERE workspace_id = ? AND id = ? FOR UPDATE`, workspaceID, linkID)
	link, err := scanLink(row)
	if err != nil {
		return QRSourceAuthority{}, err
	}

	authority := QRSourceAuthority{Link: link, RiskState: RiskMissing}
	if link.Status == "deleted" || link.DeletedAt != nil {
		authority.Reason = "deleted"
		return authority, nil
	}
	if link.Status != "active" {
		authority.Reason = "paused"
		return authority, nil
	}
	now = now.UTC()
	if link.ExpiresAt != nil && !link.ExpiresAt.After(now) {
		authority.Reason = "expired"
		return authority, nil
	}
	if link.ClickLimit != nil && link.ClickCount >= *link.ClickLimit {
		authority.Reason = "exhausted"
		return authority, nil
	}
	if link.OneTime && link.ClickCount >= 1 {
		authority.Reason = "exhausted"
		return authority, nil
	}

	canonicalHost := link.Hostname
	if link.DomainKind == "custom" {
		canonicalHost, err = s.authorizeCustomDomainRoutingTx(ctx, tx, link.WorkspaceID, link.Hostname, link.DomainKind, now)
		if errors.Is(err, ErrCustomDomainUnavailable) {
			authority.Reason = "custom-domain-unavailable"
			return authority, nil
		}
		if err != nil {
			return QRSourceAuthority{}, err
		}
	}

	_, state, err := risk.Resolve(ctx, link.ID, link.RiskFingerprint, now)
	if err != nil {
		return QRSourceAuthority{}, err
	}
	authority.RiskState = state
	if state != RiskAllow {
		authority.Reason = "risk-" + string(state)
		return authority, nil
	}

	public := &url.URL{Scheme: "https", Host: canonicalHost, Path: "/" + link.Code}
	authority.PublicURL = public.String()
	authority.Ready = true
	authority.Reason = "ready"
	if err := tx.Commit(); err != nil {
		return QRSourceAuthority{}, err
	}
	return authority, nil
}
