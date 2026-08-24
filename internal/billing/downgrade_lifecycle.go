package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

// loadCurrentSubscriptionForUpdate opportunistically promotes a due scheduled
// downgrade before returning current billing state. Entitlement authority does
// not depend on this promotion: old/new grants already meet at EffectiveAt.
func loadCurrentSubscriptionForUpdate(ctx context.Context, tx *sql.Tx, workspaceID string, now time.Time) (Subscription, error) {
	var subscription Subscription
	var term, grace, cancel sql.NullTime
	err := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,plan_id,status,starts_at,current_term_ends_at,grace_ends_at,cancel_at,version,created_at,updated_at
FROM workspace_subscriptions
WHERE workspace_id=? AND status IN ('active','grace','overdue')
ORDER BY starts_at DESC,id DESC
LIMIT 1
FOR UPDATE`, workspaceID).Scan(
		&subscription.ID, &subscription.WorkspaceID, &subscription.PlanID, &subscription.Status,
		&subscription.StartsAt, &term, &grace, &cancel, &subscription.Version,
		&subscription.CreatedAt, &subscription.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return promoteDuePendingSubscriptionTx(ctx, tx, workspaceID, now)
	}
	if err != nil {
		return Subscription{}, err
	}
	normalizeSubscriptionTimes(&subscription, term, grace, cancel)
	if subscription.Status != SubscriptionGrace || subscription.GraceEndsAt == nil || now.Before(subscription.GraceEndsAt.UTC()) {
		return subscription, nil
	}

	targetID := downgradeSubscriptionID(subscription.ID, *subscription.GraceEndsAt)
	target, err := loadSubscriptionForUpdate(ctx, tx, targetID)
	if err != nil {
		return Subscription{}, err
	}
	if target.WorkspaceID != workspaceID || target.Status != SubscriptionPending || now.Before(target.StartsAt.UTC()) {
		return Subscription{}, ErrConflict
	}
	correlationID := fmt.Sprintf("billing-downgrade-promote-%s", target.ID)
	if err := promoteP06DowngradeTargetTx(ctx, tx, target, correlationID); err != nil {
		return Subscription{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE workspace_subscriptions
SET status='expired',version=version+1,updated_at=?
WHERE id=? AND workspace_id=? AND status='grace'`, now, subscription.ID, workspaceID); err != nil {
		return Subscription{}, err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE workspace_subscriptions
SET status='active',version=version+1,updated_at=?
WHERE id=? AND workspace_id=? AND status='pending'`, now, target.ID, workspaceID)
	if err != nil {
		return Subscription{}, err
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Subscription{}, err
		}
		return Subscription{}, ErrConflict
	}
	if err := appendAuditTx(ctx, tx, workspaceID, "system:billing-lifecycle", "billing.downgrade.promote", "subscription", target.ID, "scheduled_downgrade_effective", correlationID, "success", map[string]any{
		"previous_subscription_id": subscription.ID,
		"plan_id":                  target.PlanID,
		"effective_at":             target.StartsAt.Format(time.RFC3339Nano),
	}); err != nil {
		return Subscription{}, err
	}
	return loadSubscriptionForUpdate(ctx, tx, target.ID)
}

func promoteDuePendingSubscriptionTx(ctx context.Context, tx *sql.Tx, workspaceID string, now time.Time) (Subscription, error) {
	var id string
	err := tx.QueryRowContext(ctx, `
SELECT id FROM workspace_subscriptions
WHERE workspace_id=? AND status='pending' AND starts_at<=?
ORDER BY starts_at DESC,id DESC
LIMIT 1
FOR UPDATE`, workspaceID, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	target, err := loadSubscriptionForUpdate(ctx, tx, id)
	if err != nil {
		return Subscription{}, err
	}
	correlationID := fmt.Sprintf("billing-downgrade-promote-%s", target.ID)
	if err := promoteP06DowngradeTargetTx(ctx, tx, target, correlationID); err != nil {
		return Subscription{}, err
	}
	res, err := tx.ExecContext(ctx, `
UPDATE workspace_subscriptions
SET status='active',version=version+1,updated_at=?
WHERE id=? AND workspace_id=? AND status='pending'`, now, id, workspaceID)
	if err != nil {
		return Subscription{}, err
	}
	if affected, err := res.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Subscription{}, err
		}
		return Subscription{}, ErrConflict
	}
	if err := appendAuditTx(ctx, tx, workspaceID, "system:billing-lifecycle", "billing.downgrade.promote", "subscription", target.ID, "scheduled_downgrade_effective", correlationID, "success", map[string]any{
		"plan_id":      target.PlanID,
		"effective_at": target.StartsAt.Format(time.RFC3339Nano),
	}); err != nil {
		return Subscription{}, err
	}
	return loadSubscriptionForUpdate(ctx, tx, id)
}

func promoteP06DowngradeTargetTx(ctx context.Context, tx *sql.Tx, target Subscription, correlationID string) error {
	var limit uint64
	err := tx.QueryRowContext(ctx, `
SELECT limit_value FROM billing_plan_entitlements
WHERE plan_id=? AND capability=?`, target.PlanID, domains.CustomDomainsCapability).Scan(&limit)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := domains.ExpirePlanSourceTx(ctx, tx, target.WorkspaceID, p13DomainPlanSourceKey, "billing_downgrade_effective_without_custom_domains", correlationID); err != nil {
			return err
		}
		_, err = domains.ExpirePlanSourceTx(ctx, tx, target.WorkspaceID, p13DomainTargetPlanSourceKey, "billing_downgrade_target_promoted", correlationID)
		return err
	}
	if err != nil {
		return err
	}
	if limit == 0 || limit > uint64(^uint32(0)) {
		return ErrConflict
	}
	if _, err := domains.UpsertPlanSourceTx(ctx, tx, domains.PlanSourceInput{
		WorkspaceID:    target.WorkspaceID,
		SourceKey:      p13DomainPlanSourceKey,
		Status:         domains.EntitlementActive,
		DomainLimit:    uint32(limit),
		StartsAt:       target.StartsAt,
		ExpiresAt:      target.CurrentTermEndsAt,
		DecisionReason: "billing_downgrade_effective",
	}, correlationID); err != nil {
		return err
	}
	_, err = domains.ExpirePlanSourceTx(ctx, tx, target.WorkspaceID, p13DomainTargetPlanSourceKey, "billing_downgrade_target_promoted", correlationID)
	return err
}

func loadSubscriptionForUpdate(ctx context.Context, tx *sql.Tx, id string) (Subscription, error) {
	var subscription Subscription
	var term, grace, cancel sql.NullTime
	err := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,plan_id,status,starts_at,current_term_ends_at,grace_ends_at,cancel_at,version,created_at,updated_at
FROM workspace_subscriptions
WHERE id=?
FOR UPDATE`, id).Scan(
		&subscription.ID, &subscription.WorkspaceID, &subscription.PlanID, &subscription.Status,
		&subscription.StartsAt, &term, &grace, &cancel, &subscription.Version,
		&subscription.CreatedAt, &subscription.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	if err != nil {
		return Subscription{}, err
	}
	normalizeSubscriptionTimes(&subscription, term, grace, cancel)
	return subscription, nil
}

func normalizeSubscriptionTimes(subscription *Subscription, term, grace, cancel sql.NullTime) {
	subscription.StartsAt = subscription.StartsAt.UTC()
	subscription.CreatedAt = subscription.CreatedAt.UTC()
	subscription.UpdatedAt = subscription.UpdatedAt.UTC()
	if term.Valid {
		value := term.Time.UTC()
		subscription.CurrentTermEndsAt = &value
	}
	if grace.Valid {
		value := grace.Time.UTC()
		subscription.GraceEndsAt = &value
	}
	if cancel.Valid {
		value := cancel.Time.UTC()
		subscription.CancelAt = &value
	}
}

func lockNoPendingDowngrade(ctx context.Context, tx *sql.Tx, workspaceID string) error {
	rows, err := tx.QueryContext(ctx, `
SELECT id FROM workspace_subscriptions
WHERE workspace_id=? AND status='pending'
FOR UPDATE`, workspaceID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return ErrConflict
	}
	return rows.Err()
}
