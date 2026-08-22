package files

import (
	"errors"
	"testing"
)

func TestTypePolicyRequiresExtensionDeclaredAndMagicAgreement(t *testing.T) {
	policy, err := ParseTypePolicy("pdf=application/pdf;png=image/png")
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.7\n1 0 obj\n")
	name, declared, detected, err := policy.Validate("report.PDF", "application/pdf; charset=binary", pdf)
	if err != nil {
		t.Fatal(err)
	}
	if name != "report.PDF" || declared != "application/pdf" || detected != "application/pdf" {
		t.Fatalf("unexpected normalized result name=%q declared=%q detected=%q", name, declared, detected)
	}
	if _, _, _, err := policy.Validate("report.pdf", "image/png", pdf); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("declared mismatch must fail: %v", err)
	}
	if _, _, _, err := policy.Validate("report.png", "image/png", pdf); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("magic mismatch must fail: %v", err)
	}
	if _, _, _, err := policy.Validate("../report.pdf", "application/pdf", pdf); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("path-like original name must fail: %v", err)
	}
	if _, _, _, err := policy.Validate("payload.exe", "application/octet-stream", []byte("MZ")); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unconfigured extension must fail: %v", err)
	}
}

func TestTypePolicyRejectsAmbiguousConfiguration(t *testing.T) {
	for _, raw := range []string{"", "pdf", "pdf=", "pdf=application/pdf;pdf=application/pdf", "../pdf=application/pdf"} {
		if _, err := ParseTypePolicy(raw); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid config for %q, got %v", raw, err)
		}
	}
}
