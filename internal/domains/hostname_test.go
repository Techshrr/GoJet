package domains

import "testing"

func TestHostnamePolicyIDNAAndRegistrableSuffix(t *testing.T) {
	policy, err := NewHostnamePolicy([]string{"gojet.cc", "www.gojet.cc"})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]NormalizedHostname{
		"ExAmPle.COM.":       {ASCII: "example.com", Display: "example.com"},
		"bücher.de":          {ASCII: "xn--bcher-kva.de", Display: "bücher.de"},
		"XN--BCHER-KVA.DE.": {ASCII: "xn--bcher-kva.de", Display: "bücher.de"},
	}
	for input, want := range cases {
		got, normalizeErr := policy.Normalize(input)
		if normalizeErr != nil {
			t.Fatalf("Normalize(%q): %v", input, normalizeErr)
		}
		if got != want {
			t.Fatalf("Normalize(%q)=%+v want %+v", input, got, want)
		}
	}
}

func TestHostnamePolicyRejectsUnsafeOrUnregistrableNames(t *testing.T) {
	policy, err := NewHostnamePolicy([]string{"gojet.cc", "www.gojet.cc"})
	if err != nil {
		t.Fatal(err)
	}
	invalid := []string{
		"",
		"localhost",
		"127.0.0.1",
		"::1",
		"*.customer.com",
		"singlelabel",
		"foo.example",
		"foo.invalid",
		"foo.test",
		"com",
		"co.uk",
		"gojet.cc",
		"www.gojet.cc",
		"assets.gojet.cc",
		"bad_label.example.com",
	}
	for _, input := range invalid {
		if got, normalizeErr := policy.Normalize(input); normalizeErr == nil {
			t.Fatalf("unsafe hostname %q accepted as %+v", input, got)
		}
	}
}

func TestHostnamePolicyCanonicalIdentityMakesUnicodeAndPunycodeEqual(t *testing.T) {
	policy, err := NewHostnamePolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	unicodeHost, err := policy.Normalize("bücher.de")
	if err != nil {
		t.Fatal(err)
	}
	punycodeHost, err := policy.Normalize("xn--bcher-kva.de")
	if err != nil {
		t.Fatal(err)
	}
	if unicodeHost.ASCII != punycodeHost.ASCII {
		t.Fatalf("IDNA aliases have different persisted identity: %q vs %q", unicodeHost.ASCII, punycodeHost.ASCII)
	}
}

func TestGoJetHostnamePolicyRejectsPlatformRootsAndDescendants(t *testing.T) {
	policy := GoJetHostnamePolicy()
	for _, input := range []string{
		"gojet.cc",
		"www.gojet.cc",
		"api.gojet.cc",
		"gojet.cn",
		"www.gojet.cn",
		"redirect.gojet.cn",
	} {
		if got, err := policy.Normalize(input); err == nil {
			t.Fatalf("GoJet platform hostname %q accepted as %+v", input, got)
		}
	}
}
