package qrcodes

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestRenderDeterministicPNGAndSVG(t *testing.T) {
	target := "https://gojet.cc/example"
	for _, format := range []string{"png", "svg"} {
		first, err := Render(target, format)
		if err != nil {
			t.Fatalf("Render(%s): %v", format, err)
		}
		second, err := Render(target, format)
		if err != nil {
			t.Fatalf("Render second(%s): %v", format, err)
		}
		if !bytes.Equal(first.Bytes, second.Bytes) || first.SHA256 != second.SHA256 {
			t.Fatalf("%s output is not deterministic", format)
		}
		if len(first.SHA256) != 64 {
			t.Fatalf("%s digest length = %d", format, len(first.SHA256))
		}
		if _, err := hex.DecodeString(first.SHA256); err != nil {
			t.Fatalf("%s digest is not hex: %v", format, err)
		}
	}

	pngArtifact, err := Render(target, "png")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pngArtifact.Bytes, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		t.Fatal("PNG signature missing")
	}

	svgArtifact, err := Render(target, "svg")
	if err != nil {
		t.Fatal(err)
	}
	svg := string(svgArtifact.Bytes)
	for _, forbidden := range []string{"<script", "javascript:", "xlink:href=", " href="} {
		if strings.Contains(strings.ToLower(svg), forbidden) {
			t.Fatalf("SVG contains active/external content token %q", forbidden)
		}
	}
	if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "<path") {
		t.Fatal("SVG does not contain expected vector QR structure")
	}
}

func TestRenderRejectsUnsupportedFormat(t *testing.T) {
	if _, err := Render("https://gojet.cc/example", "pdf"); err != ErrUnsupportedFormat {
		t.Fatalf("expected ErrUnsupportedFormat, got %v", err)
	}
}
