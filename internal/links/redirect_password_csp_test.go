package links

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordChallengeCSPAllowsHTTPRedirectChainsWithoutDestinationDisclosure(t *testing.T) {
	recorder := httptest.NewRecorder()
	handler := &RedirectHandler{}
	handler.writePasswordChallenge(recorder, http.StatusOK, "protected-link", "")

	response := recorder.Result()
	defer response.Body.Close()

	csp := response.Header.Get("Content-Security-Policy")
	for _, required := range []string{
		"default-src 'none'",
		"form-action 'self' http: https:",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, required) {
			t.Fatalf("password challenge CSP missing %q: %q", required, csp)
		}
	}

	// Link destinations are deliberately absent from the challenge policy.
	// Scheme sources are sufficient for Chrome redirect-chain compatibility
	// and avoid disclosing a protected link's concrete target origin.
	if strings.Contains(csp, "http://") || strings.Contains(csp, "https://") {
		t.Fatalf("password challenge CSP leaks a concrete destination origin: %q", csp)
	}
	if strings.Contains(csp, "javascript:") || strings.Contains(csp, "data:") {
		t.Fatalf("password challenge CSP permits a non-HTTP(S) form scheme: %q", csp)
	}
}
