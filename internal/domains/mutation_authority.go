package domains

import (
	"context"
	"strings"
	"time"
)

type DomainMutationKind string

const (
	DomainMutationActivate   DomainMutationKind = "activate"
	DomainMutationRestore    DomainMutationKind = "restore"
	DomainMutationRotate     DomainMutationKind = "rotate"
	DomainMutationAssignLink DomainMutationKind = "assign_link"
)

type DomainMutationAuthority struct {
	Domain      Domain              `json:"domain"`
	Entitlement ResolvedEntitlement `json:"entitlement"`
	Kind        DomainMutationKind  `json:"kind"`
	Allowed     bool                `json:"allowed"`
	Code        string              `json:"code"`
}

// CheckDomainMutationAuthority centralizes the server-side checkpoint semantics
// reused by activation/restoration/rotation/link-assignment handlers. It never
// trusts client feature flags or cached UI state. Persisted safety suspension
// and ownership loss are checked before entitlement or axis readiness so no
// mutation can act as a self-reactivation path.
func (s *MySQLStore) CheckDomainMutationAuthority(ctx context.Context, workspaceID string, domainID uint64, kind DomainMutationKind, now time.Time) (DomainMutationAuthority, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || domainID == 0 || now.IsZero() {
		return DomainMutationAuthority{}, ErrInvalidDomainMutation
	}
	switch kind {
	case DomainMutationActivate, DomainMutationRestore, DomainMutationRotate, DomainMutationAssignLink:
	default:
		return DomainMutationAuthority{}, ErrInvalidDomainMutation
	}
	domain, err := s.GetDomain(ctx, workspaceID, domainID)
	if err != nil {
		return DomainMutationAuthority{}, err
	}
	entitlement, err := s.ResolveEntitlement(ctx, workspaceID, now.UTC())
	if err != nil {
		return DomainMutationAuthority{}, err
	}
	decision := DomainMutationAuthority{Domain: domain, Entitlement: entitlement, Kind: kind, Allowed: false}

	if strings.TrimSpace(domain.SecurityCategory) != "" || domain.OwnershipStatus == OwnershipLost || domain.RoutingState == RoutingRevoked || domain.RoutingState == RoutingRemoved {
		decision.Code = "security_suspended"
		return decision, ErrDomainSecuritySuspended
	}
	if !entitlement.MutationAllowed {
		decision.Code = "entitlement_required"
		return decision, ErrEntitlementRequired
	}
	if kind == DomainMutationRotate {
		decision.Allowed = true
		decision.Code = "allowed"
		return decision, nil
	}
	if domain.OwnershipStatus != OwnershipVerified {
		decision.Code = "ownership_required"
		return decision, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		decision.Code = "ingress_dns_required"
		return decision, ErrIngressDNSRequired
	}
	if domain.HTTPSStatus != HTTPSActive {
		decision.Code = "https_required"
		return decision, ErrHTTPSRequired
	}
	if domain.RiskStatus != RiskAllow {
		decision.Code = "domain_risk_required"
		return decision, ErrDomainRiskEvaluation
	}
	decision.Allowed = true
	decision.Code = "allowed"
	return decision, nil
}
