package links

import (
	"strings"
	"testing"
)

func TestPasswordVerifierDoesNotContainPlaintext(t *testing.T) {
	const password = "P05-Closure-Password-2026!"

	verifier, err := HashLinkPassword(password)
	if err != nil {
		t.Fatalf("HashLinkPassword() error = %v", err)
	}
	if strings.Contains(verifier, password) {
		t.Fatal("password verifier contains plaintext password")
	}
	if !VerifyLinkPassword(verifier, password) {
		t.Fatal("password verifier does not accept the original password")
	}
	if VerifyLinkPassword(verifier, password+"-wrong") {
		t.Fatal("password verifier accepted a different password")
	}
}
