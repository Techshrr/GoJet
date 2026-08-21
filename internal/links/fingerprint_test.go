package links

import (
	"errors"
	"reflect"
	"testing"
)

func TestRiskFingerprintTargetSet(t *testing.T) {
	routing := []RoutingRule{
		{ID: "us", MatchType: "country", MatchValue: "US", Destination: "HTTPS://Example.COM:443/us?b=2&a=1#ignored", Enabled: true},
		{ID: "disabled", MatchType: "country", MatchValue: "DE", Destination: "https://disabled.example/", Enabled: false},
	}
	variants := []ABVariant{
		{ID: "a", Destination: "https://example.com/a", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://example.com/b", Weight: 50, Enabled: true},
	}

	fingerprint, targets, err := RiskFingerprint("https://example.com", routing, variants)
	if err != nil {
		t.Fatalf("RiskFingerprint() error = %v", err)
	}

	wantTargets := []string{
		"https://example.com/",
		"https://example.com/a",
		"https://example.com/b",
		"https://example.com/us?a=1&b=2",
	}
	if !reflect.DeepEqual(targets, wantTargets) {
		t.Fatalf("targets = %#v, want %#v", targets, wantTargets)
	}
	if len(fingerprint) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(fingerprint))
	}

	fingerprint2, targets2, err := RiskFingerprint("https://EXAMPLE.com:443/", []RoutingRule{
		{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://example.com/us?a=1&b=2", Enabled: true},
	}, []ABVariant{
		{ID: "b", Destination: "https://example.com/b", Weight: 50, Enabled: true},
		{ID: "a", Destination: "https://example.com/a", Weight: 50, Enabled: true},
	})
	if err != nil {
		t.Fatalf("second RiskFingerprint() error = %v", err)
	}
	if fingerprint2 != fingerprint || !reflect.DeepEqual(targets2, wantTargets) {
		t.Fatalf("fingerprint must be deterministic: got %q/%#v want %q/%#v", fingerprint2, targets2, fingerprint, wantTargets)
	}
}

func TestRiskFingerprintChangesOnReachableTargetMutation(t *testing.T) {
	base, _, err := RiskFingerprint("https://example.com", nil, []ABVariant{
		{ID: "a", Destination: "https://a.example/", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b.example/", Weight: 50, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	mutated, _, err := RiskFingerprint("https://example.com", nil, []ABVariant{
		{ID: "a", Destination: "https://a.example/", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://c.example/", Weight: 50, Enabled: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if base == mutated {
		t.Fatal("fingerprint did not change after reachable target mutation")
	}
}

func TestReachableTargetSetDeduplicatesEquivalentURLs(t *testing.T) {
	_, targets, err := RiskFingerprint(
		"https://Example.com:443/path?z=2&a=1",
		[]RoutingRule{{ID: "same", Destination: "https://example.com/path?a=1&z=2#fragment", Enabled: true}},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("target count = %d, want 1: %#v", len(targets), targets)
	}
}

func TestNormalizeDestinationRejectsUnsafeForms(t *testing.T) {
	for _, raw := range []string{
		"",
		"/relative",
		"ftp://example.com/file",
		"https://user:pass@example.com/private",
	} {
		if _, err := NormalizeDestination(raw); !errors.Is(err, ErrInvalidDestination) {
			t.Errorf("NormalizeDestination(%q) error = %v, want ErrInvalidDestination", raw, err)
		}
	}
}

func TestValidateABWeights(t *testing.T) {
	valid := []ABVariant{
		{ID: "a", Destination: "https://a.example/", Weight: 40, Enabled: true},
		{ID: "b", Destination: "https://b.example/", Weight: 60, Enabled: true},
	}
	if err := ValidateABWeights(valid); err != nil {
		t.Fatalf("valid weights rejected: %v", err)
	}

	invalidCases := [][]ABVariant{
		{{ID: "only", Destination: "https://a.example/", Weight: 100, Enabled: true}},
		{{ID: "a", Weight: 50, Enabled: true}, {ID: "b", Weight: 40, Enabled: true}},
		{{ID: "a", Weight: 50, Enabled: true}, {ID: "a", Weight: 50, Enabled: true}},
		{{ID: "a", Weight: 0, Enabled: true}, {ID: "b", Weight: 100, Enabled: true}},
	}
	for i, variants := range invalidCases {
		if err := ValidateABWeights(variants); !errors.Is(err, ErrInvalidABWeights) {
			t.Errorf("invalid case %d error = %v, want ErrInvalidABWeights", i, err)
		}
	}
}
