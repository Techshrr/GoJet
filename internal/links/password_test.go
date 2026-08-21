package links

import (
	"strings"
	"testing"
)

func TestLinkPasswordHashAndVerify(t *testing.T) {
	password := "correct horse battery staple"
	first, err := HashLinkPassword(password)
	if err != nil {
		t.Fatalf("HashLinkPassword: %v", err)
	}
	second, err := HashLinkPassword(password)
	if err != nil {
		t.Fatalf("HashLinkPassword second: %v", err)
	}
	if first == second {
		t.Fatal("independent hashes reused a salt")
	}
	if strings.Contains(first, password) {
		t.Fatal("encoded verifier contains plaintext password")
	}
	if !strings.HasPrefix(first, "pbkdf2-sha256$1$600000$") {
		t.Fatalf("unexpected verifier format: %q", first)
	}
	if !VerifyLinkPassword(first, password) {
		t.Fatal("correct password did not verify")
	}
	if VerifyLinkPassword(first, "incorrect password") {
		t.Fatal("incorrect password verified")
	}
}

func TestLinkPasswordValidationAndMalformedVerifierFailClosed(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("x", linkPasswordMaxBytes+1), string([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8})} {
		if _, err := HashLinkPassword(password); err == nil {
			t.Fatalf("invalid password accepted: %q", password)
		}
	}
	for _, encoded := range []string{"", "pbkdf2-sha256$1$1$bad$bad", "pbkdf2-sha256$2$600000$bad$bad", "unknown$1$600000$bad$bad"} {
		if VerifyLinkPassword(encoded, "long-enough-password") {
			t.Fatalf("malformed verifier accepted: %q", encoded)
		}
	}
}
