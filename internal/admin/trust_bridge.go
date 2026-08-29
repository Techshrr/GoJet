package admin

import (
	"context"
	"strings"

	"github.com/Techshrr/GoJet/internal/trust"
)

// P16TrustAuthorizer adapts the P17 durable administrator principal to the
// exact permission contract already consumed by P16. It does not introduce a
// superuser shortcut and it binds the actor ID to the authenticated principal.
type P16TrustAuthorizer struct {
	principal Principal
}

func NewP16TrustAuthorizer(principal Principal) P16TrustAuthorizer {
	return P16TrustAuthorizer{principal: principal}
}

func (a P16TrustAuthorizer) Authorize(_ context.Context, actorID, permission string) error {
	actorID = strings.TrimSpace(actorID)
	permission = strings.TrimSpace(permission)
	if actorID == "" || actorID != a.principal.Administrator.ID {
		return trust.ErrUnauthorized
	}
	if permission != trust.SecurityManagePermission && permission != trust.DomainsRiskManagePermission {
		return trust.ErrUnauthorized
	}
	if !a.principal.Has(permission) {
		return trust.ErrUnauthorized
	}
	return nil
}

type P16DestinationRiskSnapshot struct {
	ID                uint64 `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	LinkID            uint64 `json:"link_id"`
	RiskFingerprint   string `json:"risk_fingerprint"`
	PolicyVersion     string `json:"policy_version"`
	ScanStatus        string `json:"scan_status"`
	DecisionState     string `json:"decision_state"`
	ReasonCategory    string `json:"reason_category"`
	HasActiveOverride bool   `json:"has_active_override"`
	ProviderCount     uint32 `json:"provider_count"`
}

type P16DomainRiskSnapshot struct {
	EvaluationID      uint64 `json:"evaluation_id"`
	WorkspaceID       string `json:"workspace_id"`
	DomainID          uint64 `json:"domain_id"`
	PolicyVersion     string `json:"policy_version"`
	State             string `json:"state"`
	ReasonCategory    string `json:"reason_category"`
	EntitlementStatus string `json:"entitlement_status"`
	OwnershipStatus   string `json:"ownership_status"`
	IngressDNSStatus  string `json:"ingress_dns_status"`
	HTTPSStatus       string `json:"https_status"`
	RoutingStatus     string `json:"routing_status"`
	ProviderCount     uint32 `json:"provider_count"`
}

type P16AbuseSnapshot struct {
	ID           uint64 `json:"id"`
	WorkspaceID  string `json:"workspace_id"`
	ResourceType string `json:"resource_type"`
	Status       string `json:"status"`
	Version      uint64 `json:"version"`
}

// P16DestinationRisk returns only P16's normalized administrator projection.
// Provider evidence and reachable target material stay server-side exactly as
// enforced by P16 admin_risk.go.
func (s *Service) P16DestinationRisk(ctx context.Context, p Principal, riskID uint64) (P16DestinationRiskSnapshot, error) {
	if err := s.Require(p, PermissionSecurityManage); err != nil {
		return P16DestinationRiskSnapshot{}, err
	}
	record, err := trust.NewStore(s.db).GetAdminDestinationRisk(ctx, riskID)
	if err != nil {
		return P16DestinationRiskSnapshot{}, err
	}
	return P16DestinationRiskSnapshot{
		ID:                record.ID,
		WorkspaceID:       record.WorkspaceID,
		LinkID:            record.LinkID,
		RiskFingerprint:   record.RiskFingerprint,
		PolicyVersion:     record.PolicyVersion,
		ScanStatus:        string(record.ScanStatus),
		DecisionState:     string(record.DecisionState),
		ReasonCategory:    record.ReasonCategory,
		HasActiveOverride: record.HasActiveOverride,
		ProviderCount:     record.ProviderCount,
	}, nil
}

func (s *Service) P16DomainRisk(ctx context.Context, p Principal, domainID uint64) (P16DomainRiskSnapshot, error) {
	if err := s.Require(p, PermissionDomainsRiskManage); err != nil {
		return P16DomainRiskSnapshot{}, err
	}
	record, err := trust.NewStore(s.db).GetAdminDomainRisk(ctx, domainID)
	if err != nil {
		return P16DomainRiskSnapshot{}, err
	}
	return P16DomainRiskSnapshot{
		EvaluationID:      record.EvaluationID,
		WorkspaceID:       record.WorkspaceID,
		DomainID:          record.DomainID,
		PolicyVersion:     record.PolicyVersion,
		State:             string(record.State),
		ReasonCategory:    record.ReasonCategory,
		EntitlementStatus: record.EntitlementStatus,
		OwnershipStatus:   record.OwnershipStatus,
		IngressDNSStatus:  record.IngressDNSStatus,
		HTTPSStatus:       record.HTTPSStatus,
		RoutingStatus:     record.RoutingStatus,
		ProviderCount:     record.ProviderCount,
	}, nil
}

func (s *Service) P16Abuse(ctx context.Context, p Principal, reportID uint64) (P16AbuseSnapshot, error) {
	if err := s.Require(p, PermissionSecurityManage); err != nil {
		return P16AbuseSnapshot{}, err
	}
	record, err := trust.NewStore(s.db).GetAbuseReport(ctx, reportID)
	if err != nil {
		return P16AbuseSnapshot{}, err
	}
	return P16AbuseSnapshot{
		ID:           record.ID,
		WorkspaceID:  record.WorkspaceID,
		ResourceType: string(record.ResourceType),
		Status:       string(record.Status),
		Version:      record.Version,
	}, nil
}
