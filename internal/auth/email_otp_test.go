package auth

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Techshrr/GoJet/internal/securetoken"
)

func TestEmailCodeFormatAndSeparation(t *testing.T) {
	key, _ := securetoken.NewKey("test", []byte(strings.Repeat("a", 32)))
	other, _ := securetoken.NewKey("other", []byte(strings.Repeat("b", 32)))
	leadingZero := false
	for i := 0; i < 200; i++ {
		id := fmt.Sprintf("grant-%d", i)
		code, err := NumericEmailCode(key, "email_verification", id)
		if err != nil || !IsNumericEmailCode(code) {
			t.Fatalf("invalid code format: %v", err)
		}
		again, _ := NumericEmailCode(key, "email_verification", id)
		if code != again {
			t.Fatal("mail retry must reproduce the same code")
		}
		leadingZero = leadingZero || code[0] == '0'
	}
	if !leadingZero {
		t.Fatal("leading zero must be preserved")
	}
	a, _ := NumericEmailCode(key, "email_verification", "grant-1")
	b, _ := NumericEmailCode(key, "password_reset", "grant-1")
	c, _ := NumericEmailCode(other, "email_verification", "grant-1")
	if a == b || a == c {
		t.Fatal("fixed test vectors must differ across key and purpose")
	}
	if _, err := NumericEmailCode(securetoken.Key{}, "email_verification", "grant-1"); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestEmailCodeOnlyAcceptsSixASCIIDigits(t *testing.T) {
	for _, code := range []string{"000001", "123456", "999999"} {
		if !IsNumericEmailCode(code) {
			t.Fatal("valid code rejected")
		}
	}
	for _, code := range []string{"", "12345", "1234567", "12a456", "１２３４５６", "123 45", "123456\n", "gvc_123456"} {
		if IsNumericEmailCode(code) {
			t.Fatal("invalid code accepted")
		}
	}
}
