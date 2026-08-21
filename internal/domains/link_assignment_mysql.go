package domains

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// AuthorizeCustomDomainAssignmentTx is the same-transaction authority consumed
// by the Links store. The custom-domain row is locked until the caller commits
// its Link create/update, eliminating a check-then-write window where a domain
// could be suspended or lose trust between authorization and assignment.
func (s *MySQLStore) AuthorizeCustomDomainAssignmentTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname string, now time.Time) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || tx == nil || workspaceID == "" || now.IsZero() {
		return "", ErrInvalidDomainMutation
	}
	normalized, err := GoJetHostnamePolicy().Normalize(hostname)
	if err != nil {
		return "", err
	}
	domain, err := loadDomainByHostnameForUpdate(ctx, tx, workspaceID, normalized.ASCII)
	if err != nil {
		return "", err
	}
	entitlement, err := s.resolveEntitlementTx(ctx, tx, workspaceID, now.UTC())
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(domain.SecurityCategory) != "" || domain.OwnershipStatus == OwnershipLost || domain.RoutingState == RoutingSuspended || domain.RoutingState == RoutingRevoked || domain.RoutingState == RoutingRemoved {
		return "", ErrDomainSecuritySuspended
	}
	if !entitlement.MutationAllowed {
		return "", ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		return "", ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		return "", ErrIngressDNSRequired
	}
	if domain.HTTPSStatus != HTTPSActive {
		return "", ErrHTTPSRequired
	}
	if domain.RiskStatus != RiskAllow {
		return "", ErrDomainRiskEvaluation
	}
	return normalized.ASCII, nil
}

// AuthorizeCustomDomainRoutingTx is the runtime redirect authority. Unlike new
// Link assignment, it intentionally consumes ExistingRoutingAllowed so a normal
// plan downgrade may keep an already-enabled custom host alive only during the
// exact billing grace window. Pending domains do not become routable merely
// because their trust axes are ready; activate remains a distinct authority.
func (s *MySQLStore) AuthorizeCustomDomainRoutingTx(ctx context.Context, tx *sql.Tx, workspaceID, hostname string, now time.Time) (string, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || tx == nil || workspaceID == "" || now.IsZero() {
		return "", ErrInvalidDomainMutation
	}
	normalized, err := GoJetHostnamePolicy().Normalize(hostname)
	if err != nil {
		return "", err
	}
	domain, err := loadDomainByHostnameForUpdate(ctx, tx, workspaceID, normalized.ASCII)
	if err != nil {
		return "", err
	}
	entitlement, err := s.resolveEntitlementTx(ctx, tx, workspaceID, now.UTC())
	if err != nil {
		return "", err
	}
	if domain.RoutingState != RoutingEnabled || strings.TrimSpace(domain.SecurityCategory) != "" || domain.OwnershipStatus != OwnershipVerified || domain.IngressDNSStatus != IngressValid || domain.HTTPSStatus != HTTPSActive || domain.RiskStatus != RiskAllow || !entitlement.ExistingRoutingAllowed {
		return "", ErrDomainSecuritySuspended
	}
	return normalized.ASCII, nil
}

func loadDomainByHostnameForUpdate(ctx context.Context, tx *sql.Tx, workspaceID, hostnameASCII string) (Domain, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, hostname_ascii, display_hostname, routing_state,
		       ownership_status, ingress_dns_status, https_status, risk_status,
		       ownership_token_version, ownership_secret_issued_at, ownership_verified_at,
		       ingress_dns_checked_at, https_checked_at, risk_checked_at, risk_policy_version,
		       risk_evidence_ref, grace_started_at, grace_until, security_category,
		       created_at, updated_at, removed_at
		FROM custom_domains
		WHERE workspace_id = ? AND hostname_ascii = ? AND removed_at IS NULL
		FOR UPDATE`, workspaceID, hostnameASCII)
	domain, err := scanDomain(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, ErrDomainNotFound
	}
	return domain, err
}
