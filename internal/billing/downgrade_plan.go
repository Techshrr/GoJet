package billing

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

func loadDowngradePlansForUpdate(ctx context.Context, tx *sql.Tx, currentID, targetID uint64) (downgradePlanSnapshot, downgradePlanSnapshot, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT id,status,currency,amount_minor,billing_period
FROM billing_plans
WHERE id IN (?,?)
ORDER BY id
FOR UPDATE`, currentID, targetID)
	if err != nil {
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	plans := map[uint64]downgradePlanSnapshot{}
	for rows.Next() {
		var plan downgradePlanSnapshot
		if err := rows.Scan(&plan.ID, &plan.Status, &plan.Money.Currency, &plan.Money.AmountMinor, &plan.Period); err != nil {
			rows.Close()
			return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
		}
		plan.Entitlements = map[string]uint64{}
		plans[plan.ID] = plan
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	_, currentOK := plans[currentID]
	_, targetOK := plans[targetID]
	if !currentOK || !targetOK {
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, ErrNotFound
	}

	entRows, err := tx.QueryContext(ctx, `
SELECT plan_id,capability,limit_value
FROM billing_plan_entitlements
WHERE plan_id IN (?,?)
ORDER BY plan_id,capability
FOR SHARE`, currentID, targetID)
	if err != nil {
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	for entRows.Next() {
		var planID uint64
		var capability string
		var limit uint64
		if err := entRows.Scan(&planID, &capability, &limit); err != nil {
			entRows.Close()
			return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
		}
		plan := plans[planID]
		plan.Entitlements[capability] = limit
		plans[planID] = plan
	}
	if err := entRows.Err(); err != nil {
		entRows.Close()
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	if err := entRows.Close(); err != nil {
		return downgradePlanSnapshot{}, downgradePlanSnapshot{}, err
	}
	return plans[currentID], plans[targetID], nil
}

func strictPlanDowngrade(current, target downgradePlanSnapshot) bool {
	if current.ID == 0 || target.ID == 0 || current.ID == target.ID ||
		current.Money.Currency != target.Money.Currency || current.Period != target.Period ||
		target.Money.AmountMinor > current.Money.AmountMinor {
		return false
	}
	strict := target.Money.AmountMinor < current.Money.AmountMinor
	for capability, targetLimit := range target.Entitlements {
		currentLimit, ok := current.Entitlements[capability]
		if !ok || targetLimit > currentLimit {
			return false
		}
		if targetLimit < currentLimit {
			strict = true
		}
	}
	for capability := range current.Entitlements {
		if _, ok := target.Entitlements[capability]; !ok {
			strict = true
		}
	}
	return strict
}

func projectP06ScheduledDowngradeTx(
	ctx context.Context,
	tx *sql.Tx,
	subscription Subscription,
	currentPlan, targetPlan downgradePlanSnapshot,
	graceStart, graceEnd time.Time,
	targetTermEnd *time.Time,
) error {
	currentLimit := currentPlan.Entitlements[domains.CustomDomainsCapability]
	targetLimit := targetPlan.Entitlements[domains.CustomDomainsCapability]
	if currentLimit > uint64(^uint32(0)) || targetLimit > uint64(^uint32(0)) {
		return ErrConflict
	}
	correlationID := fmt.Sprintf("billing-downgrade-%s-v%d", subscription.ID, subscription.Version+1)

	// P06 owns normal custom-domain downgrade semantics. A normal package
	// downgrade immediately enters its non-extendable seven-day grace whenever
	// a plan-owned custom-domain entitlement currently exists, even when the
	// target plan happens to retain the same numeric domain limit. A valid
	// manual approval remains an independent P06 source and can continue service
	// through the existing resolver without being rewritten here.
	if currentLimit == 0 {
		_, err := domains.ExpirePlanSourceTx(ctx, tx, subscription.WorkspaceID, p13DomainTargetPlanSourceKey, "billing_downgrade_target_not_required", correlationID)
		return err
	}
	if _, err := domains.ApplyNormalPlanDowngradeTx(ctx, tx, domains.NormalPlanDowngradeInput{
		WorkspaceID:    subscription.WorkspaceID,
		SourceKey:      p13DomainPlanSourceKey,
		DegradedAt:     graceStart,
		DecisionReason: "billing_plan_downgrade",
		CorrelationID:  correlationID,
	}); err != nil {
		return err
	}
	if targetLimit == 0 {
		_, err := domains.ExpirePlanSourceTx(ctx, tx, subscription.WorkspaceID, p13DomainTargetPlanSourceKey, "billing_downgrade_removes_custom_domains", correlationID)
		return err
	}
	_, err := domains.UpsertPlanSourceTx(ctx, tx, domains.PlanSourceInput{
		WorkspaceID:    subscription.WorkspaceID,
		SourceKey:      p13DomainTargetPlanSourceKey,
		Status:         domains.EntitlementActive,
		DomainLimit:    uint32(targetLimit),
		StartsAt:       graceEnd,
		ExpiresAt:      targetTermEnd,
		DecisionReason: "billing_downgrade_target_entitlement",
	}, correlationID)
	return err
}

func downgradeSubscriptionID(currentSubscriptionID string, effectiveAt time.Time) string {
	sum := sha256String(currentSubscriptionID + "|downgrade|" + effectiveAt.UTC().Format(time.RFC3339Nano))
	return "sub_" + sum[:24]
}
