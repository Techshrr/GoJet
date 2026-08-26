package auth

import (
	"strings"
	"testing"
)

func TestOpaqueSecretHashesOnlyDerivedAuthority(t *testing.T) {
	secret, err := NewOpaqueSecret("gst_", 32)
	if err != nil {
		t.Fatalf("NewOpaqueSecret: %v", err)
	}
	if !strings.HasPrefix(secret.Value, "gst_") || len(secret.Value) < 40 {
		t.Fatalf("unexpected opaque secret shape: %q", secret.Value)
	}
	if !EqualOpaqueHash(secret.Hash, HashOpaque(secret.Value)) {
		t.Fatal("returned hash does not match opaque value")
	}
	other, err := NewOpaqueSecret("gst_", 32)
	if err != nil {
		t.Fatalf("NewOpaqueSecret other: %v", err)
	}
	if other.Value == secret.Value || EqualOpaqueHash(other.Hash, secret.Hash) {
		t.Fatal("independent opaque secrets must not collide")
	}
}

func TestOpaqueSecretRejectsWeakOrUnsafeShape(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		bytes  int
	}{
		{prefix: "x", bytes: 8},
		{prefix: "bad prefix", bytes: 32},
		{prefix: strings.Repeat("x", 25), bytes: 32},
		{prefix: "x", bytes: 65},
	} {
		if _, err := NewOpaqueSecret(tc.prefix, tc.bytes); err != ErrInvalid {
			t.Fatalf("NewOpaqueSecret(%q,%d) error=%v, want ErrInvalid", tc.prefix, tc.bytes, err)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	got, err := NormalizeEmail("  Ethan.User@Example.COM  ")
	if err != nil {
		t.Fatalf("NormalizeEmail: %v", err)
	}
	if got != "ethan.user@example.com" {
		t.Fatalf("normalized=%q", got)
	}
	for _, raw := range []string{"", "a@@example.com", "a @example.com", "@example.com", "a@.example.com", "a@example..com", "a@example.com."} {
		if _, err := NormalizeEmail(raw); err != ErrInvalid {
			t.Fatalf("NormalizeEmail(%q) error=%v, want ErrInvalid", raw, err)
		}
	}
}

func TestProviderInventoryIsFrozen(t *testing.T) {
	want := []string{"google", "facebook", "github", "qq", "wechat", "rainbow"}
	if len(Providers) != len(want) {
		t.Fatalf("providers=%v", Providers)
	}
	for i, provider := range want {
		if Providers[i] != provider || !ValidProvider(provider) {
			t.Fatalf("provider inventory drift at %d: got %q want %q", i, Providers[i], provider)
		}
	}
	for _, provider := range []string{"", "Google", "twitter", "apple", "unknown"} {
		if ValidProvider(provider) {
			t.Fatalf("unexpected provider accepted: %q", provider)
		}
	}
}
