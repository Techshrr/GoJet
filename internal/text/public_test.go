package textshares

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicTemplateEscapesUserText(t *testing.T) {
	var output strings.Builder
	err := publicTextTemplate.Execute(&output, publicPageData{
		State: "available", Headline: "Text available", Title: `<script>alert("title")</script>`,
		Content: `<img src=x onerror=alert(1)>`, ShowContent: true, AbuseURL: "/abuse/report",
	})
	if err != nil {
		t.Fatal(err)
	}
	html := output.String()
	if strings.Contains(html, "<script>") || strings.Contains(html, "<img src=x") {
		t.Fatalf("user text rendered as active markup: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "&lt;img src=x") {
		t.Fatalf("escaped user text missing: %s", html)
	}
}

func TestPublicAuthCookieBindsSlugAndPasswordVerifier(t *testing.T) {
	a := &API{publicAuthKey: []byte("0123456789abcdef0123456789abcdef")}
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	current := storedResource{Resource: Resource{PublicSlug: "abcdefghijklmnopqrstuvwx"}, PasswordHash: "verifier-a"}
	request := httptest.NewRequest("GET", "https://gojet.test/t/abcdefghijklmnopqrstuvwx", nil)
	recorder := httptest.NewRecorder()
	a.setPublicAuthCookie(recorder, request, current, now)
	response := recorder.Result()
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one auth cookie, got %d", len(cookies))
	}
	check := httptest.NewRequest("GET", "https://gojet.test/t/abcdefghijklmnopqrstuvwx", nil)
	check.AddCookie(cookies[0])
	if !a.requestHasPublicAuth(check, current, now.Add(time.Minute)) {
		t.Fatal("expected valid public auth cookie")
	}
	changed := current
	changed.PasswordHash = "verifier-b"
	if a.requestHasPublicAuth(check, changed, now.Add(time.Minute)) {
		t.Fatal("password change must invalidate public auth cookie")
	}
}

func TestNormalizeTextInputs(t *testing.T) {
	if _, err := normalizeTitle("   "); err == nil {
		t.Fatal("blank title accepted")
	}
	if got, err := normalizeTitle("  Example  "); err != nil || got != "Example" {
		t.Fatalf("title normalization failed: %q %v", got, err)
	}
	if _, err := normalizeContent("\n\t "); err == nil {
		t.Fatal("blank content accepted")
	}
	if got, err := normalizeVisibility(""); err != nil || got != "private" {
		t.Fatalf("default visibility failed: %q %v", got, err)
	}
}
