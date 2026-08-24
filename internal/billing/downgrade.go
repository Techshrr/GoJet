package billing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
)

const p13DomainTargetPlanSourceKey = "p13:billing:target"

type ScheduleDowngradeInput struct {
	WorkspaceID     string
	TargetPlanID    uint64
	ExpectedVersion uint64
	ActorID         string
	Now             time.Time
}

type DowngradeSchedule struct {
	Current       Subscription `json:"current"`
	Target        Subscription `json:"target"`
	GraceStartsAt time.Time    `json:"grace_starts_at"`
	EffectiveAt   time.Time    `json:"effective_at"`
}

type downgradePlanSnapshot struct {
	ID           uint64
	Status       PlanStatus
	Money        Money
	Period       BillingPeriod
	Entitlements map[string]uint64
}

// ScheduleDowngrade persists a no-charge plan downgrade without pretending the
// future target is already current. The current subscription is moved into the
// inherited seven-day P06 grace immediately, while a separate pending
// subscription and future billing grants become authoritative at the exact
// grace boundary.
func (s *Store) ScheduleDowngrade(ctx context.Context, input ScheduleDowngradeInput) (DowngradeSchedule, bool, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.TargetPlanID == 0 ||
		input.ExpectedVersion == 0 || input.ActorID == "" || input.Now.IsZero() {
		return DowngradeSchedule{}, false, ErrInvalidInput
	}
	now := input.Now.UTC()

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	defer tx.Rollback()

	current, err := loadCurrentSubscriptionForUpdate(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	if current.Status == SubscriptionGrace {
		if current.GraceEndsAt == nil || (input.ExpectedVersion != current.Version && input.ExpectedVersion+1 != current.Version) {
			return DowngradeSchedule{}, false, ErrConflict
		}
		targetID := downgradeSubscriptionID(current.ID, *current.GraceEndsAt)
		target, err := loadSubscriptionForUpdate(ctx, tx, targetID)
		if err != nil || target.WorkspaceID != input.WorkspaceID || target.PlanID != input.TargetPlanID || target.Status != SubscriptionPending {
			return DowngradeSchedule{}, false, ErrConflict
		}
		graceStart := current.GraceEndsAt.Add(-domains.NormalDowngradeGrace)
		if err := tx.Commit(); err != nil {
			return DowngradeSchedule{}, false, err
		}
		return DowngradeSchedule{Current: current, Target: target, GraceStartsAt: graceStart, EffectiveAt: *current.GraceEndsAt}, false, nil
	}
	if current.Status != SubscriptionActive || current.Version != input.ExpectedVersion || !now.After(current.StartsAt.UTC()) {
		return DowngradeSchedule{}, false, ErrConflict
	}
	graceStart := now
	graceEnd := graceStart.Add(domains.NormalDowngradeGrace)

	currentPlan, targetPlan, err := loadDowngradePlansForUpdate(ctx, tx, current.PlanID, input.TargetPlanID)
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	if targetPlan.Status != PlanActive || !strictPlanDowngrade(currentPlan, targetPlan) {
		return DowngradeSchedule{}, false, ErrConflict
	}
	if err := lockNoPendingDowngrade(ctx, tx, input.WorkspaceID); err != nil {
		return DowngradeSchedule{}, false, err
	}

	targetTermEnd := termEndFor(targetPlan.Period, graceEnd)
	if targetTermEnd == nil {
		return DowngradeSchedule{}, false, ErrConflict
	}
	targetID := downgradeSubscriptionID(current.ID, graceEnd)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO workspace_subscriptions
(id,workspace_id,plan_id,status,starts_at,current_term_ends_at,version,created_at,updated_at)
VALUES (?,?,?,'pending',?,?,1,?,?)`,
		targetID, input.WorkspaceID, targetPlan.ID, graceEnd, targetTermEnd, now, now); err != nil {
		return DowngradeSchedule{}, false, wrapConflict(err)
	}

	// General billing authority follows the same exact grace handoff: the old
	// source remains effective during grace and the target starts at the same
	// microsecond the old source stops contributing.
	if _, err := tx.ExecContext(ctx, `
UPDATE entitlement_grants
SET ends_at=?,updated_at=?
WHERE workspace_id=? AND source_type='billing' AND source_id=? AND revoked_at IS NULL`,
		graceEnd, now, input.WorkspaceID, current.ID); err != nil {
		return DowngradeSchedule{}, false, err
	}

	capabilities := make([]string, 0, len(targetPlan.Entitlements))
	for capability := range targetPlan.Entitlements {
		capabilities = append(capabilities, capability)
	}
	sort.Strings(capabilities)
	for _, capability := range capabilities {
		limit := targetPlan.Entitlements[capability]
		provenance, _ := json.Marshal(map[string]any{
			"plan_id":             targetPlan.ID,
			"subscription_id":     targetID,
			"source":              "billing_downgrade",
			"effective_at":        graceEnd.Format(time.RFC3339Nano),
			"parent_subscription": current.ID,
			"grace_seconds":       int64(domains.NormalDowngradeGrace / time.Second),
		})
		if _, err := tx.ExecContext(ctx, `
INSERT INTO entitlement_grants
(workspace_id,capability,source_type,source_id,limit_value,starts_at,ends_at,revoked_at,provenance_json,created_at,updated_at)
VALUES (?,?,'billing',?,?,?,?,NULL,CAST(? AS JSON),?,?)`,
			input.WorkspaceID, capability, targetID, limit, graceEnd, targetTermEnd,
			string(provenance), now, now); err != nil {
			return DowngradeSchedule{}, false, wrapConflict(err)
		}
	}

	if err := projectP06ScheduledDowngradeTx(ctx, tx, current, currentPlan, targetPlan, graceStart, graceEnd, targetTermEnd); err != nil {
		return DowngradeSchedule{}, false, err
	}

	// current_term_ends_at is deliberately moved to the downgrade instant so
	// the existing schema's term/grace invariant expresses an immediate normal
	// downgrade: term ends now, seven-day grace follows, target starts at grace.
	res, err := tx.ExecContext(ctx, `
UPDATE workspace_subscriptions
SET status='grace',current_term_ends_at=?,grace_ends_at=?,cancel_at=NULL,version=version+1,updated_at=?
WHERE id=? AND workspace_id=? AND status='active' AND version=?`,
		graceStart, graceEnd, now, current.ID, input.WorkspaceID, input.ExpectedVersion)
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	if affected != 1 {
		return DowngradeSchedule{}, false, ErrConflict
	}

	updated, err := loadSubscription(ctx, tx, current.ID)
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	target, err := loadSubscription(ctx, tx, targetID)
	if err != nil {
		return DowngradeSchedule{}, false, err
	}
	correlationID := fmt.Sprintf("billing-downgrade-%s-v%d", current.ID, updated.Version)
	if err := appendAuditTx(ctx, tx, input.WorkspaceID, input.ActorID, "billing.downgrade.schedule", "subscription", current.ID, "workspace_owner_downgrade", correlationID, "success", map[string]any{
		"target_plan_id":         targetPlan.ID,
		"target_subscription_id": targetID,
		"grace_starts_at":        graceStart.Format(time.RFC3339Nano),
		"effective_at":           graceEnd.Format(time.RFC3339Nano),
		"grace_seconds":          int64(domains.NormalDowngradeGrace / time.Second),
	}); err != nil {
		return DowngradeSchedule{}, false, err
	}
	if _, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, workspace.NotificationInput{
		WorkspaceID:  input.WorkspaceID,
		Category:     "billing",
		EventKey:     "downgrade_scheduled",
		DedupeKey:    fmt.Sprintf("billing:downgrade_scheduled:%s:v%d", current.ID, updated.Version),
		Title:        "Plan downgrade scheduled",
		Summary:      "Your current plan remains effective only through the scheduled seven-day downgrade grace period.",
		DeepLink:     "/app/billing",
		ResourceType: "billing_subscription",
		ResourceID:   current.ID,
	}); err != nil {
		return DowngradeSchedule{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return DowngradeSchedule{}, false, err
	}
	return DowngradeSchedule{Current: updated, Target: target, GraceStartsAt: graceStart, EffectiveAt: graceEnd}, true, nil
}
