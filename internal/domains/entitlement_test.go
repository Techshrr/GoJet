package domains

import (
	"errors"
	"testing"
	"time"
)

func TestResolveEntitlementNoneAndRequested(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)

	locked, err := ResolveEntitlement(now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if locked.Source != SourceNone || locked.Status != EntitlementExpired || locked.MutationAllowed || locked.ExistingRoutingAllowed {
		t.Fatalf("unexpected locked resolution: %+v", locked)
	}

	requested, err := ResolveEntitlement(now, nil, &AccessRequest{
		WorkspaceID:     "ws-requested",
		SupportTicketID: "ticket-1001",
		SubmittedAt:     now.Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if requested.Source != SourceNone || requested.Status != EntitlementRequested || requested.SupportTicketID != "ticket-1001" {
		t.Fatalf("ticket request incorrectly resolved: %+v", requested)
	}
	if requested.MutationAllowed || requested.ExistingRoutingAllowed || requested.DomainLimit != 0 {
		t.Fatalf("support request granted authority: %+v", requested)
	}
}

func TestResolveEntitlementActivePlan(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	source := planSource("business-subscription-1", 5, now.Add(-24*time.Hour), nil)

	resolved, err := ResolveEntitlement(now, []EntitlementSource{source}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourcePlan || resolved.Status != EntitlementActive || resolved.DomainLimit != 5 {
		t.Fatalf("unexpected plan resolution: %+v", resolved)
	}
	if !resolved.MutationAllowed || !resolved.ExistingRoutingAllowed || resolved.GracePeriod {
		t.Fatalf("active plan authority flags are wrong: %+v", resolved)
	}
}

func TestManualApprovalMustBeIndependentAndBounded(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	invalid := EntitlementSource{
		WorkspaceID: "ws-1",
		Source:      SourceManualApproval,
		SourceKey:   "approval-1",
		Status:      EntitlementActive,
		DomainLimit: 2,
		StartsAt:    now.Add(-time.Hour),
	}
	if err := ValidateEntitlementSource(invalid); !errors.Is(err, ErrInvalidEntitlementSource) {
		t.Fatalf("malformed manual approval accepted: %v", err)
	}

	expires := now.Add(30 * 24 * time.Hour)
	valid := invalid
	valid.ExpiresAt = &expires
	valid.GrantedBy = "admin-entitlements-1"
	valid.SupportTicketID = "ticket-2001"
	valid.DecisionReason = "approved after independent entitlement review"
	if err := ValidateEntitlementSource(valid); err != nil {
		t.Fatalf("valid manual approval rejected: %v", err)
	}
}

func TestResolveEntitlementUsesHighestValidDomainLimit(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	plan := planSource("business-subscription-1", 3, now.Add(-24*time.Hour), nil)
	manual := manualSource("approval-1", 7, now.Add(-time.Hour), now.Add(30*24*time.Hour))

	resolved, err := ResolveEntitlement(now, []EntitlementSource{plan, manual}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourceManualApproval || resolved.DomainLimit != 7 || len(resolved.ValidSources) != 2 {
		t.Fatalf("coexisting sources did not select highest valid limit: %+v", resolved)
	}

	plan.DomainLimit = 9
	resolved, err = ResolveEntitlement(now, []EntitlementSource{manual, plan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourcePlan || resolved.DomainLimit != 9 || len(resolved.ValidSources) != 2 {
		t.Fatalf("plan/manual coexistence resolution changed incorrectly: %+v", resolved)
	}
}

func TestSecurityStateOverridesOtherwiseActiveSources(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	plan := planSource("business-subscription-1", 10, now.Add(-24*time.Hour), nil)
	manual := manualSource("approval-1", 4, now.Add(-time.Hour), now.Add(30*24*time.Hour))
	manual.Status = EntitlementSuspended
	manual.DecisionReason = "security_review"
	manual.SecurityCategory = "security"

	resolved, err := ResolveEntitlement(now, []EntitlementSource{plan, manual}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != EntitlementSuspended || resolved.Source != SourceManualApproval {
		t.Fatalf("security suspension did not override entitlement: %+v", resolved)
	}
	if resolved.MutationAllowed || resolved.ExistingRoutingAllowed {
		t.Fatalf("security-suspended entitlement remained usable: %+v", resolved)
	}
}

func TestNormalDowngradeGraceAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	degradedAt := now.Add(-24 * time.Hour)
	graceUntil := degradedAt.Add(NormalDowngradeGrace)
	plan := planSource("business-subscription-1", 5, now.Add(-30*24*time.Hour), nil)
	plan.DegradedAt = &degradedAt
	plan.GraceUntil = &graceUntil
	plan.DecisionReason = "normal_plan_downgrade"

	grace, err := ResolveEntitlement(now, []EntitlementSource{plan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if grace.Status != EntitlementActive || !grace.GracePeriod || grace.MutationAllowed || !grace.ExistingRoutingAllowed {
		t.Fatalf("normal downgrade grace policy is wrong: %+v", grace)
	}
	if grace.GraceUntil == nil || !grace.GraceUntil.Equal(graceUntil) {
		t.Fatalf("grace deadline mismatch: %+v", grace)
	}

	after := graceUntil.Add(time.Microsecond)
	expired, err := ResolveEntitlement(after, []EntitlementSource{plan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if expired.Source != SourceNone || expired.Status != EntitlementExpired || expired.ExistingRoutingAllowed {
		t.Fatalf("grace did not expire fail-closed: %+v", expired)
	}
}

func TestManualApprovalContinuesServiceAfterPlanDowngrade(t *testing.T) {
	now := time.Date(2026, 8, 21, 15, 0, 0, 0, time.UTC)
	degradedAt := now.Add(-time.Hour)
	graceUntil := degradedAt.Add(NormalDowngradeGrace)
	plan := planSource("business-subscription-1", 10, now.Add(-30*24*time.Hour), nil)
	plan.DegradedAt = &degradedAt
	plan.GraceUntil = &graceUntil
	manual := manualSource("approval-1", 4, now.Add(-24*time.Hour), now.Add(30*24*time.Hour))

	resolved, err := ResolveEntitlement(now, []EntitlementSource{plan, manual}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourceManualApproval || resolved.DomainLimit != 4 || resolved.GracePeriod {
		t.Fatalf("valid manual approval did not replace degraded plan authority: %+v", resolved)
	}
	if !resolved.MutationAllowed || !resolved.ExistingRoutingAllowed {
		t.Fatalf("valid manual approval failed to continue service: %+v", resolved)
	}
}

func planSource(key string, limit uint32, starts time.Time, expires *time.Time) EntitlementSource {
	return EntitlementSource{
		WorkspaceID: "ws-1",
		Source:      SourcePlan,
		SourceKey:   key,
		Status:      EntitlementActive,
		DomainLimit: limit,
		StartsAt:    starts,
		ExpiresAt:   expires,
	}
}

func manualSource(key string, limit uint32, starts, expires time.Time) EntitlementSource {
	return EntitlementSource{
		WorkspaceID:     "ws-1",
		Source:          SourceManualApproval,
		SourceKey:       key,
		Status:          EntitlementActive,
		DomainLimit:     limit,
		StartsAt:        starts,
		ExpiresAt:       &expires,
		GrantedBy:       "admin-entitlements-1",
		SupportTicketID: "ticket-2001",
		DecisionReason:  "approved after independent entitlement review",
	}
}
