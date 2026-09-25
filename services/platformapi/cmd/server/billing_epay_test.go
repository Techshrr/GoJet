package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

type fakeEpayCallbackStore struct {
	calls int
	cmd   billing.CallbackCommand
	err   error
}

func (f *fakeEpayCallbackStore) ApplyAuthenticatedCallback(_ context.Context, cmd billing.CallbackCommand) (billing.CallbackResult, error) {
	f.calls++
	f.cmd = cmd
	return billing.CallbackResult{}, f.err
}

func signedEpayQuery(t *testing.T, key string, overrides map[string]string) string {
	t.Helper()
	params := map[string]string{
		"pid":          "1001",
		"trade_no":     "202609080001",
		"out_trade_no": "ord_0123456789abcdef",
		"type":         "alipay",
		"name":         "GoJet plan",
		"money":        "12.34",
		"trade_status": "TRADE_SUCCESS",
		"param":        "",
		"sign_type":    "MD5",
	}
	for name, value := range overrides {
		params[name] = value
	}
	params["sign"] = epayMD5Signature(params, key)
	values := make(url.Values, len(params))
	for name, value := range params {
		values.Set(name, value)
	}
	return values.Encode()
}

func TestEpayCallbackAuthenticatesNormalizesAndAcknowledges(t *testing.T) {
	store := &fakeEpayCallbackStore{}
	fixedNow := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	handler := epayCallbackHandler{
		enabled: true,
		store:   store,
		verifier: epayCallbackVerifier{
			pid: "1001",
			key: "merchant-secret-key",
			now: func() time.Time { return fixedNow },
		},
	}
	r := httptest.NewRequest(http.MethodGet, "/api/payments/callbacks/epay?"+signedEpayQuery(t, "merchant-secret-key", nil), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK || w.Body.String() != "success" || store.calls != 1 {
		t.Fatalf("status=%d body=%q calls=%d", w.Code, w.Body.String(), store.calls)
	}
	if got := w.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Fatalf("content-type=%q", got)
	}
	cmd := store.cmd
	if cmd.Provider != billing.ProviderEpay || cmd.ProviderEventID != "202609080001" || cmd.ProviderTransactionID != "202609080001" || cmd.OrderID != "ord_0123456789abcdef" {
		t.Fatalf("identity cmd=%+v", cmd)
	}
	if cmd.EventType != "payment.paid" || cmd.Outcome != billing.TransactionPaid || cmd.Money.Currency != "CNY" || cmd.Money.AmountMinor != 1234 || !cmd.ReceivedAt.Equal(fixedNow) {
		t.Fatalf("normalized cmd=%+v", cmd)
	}
	if !strings.HasPrefix(cmd.CorrelationID, "epay:") || len(cmd.CorrelationID) != 69 {
		t.Fatalf("correlation=%q", cmd.CorrelationID)
	}
}

func TestEpayCallbackRejectsTamperedAmountBeforeStore(t *testing.T) {
	store := &fakeEpayCallbackStore{}
	query := signedEpayQuery(t, "merchant-secret-key", nil)
	values, err := url.ParseQuery(query)
	if err != nil {
		t.Fatal(err)
	}
	values.Set("money", "99.99")
	r := httptest.NewRequest(http.MethodGet, "/api/payments/callbacks/epay?"+values.Encode(), nil)
	w := httptest.NewRecorder()
	handler := epayCallbackHandler{enabled: true, store: store, verifier: epayCallbackVerifier{pid: "1001", key: "merchant-secret-key"}}
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || w.Body.String() != "fail" || store.calls != 0 {
		t.Fatalf("status=%d body=%q calls=%d", w.Code, w.Body.String(), store.calls)
	}
}

func TestEpayCallbackRejectsDuplicateParameters(t *testing.T) {
	store := &fakeEpayCallbackStore{}
	query := signedEpayQuery(t, "merchant-secret-key", nil) + "&pid=1001"
	r := httptest.NewRequest(http.MethodGet, "/api/payments/callbacks/epay?"+query, nil)
	w := httptest.NewRecorder()
	handler := epayCallbackHandler{enabled: true, store: store, verifier: epayCallbackVerifier{pid: "1001", key: "merchant-secret-key"}}
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || store.calls != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, store.calls)
	}
}

func TestEpayAuthenticatedNonSuccessDoesNotMutateAndStopsRetry(t *testing.T) {
	store := &fakeEpayCallbackStore{}
	query := signedEpayQuery(t, "merchant-secret-key", map[string]string{"trade_status": "WAIT_BUYER_PAY"})
	r := httptest.NewRequest(http.MethodGet, "/api/payments/callbacks/epay?"+query, nil)
	w := httptest.NewRecorder()
	handler := epayCallbackHandler{enabled: true, store: store, verifier: epayCallbackVerifier{pid: "1001", key: "merchant-secret-key"}}
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "success" || store.calls != 0 {
		t.Fatalf("status=%d body=%q calls=%d", w.Code, w.Body.String(), store.calls)
	}
}

func TestEpayDisabledFailsClosed(t *testing.T) {
	store := &fakeEpayCallbackStore{}
	r := httptest.NewRequest(http.MethodGet, "/api/payments/callbacks/epay?x=y", nil)
	w := httptest.NewRecorder()
	handler := epayCallbackHandler{enabled: false, store: store}
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "fail" || store.calls != 0 {
		t.Fatalf("status=%d body=%q calls=%d", w.Code, w.Body.String(), store.calls)
	}
}

func TestBuildProductionEpayCallbackHandlerFailsClosedOnMissingCredentials(t *testing.T) {
	t.Setenv("GOJET_BILLING_EPAY_ENABLED", "1")
	t.Setenv("GOJET_BILLING_EPAY_PID", "1001")
	t.Setenv("GOJET_BILLING_EPAY_KEY", "")
	if _, err := buildProductionEpayCallbackHandler(&fakeEpayCallbackStore{}); err != billing.ErrCallbackUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestParseEpayCNYMinorIsExact(t *testing.T) {
	cases := map[string]int64{"1": 100, "1.2": 120, "1.23": 123, "0001.01": 101}
	for input, want := range cases {
		got, err := parseEpayCNYMinor(input)
		if err != nil || got != want {
			t.Fatalf("input=%q got=%d want=%d err=%v", input, got, want, err)
		}
	}
	for _, input := range []string{"", "0", "0.00", "1.234", "-1.00", "+1.00", "1e2", ".50"} {
		if _, err := parseEpayCNYMinor(input); err == nil {
			t.Fatalf("invalid amount accepted: %q", input)
		}
	}
}
