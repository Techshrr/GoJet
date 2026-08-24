package support

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSubmissionIdempotencyHashIsDeterministicAndScoped(t *testing.T) {
	a, err := SubmissionIdempotencyHash(SubmissionTicketCreate, "ws-1:user-1", "idem-123")
	if err != nil {
		t.Fatal(err)
	}
	b, err := SubmissionIdempotencyHash(SubmissionTicketCreate, "ws-1:user-1", "idem-123")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("same logical submission produced different digest")
	}
	otherSurface, _ := SubmissionIdempotencyHash(SubmissionTicketReply, "ws-1:user-1", "idem-123")
	otherScope, _ := SubmissionIdempotencyHash(SubmissionTicketCreate, "ws-2:user-1", "idem-123")
	if a == otherSurface || a == otherScope {
		t.Fatal("idempotency digest was not scoped")
	}
	if strings.Contains(string(a[:]), "idem-123") {
		t.Fatal("raw idempotency key appeared in digest bytes")
	}
}

func TestSubmissionRateIdentityDoesNotExposeRawAddress(t *testing.T) {
	withPort := submissionRateIdentity("203.0.113.25:443")
	withoutPort := submissionRateIdentity("203.0.113.25")
	if withPort != withoutPort {
		t.Fatalf("same client address hashed differently: %q != %q", withPort, withoutPort)
	}
	key := submissionRateKey(SubmissionPublicContact, "203.0.113.25:443")
	if strings.Contains(key, "203.0.113.25") {
		t.Fatalf("raw client address leaked into Redis key: %q", key)
	}
	if !strings.HasPrefix(key, "support:rate:public-contact:") {
		t.Fatalf("unexpected Redis namespace: %q", key)
	}
}

func TestRedisSubmissionGuardRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewRedisSubmissionGuard(nil, 10, time.Minute, time.Minute); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil client error=%v", err)
	}
}

func TestTurnstileHTTPVerifierReducesProviderResponseToSuccessBit(t *testing.T) {
	const rawToken = "turnstile-sensitive-token"
	const secret = "turnstile-secret"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method=%s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Errorf("content-type=%q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("secret") != secret || r.Form.Get("response") != rawToken {
			t.Errorf("provider request fields were not exact")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	verifier, err := newTurnstileHTTPVerifier(secret, server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	result, err := verifier.Verify(context.Background(), rawToken)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success {
		t.Fatal("successful provider verification was rejected")
	}
}

func TestTurnstileHTTPVerifierFailsClosedOnProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("provider diagnostic must not escape"))
	}))
	defer server.Close()

	verifier, err := newTurnstileHTTPVerifier("secret", server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifier.Verify(context.Background(), "token"); !errors.Is(err, ErrTurnstileRejected) {
		t.Fatalf("provider failure error=%v", err)
	}
}

func TestVerifyProtectedSubmissionRejectsFailedProviderBeforeReplayClaim(t *testing.T) {
	verifier := &recordingVerifier{ok: false}
	replay := &recordingReplayStore{claim: true}
	if err := VerifyProtectedSubmission(context.Background(), "raw-token", verifier, replay); !errors.Is(err, ErrTurnstileRejected) {
		t.Fatalf("verification error=%v", err)
	}
	if replay.digest != ([32]byte{}) {
		t.Fatal("replay store was mutated after failed provider verification")
	}
}
