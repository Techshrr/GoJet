package links

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicSurfaceTokenProjectionMatchesCanonicalGeneratedCSS(t *testing.T) {
	root := repositoryRoot(t)
	canonicalBytes, err := os.ReadFile(filepath.Join(root, "frontend", "packages", "tokens", "generated", "tokens.css"))
	if err != nil {
		t.Fatalf("read canonical generated tokens: %v", err)
	}
	canonical := string(canonicalBytes)
	checked := 0
	for _, raw := range strings.Split(publicSurfaceTokenDefinitions, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "--gojet-") {
			continue
		}
		if !strings.Contains(canonical, line) {
			t.Fatalf("public surface token projection drifted from canonical generated CSS: %q", line)
		}
		checked++
	}
	if checked < 35 {
		t.Fatalf("public surface token projection unexpectedly small: %d", checked)
	}
}

func TestSafetySurfaceUsesGovernedStateSemantics(t *testing.T) {
	var rendered bytes.Buffer
	if err := safetyTemplate.Execute(&rendered, safetyView{
		Title:   "Link under review",
		Message: "This link is not available while its destination is being reviewed.",
		Code:    "safe-reference",
		Reason:  "review",
	}); err != nil {
		t.Fatalf("render safety surface: %v", err)
	}
	html := rendered.String()
	for _, required := range []string{
		`data-gojet-public-surface="safety"`,
		`data-safety-state="review"`,
		`data-tone="warning"`,
		`aria-labelledby="gojet-public-title"`,
		`<svg class="gj-public-icon"`,
		`<h1 id="gojet-public-title">Link under review</h1>`,
		`<strong>Next step:</strong>`,
		`Reference: <code>safe-reference</code>`,
		`@media (prefers-color-scheme: dark)`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("safety surface missing governed semantic %q", required)
		}
	}
	if strings.Contains(html, "href=") {
		t.Fatal("safety surface must not expose bypass links")
	}
	if strings.Contains(html, "https://unsafe.example") {
		t.Fatal("safety surface leaked a destination")
	}
}

func TestPasswordSurfaceIsBrandedAccessibleAndDestinationFree(t *testing.T) {
	var rendered bytes.Buffer
	if err := passwordTemplate.Execute(&rendered, passwordView{Code: "protected-reference", Message: "The password was not accepted."}); err != nil {
		t.Fatalf("render password surface: %v", err)
	}
	html := rendered.String()
	for _, required := range []string{
		`data-gojet-public-surface="password"`,
		`<span class="gj-public-wordmark">GoJet</span>`,
		`aria-labelledby="gojet-public-title"`,
		`<svg class="gj-public-icon"`,
		`<h1 id="gojet-public-title">Password required</h1>`,
		`class="gj-public-alert" role="alert"`,
		`<label for="gojet-link-password">Password</label>`,
		`autocomplete="current-password"`,
		`<button type="submit">Continue</button>`,
		`Reference: <code>protected-reference</code>`,
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("password surface missing governed semantic %q", required)
		}
	}
	if strings.Contains(html, "href=") {
		t.Fatal("password challenge must not expose links")
	}
	if strings.Contains(html, "https://destination.example") {
		t.Fatal("password challenge leaked a destination")
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %q", dir)
		}
		dir = parent
	}
}
