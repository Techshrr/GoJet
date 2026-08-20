package links

import (
	"net/url"
	"testing"
)

func TestSelectTargetRoutingPrecedesAB(t *testing.T) {
	link := Link{
		ID: 42,
		PrimaryDestination: "https://primary.example/",
		Routing: []RoutingRule{
			{ID: "zh", MatchType: "language", MatchValue: "zh", Destination: "https://cn.example/", Enabled: true},
		},
		AB: []ABVariant{
			{ID: "a", Destination: "https://a.example/", Weight: 50, Enabled: true},
			{ID: "b", Destination: "https://b.example/", Weight: 50, Enabled: true},
		},
	}
	selected, err := SelectTarget(link, ResolveContext{Language: "zh-CN", ABSeed: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Source != "routing" || selected.SourceID != "zh" || selected.Destination != "https://cn.example/" {
		t.Fatalf("unexpected routing selection: %#v", selected)
	}
}

func TestSelectTargetABWhenNoRoutingMatch(t *testing.T) {
	link := Link{
		ID: 77,
		PrimaryDestination: "https://primary.example/",
		Routing: []RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://us.example/", Enabled: true}},
		AB: []ABVariant{
			{ID: "a", Destination: "https://a.example/", Weight: 40, Enabled: true},
			{ID: "b", Destination: "https://b.example/", Weight: 60, Enabled: true},
		},
	}
	first, err := SelectTarget(link, ResolveContext{Country: "SG", ABSeed: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SelectTarget(link, ResolveContext{Country: "SG", ABSeed: "stable"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Source != "ab" {
		t.Fatalf("A/B selection must be deterministic: first=%#v second=%#v", first, second)
	}
	if err := VerifySelectedTargetIsFingerprintMember(link, first.Destination); err != nil {
		t.Fatalf("A/B target must be fingerprint member: %v", err)
	}
}

func TestApplyUTMAfterSelection(t *testing.T) {
	got, err := ApplyUTM("https://example.com/path?keep=1&utm_source=old", UTMConfig{
		Source: "gojet", Medium: "short", Campaign: "launch",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	query := u.Query()
	if query.Get("keep") != "1" || query.Get("utm_source") != "gojet" || query.Get("utm_medium") != "short" || query.Get("utm_campaign") != "launch" {
		t.Fatalf("unexpected UTM query: %v", query)
	}
}

func TestRoutingMatchTypes(t *testing.T) {
	cases := []struct {
		rule RoutingRule
		ctx  ResolveContext
	}{
		{RoutingRule{MatchType: "country", MatchValue: "US"}, ResolveContext{Country: "us"}},
		{RoutingRule{MatchType: "device", MatchValue: "mobile"}, ResolveContext{Device: "MOBILE"}},
		{RoutingRule{MatchType: "language", MatchValue: "zh"}, ResolveContext{Language: "zh-CN"}},
		{RoutingRule{MatchType: "source", MatchValue: "example.com"}, ResolveContext{SourceHostname: "EXAMPLE.com"}},
	}
	for i, test := range cases {
		if !routingRuleMatches(test.rule, test.ctx) {
			t.Errorf("case %d did not match", i)
		}
	}
}
