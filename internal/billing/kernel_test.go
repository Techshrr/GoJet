package billing

import (
	"errors"
	"testing"
	"time"
)

func TestFrozenProviders(t *testing.T) {
	providers := FrozenProviders()
	if len(providers) != 6 {
		t.Fatalf("provider count=%d", len(providers))
	}
	for _, p := range []Provider{ProviderAlipay, ProviderWeChat, ProviderEpay, ProviderPayPal, ProviderStripe, ProviderCrypto} {
		if !IsFrozenProvider(p) {
			t.Fatalf("missing %q", p)
		}
	}
	if IsFrozenProvider("unknown") {
		t.Fatal("unknown provider accepted")
	}
}

func TestMoneyValidation(t *testing.T) {
	if err := (Money{Currency: "USD", AmountMinor: 100}).Validate(false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is((Money{Currency: "usd", AmountMinor: 100}).Validate(false), ErrInvalidMoney) {
		t.Fatal("lowercase currency accepted")
	}
	if !errors.Is((Money{Currency: "USD", AmountMinor: 0}).Validate(false), ErrInvalidMoney) {
		t.Fatal("zero payable amount accepted")
	}
}

func TestResolveEntitlementHardDenyWins(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	grants := []EntitlementGrant{
		{WorkspaceID: "ws", Capability: "custom_domains", SourceType: SourceBilling, SourceID: "sub_1", LimitValue: 20, StartsAt: now.Add(-time.Hour)},
		{WorkspaceID: "ws", Capability: "custom_domains", SourceType: SourceHardDeny, SourceID: "security", StartsAt: now.Add(-time.Hour)},
	}
	got, err := ResolveEntitlement(now, "ws", "custom_domains", grants)
	if err != nil {
		t.Fatal(err)
	}
	if got.Allowed || got.SourceType != SourceHardDeny {
		t.Fatalf("got=%+v", got)
	}
}

func TestResolveEntitlementUsesNonAdditiveMaximum(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	grants := []EntitlementGrant{
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceManual, SourceID: "manual", LimitValue: 10, StartsAt: now.Add(-time.Hour)},
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceBilling, SourceID: "sub", LimitValue: 25, StartsAt: now.Add(-time.Hour)},
	}
	got, err := ResolveEntitlement(now, "ws", "links", grants)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Allowed || got.LimitValue != 25 || got.SourceID != "sub" {
		t.Fatalf("got=%+v", got)
	}
}

func TestResolveEntitlementTiePrefersManualThenInherited(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	grants := []EntitlementGrant{
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceBilling, SourceID: "billing", LimitValue: 25, StartsAt: now.Add(-time.Hour)},
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceInherited, SourceID: "inherited", LimitValue: 25, StartsAt: now.Add(-time.Hour)},
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceManual, SourceID: "manual", LimitValue: 25, StartsAt: now.Add(-time.Hour)},
	}
	got, err := ResolveEntitlement(now, "ws", "links", grants)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceType != SourceManual || got.SourceID != "manual" {
		t.Fatalf("got=%+v", got)
	}
}

func TestResolveEntitlementIgnoresExpiredAndRevoked(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	end := now.Add(-time.Minute)
	revoked := now.Add(-time.Second)
	grants := []EntitlementGrant{
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceBilling, SourceID: "expired", LimitValue: 50, StartsAt: now.Add(-time.Hour), EndsAt: &end},
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceManual, SourceID: "revoked", LimitValue: 60, StartsAt: now.Add(-time.Hour), RevokedAt: &revoked},
		{WorkspaceID: "ws", Capability: "links", SourceType: SourceBaseline, SourceID: "free", LimitValue: 5, StartsAt: now.Add(-time.Hour)},
	}
	got, err := ResolveEntitlement(now, "ws", "links", grants)
	if err != nil {
		t.Fatal(err)
	}
	if got.LimitValue != 5 || got.SourceID != "free" {
		t.Fatalf("got=%+v", got)
	}
}

func TestDeterministicTestVerifier(t *testing.T) {
	v := DeterministicTestVerifier{Secrets: map[Provider][]byte{ProviderStripe: []byte("0123456789abcdef0123456789abcdef")}}
	body := []byte(`{"status":"paid"}`)
	sig, err := v.Sign(ProviderStripe, "evt_1", "txn_1", body)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Verify(ProviderStripe, "evt_1", "txn_1", body, sig); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(v.Verify(ProviderStripe, "evt_1", "txn_1", []byte(`{"status":"failed"}`), sig), ErrInvalidCallbackSignature) {
		t.Fatal("tamper accepted")
	}
	if _, err := v.Sign("unknown", "evt", "txn", body); !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("err=%v", err)
	}
}
