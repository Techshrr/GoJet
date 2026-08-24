package billing

import (
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

func TestStrictPlanDowngrade(t *testing.T) {
	t.Parallel()

	current := downgradePlanSnapshot{
		ID:     1,
		Money:  Money{Currency: "USD", AmountMinor: 2000},
		Period: BillingMonthly,
		Entitlements: map[string]uint64{
			"links":          1000,
			"custom_domains": 10,
		},
	}
	target := downgradePlanSnapshot{
		ID:     2,
		Status: PlanActive,
		Money:  Money{Currency: "USD", AmountMinor: 1000},
		Period: BillingMonthly,
		Entitlements: map[string]uint64{
			"links":          500,
			"custom_domains": 5,
		},
	}
	if !strictPlanDowngrade(current, target) {
		t.Fatal("expected a lower-price, lower-limit plan to be a strict downgrade")
	}

	target.Entitlements["custom_domains"] = 11
	if strictPlanDowngrade(current, target) {
		t.Fatal("target plan must not increase any entitlement")
	}

	target.Entitlements["custom_domains"] = 5
	target.Money.Currency = "EUR"
	if strictPlanDowngrade(current, target) {
		t.Fatal("cross-currency change must not be treated as a downgrade")
	}
}

func TestStrictPlanDowngradeRequiresARealDecrease(t *testing.T) {
	t.Parallel()

	current := downgradePlanSnapshot{
		ID:     1,
		Money:  Money{Currency: "USD", AmountMinor: 1000},
		Period: BillingMonthly,
		Entitlements: map[string]uint64{
			"links": 100,
		},
	}
	same := downgradePlanSnapshot{
		ID:     2,
		Money:  Money{Currency: "USD", AmountMinor: 1000},
		Period: BillingMonthly,
		Entitlements: map[string]uint64{
			"links": 100,
		},
	}
	if strictPlanDowngrade(current, same) {
		t.Fatal("equivalent plans are not a downgrade")
	}

	removed := same
	removed.Entitlements = map[string]uint64{}
	if !strictPlanDowngrade(current, removed) {
		t.Fatal("removing an entitlement is a strict downgrade")
	}
}

func TestDowngradeSubscriptionIDIsBoundaryScoped(t *testing.T) {
	t.Parallel()
	effective := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	first := downgradeSubscriptionID("sub_current", effective)
	if first == "" || first[:4] != "sub_" {
		t.Fatalf("unexpected downgrade subscription id: %s", first)
	}
	if first != downgradeSubscriptionID("sub_current", effective) {
		t.Fatal("downgrade subscription id must be deterministic")
	}
	if first == downgradeSubscriptionID("sub_current", effective.Add(time.Microsecond)) {
		t.Fatal("different effective boundaries must not share a target subscription id")
	}
}

func TestDowngradeGrantBoundarySwitchesAtomically(t *testing.T) {
	t.Parallel()
	effective := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	oldEnd := effective
	newEnd := effective.AddDate(0, 1, 0)
	grants := []EntitlementGrant{
		{
			WorkspaceID: "ws-1", Capability: "links", SourceType: SourceBilling,
			SourceID: "sub_old", LimitValue: 1000,
			StartsAt: effective.AddDate(0, -1, 0), EndsAt: &oldEnd,
		},
		{
			WorkspaceID: "ws-1", Capability: "links", SourceType: SourceBilling,
			SourceID: "sub_target", LimitValue: 100,
			StartsAt: effective, EndsAt: &newEnd,
		},
	}

	before, err := ResolveEntitlement(effective.Add(-time.Microsecond), "ws-1", "links", grants)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Allowed || before.LimitValue != 1000 || before.SourceID != "sub_old" {
		t.Fatalf("old grant must remain authoritative immediately before boundary: %+v", before)
	}

	at, err := ResolveEntitlement(effective, "ws-1", "links", grants)
	if err != nil {
		t.Fatal(err)
	}
	if !at.Allowed || at.LimitValue != 100 || at.SourceID != "sub_target" {
		t.Fatalf("target grant must become authoritative at exact boundary: %+v", at)
	}
}

func TestP06DowngradeGraceHandsOffToLowerTarget(t *testing.T) {
	t.Parallel()
	graceStart := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	effective := graceStart.Add(domains.NormalDowngradeGrace)
	oldExpiry := graceStart
	targetExpiry := effective.AddDate(0, 1, 0)
	current := domains.EntitlementSource{
		WorkspaceID: "ws-1", Source: domains.SourcePlan, SourceKey: p13DomainPlanSourceKey,
		Status: domains.EntitlementActive, DomainLimit: 10,
		StartsAt: graceStart.AddDate(0, -1, 0), ExpiresAt: &oldExpiry,
		DegradedAt: &graceStart, GraceUntil: &effective, DecisionReason: "billing_plan_downgrade",
	}
	target := domains.EntitlementSource{
		WorkspaceID: "ws-1", Source: domains.SourcePlan, SourceKey: p13DomainTargetPlanSourceKey,
		Status: domains.EntitlementActive, DomainLimit: 3,
		StartsAt: effective, ExpiresAt: &targetExpiry, DecisionReason: "billing_downgrade_target_entitlement",
	}

	grace, err := domains.ResolveEntitlement(graceStart.Add(time.Microsecond), []domains.EntitlementSource{current, target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !grace.GracePeriod || grace.MutationAllowed || !grace.ExistingRoutingAllowed || grace.DomainLimit != 10 {
		t.Fatalf("P06 grace authority must preserve routing while blocking new mutations: %+v", grace)
	}

	at, err := domains.ResolveEntitlement(effective, []domains.EntitlementSource{current, target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if at.DomainLimit != 3 || at.GracePeriod || !at.MutationAllowed || !at.ExistingRoutingAllowed {
		t.Fatalf("lower target must take over at exact grace boundary: %+v", at)
	}
}

func TestP06PackageDowngradeUsesGraceEvenWhenDomainLimitIsUnchanged(t *testing.T) {
	t.Parallel()
	graceStart := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	effective := graceStart.Add(domains.NormalDowngradeGrace)
	oldExpiry := graceStart
	targetExpiry := effective.AddDate(0, 1, 0)
	current := domains.EntitlementSource{
		WorkspaceID: "ws-1", Source: domains.SourcePlan, SourceKey: p13DomainPlanSourceKey,
		Status: domains.EntitlementActive, DomainLimit: 5,
		StartsAt: graceStart.AddDate(0, -1, 0), ExpiresAt: &oldExpiry,
		DegradedAt: &graceStart, GraceUntil: &effective, DecisionReason: "billing_plan_downgrade",
	}
	target := domains.EntitlementSource{
		WorkspaceID: "ws-1", Source: domains.SourcePlan, SourceKey: p13DomainTargetPlanSourceKey,
		Status: domains.EntitlementActive, DomainLimit: 5,
		StartsAt: effective, ExpiresAt: &targetExpiry, DecisionReason: "billing_downgrade_target_entitlement",
	}

	grace, err := domains.ResolveEntitlement(graceStart.Add(time.Microsecond), []domains.EntitlementSource{current, target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !grace.GracePeriod || grace.MutationAllowed || !grace.ExistingRoutingAllowed || grace.DomainLimit != 5 {
		t.Fatalf("package downgrade must enter P06 grace even with unchanged domain limit: %+v", grace)
	}

	at, err := domains.ResolveEntitlement(effective, []domains.EntitlementSource{current, target}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if at.DomainLimit != 5 || at.GracePeriod || !at.MutationAllowed || !at.ExistingRoutingAllowed {
		t.Fatalf("same-limit target must resume normal P06 authority after grace: %+v", at)
	}
}
