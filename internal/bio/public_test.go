package bio

import (
	"strings"
	"testing"

	"github.com/Techshrr/GoJet/internal/links"
)

func TestPublicBioTemplateEscapesUGCAndKeepsRequiredRel(t *testing.T) {
	var output strings.Builder
	err := publicBioTemplate.Execute(&output, publicPageData{
		State:    "published",
		Headline: "Bio page",
		Message:  "Published",
		Title:    `<script>alert("title")</script>`,
		Bio:      `<img src=x onerror=alert(1)>`,
		Links: []publicLinkData{
			{Position: 0, Label: `<b>Allowed</b>`, RiskStatus: "allowed", Href: "https://example.com/", Navigable: true},
			{Position: 1, Label: `<i>Review</i>`, RiskStatus: "review", Navigable: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "<script>") || strings.Contains(html, "<img src=x") || strings.Contains(html, "<b>Allowed</b>") {
		t.Fatalf("UGC rendered as active markup: %s", html)
	}
	if !strings.Contains(html, `rel="ugc nofollow"`) {
		t.Fatalf("required UGC rel missing: %s", html)
	}
	if strings.Contains(html, `href=""`) || strings.Contains(html, `Review</i></a>`) {
		t.Fatalf("review child unexpectedly navigable: %s", html)
	}
	if strings.Contains(strings.ToLower(html), `rel="canonical"`) || strings.Contains(strings.ToLower(html), "hreflang") {
		t.Fatalf("Bio public surface expanded canonical/hreflang surface: %s", html)
	}
}

func TestNormalizeChildrenUsesLinksDestinationAuthority(t *testing.T) {
	input := []ChildInput{{Position: 0, Label: "Site", DestinationURL: "HTTPS://Example.COM:443/a/../b?z=2&a=1#fragment"}}
	got, err := normalizeChildren(input, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DestinationURL != "https://example.com/b?a=1&z=2" {
		t.Fatalf("unexpected normalized destination: %#v", got)
	}
	expectedFingerprint, _, err := links.RiskFingerprint(got[0].DestinationURL, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].DestinationFingerprint != expectedFingerprint {
		t.Fatalf("fingerprint mismatch: got %s want %s", got[0].DestinationFingerprint, expectedFingerprint)
	}
	for _, unsafe := range []string{"javascript:alert(1)", "data:text/html,boom", "https://user:pass@example.com/"} {
		if _, err := normalizeChildren([]ChildInput{{Position: 0, Label: "Unsafe", DestinationURL: unsafe}}, true); err == nil {
			t.Fatalf("unsafe destination accepted: %s", unsafe)
		}
	}
}

func TestPublicJSONRedactsNonAllowedDestinations(t *testing.T) {
	page := Page{Links: []ChildLink{
		{ID: 1, Position: 0, Label: "Allowed", DestinationURL: "https://allowed.example/", RiskStatus: "allowed"},
		{ID: 2, Position: 1, Label: "Review", DestinationURL: "https://review.example/", RiskStatus: "review"},
		{ID: 3, Position: 2, Label: "Blocked", DestinationURL: "https://blocked.example/", RiskStatus: "blocked"},
	}}
	got := publicJSONLinks(page, true)
	if got[0].URL == "" {
		t.Fatal("allowed destination was not navigable")
	}
	if got[1].URL != "" || got[2].URL != "" {
		t.Fatalf("non-allowed destinations leaked as active URLs: %#v", got)
	}
	paused := publicJSONLinks(page, false)
	for _, item := range paused {
		if item.URL != "" {
			t.Fatalf("paused Bio leaked active URL: %#v", paused)
		}
	}
}

func TestRiskStateMappingIsFailClosed(t *testing.T) {
	cases := map[links.RiskState]string{
		links.RiskAllow:     "allowed",
		links.RiskReview:    "review",
		links.RiskBlock:     "blocked",
		links.RiskMissing:   "review",
		links.RiskMalformed: "review",
		links.RiskStale:     "review",
	}
	for state, expected := range cases {
		if got := mapRiskState(state); got != expected {
			t.Fatalf("%s mapped to %s, want %s", state, got, expected)
		}
	}
}
