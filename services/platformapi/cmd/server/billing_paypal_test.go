package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

type fakePayPalIntentResolver struct {
	calls  int
	ref    string
	now    time.Time
	intent billing.ProviderIntent
	err    error
}

func (f *fakePayPalIntentResolver) ResolveProviderIntentByProviderReference(_ context.Context, provider billing.Provider, ref string, now time.Time) (billing.ProviderIntent, error) {
	f.calls++
	f.ref = ref
	f.now = now
	if provider != billing.ProviderPayPal {
		return billing.ProviderIntent{}, billing.ErrInvalidInput
	}
	return f.intent, f.err
}

type fakePayPalHTTPClient struct {
	tokenStatus        int
	verificationStatus int
	verificationResult string
	tokenErr           error
	verificationErr    error
	tokenCalls         int
	verificationCalls  int
	tokenURL           string
	verifyURL          string
	basicUser          string
	basicPassword      string
	tokenBody          string
	verifyAuth         string
	verifyBody         []byte
}

func (f *fakePayPalHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	switch req.URL.Path {
	case "/v1/oauth2/token":
		f.tokenCalls++
		f.tokenURL = req.URL.String()
		f.basicUser, f.basicPassword, _ = req.BasicAuth()
		f.tokenBody = string(body)
		if f.tokenErr != nil {
			return nil, f.tokenErr
		}
		status := f.tokenStatus
		if status == 0 {
			status = http.StatusOK
		}
		return paypalTestResponse(status, `{"access_token":"ci-access-token","token_type":"Bearer"}`), nil
	case "/v1/notifications/verify-webhook-signature":
		f.verificationCalls++
		f.verifyURL = req.URL.String()
		f.verifyAuth = req.Header.Get("Authorization")
		f.verifyBody = append([]byte(nil), body...)
		if f.verificationErr != nil {
			return nil, f.verificationErr
		}
		status := f.verificationStatus
		if status == 0 {
			status = http.StatusOK
		}
		result := f.verificationResult
		if result == "" {
			result = "SUCCESS"
		}
		return paypalTestResponse(status, `{"verification_status":"`+result+`"}`), nil
	default:
		return nil, errors.New("unexpected PayPal test URL")
	}
}

func paypalTestResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func paypalIntentForTest() billing.ProviderIntent {
	return billing.ProviderIntent{
		ID:                    "pint_paypal_123",
		WorkspaceID:           "ws_paypal_123",
		OrderID:               "ord_paypal_123",
		Provider:              billing.ProviderPayPal,
		MerchantReference:     "gj_paypal_123",
		ProviderReference:     "5O190127TN364715T",
		SettlementAssetKind:   billing.SettlementAssetFiat,
		SettlementAsset:       "USD",
		SettlementAmountUnits: 1234,
		Status:                billing.ProviderIntentActive,
	}
}

func paypalEventForTest(t *testing.T, eventType, status, currency, value string, eventTime time.Time) []byte {
	t.Helper()
	return []byte(`{"id":"WH-2WR32451HC0233532-67976317FL4543714","event_type":"` + eventType + `","create_time":"` + eventTime.UTC().Format(time.RFC3339Nano) + `","resource":{"id":"2GG279541U471931P","status":"` + status + `","amount":{"currency_code":"` + currency + `","value":"` + value + `"},"supplementary_data":{"related_ids":{"order_id":"5O190127TN364715T"}}}}`)
}

func paypalRequestForTest(raw []byte) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/paypal", bytes.NewReader(raw))
	r.Header.Set("PayPal-Auth-Algo", "SHA256withRSA")
	r.Header.Set("PayPal-Cert-Url", "https://api.paypal.com/v1/notifications/certs/CERT-123")
	r.Header.Set("PayPal-Transmission-Id", "69cd13f0-d67a-11e5-baa3-778b53f4ae55")
	r.Header.Set("PayPal-Transmission-Sig", "test-signature")
	r.Header.Set("PayPal-Transmission-Time", "2026-09-09T13:00:00Z")
	return r
}

func paypalVerifierForTest(store paypalProviderIntentResolver, client paypalHTTPDoer, fixedNow time.Time) paypalCallbackVerifier {
	return paypalCallbackVerifier{
		apiBase:      "https://api-m.sandbox.paypal.com",
		clientID:     "client-id",
		clientSecret: "client-secret",
		webhookID:    "WH-WEBHOOK-123",
		client:       client,
		store:        store,
		now:          func() time.Time { return fixedNow },
	}
}

func TestPayPalCallbackPostbackAuthenticatesBindsAndNormalizesPaid(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	eventTime := fixedNow.Add(-time.Minute)
	store := &fakePayPalIntentResolver{intent: paypalIntentForTest()}
	client := &fakePayPalHTTPClient{}
	verifier := paypalVerifierForTest(store, client, fixedNow)
	raw := paypalEventForTest(t, "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "USD", "12.34", eventTime)

	cmd, err := verifier.VerifyAndNormalize(paypalRequestForTest(raw), billing.ProviderPayPal)
	if err != nil {
		t.Fatal(err)
	}
	if client.tokenCalls != 1 || client.verificationCalls != 1 {
		t.Fatalf("token=%d verify=%d", client.tokenCalls, client.verificationCalls)
	}
	if client.tokenURL != "https://api-m.sandbox.paypal.com/v1/oauth2/token" || client.verifyURL != "https://api-m.sandbox.paypal.com/v1/notifications/verify-webhook-signature" {
		t.Fatalf("token=%q verify=%q", client.tokenURL, client.verifyURL)
	}
	if client.basicUser != "client-id" || client.basicPassword != "client-secret" || client.tokenBody != "grant_type=client_credentials" || client.verifyAuth != "Bearer ci-access-token" {
		t.Fatal("PayPal OAuth request contract mismatch")
	}
	if !bytes.Contains(client.verifyBody, append([]byte(`"webhook_event":`), raw...)) {
		t.Fatal("verification postback did not preserve webhook event bytes")
	}
	if !bytes.Contains(client.verifyBody, []byte(`"webhook_id":"WH-WEBHOOK-123"`)) || !bytes.Contains(client.verifyBody, []byte(`"transmission_id":"69cd13f0-d67a-11e5-baa3-778b53f4ae55"`)) {
		t.Fatal("verification postback missing frozen authority fields")
	}
	if store.calls != 1 || store.ref != "5O190127TN364715T" || !store.now.Equal(fixedNow) {
		t.Fatalf("store calls=%d ref=%q now=%s", store.calls, store.ref, store.now)
	}
	if cmd.Provider != billing.ProviderPayPal || cmd.ProviderEventID != "WH-2WR32451HC0233532-67976317FL4543714" || cmd.ProviderTransactionID != "2GG279541U471931P" || cmd.OrderID != "ord_paypal_123" {
		t.Fatalf("identity cmd=%+v", cmd)
	}
	if cmd.EventType != "PAYMENT.CAPTURE.COMPLETED" || cmd.Outcome != billing.TransactionPaid || cmd.Money != (billing.Money{Currency: "USD", AmountMinor: 1234}) || !cmd.ReceivedAt.Equal(eventTime) {
		t.Fatalf("normalized cmd=%+v", cmd)
	}
	if !strings.HasPrefix(cmd.CorrelationID, "paypal:") || len(cmd.CorrelationID) != 71 {
		t.Fatalf("correlation=%q", cmd.CorrelationID)
	}
	ack := verifier.SuccessAcknowledgement(billing.ProviderPayPal)
	if ack.StatusCode != http.StatusOK || ack.ContentType != "" || ack.Body != "" {
		t.Fatalf("ack=%+v", ack)
	}
}

func TestPayPalCallbackMapsDeniedAfterSameBindingChecks(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	store := &fakePayPalIntentResolver{intent: paypalIntentForTest()}
	client := &fakePayPalHTTPClient{}
	verifier := paypalVerifierForTest(store, client, fixedNow)
	raw := paypalEventForTest(t, "PAYMENT.CAPTURE.DENIED", "DENIED", "USD", "12.34", fixedNow)

	cmd, err := verifier.VerifyAndNormalize(paypalRequestForTest(raw), billing.ProviderPayPal)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Outcome != billing.TransactionFailed || cmd.EventType != "PAYMENT.CAPTURE.DENIED" || store.calls != 1 {
		t.Fatalf("cmd=%+v calls=%d", cmd, store.calls)
	}
}

func TestPayPalAuthenticatedNonSettlementAndRefundEventsDoNotConsultBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	for _, eventType := range []string{"PAYMENT.CAPTURE.PENDING", "PAYMENT.CAPTURE.REFUNDED", "CHECKOUT.ORDER.APPROVED"} {
		t.Run(eventType, func(t *testing.T) {
			store := &fakePayPalIntentResolver{intent: paypalIntentForTest()}
			client := &fakePayPalHTTPClient{}
			verifier := paypalVerifierForTest(store, client, fixedNow)
			raw := paypalEventForTest(t, eventType, "PENDING", "USD", "12.34", fixedNow)
			_, err := verifier.VerifyAndNormalize(paypalRequestForTest(raw), billing.ProviderPayPal)
			if !errors.Is(err, billing.ErrCallbackIgnored) || store.calls != 0 || client.verificationCalls != 1 {
				t.Fatalf("err=%v calls=%d verify=%d", err, store.calls, client.verificationCalls)
			}
		})
	}
}

func TestPayPalPostbackFailureAndDependencyErrorsFailClosedBeforeBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	raw := paypalEventForTest(t, "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "USD", "12.34", fixedNow)
	cases := []struct {
		name   string
		client *fakePayPalHTTPClient
		want   error
	}{
		{name: "verification failure", client: &fakePayPalHTTPClient{verificationResult: "FAILURE"}, want: billing.ErrCallbackUnauthorized},
		{name: "oauth dependency", client: &fakePayPalHTTPClient{tokenErr: errors.New("oauth unavailable")}, want: billing.ErrCallbackUnavailable},
		{name: "oauth status", client: &fakePayPalHTTPClient{tokenStatus: http.StatusUnauthorized}, want: billing.ErrCallbackUnavailable},
		{name: "verification dependency", client: &fakePayPalHTTPClient{verificationErr: errors.New("verify unavailable")}, want: billing.ErrCallbackUnavailable},
		{name: "verification status", client: &fakePayPalHTTPClient{verificationStatus: http.StatusInternalServerError}, want: billing.ErrCallbackUnavailable},
		{name: "unknown verification result", client: &fakePayPalHTTPClient{verificationResult: "UNKNOWN"}, want: billing.ErrCallbackUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePayPalIntentResolver{intent: paypalIntentForTest()}
			verifier := paypalVerifierForTest(store, tc.client, fixedNow)
			_, err := verifier.VerifyAndNormalize(paypalRequestForTest(raw), billing.ProviderPayPal)
			if !errors.Is(err, tc.want) || store.calls != 0 {
				t.Fatalf("err=%v want=%v calls=%d", err, tc.want, store.calls)
			}
		})
	}
}

func TestPayPalRejectsMalformedOrDuplicateHeadersBeforeOutboundVerification(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	raw := paypalEventForTest(t, "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "USD", "12.34", fixedNow)
	cases := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "missing", mutate: func(r *http.Request) { r.Header.Del("PayPal-Transmission-Sig") }},
		{name: "duplicate", mutate: func(r *http.Request) { r.Header.Add("PayPal-Transmission-Id", "duplicate") }},
		{name: "oversized", mutate: func(r *http.Request) { r.Header.Set("PayPal-Auth-Algo", strings.Repeat("A", 101)) }},
		{name: "whitespace", mutate: func(r *http.Request) { r.Header.Set("PayPal-Transmission-Time", " 2026-09-09T13:00:00Z") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePayPalIntentResolver{intent: paypalIntentForTest()}
			client := &fakePayPalHTTPClient{}
			verifier := paypalVerifierForTest(store, client, fixedNow)
			r := paypalRequestForTest(raw)
			tc.mutate(r)
			_, err := verifier.VerifyAndNormalize(r, billing.ProviderPayPal)
			if !errors.Is(err, billing.ErrCallbackUnauthorized) || client.tokenCalls != 0 || client.verificationCalls != 0 || store.calls != 0 {
				t.Fatalf("err=%v token=%d verify=%d store=%d", err, client.tokenCalls, client.verificationCalls, store.calls)
			}
		})
	}
}

func TestPayPalBindingAndMoneyFailuresFailClosed(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 13, 5, 0, 0, time.UTC)
	cases := []struct {
		name     string
		currency string
		value    string
		intent   billing.ProviderIntent
		storeErr error
		calls    int
		want     error
	}{
		{name: "bad amount", currency: "USD", value: "12.345", intent: paypalIntentForTest(), calls: 0, want: billing.ErrCallbackUnauthorized},
		{name: "lower currency", currency: "usd", value: "12.34", intent: paypalIntentForTest(), calls: 0, want: billing.ErrCallbackUnauthorized},
		{name: "bound amount", currency: "USD", value: "12.34", intent: func() billing.ProviderIntent { v := paypalIntentForTest(); v.SettlementAmountUnits = 9999; return v }(), calls: 1, want: billing.ErrCallbackUnauthorized},
		{name: "bound currency", currency: "USD", value: "12.34", intent: func() billing.ProviderIntent { v := paypalIntentForTest(); v.SettlementAsset = "EUR"; return v }(), calls: 1, want: billing.ErrCallbackUnauthorized},
		{name: "bound provider", currency: "USD", value: "12.34", intent: func() billing.ProviderIntent { v := paypalIntentForTest(); v.Provider = billing.ProviderStripe; return v }(), calls: 1, want: billing.ErrCallbackUnauthorized},
		{name: "missing binding", currency: "USD", value: "12.34", storeErr: billing.ErrNotFound, calls: 1, want: billing.ErrCallbackUnauthorized},
		{name: "expired binding", currency: "USD", value: "12.34", storeErr: billing.ErrConflict, calls: 1, want: billing.ErrCallbackUnauthorized},
		{name: "db unavailable", currency: "USD", value: "12.34", storeErr: errors.New("db unavailable"), calls: 1, want: billing.ErrCallbackUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakePayPalIntentResolver{intent: tc.intent, err: tc.storeErr}
			client := &fakePayPalHTTPClient{}
			verifier := paypalVerifierForTest(store, client, fixedNow)
			raw := paypalEventForTest(t, "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", tc.currency, tc.value, fixedNow)
			_, err := verifier.VerifyAndNormalize(paypalRequestForTest(raw), billing.ProviderPayPal)
			if !errors.Is(err, tc.want) || store.calls != tc.calls {
				t.Fatalf("err=%v want=%v calls=%d wantCalls=%d", err, tc.want, store.calls, tc.calls)
			}
		})
	}
}

func TestParsePayPalAmountMinorUsesCurrentPayPalCurrencyPrecision(t *testing.T) {
	cases := []struct {
		currency string
		value    string
		want     int64
		ok       bool
	}{
		{currency: "USD", value: "12.34", want: 1234, ok: true},
		{currency: "USD", value: "12.3", want: 1230, ok: true},
		{currency: "USD", value: "12", want: 1200, ok: true},
		{currency: "JPY", value: "1234", want: 1234, ok: true},
		{currency: "HUF", value: "10", want: 10, ok: true},
		{currency: "TWD", value: "9", want: 9, ok: true},
		{currency: "JPY", value: "12.00", ok: false},
		{currency: "USD", value: "12.345", ok: false},
		{currency: "usd", value: "12.34", ok: false},
		{currency: "TND", value: "12.345", ok: false},
		{currency: "USD", value: "-1.00", ok: false},
		{currency: "USD", value: "0", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.currency+"_"+tc.value, func(t *testing.T) {
			got, currency, ok := parsePayPalAmountMinor(tc.currency, tc.value)
			if ok != tc.ok || got != tc.want || (ok && currency != tc.currency) {
				t.Fatalf("got=%d currency=%q ok=%v", got, currency, ok)
			}
		})
	}
}

func TestPayPalProductionConfigDerivesFixedOriginAndFailsClosed(t *testing.T) {
	store := &fakePayPalIntentResolver{}
	for _, tc := range []struct {
		env      string
		wantBase string
	}{
		{env: "live", wantBase: "https://api-m.paypal.com"},
		{env: "sandbox", wantBase: "https://api-m.sandbox.paypal.com"},
	} {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("GOJET_BILLING_PAYPAL_ENABLED", "1")
			t.Setenv("GOJET_BILLING_PAYPAL_ENV", tc.env)
			t.Setenv("GOJET_BILLING_PAYPAL_CLIENT_ID", "client-id")
			t.Setenv("GOJET_BILLING_PAYPAL_CLIENT_SECRET", "client-secret")
			t.Setenv("GOJET_BILLING_PAYPAL_WEBHOOK_ID", "WH-WEBHOOK-123")
			verifier, enabled, err := buildProductionPayPalCallbackVerifier(store)
			if err != nil || !enabled || verifier.apiBase != tc.wantBase || verifier.client == nil {
				t.Fatalf("enabled=%v err=%v base=%q", enabled, err, verifier.apiBase)
			}
		})
	}

	t.Setenv("GOJET_BILLING_PAYPAL_ENABLED", "1")
	t.Setenv("GOJET_BILLING_PAYPAL_ENV", "custom")
	t.Setenv("GOJET_BILLING_PAYPAL_CLIENT_ID", "client-id")
	t.Setenv("GOJET_BILLING_PAYPAL_CLIENT_SECRET", "client-secret")
	t.Setenv("GOJET_BILLING_PAYPAL_WEBHOOK_ID", "WH-WEBHOOK-123")
	if _, enabled, err := buildProductionPayPalCallbackVerifier(store); enabled || !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("enabled=%v err=%v", enabled, err)
	}
}

func TestPayPalProductionHTTPClientRejectsRedirectsAndPrivateDestinations(t *testing.T) {
	client := newProductionPayPalHTTPClient("api-m.paypal.com")
	if err := client.CheckRedirect(httptest.NewRequest(http.MethodGet, "https://example.com", nil), nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("redirect err=%v", err)
	}
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{ip: "8.8.8.8", want: true},
		{ip: "1.1.1.1", want: true},
		{ip: "127.0.0.1", want: false},
		{ip: "10.0.0.1", want: false},
		{ip: "169.254.169.254", want: false},
		{ip: "::1", want: false},
		{ip: "fc00::1", want: false},
	} {
		if got := publicPayPalDestinationIP(net.ParseIP(tc.ip)); got != tc.want {
			t.Fatalf("ip=%s got=%v want=%v", tc.ip, got, tc.want)
		}
	}
}
