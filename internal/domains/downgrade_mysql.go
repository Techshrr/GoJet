package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type NormalPlanDowngradeInput struct {
	WorkspaceID   string
	SourceKey     string
	DegradedAt    time.Time
	DecisionReason string
	CorrelationID string
}

// ApplyNormalPlanDowngrade records the one authoritative normal-downgrade
// instant. Replays at the same instant are idempotent; attempts to move the
// instant later are rejected so a caller cannot extend the seven-day grace.
func (s *MySQLStore) ApplyNormalPlanDowngrade(ctx context.Context, input NormalPlanDowngradeInput) (EntitlementSource, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.SourceKey == "" || input.DegradedAt.IsZero() || input.DecisionReason == "" || input.CorrelationID == "" {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	degradedAt := input.DegradedAt.UTC()
	graceUntil := degradedAt.Add(NormalDowngradeGrace)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return EntitlementSource{}, err
	}
	defer tx.Rollback()

	source, err := loadEntitlementSourceByKeyForUpdate(ctx, tx, input.WorkspaceID, SourcePlan, input.SourceKey)
	if err != nil {
		return EntitlementSource{}, err
	}
	if source.Status != EntitlementActive || degradedAt.Before(source.StartsAt.UTC()) {
		return EntitlementSource{}, ErrEntitlementConflict
	}
	if source.DegradedAt != nil {
		if source.DegradedAt.UTC().Equal(degradedAt) && source.GraceUntil != nil && source.GraceUntil.UTC().Equal(graceUntil) {
			if err := tx.Commit(); err != nil {
				return EntitlementSource{}, err
			}
			return source, nil
		}
		return EntitlementSource{}, ErrEntitlementConflict
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET degraded_at = ?, grace_until = ?, decision_reason = ?, security_category = NULL
		WHERE id = ? AND workspace_id = ? AND source = 'plan' AND status = 'active'`,
		degradedAt, graceUntil, input.DecisionReason, source.ID, input.WorkspaceID); err != nil {
		return EntitlementSource{}, err
	}
	updated, err := loadEntitlementSourceByKey(ctx, tx, input.WorkspaceID, SourcePlan, input.SourceKey)
	if err != nil {
		return EntitlementSource{}, err
	}
	if updated.DegradedAt == nil || updated.GraceUntil == nil || updated.GraceUntil.Sub(*updated.DegradedAt) != NormalDowngradeGrace {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, nil, &updated.ID, "system:plan-entitlement", "domain.entitlement.plan.downgrade", "success", input.DecisionReason, input.CorrelationID, map[string]any{
		"source":         SourcePlan,
		"degraded_at":    degradedAt,
		"grace_until":    graceUntil,
		"grace_seconds":  int64(NormalDowngradeGrace / time.Second),
		"grace_extendable": false,
	}); err != nil {
		return EntitlementSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return EntitlementSource{}, err
	}
	return updated, nil
}

func loadEntitlementSourceByKeyForUpdate(ctx context.Context, tx *sql.Tx, workspaceID string, kind EntitlementSourceKind, key string) (EntitlementSource, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
		       degraded_at, grace_until, granted_by, support_ticket_id, decision_reason, security_category
		FROM custom_domain_entitlement_sources
		WHERE workspace_id = ? AND source = ? AND source_key = ?
		FOR UPDATE`, workspaceID, kind, key)
	return scanEntitlementSource(row)
}
