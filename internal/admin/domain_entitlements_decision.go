package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

func (s *Service) DecideDomainEntitlement(ctx context.Context, p Principal, workspaceID string, input DomainEntitlementDecisionInput, authority MutationAuthority, now time.Time) (DomainEntitlementDecisionResult, bool, error) {
	if err := s.RequireHighRisk(p, PermissionDomainsEntitlementsManage, authority, now); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	input.Action = strings.ToLower(strings.TrimSpace(input.Action))
	if !validID(workspaceID, 64) {
		return DomainEntitlementDecisionResult{}, false, ErrInvalid
	}
	if err := validateDomainEntitlementDecisionInput(input, now); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		WorkspaceID string
		Input       DomainEntitlementDecisionInput
	}{WorkspaceID: workspaceID, Input: input})
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	actionName := "admin.domain_entitlement." + input.Action

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	defer tx.Rollback()

	if replay, ok, err := loadIdempotency[DomainEntitlementDecisionResult](ctx, tx, p.Administrator.ID, actionName, authority.IdempotencyKey, fingerprint); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	} else if ok {
		return replay, true, nil
	}

	var workspaceStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=? FOR UPDATE`, workspaceID).Scan(&workspaceStatus); errors.Is(err, sql.ErrNoRows) {
		return DomainEntitlementDecisionResult{}, false, ErrNotFound
	} else if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	if workspaceStatus != "active" && workspaceStatus != "suspended" {
		return DomainEntitlementDecisionResult{}, false, ErrConflict
	}

	before, err := resolveDomainEntitlementTx(ctx, tx, workspaceID, now)
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	decisionID, err := newOpaque("aed_", 18)
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}

	var sourceID *uint64
	var supportTicketID string
	var affectedRoutes uint32

	switch input.Action {
	case "approve":
		id, ticket, err := approveDomainEntitlementTx(ctx, tx, workspaceID, p.Administrator.ID, decisionID, input, authority, now)
		if err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
		sourceID = &id
		supportTicketID = ticket
	case "deny":
		ticket, err := denyDomainEntitlementTx(ctx, tx, workspaceID, p.Administrator.ID, input.UserVisibleCategory, authority)
		if err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
		supportTicketID = ticket
	case "suspend":
		if before.Status == domains.EntitlementSuspended || before.Status == domains.EntitlementRevoked || before.Source == domains.SourceNone {
			return DomainEntitlementDecisionResult{}, false, ErrConflict
		}
		affectedRoutes, err = setDomainEntitlementControlTx(ctx, tx, workspaceID, string(domains.EntitlementSuspended), p.Administrator.ID, decisionID, authority.Reason, input.EffectiveAt.UTC())
		if err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
		if err := appendDomainEntitlementAuditTx(ctx, tx, workspaceID, nil, p.Administrator.ID, "domain.entitlement.admin.suspend", authority.Reason, authority.CorrelationID, map[string]any{"scope": domainEntitlementScopeWorkspace, "affected_routes": affectedRoutes}); err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
	case "revoke":
		if before.Status == domains.EntitlementRevoked || before.Source == domains.SourceNone {
			return DomainEntitlementDecisionResult{}, false, ErrConflict
		}
		affectedRoutes, err = setDomainEntitlementControlTx(ctx, tx, workspaceID, string(domains.EntitlementRevoked), p.Administrator.ID, decisionID, authority.Reason, now)
		if err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
		if err := appendDomainEntitlementAuditTx(ctx, tx, workspaceID, nil, p.Administrator.ID, "domain.entitlement.admin.revoke", authority.Reason, authority.CorrelationID, map[string]any{"existing_link_impact": domainEntitlementDisableRouting, "affected_routes": affectedRoutes}); err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
	case "restore":
		if err := restoreDomainEntitlementControlTx(ctx, tx, workspaceID, input.CurrentSecurityOwnershipEvidence, p.Administrator.ID, authority, now); err != nil {
			return DomainEntitlementDecisionResult{}, false, err
		}
	default:
		return DomainEntitlementDecisionResult{}, false, ErrInvalid
	}

	after, err := resolveDomainEntitlementTx(ctx, tx, workspaceID, now)
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO admin_domain_entitlement_decisions (
			id,workspace_id,action,actor_id,request_correlation_id,reason,
			support_ticket_id,source_id,domain_limit,starts_at,expires_at,effective_at,
			scope,confirmation,existing_link_impact,user_visible_category,
			current_security_ownership_evidence,affected_routes,before_json,after_json,created_at
		) VALUES (?,?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),NULLIF(?,''),?,CAST(? AS JSON),CAST(? AS JSON),?)`,
		decisionID, workspaceID, input.Action, p.Administrator.ID, authority.CorrelationID, authority.Reason,
		supportTicketID, sourceID, input.DomainLimit, nullableTime(input.StartsAt), nullableTime(input.ExpiresAt), nullableTime(input.EffectiveAt),
		input.Scope, input.Confirmation, input.ExistingLinkImpact, input.UserVisibleCategory,
		input.CurrentSecurityOwnershipEvidence, affectedRoutes, string(beforeJSON), string(afterJSON), now,
	); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}

	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind:     "administrator",
		ActorID:       p.Administrator.ID,
		Action:        actionName,
		ResourceType:  "domain_entitlement",
		ResourceID:    workspaceID,
		Result:        "success",
		CorrelationID: authority.CorrelationID,
		Reason:        authority.Reason,
		Before:        map[string]any{"status": string(before.Status)},
		After:         map[string]any{"status": string(after.Status)},
		CreatedAt:     now,
	})
	if err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	result := DomainEntitlementDecisionResult{DecisionID: decisionID, WorkspaceID: workspaceID, Action: input.Action, Entitlement: after, AffectedRoutes: affectedRoutes}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, actionName, authority.IdempotencyKey, fingerprint, result, auditID, now); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return DomainEntitlementDecisionResult{}, false, err
	}
	return result, false, nil
}

func validateDomainEntitlementDecisionInput(input DomainEntitlementDecisionInput, now time.Time) error {
	switch input.Action {
	case "approve":
		if input.DomainLimit == nil || *input.DomainLimit == 0 || input.StartsAt == nil || input.ExpiresAt == nil ||
			input.StartsAt.IsZero() || input.ExpiresAt.IsZero() || !input.ExpiresAt.After(*input.StartsAt) ||
			!validID(strings.TrimSpace(input.SupportTicketID), 128) {
			return ErrInvalid
		}
	case "deny":
		category := strings.TrimSpace(input.UserVisibleCategory)
		if !validAuditToken(category, 64) {
			return ErrInvalid
		}
	case "suspend":
		if input.EffectiveAt == nil || input.EffectiveAt.IsZero() || input.EffectiveAt.UTC().After(now.UTC()) || strings.TrimSpace(input.Scope) != domainEntitlementScopeWorkspace {
			return ErrInvalid
		}
	case "revoke":
		if strings.TrimSpace(input.Confirmation) != domainEntitlementRevokeConfirm || strings.TrimSpace(input.ExistingLinkImpact) != domainEntitlementDisableRouting {
			return ErrInvalid
		}
	case "restore":
		evidence := strings.TrimSpace(input.CurrentSecurityOwnershipEvidence)
		if !validEvidenceReference(evidence) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validAuditToken(value string, max int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > max {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func validEvidenceReference(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 255 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == ':' || r == '/' || r == '@' || r == '#' {
			continue
		}
		return false
	}
	return true
}
