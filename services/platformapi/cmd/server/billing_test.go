package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Techshrr/GoJet/internal/billing"
)

func TestDeterministicBillingCallbackVerifier(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	raw := `{"event_id":"evt_1","transaction_id":"txn_1","order_id":"ord_1","event_type":"payment.paid","outcome":"paid","currency":"USD","amount_minor":100,"received_at":"2026-08-24T04:00:00Z","correlation_id":"corr_1"}`
	signer := billing.DeterministicTestVerifier{Secrets: map[billing.Provider][]byte{billing.ProviderStripe: secret}}
	sig, err := signer.Sign(billing.ProviderStripe, "evt_1", "txn_1", []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	verifier := deterministicBillingCallbackVerifier{verifier: signer}
	r := httptest.NewRequest("POST", "/api/payments/callbacks/stripe", strings.NewReader(raw))
	r.Header.Set("X-GoJet-Test-Callback-Signature", sig)
	cmd, err := verifier.VerifyAndNormalize(r, billing.ProviderStripe)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.ProviderEventID != "evt_1" || cmd.ProviderTransactionID != "txn_1" || cmd.OrderID != "ord_1" || cmd.Outcome != billing.TransactionPaid || cmd.Money.AmountMinor != 100 {
		t.Fatalf("cmd=%+v", cmd)
	}
}

func TestDeterministicBillingCallbackVerifierRejectsTamper(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	original := `{"event_id":"evt_1","transaction_id":"txn_1","order_id":"ord_1","event_type":"payment.paid","outcome":"paid","currency":"USD","amount_minor":100,"received_at":"2026-08-24T04:00:00Z","correlation_id":"corr_1"}`
	tampered := strings.Replace(original, `"amount_minor":100`, `"amount_minor":999`, 1)
	signer := billing.DeterministicTestVerifier{Secrets: map[billing.Provider][]byte{billing.ProviderStripe: secret}}
	sig, err := signer.Sign(billing.ProviderStripe, "evt_1", "txn_1", []byte(original))
	if err != nil {
		t.Fatal(err)
	}
	verifier := deterministicBillingCallbackVerifier{verifier: signer}
	r := httptest.NewRequest("POST", "/api/payments/callbacks/stripe", strings.NewReader(tampered))
	r.Header.Set("X-GoJet-Test-Callback-Signature", sig)
	if _, err := verifier.VerifyAndNormalize(r, billing.ProviderStripe); err == nil {
		t.Fatal("tampered callback accepted")
	}
}

func TestBillingPrincipalResolverIsExplicitlyTestOnly(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/workspaces/ws/orders/ord", nil)
	r.Header.Set("X-GoJet-Test-Actor", "actor")
	r.Header.Set("X-GoJet-Test-Email", "actor@example.test")
	if _, err := (billingPrincipalResolver{testAuth: false}).ResolvePrincipal(r); err != billing.ErrAuthenticationUnavailable {
		t.Fatalf("err=%v", err)
	}
	principal, err := (billingPrincipalResolver{testAuth: true}).ResolvePrincipal(r)
	if err != nil || principal.UserID != "actor" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
}
