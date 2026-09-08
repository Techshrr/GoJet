package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

type fakeStripeIntentResolver struct {
	calls  int
	ref    string
	now    time.Time
	intent billing.ProviderIntent
	err    error
}

func (f *fakeStripeIntentResolver) ResolveProviderIntentByProviderReference(_ context.Context, provider billing.Provider, ref string, now time.Time) (billing.ProviderIntent, error) {
	f.calls++
	f.ref = ref
	f.now = now
	if provider != billing.ProviderStripe {
		return billing.ProviderIntent{}, billing.ErrInvalidInput
	}
	return f.intent, f.err
}

func stripeWebhookBody(eventType string, livemode bool, amount int64, currency string) []byte {
	return []byte(fmt.Sprintf(`{"id":"evt_gojet_123","object":"event","type":%q,"livemode":%t,"created":1788883200,"data":{"object":{"id":"pi_gojet_123","object":"payment_intent","amount":%d,"currency":%q,"status":"succeeded"}}}`, eventType, livemode, amount, currency))
}

func stripeSignatureForTest(secret string, timestamp int64, raw []byte) string {
	stamp := fmt.Sprintf("%d", timestamp)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(raw)
	return "t=" + stamp + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func stripeRequestForTest(secret string, timestamp int64, raw []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/stripe", bytes.NewReader(raw))
	r.Header.Set("Stripe-Signature", stripeSignatureForTest(secret, timestamp, raw))
	return r
}

func stripeIntentForTest() billing.ProviderIntent {
	return billing.ProviderIntent{
		ID:                    "pint_gojet_123",
		WorkspaceID:           "ws_gojet_123",
		OrderID:               "ord_gojet_123",
		Provider:              billing.ProviderStripe,
		MerchantReference:     "gj_gojet_123",
		ProviderReference:     "pi_gojet_123",
		SettlementAssetKind:   billing.SettlementAssetFiat,
		SettlementAsset:       "USD",
		SettlementAmountUnits: 1234,
		Status:                billing.ProviderIntentActive,
	}
}

func TestStripeCallbackAuthenticatesBindsAndNormalizesPaid(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
	verifier := stripeCallbackVerifier{
		webhookSecret: secret,
		livemode:      false,
		store:         store,
		now:           func() time.Time { return fixedNow },
	}
	raw := stripeWebhookBody("payment_intent.succeeded", false, 1234, "usd")
	cmd, err := verifier.VerifyAndNormalize(stripeRequestForTest(secret, fixedNow.Unix(), raw), billing.ProviderStripe)
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.ref != "pi_gojet_123" || !store.now.Equal(fixedNow) {
		t.Fatalf("store calls=%d ref=%q now=%s", store.calls, store.ref, store.now)
	}
	if cmd.Provider != billing.ProviderStripe || cmd.ProviderEventID != "evt_gojet_123" || cmd.ProviderTransactionID != "pi_gojet_123" || cmd.OrderID != "ord_gojet_123" {
		t.Fatalf("identity cmd=%+v", cmd)
	}
	if cmd.EventType != "payment_intent.succeeded" || cmd.Outcome != billing.TransactionPaid || cmd.Money != (billing.Money{Currency: "USD", AmountMinor: 1234}) || !cmd.ReceivedAt.Equal(fixedNow) {
		t.Fatalf("normalized cmd=%+v", cmd)
	}
	if !strings.HasPrefix(cmd.CorrelationID, "stripe:") || len(cmd.CorrelationID) != 71 {
		t.Fatalf("correlation=%q", cmd.CorrelationID)
	}
	ack := verifier.SuccessAcknowledgement(billing.ProviderStripe)
	if ack.StatusCode != http.StatusOK || ack.ContentType != "" || ack.Body != "" {
		t.Fatalf("ack=%+v", ack)
	}
}

func TestStripePaymentFailedNormalizesFailed(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
	verifier := stripeCallbackVerifier{webhookSecret: secret, store: store, now: func() time.Time { return fixedNow }}
	raw := stripeWebhookBody("payment_intent.payment_failed", false, 1234, "usd")
	cmd, err := verifier.VerifyAndNormalize(stripeRequestForTest(secret, fixedNow.Unix(), raw), billing.ProviderStripe)
	if err != nil || cmd.Outcome != billing.TransactionFailed {
		t.Fatalf("cmd=%+v err=%v", cmd, err)
	}
}

func TestStripeAuthenticatedIgnoredEventDoesNotConsultBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
	verifier := stripeCallbackVerifier{webhookSecret: secret, store: store, now: func() time.Time { return fixedNow }}
	raw := stripeWebhookBody("payment_intent.processing", false, 1234, "usd")
	_, err := verifier.VerifyAndNormalize(stripeRequestForTest(secret, fixedNow.Unix(), raw), billing.ProviderStripe)
	if !errors.Is(err, billing.ErrCallbackIgnored) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestStripeRejectsTamperedBodyBeforeBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
	verifier := stripeCallbackVerifier{webhookSecret: secret, store: store, now: func() time.Time { return fixedNow }}
	original := stripeWebhookBody("payment_intent.succeeded", false, 1234, "usd")
	r := stripeRequestForTest(secret, fixedNow.Unix(), original)
	r.Body = io.NopCloser(bytes.NewReader(bytes.ReplaceAll(original, []byte("1234"), []byte("9999"))))
	_, err := verifier.VerifyAndNormalize(r, billing.ProviderStripe)
	if !errors.Is(err, billing.ErrCallbackUnauthorized) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestStripeRejectsStaleFutureAndWrongLivemodeBeforeBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	for _, tc := range []struct {
		name             string
		timestamp        int64
		bodyLivemode     bool
		verifierLivemode bool
	}{
		{name: "stale", timestamp: fixedNow.Add(-301 * time.Second).Unix()},
		{name: "future", timestamp: fixedNow.Add(301 * time.Second).Unix()},
		{name: "livemode", timestamp: fixedNow.Unix(), bodyLivemode: true, verifierLivemode: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
			verifier := stripeCallbackVerifier{webhookSecret: secret, livemode: tc.verifierLivemode, store: store, now: func() time.Time { return fixedNow }}
			raw := stripeWebhookBody("payment_intent.succeeded", tc.bodyLivemode, 1234, "usd")
			_, err := verifier.VerifyAndNormalize(stripeRequestForTest(secret, tc.timestamp, raw), billing.ProviderStripe)
			if !errors.Is(err, billing.ErrCallbackUnauthorized) || store.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, store.calls)
			}
		})
	}
}

func TestStripeBindingAmountCurrencyAndLifecycleFailuresFailClosed(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	secret := "whsec_gojet_test_secret"
	cases := []struct {
		name   string
		intent billing.ProviderIntent
		err    error
		want   error
	}{
		{name: "amount", intent: func() billing.ProviderIntent { v := stripeIntentForTest(); v.SettlementAmountUnits = 9999; return v }(), want: billing.ErrCallbackUnauthorized},
		{name: "currency", intent: func() billing.ProviderIntent { v := stripeIntentForTest(); v.SettlementAsset = "EUR"; return v }(), want: billing.ErrCallbackUnauthorized},
		{name: "missing", err: billing.ErrNotFound, want: billing.ErrCallbackUnauthorized},
		{name: "expired", err: billing.ErrConflict, want: billing.ErrCallbackUnauthorized},
		{name: "db", err: errors.New("database unavailable"), want: billing.ErrCallbackUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStripeIntentResolver{intent: tc.intent, err: tc.err}
			verifier := stripeCallbackVerifier{webhookSecret: secret, store: store, now: func() time.Time { return fixedNow }}
			raw := stripeWebhookBody("payment_intent.succeeded", false, 1234, "usd")
			_, err := verifier.VerifyAndNormalize(stripeRequestForTest(secret, fixedNow.Unix(), raw), billing.ProviderStripe)
			if !errors.Is(err, tc.want) || store.calls != 1 {
				t.Fatalf("err=%v want=%v calls=%d", err, tc.want, store.calls)
			}
		})
	}
}

func TestStripeBodyLimitFailsBeforeBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 8, 16, 0, 0, 0, time.UTC)
	store := &fakeStripeIntentResolver{intent: stripeIntentForTest()}
	verifier := stripeCallbackVerifier{webhookSecret: "whsec_gojet_test_secret", store: store, now: func() time.Time { return fixedNow }}
	raw := bytes.Repeat([]byte("x"), maxProductionCallbackBodyBytes+1)
	r := stripeRequestForTest("whsec_gojet_test_secret", fixedNow.Unix(), raw)
	_, err := verifier.VerifyAndNormalize(r, billing.ProviderStripe)
	if !errors.Is(err, billing.ErrCallbackUnauthorized) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestBuildProductionStripeCallbackVerifierFailsClosedOnIncompleteEnabledConfig(t *testing.T) {
	t.Setenv("GOJET_BILLING_STRIPE_ENABLED", "1")
	t.Setenv("GOJET_BILLING_STRIPE_SECRET_KEY", "sk_test_gojet")
	t.Setenv("GOJET_BILLING_STRIPE_WEBHOOK_SECRET", "")
	t.Setenv("GOJET_BILLING_STRIPE_LIVEMODE", "0")
	if _, _, err := buildProductionStripeCallbackVerifier(&fakeStripeIntentResolver{}); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildProductionStripeCallbackVerifierAcceptsCompleteConfig(t *testing.T) {
	t.Setenv("GOJET_BILLING_STRIPE_ENABLED", "1")
	t.Setenv("GOJET_BILLING_STRIPE_SECRET_KEY", "sk_test_gojet")
	t.Setenv("GOJET_BILLING_STRIPE_WEBHOOK_SECRET", "whsec_gojet_test_secret")
	t.Setenv("GOJET_BILLING_STRIPE_LIVEMODE", "0")
	verifier, enabled, err := buildProductionStripeCallbackVerifier(&fakeStripeIntentResolver{})
	if err != nil || !enabled || verifier.webhookSecret == "" || verifier.livemode {
		t.Fatalf("enabled=%v verifier=%+v err=%v", enabled, verifier, err)
	}
}

func TestProductionCallbackDispatcherFailsClosedForDisabledKnownProvider(t *testing.T) {
	dispatcher := productionBillingCallbackDispatcher{adapters: map[billing.Provider]billing.CallbackRequestVerifier{}}
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/paypal", strings.NewReader("{}"))
	_, err := dispatcher.VerifyAndNormalize(r, billing.ProviderPayPal)
	if !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
