package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

type fakeAlipayIntentResolver struct {
	calls  int
	ref    string
	now    time.Time
	intent billing.ProviderIntent
	err    error
}

func (f *fakeAlipayIntentResolver) ResolveProviderIntentByMerchantReference(_ context.Context, provider billing.Provider, ref string, now time.Time) (billing.ProviderIntent, error) {
	f.calls++
	f.ref = ref
	f.now = now
	if provider != billing.ProviderAlipay {
		return billing.ProviderIntent{}, billing.ErrInvalidInput
	}
	return f.intent, f.err
}

func alipayIntentForTest() billing.ProviderIntent {
	return billing.ProviderIntent{
		ID:                    "pint_alipay_123",
		WorkspaceID:           "ws_alipay_123",
		OrderID:               "ord_alipay_123",
		Provider:              billing.ProviderAlipay,
		MerchantReference:     "gj_alipay_123",
		SettlementAssetKind:   billing.SettlementAssetFiat,
		SettlementAsset:       "CNY",
		SettlementAmountUnits: 1234,
		Status:                billing.ProviderIntentActive,
	}
}

func newAlipayTestKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func alipayFieldsForTest(status string) map[string]string {
	return map[string]string{
		"notify_time":  "2026-09-09 22:30:00",
		"notify_type":  "trade_status_sync",
		"notify_id":    "notify_alipay_123",
		"sign_type":    "RSA2",
		"trade_no":     "2026090922001400011234567890",
		"app_id":       "2026000000000001",
		"auth_app_id":  "2026000000000001",
		"out_trade_no": "gj_alipay_123",
		"seller_id":    "2088000000000001",
		"trade_status": status,
		"total_amount": "12.34",
		"subject":      "GoJet 订阅",
	}
}

func signAlipayFieldsForTest(t *testing.T, fields map[string]string, privateKey *rsa.PrivateKey) {
	t.Helper()
	content, ok := buildAlipaySignatureContent(fields)
	if !ok {
		t.Fatal("could not build Alipay signing content")
	}
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	fields["sign"] = base64.StdEncoding.EncodeToString(signature)
}

func alipayRequestForTest(fields map[string]string) *http.Request {
	values := make(url.Values, len(fields))
	for key, value := range fields {
		values.Set(key, value)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/alipay", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	return r
}

func alipayVerifierForTest(store alipayProviderIntentResolver, publicKeys map[string]*rsa.PublicKey, fixedNow time.Time) alipayCallbackVerifier {
	return alipayCallbackVerifier{
		appID:      "2026000000000001",
		sellerID:   "2088000000000001",
		publicKeys: publicKeys,
		store:      store,
		now:        func() time.Time { return fixedNow },
	}
}

func TestAlipayCallbackRSA2BindsMoneyAndNormalizesPaid(t *testing.T) {
	privateKey, _ := newAlipayTestKey(t)
	fixedNow := time.Date(2026, 9, 9, 14, 35, 0, 0, time.UTC)
	store := &fakeAlipayIntentResolver{intent: alipayIntentForTest()}
	verifier := alipayVerifierForTest(store, map[string]*rsa.PublicKey{"current": &privateKey.PublicKey}, fixedNow)
	fields := alipayFieldsForTest("TRADE_SUCCESS")
	signAlipayFieldsForTest(t, fields, privateKey)

	cmd, err := verifier.VerifyAndNormalize(alipayRequestForTest(fields), billing.ProviderAlipay)
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.ref != "gj_alipay_123" || !store.now.Equal(fixedNow) {
		t.Fatalf("calls=%d ref=%q now=%s", store.calls, store.ref, store.now)
	}
	if cmd.Provider != billing.ProviderAlipay || cmd.ProviderEventID != "notify_alipay_123" || cmd.ProviderTransactionID != "2026090922001400011234567890" || cmd.OrderID != "ord_alipay_123" {
		t.Fatalf("identity cmd=%+v", cmd)
	}
	wantTime := time.Date(2026, 9, 9, 14, 30, 0, 0, time.UTC)
	if cmd.EventType != "TRADE_SUCCESS" || cmd.Outcome != billing.TransactionPaid || cmd.Money != (billing.Money{Currency: "CNY", AmountMinor: 1234}) || !cmd.ReceivedAt.Equal(wantTime) {
		t.Fatalf("normalized cmd=%+v", cmd)
	}
	if !strings.HasPrefix(cmd.CorrelationID, "alipay:") || len(cmd.CorrelationID) != 71 {
		t.Fatalf("correlation=%q", cmd.CorrelationID)
	}
	ack := verifier.SuccessAcknowledgement(billing.ProviderAlipay)
	if ack.StatusCode != http.StatusOK || ack.ContentType != alipaySuccessContentType || ack.Body != "success" {
		t.Fatalf("ack=%+v", ack)
	}
}

func TestAlipayTradeFinishedIsPaidUnderSameBinding(t *testing.T) {
	privateKey, _ := newAlipayTestKey(t)
	fixedNow := time.Date(2026, 9, 9, 14, 35, 0, 0, time.UTC)
	store := &fakeAlipayIntentResolver{intent: alipayIntentForTest()}
	verifier := alipayVerifierForTest(store, map[string]*rsa.PublicKey{"current": &privateKey.PublicKey}, fixedNow)
	fields := alipayFieldsForTest("TRADE_FINISHED")
	signAlipayFieldsForTest(t, fields, privateKey)
	cmd, err := verifier.VerifyAndNormalize(alipayRequestForTest(fields), billing.ProviderAlipay)
	if err != nil || cmd.Outcome != billing.TransactionPaid || cmd.EventType != "TRADE_FINISHED" || store.calls != 1 {
		t.Fatalf("cmd=%+v calls=%d err=%v", cmd, store.calls, err)
	}
}

func TestAlipayAuthenticatedNonSettlementAndRefundStatesAreIgnoredBeforeBinding(t *testing.T) {
	privateKey, _ := newAlipayTestKey(t)
	fixedNow := time.Date(2026, 9, 9, 14, 35, 0, 0, time.UTC)
	for _, status := range []string{"TRADE_CLOSED", "WAIT_BUYER_PAY", "UNKNOWN_STATE"} {
		t.Run(status, func(t *testing.T) {
			store := &fakeAlipayIntentResolver{intent: alipayIntentForTest()}
			verifier := alipayVerifierForTest(store, map[string]*rsa.PublicKey{"current": &privateKey.PublicKey}, fixedNow)
			fields := alipayFieldsForTest(status)
			if status == "TRADE_CLOSED" {
				fields["refund_fee"] = "12.34"
				fields["out_biz_no"] = "refund_ignored_123"
			}
			signAlipayFieldsForTest(t, fields, privateKey)
			_, err := verifier.VerifyAndNormalize(alipayRequestForTest(fields), billing.ProviderAlipay)
			if !errors.Is(err, billing.ErrCallbackIgnored) || store.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, store.calls)
			}
		})
	}
}

func TestAlipayVerificationAndBindingFailuresFailClosed(t *testing.T) {
	privateKey, _ := newAlipayTestKey(t)
	otherKey, _ := newAlipayTestKey(t)
	fixedNow := time.Date(2026, 9, 9, 14, 35, 0, 0, time.UTC)
	cases := []struct {
		name     string
		mutate   func(map[string]string)
		storeErr error
		intent   billing.ProviderIntent
		want     error
		calls    int
		signKey  *rsa.PrivateKey
	}{
		{name: "wrong signature", want: billing.ErrCallbackUnauthorized, signKey: otherKey},
		{name: "wrong app", mutate: func(v map[string]string) { v["app_id"] = "2026000000000002" }, want: billing.ErrCallbackUnauthorized, signKey: privateKey},
		{name: "wrong seller", mutate: func(v map[string]string) { v["seller_id"] = "2088000000000002" }, want: billing.ErrCallbackUnauthorized, signKey: privateKey},
		{name: "legacy RSA", mutate: func(v map[string]string) { v["sign_type"] = "RSA" }, want: billing.ErrCallbackUnauthorized, signKey: privateKey},
		{name: "bad amount", mutate: func(v map[string]string) { v["total_amount"] = "12.345" }, want: billing.ErrCallbackUnauthorized, signKey: privateKey},
		{name: "missing binding", storeErr: billing.ErrNotFound, want: billing.ErrCallbackUnauthorized, calls: 1, signKey: privateKey},
		{name: "expired binding", storeErr: billing.ErrConflict, want: billing.ErrCallbackUnauthorized, calls: 1, signKey: privateKey},
		{name: "db unavailable", storeErr: errors.New("db unavailable"), want: billing.ErrCallbackUnavailable, calls: 1, signKey: privateKey},
		{name: "bound amount", intent: func() billing.ProviderIntent { v := alipayIntentForTest(); v.SettlementAmountUnits = 9999; return v }(), want: billing.ErrCallbackUnauthorized, calls: 1, signKey: privateKey},
		{name: "bound currency", intent: func() billing.ProviderIntent { v := alipayIntentForTest(); v.SettlementAsset = "USD"; return v }(), want: billing.ErrCallbackUnauthorized, calls: 1, signKey: privateKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intent := tc.intent
			if intent.ID == "" {
				intent = alipayIntentForTest()
			}
			store := &fakeAlipayIntentResolver{intent: intent, err: tc.storeErr}
			verifier := alipayVerifierForTest(store, map[string]*rsa.PublicKey{"current": &privateKey.PublicKey}, fixedNow)
			fields := alipayFieldsForTest("TRADE_SUCCESS")
			if tc.mutate != nil {
				tc.mutate(fields)
			}
			signAlipayFieldsForTest(t, fields, tc.signKey)
			_, err := verifier.VerifyAndNormalize(alipayRequestForTest(fields), billing.ProviderAlipay)
			if !errors.Is(err, tc.want) || store.calls != tc.calls {
				t.Fatalf("err=%v want=%v calls=%d wantCalls=%d", err, tc.want, store.calls, tc.calls)
			}
		})
	}
}

func TestAlipayRejectsDuplicateFormFieldsAndWrongContentType(t *testing.T) {
	privateKey, _ := newAlipayTestKey(t)
	store := &fakeAlipayIntentResolver{intent: alipayIntentForTest()}
	verifier := alipayVerifierForTest(store, map[string]*rsa.PublicKey{"current": &privateKey.PublicKey}, time.Now().UTC())
	fields := alipayFieldsForTest("TRADE_SUCCESS")
	signAlipayFieldsForTest(t, fields, privateKey)
	values := make(url.Values)
	for key, value := range fields {
		values.Set(key, value)
	}
	body := values.Encode() + "&notify_id=duplicate"
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/alipay", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if _, err := verifier.VerifyAndNormalize(r, billing.ProviderAlipay); !errors.Is(err, billing.ErrCallbackUnauthorized) {
		t.Fatalf("duplicate field err=%v", err)
	}

	r = alipayRequestForTest(fields)
	r.Header.Set("Content-Type", "application/json")
	if _, err := verifier.VerifyAndNormalize(r, billing.ProviderAlipay); !errors.Is(err, billing.ErrCallbackUnauthorized) {
		t.Fatalf("content-type err=%v", err)
	}
}

func TestAlipayPublicKeyRotationAndConfigFailClosed(t *testing.T) {
	first, firstPEM := newAlipayTestKey(t)
	second, secondPEM := newAlipayTestKey(t)
	keysRaw, err := json.Marshal(map[string]string{"current": firstPEM, "previous": secondPEM})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseAlipayPublicKeysJSON(string(keysRaw))
	if err != nil || len(parsed) != 2 {
		t.Fatalf("parsed=%d err=%v", len(parsed), err)
	}
	fields := alipayFieldsForTest("TRADE_SUCCESS")
	signAlipayFieldsForTest(t, fields, second)
	if !verifyAlipayRSA2(fields, parsed) {
		t.Fatal("previous reviewed key did not verify")
	}
	_ = first

	duplicate := `{"current":` + strconvQuote(firstPEM) + `,"current":` + strconvQuote(firstPEM) + `}`
	if _, err := parseAlipayPublicKeysJSON(duplicate); err == nil {
		t.Fatal("duplicate Alipay public-key label accepted")
	}
}

func strconvQuote(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func TestParseAlipayAmountMinor(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
		ok    bool
	}{
		{value: "12.34", want: 1234, ok: true},
		{value: "12.3", want: 1230, ok: true},
		{value: "12", want: 1200, ok: true},
		{value: "0.01", want: 1, ok: true},
		{value: "12.345", ok: false},
		{value: "-1.00", ok: false},
		{value: "+1.00", ok: false},
		{value: "0", ok: false},
		{value: "1.", ok: false},
	} {
		got, ok := parseAlipayAmountMinor(tc.value)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("value=%q got=%d ok=%v", tc.value, got, ok)
		}
	}
}

func TestAlipayProductionConfigDerivesReviewedPublicKeys(t *testing.T) {
	_, publicPEM := newAlipayTestKey(t)
	keysRaw, err := json.Marshal(map[string]string{"current": publicPEM})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeAlipayIntentResolver{}
	t.Setenv("GOJET_BILLING_ALIPAY_ENABLED", "1")
	t.Setenv("GOJET_BILLING_ALIPAY_APP_ID", "2026000000000001")
	t.Setenv("GOJET_BILLING_ALIPAY_SELLER_ID", "2088000000000001")
	t.Setenv("GOJET_BILLING_ALIPAY_PUBLIC_KEYS_JSON", string(keysRaw))
	verifier, enabled, err := buildProductionAlipayCallbackVerifier(store)
	if err != nil || !enabled || verifier.appID == "" || verifier.sellerID == "" || len(verifier.publicKeys) != 1 {
		t.Fatalf("enabled=%v verifier=%+v err=%v", enabled, verifier, err)
	}

	t.Setenv("GOJET_BILLING_ALIPAY_PUBLIC_KEYS_JSON", "{}")
	if _, enabled, err := buildProductionAlipayCallbackVerifier(store); enabled || !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("empty keys enabled=%v err=%v", enabled, err)
	}
}
