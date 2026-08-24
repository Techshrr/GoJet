package billing

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var planCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var capabilityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,95}$`)
var entitlementUnitPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,31}$`)

type CreatePlanInput struct {
	Code          string
	Name          string
	Status        PlanStatus
	Money         Money
	BillingPeriod BillingPeriod
	Entitlements  []PlanEntitlement
	ActorID       string
	CorrelationID string
}

type UpdatePlanInput struct {
	PlanID          uint64
	Name            string
	Status          PlanStatus
	Money           Money
	BillingPeriod   BillingPeriod
	Entitlements    []PlanEntitlement
	ExpectedVersion uint64
	ActorID         string
	CorrelationID   string
}

func (s *Store) ListAdminPlans(ctx context.Context) ([]Plan, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,code,name,status,currency,amount_minor,billing_period,version,created_at,updated_at
FROM billing_plans ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []Plan{}
	for rows.Next() {
		var plan Plan
		if err := rows.Scan(&plan.ID, &plan.Code, &plan.Name, &plan.Status, &plan.Money.Currency, &plan.Money.AmountMinor, &plan.BillingPeriod, &plan.Version, &plan.CreatedAt, &plan.UpdatedAt); err != nil {
			return nil, err
		}
		plan.CreatedAt = plan.CreatedAt.UTC()
		plan.UpdatedAt = plan.UpdatedAt.UTC()
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range plans {
		items, err := s.listPlanEntitlements(ctx, plans[index].ID)
		if err != nil {
			return nil, err
		}
		plans[index].Entitlements = items
	}
	return plans, nil
}

func (s *Store) GetAdminPlan(ctx context.Context, planID uint64) (Plan, error) {
	if s == nil || s.db == nil || planID == 0 {
		return Plan{}, ErrInvalidInput
	}
	var plan Plan
	err := s.db.QueryRowContext(ctx, `
SELECT id,code,name,status,currency,amount_minor,billing_period,version,created_at,updated_at
FROM billing_plans WHERE id=?`, planID).Scan(
		&plan.ID, &plan.Code, &plan.Name, &plan.Status, &plan.Money.Currency, &plan.Money.AmountMinor,
		&plan.BillingPeriod, &plan.Version, &plan.CreatedAt, &plan.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Plan{}, ErrNotFound
	}
	if err != nil {
		return Plan{}, err
	}
	plan.CreatedAt = plan.CreatedAt.UTC()
	plan.UpdatedAt = plan.UpdatedAt.UTC()
	plan.Entitlements, err = s.listPlanEntitlements(ctx, plan.ID)
	return plan, err
}

func (s *Store) CreateAdminPlan(ctx context.Context, input CreatePlanInput) (Plan, error) {
	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
	input.Money.Currency = strings.TrimSpace(input.Money.Currency)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	entitlements, entitlementErr := normalizePlanEntitlements(input.Entitlements)
	if s == nil || s.db == nil || !planCodePattern.MatchString(input.Code) || input.Name == "" || len(input.Name) > 160 ||
		(input.Status != PlanDraft && input.Status != PlanActive) || !validBillingPeriod(input.BillingPeriod) ||
		input.Money.Validate(false) != nil || input.ActorID == "" || input.CorrelationID == "" || entitlementErr != nil {
		return Plan{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version)
VALUES (?,?,?,?,?,?,1)`, input.Code, input.Name, input.Status, input.Money.Currency, input.Money.AmountMinor, input.BillingPeriod)
	if err != nil {
		return Plan{}, wrapConflict(err)
	}
	planID, err := result.LastInsertId()
	if err != nil {
		return Plan{}, err
	}
	if err := replacePlanEntitlementsTx(ctx, tx, uint64(planID), 1, entitlements); err != nil {
		return Plan{}, err
	}
	if err := appendAuditTx(ctx, tx, "", input.ActorID, "billing.plan.create", "billing_plan", strconv.FormatUint(uint64(planID), 10), "admin_plan_create", input.CorrelationID, "success", map[string]any{
		"plan_id": uint64(planID), "code": input.Code, "status": input.Status, "currency": input.Money.Currency,
	}); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, err
	}
	return s.GetAdminPlan(ctx, uint64(planID))
}

func (s *Store) UpdateAdminPlan(ctx context.Context, input UpdatePlanInput) (Plan, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Money.Currency = strings.TrimSpace(input.Money.Currency)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	entitlements, entitlementErr := normalizePlanEntitlements(input.Entitlements)
	if s == nil || s.db == nil || input.PlanID == 0 || input.ExpectedVersion == 0 || input.Name == "" || len(input.Name) > 160 ||
		!validPlanStatus(input.Status) || !validBillingPeriod(input.BillingPeriod) || input.Money.Validate(false) != nil ||
		input.ActorID == "" || input.CorrelationID == "" || entitlementErr != nil {
		return Plan{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Plan{}, err
	}
	defer tx.Rollback()
	var currentStatus PlanStatus
	var currentVersion uint64
	if err := tx.QueryRowContext(ctx, `SELECT status,version FROM billing_plans WHERE id=? FOR UPDATE`, input.PlanID).Scan(&currentStatus, &currentVersion); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Plan{}, ErrNotFound
		}
		return Plan{}, err
	}
	if currentVersion != input.ExpectedVersion || !planStatusTransitionAllowed(currentStatus, input.Status) {
		return Plan{}, ErrConflict
	}
	newVersion := currentVersion + 1
	result, err := tx.ExecContext(ctx, `
UPDATE billing_plans
SET name=?,status=?,currency=?,amount_minor=?,billing_period=?,version=?,updated_at=CURRENT_TIMESTAMP(6)
WHERE id=? AND version=?`, input.Name, input.Status, input.Money.Currency, input.Money.AmountMinor, input.BillingPeriod, newVersion, input.PlanID, currentVersion)
	if err != nil {
		return Plan{}, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		if err != nil {
			return Plan{}, err
		}
		return Plan{}, ErrConflict
	}
	if err := replacePlanEntitlementsTx(ctx, tx, input.PlanID, newVersion, entitlements); err != nil {
		return Plan{}, err
	}
	if err := appendAuditTx(ctx, tx, "", input.ActorID, "billing.plan.update", "billing_plan", strconv.FormatUint(input.PlanID, 10), "admin_plan_update", input.CorrelationID, "success", map[string]any{
		"plan_id": input.PlanID, "from_status": currentStatus, "to_status": input.Status, "version": newVersion,
	}); err != nil {
		return Plan{}, err
	}
	if err := tx.Commit(); err != nil {
		return Plan{}, err
	}
	return s.GetAdminPlan(ctx, input.PlanID)
}

func replacePlanEntitlementsTx(ctx context.Context, tx *sql.Tx, planID, sourceVersion uint64, items []PlanEntitlement) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM billing_plan_entitlements WHERE plan_id=?`, planID); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO billing_plan_entitlements (plan_id,capability,limit_value,unit,source_version)
VALUES (?,?,?,?,?)`, planID, item.Capability, item.LimitValue, item.Unit, sourceVersion); err != nil {
			return err
		}
	}
	return nil
}

func normalizePlanEntitlements(items []PlanEntitlement) ([]PlanEntitlement, error) {
	out := make([]PlanEntitlement, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item.Capability = strings.TrimSpace(item.Capability)
		item.Unit = strings.TrimSpace(item.Unit)
		if item.Unit == "" {
			item.Unit = "count"
		}
		if !capabilityPattern.MatchString(item.Capability) || item.LimitValue == 0 || !entitlementUnitPattern.MatchString(item.Unit) {
			return nil, ErrInvalidInput
		}
		if _, exists := seen[item.Capability]; exists {
			return nil, ErrInvalidInput
		}
		seen[item.Capability] = struct{}{}
		item.SourceVersion = 0
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Capability < out[j].Capability })
	return out, nil
}

func validBillingPeriod(period BillingPeriod) bool {
	return period == BillingOneTime || period == BillingMonthly || period == BillingYearly
}

func validPlanStatus(status PlanStatus) bool {
	return status == PlanDraft || status == PlanActive || status == PlanArchived
}

func planStatusTransitionAllowed(current, next PlanStatus) bool {
	if current == next {
		return current == PlanDraft || current == PlanActive
	}
	switch current {
	case PlanDraft:
		return next == PlanActive || next == PlanArchived
	case PlanActive:
		return next == PlanArchived
	case PlanArchived:
		return false
	default:
		return false
	}
}
