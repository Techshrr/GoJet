package textshares

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
)

func TestPublicStylesheetMatchesStrictCSP(t *testing.T) {
	var output strings.Builder
	if err := publicTextTemplate.Execute(&output, publicPageData{State: "available", Headline: "Text available"}); err != nil {
		t.Fatal(err)
	}
	html := output.String()
	_, after, ok := strings.Cut(html, "<style>")
	if !ok {
		t.Fatal("public stylesheet absent")
	}
	css, _, ok := strings.Cut(after, "</style>")
	if !ok || !strings.Contains(css, `:root[data-theme="dark"]`) || !strings.Contains(css, "--gojet-surface-canvas") {
		t.Fatal("canonical theme styles absent")
	}
	digest := sha256.Sum256([]byte(css))
	if !strings.Contains(publicTextCSP, "'sha256-"+base64.StdEncoding.EncodeToString(digest[:])+"'") {
		t.Fatal("browser CSP would reject rendered stylesheet")
	}
	if strings.Contains(publicTextCSP, "unsafe-inline") || strings.Contains(publicTextCSP, "unsafe-eval") {
		t.Fatal("public CSP was weakened")
	}
}
