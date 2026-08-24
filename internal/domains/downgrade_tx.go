package domains

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ApplyNormalPlanDowngradeTx applies the inherited P06 seven-day normal
// downgrade boundary inside a caller-owned transaction. Replays at the same
// boundary are idempotent; the boundary cannot be moved later.
func ApplyNormalPlanDowngradeTx(ctx context.Context, tx *sql.Tx, input NormalPlanDowngradeInput) (EntitlementSource, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if tx == nil || input.WorkspaceID == "" || input.SourceKey == "" || input.DegradedAt.IsZero() ||
		input.DecisionReason == "" || input.CorrelationID == "" {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	degradedAt := input.DegradedAt.UTC()
	graceUntil := degradedAt.Add(NormalDowngradeGrace)

	source, err := loadEntitlementSourceByKeyForUpdate(ctx, tx, input.WorkspaceID, SourcePlan, input.SourceKey)
	if err != nil {
		return EntitlementSource{}, err
	}
	if source.Status != EntitlementActive || degradedAt.Before(source.StartsAt.UTC()) {
		return EntitlementSource{}, ErrEntitlementConflict
	}
	if source.DegradedAt != nil {
		if source.DegradedAt.UTC().Equal(degradedAt) && source.GraceUntil != nil && source.GraceUntil.UTC().Equal(graceUntil) {
			return source, nil
		}
		return EntitlementSource{}, ErrEntitlementConflict
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domain_entitlement_sources
SET degraded_at=?,grace_until=?,decision_reason=?,security_category=NULL
WHERE id=? AND workspace_id=? AND source='plan' AND status='active'`,
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
		"source":           SourcePlan,
		"degraded_at":      degradedAt,
		"grace_until":      graceUntil,
		"grace_seconds":    int64(NormalDowngradeGrace / time.Second),
		"grace_extendable": false,
	}); err != nil {
		return EntitlementSource{}, err
	}
	return updated, nil
}
