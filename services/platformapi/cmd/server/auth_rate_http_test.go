package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func TestParseAuthRateWindow(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		raw  string
		want time.Duration
		ok   bool
	}{
		"one second": {raw: "1", want: time.Second, ok: true},
		"one day":    {raw: "86400", want: 24 * time.Hour, ok: true},
		"missing":    {raw: "", ok: false},
		"zero":       {raw: "0", ok: false},
		"too large":  {raw: "86401", ok: false},
		"overflow":   {raw: "9223372036854775807", ok: false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseAuthRateWindow(test.raw)
			if test.ok {
				if err != nil || got != test.want {
					t.Fatalf("parseAuthRateWindow(%q) = %v, %v; want %v, nil", test.raw, got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseAuthRateWindow(%q) unexpectedly succeeded", test.raw)
			}
		})
	}
}

func TestParseAuthRateLimit(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "0", "-1", "not-a-number"} {
		if _, err := parseAuthRateLimit(raw); err == nil {
			t.Fatalf("parseAuthRateLimit(%q) unexpectedly succeeded", raw)
		}
	}
	if got, err := parseAuthRateLimit(" 25 "); err != nil || got != 25 {
		t.Fatalf("parseAuthRateLimit valid = %d, %v; want 25, nil", got, err)
	}
}

func TestAuthRateRequestMapsFrozenSurfacesAndPreservesBody(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path     string
		body     string
		surface  authn.AuthRateSurface
		identity string
	}{
		{"/api/auth/register", `{"email":"Reg@example.test","password":"x"}`, authn.AuthRateRegister, "Reg@example.test"},
		{"/api/auth/login", `{"email":"Login@example.test","password":"x"}`, authn.AuthRateLogin, "Login@example.test"},
		{"/api/public/login-email-code", `{"email":"Code@example.test"}`, authn.AuthRateEmailCode, "Code@example.test"},
		{"/api/public/login-email-code", `{"code":"gcf_secret-value"}`, authn.AuthRateEmailCode, "gcf_secret-value"},
		{"/api/auth/forgotpassword", `{"email":"Recovery@example.test"}`, authn.AuthRateRecovery, "Recovery@example.test"},
	}
	for _, test := range cases {
		t.Run(test.path+test.identity, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			surface, identity, protected := authRateRequest(req)
			if !protected || surface != test.surface || identity != test.identity {
				t.Fatalf("authRateRequest = %q, %q, %v; want %q, %q, true", surface, identity, protected, test.surface, test.identity)
			}
			restored, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(restored) != test.body {
				t.Fatalf("body changed: got %q want %q", restored, test.body)
			}
		})
	}
}

func TestAuthRateRequestSkipsNonFrozenRoutes(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/verifyemail", strings.NewReader(`{"code":"gvc_x"}`))
	if _, _, protected := authRateRequest(req); protected {
		t.Fatal("verification route unexpectedly entered the four-surface rate middleware")
	}
}

func TestRetryAfterSecondsRoundsUpAndNeverReturnsZero(t *testing.T) {
	t.Parallel()
	cases := map[time.Duration]int64{
		0:                        1,
		500 * time.Millisecond:   1,
		time.Second:              1,
		time.Second + time.Nanosecond: 2,
	}
	for input, want := range cases {
		if got := retryAfterSeconds(input); got != want {
			t.Fatalf("retryAfterSeconds(%v) = %d; want %d", input, got, want)
		}
	}
}
