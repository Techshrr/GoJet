package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// WorkspacePermissionChecker is owned by the Workspace/Auth authority. P06
// consumes it as a server-side dependency instead of inventing or trusting a
// client-supplied role. Implementations must answer the actor's current manage
// permission for the requested Workspace.
type WorkspacePermissionChecker interface {
	CanManageCustomDomains(ctx context.Context, workspaceID, actorID string) (bool, error)
}

type DomainAuthorityService struct {
	store       *MySQLStore
	permissions WorkspacePermissionChecker
	ownership   *OwnershipVerifier
}

func NewDomainAuthorityService(store *MySQLStore, permissions WorkspacePermissionChecker, ownership *OwnershipVerifier) (*DomainAuthorityService, error) {
	if store == nil || store.db == nil || permissions == nil || ownership == nil || ownership.store != store {
		return nil, ErrInvalidDomainMutation
	}
	return &DomainAuthorityService{store: store, permissions: permissions, ownership: ownership}, nil
}

func (s *DomainAuthorityService) requireManage(ctx context.Context, workspaceID, actorID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	if s == nil || s.store == nil || s.permissions == nil || workspaceID == "" || actorID == "" {
		return ErrInvalidDomainMutation
	}
	allowed, err := s.permissions.CanManageCustomDomains(ctx, workspaceID, actorID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrWorkspacePermissionRequired
	}
	return nil
}

// VerifyOwnershipTXT checks Workspace manage permission before any resolver
// traffic. The underlying verifier then independently re-checks entitlement and
// the current ownership verifier under the existing transactional authority.
func (s *DomainAuthorityService) VerifyOwnershipTXT(ctx context.Context, input VerifyOwnershipTXTInput) (OwnershipVerificationResult, error) {
	if err := s.requireManage(ctx, input.WorkspaceID, input.ActorID); err != nil {
		return OwnershipVerificationResult{}, err
	}
	return s.ownership.VerifyTXT(ctx, input)
}

// RotateOwnershipSecret checks current Workspace permission at the service
// boundary; the store operation independently re-checks entitlement and safety
// state before generating any new secret material.
func (s *DomainAuthorityService) RotateOwnershipSecret(ctx context.Context, input RotateOwnershipSecretInput) (RotatedOwnershipSecret, error) {
	if err := s.requireManage(ctx, input.WorkspaceID, input.ActorID); err != nil {
		return RotatedOwnershipSecret{}, err
	}
	return s.store.RotateOwnershipSecret(ctx, input)
}

type DomainRoutingMutationInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

func (s *DomainAuthorityService) ActivateDomain(ctx context.Context, input DomainRoutingMutationInput) (Domain, error) {
	return s.mutateRouting(ctx, input, DomainMutationActivate)
}

func (s *DomainAuthorityService) RestoreDomain(ctx context.Context, input DomainRoutingMutationInput) (Domain, error) {
	return s.mutateRouting(ctx, input, DomainMutationRestore)
}

func (s *DomainAuthorityService) mutateRouting(ctx context.Context, input DomainRoutingMutationInput, kind DomainMutationKind) (Domain, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() || (kind != DomainMutationActivate && kind != DomainMutationRestore) {
		return Domain{}, ErrInvalidDomainMutation
	}
	if err := s.requireManage(ctx, input.WorkspaceID, input.ActorID); err != nil {
		return Domain{}, err
	}
	now := input.Now.UTC()

	tx, err := s.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	entitlement, err := s.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return Domain{}, err
	}
	action := "domain.activate"
	if kind == DomainMutationRestore {
		action = "domain.restore"
	}
	deny := func(code string, denyErr error, reason string) (Domain, error) {
		if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, action, "denied", reason, input.CorrelationID, map[string]any{"code": code}); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, denyErr
	}
	if strings.TrimSpace(domain.SecurityCategory) != "" || domain.OwnershipStatus == OwnershipLost || domain.RoutingState == RoutingRevoked || domain.RoutingState == RoutingRemoved {
		return deny("security_suspended", ErrDomainSecuritySuspended, "domain safety suspension active")
	}
	if !entitlement.MutationAllowed {
		return deny("entitlement_required", ErrEntitlementRequired, entitlement.DecisionReason)
	}
	if domain.OwnershipStatus != OwnershipVerified {
		return deny("ownership_required", ErrOwnershipRequired, "ownership verification required")
	}
	if domain.IngressDNSStatus != IngressValid {
		return deny("ingress_dns_required", ErrIngressDNSRequired, "valid ingress DNS required")
	}
	if domain.HTTPSStatus != HTTPSActive {
		return deny("https_required", ErrHTTPSRequired, "active HTTPS required")
	}
	if domain.RiskStatus != RiskAllow {
		return deny("domain_risk_required", ErrDomainRiskEvaluation, "domain risk allow required")
	}
	if kind == DomainMutationActivate && domain.RoutingState != RoutingPending {
		return Domain{}, ErrInvalidDomainMutation
	}
	if kind == DomainMutationRestore && domain.RoutingState != RoutingSuspended {
		return Domain{}, ErrInvalidDomainMutation
	}

	if _, err := tx.ExecContext(ctx, `UPDATE custom_domains SET routing_state='enabled' WHERE workspace_id=? AND id=?`, input.WorkspaceID, input.DomainID); err != nil {
		return Domain{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, action, "success", input.Reason, input.CorrelationID, map[string]any{
		"code": "allowed",
		"routing_state": updated.RoutingState,
		"entitlement_source": entitlement.Source,
	}); err != nil {
		return Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return updated, nil
}
