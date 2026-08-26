package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type DomainSecurityRestoreInput struct {
	WorkspaceID      string
	DomainID         uint64
	ActorID          string
	ExpectedCategory DomainSecurityCategory
	Reason           string
	CorrelationID    string
	Now              time.Time
}

// RestoreDomainSecuritySuspension is the only P16 recovery path for a P06
// security suspension. It never bypasses independent domain axes: current
// entitlement, ownership, ingress DNS, HTTPS and projected risk must all be
// ready in the same database transaction before routing can be re-enabled.
func (s *MySQLStore) RestoreDomainSecuritySuspension(ctx context.Context, input DomainSecurityRestoreInput) (Domain, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Now = input.Now.UTC().Truncate(time.Microsecond)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.Reason == "" || input.CorrelationID == "" || input.Now.IsZero() || !validExternalSecurityCategory(input.ExpectedCategory) {
		return Domain{}, ErrInvalidDomainMutation
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer func() { _ = tx.Rollback() }()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if domain.RoutingState != RoutingSuspended || strings.TrimSpace(domain.SecurityCategory) != string(input.ExpectedCategory) {
		return Domain{}, ErrDomainSecuritySuspended
	}
	entitlement, err := s.resolveEntitlementTx(ctx, tx, input.WorkspaceID, input.Now)
	if err != nil {
		return Domain{}, err
	}
	if !entitlement.ExistingRoutingAllowed {
		return Domain{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		return Domain{}, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		return Domain{}, ErrIngressDNSRequired
	}
	if domain.HTTPSStatus != HTTPSActive {
		return Domain{}, ErrHTTPSRequired
	}
	if domain.RiskStatus != RiskAllow {
		return Domain{}, ErrDomainRiskEvaluation
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET routing_state='enabled',security_category=NULL,grace_started_at=NULL,grace_until=NULL
WHERE workspace_id=? AND id=? AND routing_state='suspended' AND security_category=?`, input.WorkspaceID, input.DomainID, string(input.ExpectedCategory)); err != nil {
		return Domain{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.security.restore", "success", input.Reason, input.CorrelationID, map[string]any{
		"category":          input.ExpectedCategory,
		"routing_state":     updated.RoutingState,
		"entitlement_ready": entitlement.ExistingRoutingAllowed,
		"ownership_ready":   updated.OwnershipStatus == OwnershipVerified,
		"ingress_dns_ready": updated.IngressDNSStatus == IngressValid,
		"https_ready":       updated.HTTPSStatus == HTTPSActive,
		"risk_ready":        updated.RiskStatus == RiskAllow,
		"grace":             false,
	}); err != nil {
		return Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return updated, nil
}
