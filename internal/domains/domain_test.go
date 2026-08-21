package domains

import "testing"

func TestDomainReadinessKeepsAxesIndependent(t *testing.T) {
	entitlement := ResolvedEntitlement{MutationAllowed: true, ExistingRoutingAllowed: true}
	domain := Domain{
		RoutingState:     RoutingEnabled,
		OwnershipStatus:  OwnershipVerified,
		IngressDNSStatus: IngressValid,
		HTTPSStatus:      HTTPSActive,
		RiskStatus:       RiskAllow,
	}
	ready := domain.Readiness(entitlement)
	if !ready.ReadyForNewLinks || !ready.ReadyForRouting {
		t.Fatalf("fully ready domain rejected: %+v", ready)
	}

	domain.RiskStatus = RiskReview
	ready = domain.Readiness(entitlement)
	if ready.ReadyForNewLinks || ready.ReadyForRouting || ready.RiskReady {
		t.Fatalf("risk review did not fail closed: %+v", ready)
	}
	if !ready.OwnershipReady || !ready.IngressDNSReady || !ready.HTTPSReady {
		t.Fatalf("risk state incorrectly collapsed another axis: %+v", ready)
	}

	domain.RiskStatus = RiskAllow
	grace := ResolvedEntitlement{MutationAllowed: false, ExistingRoutingAllowed: true, GracePeriod: true}
	ready = domain.Readiness(grace)
	if ready.ReadyForNewLinks || !ready.ReadyForRouting {
		t.Fatalf("normal downgrade grace authority is wrong: %+v", ready)
	}
}

func TestNormalizeASCIIHostname(t *testing.T) {
	valid := map[string]string{
		"ExAmPle.COM.":          "example.com",
		"links.customer.test":   "links.customer.test",
		"xn--bcher-kva.example": "xn--bcher-kva.example",
	}
	for input, want := range valid {
		got, err := NormalizeASCIIHostname(input)
		if err != nil || got != want {
			t.Fatalf("NormalizeASCIIHostname(%q)=(%q,%v), want %q", input, got, err, want)
		}
	}

	invalid := []string{"", "localhost", "127.0.0.1", "*.example.com", "singlelabel", "bad_label.example", "-bad.example", "bad-.example", "bücher.example"}
	for _, input := range invalid {
		if _, err := NormalizeASCIIHostname(input); err == nil {
			t.Fatalf("invalid hostname accepted: %q", input)
		}
	}
}

func TestOwnershipSecretIsHighEntropyVerifierOnly(t *testing.T) {
	plain, hash, err := NewOwnershipSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) < 40 {
		t.Fatalf("ownership secret unexpectedly short: %d", len(plain))
	}
	if !OwnershipSecretMatches(plain, hash) {
		t.Fatal("ownership secret does not match its verifier")
	}
	if OwnershipSecretMatches(plain+"x", hash) {
		t.Fatal("ownership verifier accepted altered secret")
	}
	if OwnershipTXTName("links.example.com") != "_gojet-verification.links.example.com" {
		t.Fatal("ownership TXT name contract changed")
	}
	if OwnershipTXTValue(plain) != "gojet-verification="+plain {
		t.Fatal("ownership TXT value contract changed")
	}
}
