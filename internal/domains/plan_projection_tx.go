package domains

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// UpsertPlanSourceTx projects plan-owned custom-domain entitlement authority
// through the P06 domain package while participating in the caller's MySQL
// transaction. The caller owns commit/rollback.
func UpsertPlanSourceTx(ctx context.Context, tx *sql.Tx, input PlanSourceInput, correlationID string) (EntitlementSource, error) {
	if tx == nil {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}

	source := EntitlementSource{
		WorkspaceID:      input.WorkspaceID,
		Source:           SourcePlan,
		SourceKey:        input.SourceKey,
		Status:           input.Status,
		DomainLimit:      input.DomainLimit,
		StartsAt:         input.StartsAt.UTC(),
		ExpiresAt:        utcPtr(input.ExpiresAt),
		DegradedAt:       utcPtr(input.DegradedAt),
		GraceUntil:       utcPtr(input.GraceUntil),
		DecisionReason:   strings.TrimSpace(input.DecisionReason),
		SecurityCategory: strings.TrimSpace(input.SecurityCategory),
	}
	if err := ValidateEntitlementSource(source); err != nil {
		return EntitlementSource{}, err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources (
			workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
			degraded_at, grace_until, decision_reason, security_category
		) VALUES (?, 'plan', ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			domain_limit = VALUES(domain_limit),
			starts_at = VALUES(starts_at),
			expires_at = VALUES(expires_at),
			degraded_at = VALUES(degraded_at),
			grace_until = VALUES(grace_until),
			decision_reason = VALUES(decision_reason),
			security_category = VALUES(security_category)`,
		source.WorkspaceID, source.SourceKey, source.Status, source.DomainLimit, source.StartsAt,
		source.ExpiresAt, source.DegradedAt, source.GraceUntil, source.DecisionReason, source.SecurityCategory,
	)
	if err != nil {
		return EntitlementSource{}, err
	}

	created, err := loadEntitlementSourceByKey(ctx, tx, source.WorkspaceID, SourcePlan, source.SourceKey)
	if err != nil {
		return EntitlementSource{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, created.WorkspaceID, nil, &created.ID, "system:plan-entitlement", "domain.entitlement.plan.sync", "success", created.DecisionReason, correlationID, map[string]any{
		"source":       created.Source,
		"status":       created.Status,
		"domain_limit": created.DomainLimit,
	}); err != nil {
		return EntitlementSource{}, err
	}
	return created, nil
}

// ExpirePlanSourceTx removes only the selected plan-source contribution. It
// deliberately leaves manual approval and all P06 ownership/DNS/HTTPS/risk
// authority untouched.
func ExpirePlanSourceTx(ctx context.Context, tx *sql.Tx, workspaceID, sourceKey, reason, correlationID string) (bool, error) {
	if tx == nil {
		return false, ErrInvalidEntitlementSource
	}
	workspaceID = strings.TrimSpace(workspaceID)
	sourceKey = strings.TrimSpace(sourceKey)
	reason = strings.TrimSpace(reason)
	correlationID = strings.TrimSpace(correlationID)
	if workspaceID == "" || sourceKey == "" || reason == "" || correlationID == "" {
		return false, ErrInvalidEntitlementSource
	}

	existing, err := loadEntitlementSourceByKey(ctx, tx, workspaceID, SourcePlan, sourceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if existing.Status == EntitlementExpired {
		return false, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET status='expired',
		    degraded_at=NULL,
		    grace_until=NULL,
		    decision_reason=?,
		    security_category=NULL
		WHERE id=? AND workspace_id=? AND source='plan' AND source_key=?`,
		reason, existing.ID, workspaceID, sourceKey,
	); err != nil {
		return false, err
	}
	expired, err := loadEntitlementSourceByKey(ctx, tx, workspaceID, SourcePlan, sourceKey)
	if err != nil {
		return false, err
	}
	if err := appendDomainAuditTx(ctx, tx, workspaceID, nil, &expired.ID, "system:plan-entitlement", "domain.entitlement.plan.expire", "success", reason, correlationID, map[string]any{
		"source":       expired.Source,
		"status":       expired.Status,
		"domain_limit": expired.DomainLimit,
	}); err != nil {
		return false, err
	}
	return true, nil
}
