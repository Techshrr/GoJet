package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

type fakeWeChatIntentResolver struct {
	calls  int
	ref    string
	now    time.Time
	intent billing.ProviderIntent
	err    error
}

func (f *fakeWeChatIntentResolver) ResolveProviderIntentByMerchantReference(_ context.Context, provider billing.Provider, ref string, now time.Time) (billing.ProviderIntent, error) {
	f.calls++
	f.ref = ref
	f.now = now
	if provider != billing.ProviderWeChat {
		return billing.ProviderIntent{}, billing.ErrInvalidInput
	}
	return f.intent, f.err
}

func wechatIntentForTest() billing.ProviderIntent {
	return billing.ProviderIntent{
		ID:                    "pint_wechat_123",
		WorkspaceID:           "ws_wechat_123",
		OrderID:               "ord_wechat_123",
		Provider:              billing.ProviderWeChat,
		MerchantReference:     "gj_gojet_123",
		SettlementAssetKind:   billing.SettlementAssetFiat,
		SettlementAsset:       "CNY",
		SettlementAmountUnits: 1234,
		Status:                billing.ProviderIntentActive,
	}
}

func wechatTestPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func wechatPublicKeyPEM(t *testing.T, key *rsa.PublicKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func wechatPlatformKeysJSON(t *testing.T, serial string, key *rsa.PublicKey) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{serial: wechatPublicKeyPEM(t, key)})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func encryptWeChatResourceForTest(t *testing.T, apiV3Key, nonce, associatedData string, plaintext []byte) string {
	t.Helper()
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != gcm.NonceSize() {
		t.Fatalf("nonce size=%d want=%d", len(nonce), gcm.NonceSize())
	}
	sealed := gcm.Seal(nil, []byte(nonce), plaintext, []byte(associatedData))
	return base64.StdEncoding.EncodeToString(sealed)
}

func wechatCallbackBodyForTest(t *testing.T, apiV3Key, eventType, appID, mchID, outTradeNo, transactionID, currency string, amount int64, eventTime time.Time) []byte {
	t.Helper()
	transaction := map[string]any{
		"appid":          appID,
		"mchid":          mchID,
		"out_trade_no":   outTradeNo,
		"transaction_id": transactionID,
		"trade_state":    "SUCCESS",
		"success_time":   eventTime.Format(time.RFC3339),
		"amount": map[string]any{
			"total":    amount,
			"currency": currency,
		},
	}
	plaintext, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	nonce := "0123456789ab"
	associatedData := "transaction"
	ciphertext := encryptWeChatResourceForTest(t, apiV3Key, nonce, associatedData, plaintext)
	envelope := map[string]any{
		"id":            "EVT-WECHAT-123",
		"create_time":   eventTime.Format(time.RFC3339Nano),
		"resource_type": "encrypt-resource",
		"event_type":    eventType,
		"summary":       "test",
		"resource": map[string]any{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      ciphertext,
			"associated_data": associatedData,
			"nonce":           nonce,
			"original_type":   "transaction",
		},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func wechatSignatureForTest(t *testing.T, key *rsa.PrivateKey, timestamp int64, nonce string, raw []byte) string {
	t.Helper()
	message := strconv.FormatInt(timestamp, 10) + "\n" + nonce + "\n" + string(raw) + "\n"
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(signature)
}

func wechatRequestForTest(t *testing.T, key *rsa.PrivateKey, serial string, timestamp int64, raw []byte) *http.Request {
	t.Helper()
	nonce := "callback-nonce"
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/wechat", bytes.NewReader(raw))
	r.Header.Set("Wechatpay-Timestamp", strconv.FormatInt(timestamp, 10))
	r.Header.Set("Wechatpay-Nonce", nonce)
	r.Header.Set("Wechatpay-Serial", serial)
	r.Header.Set("Wechatpay-Signature", wechatSignatureForTest(t, key, timestamp, nonce, raw))
	r.Header.Set("Wechatpay-Signature-Type", wechatSignatureTypeRSA2048)
	return r
}

func wechatVerifierForTest(t *testing.T, store wechatProviderIntentResolver, key *rsa.PrivateKey, serial, apiV3Key string, fixedNow time.Time) wechatCallbackVerifier {
	t.Helper()
	return wechatCallbackVerifier{
		appID:        "wx-gojet-app",
		mchID:        "1900000109",
		apiV3Key:     []byte(apiV3Key),
		platformKeys: map[string]*rsa.PublicKey{serial: &key.PublicKey},
		store:        store,
		now:          func() time.Time { return fixedNow },
	}
}

func TestWeChatCallbackAuthenticatesDecryptsBindsAndNormalizesPaid(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	serial := "WECHAT-SERIAL-001"
	key := wechatTestPrivateKey(t)
	store := &fakeWeChatIntentResolver{intent: wechatIntentForTest()}
	verifier := wechatVerifierForTest(t, store, key, serial, apiV3Key, fixedNow)
	raw := wechatCallbackBodyForTest(t, apiV3Key, "TRANSACTION.SUCCESS", "wx-gojet-app", "1900000109", "gj_gojet_123", "420000000000000001", "CNY", 1234, fixedNow.Add(-time.Second))

	cmd, err := verifier.VerifyAndNormalize(wechatRequestForTest(t, key, serial, fixedNow.Unix(), raw), billing.ProviderWeChat)
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.ref != "gj_gojet_123" || !store.now.Equal(fixedNow) {
		t.Fatalf("store calls=%d ref=%q now=%s", store.calls, store.ref, store.now)
	}
	if cmd.Provider != billing.ProviderWeChat || cmd.ProviderEventID != "EVT-WECHAT-123" || cmd.ProviderTransactionID != "420000000000000001" || cmd.OrderID != "ord_wechat_123" {
		t.Fatalf("identity cmd=%+v", cmd)
	}
	if cmd.EventType != "TRANSACTION.SUCCESS" || cmd.Outcome != billing.TransactionPaid || cmd.Money != (billing.Money{Currency: "CNY", AmountMinor: 1234}) || !cmd.ReceivedAt.Equal(fixedNow.Add(-time.Second)) {
		t.Fatalf("normalized cmd=%+v", cmd)
	}
	if !strings.HasPrefix(cmd.CorrelationID, "wechat:") || len(cmd.CorrelationID) != 71 {
		t.Fatalf("correlation=%q", cmd.CorrelationID)
	}
	ack := verifier.SuccessAcknowledgement(billing.ProviderWeChat)
	if ack.StatusCode != http.StatusNoContent || ack.ContentType != "" || ack.Body != "" {
		t.Fatalf("ack=%+v", ack)
	}
}

func TestWeChatAuthenticatedIgnoredEventDoesNotConsultBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	serial := "WECHAT-SERIAL-001"
	key := wechatTestPrivateKey(t)
	store := &fakeWeChatIntentResolver{intent: wechatIntentForTest()}
	verifier := wechatVerifierForTest(t, store, key, serial, apiV3Key, fixedNow)
	raw := wechatCallbackBodyForTest(t, apiV3Key, "REFUND.SUCCESS", "wx-gojet-app", "1900000109", "gj_gojet_123", "420000000000000001", "CNY", 1234, fixedNow)

	_, err := verifier.VerifyAndNormalize(wechatRequestForTest(t, key, serial, fixedNow.Unix(), raw), billing.ProviderWeChat)
	if !errors.Is(err, billing.ErrCallbackIgnored) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestWeChatRejectsTamperedBodyBeforeDecryptOrBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	serial := "WECHAT-SERIAL-001"
	key := wechatTestPrivateKey(t)
	store := &fakeWeChatIntentResolver{intent: wechatIntentForTest()}
	verifier := wechatVerifierForTest(t, store, key, serial, apiV3Key, fixedNow)
	original := wechatCallbackBodyForTest(t, apiV3Key, "TRANSACTION.SUCCESS", "wx-gojet-app", "1900000109", "gj_gojet_123", "420000000000000001", "CNY", 1234, fixedNow)
	r := wechatRequestForTest(t, key, serial, fixedNow.Unix(), original)
	r.Body = http.NoBody
	_, err := verifier.VerifyAndNormalize(r, billing.ProviderWeChat)
	if !errors.Is(err, billing.ErrCallbackUnauthorized) || store.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, store.calls)
	}
}

func TestWeChatRejectsStaleFutureUnknownSerialAndSignatureProbeBeforeBinding(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	serial := "WECHAT-SERIAL-001"
	key := wechatTestPrivateKey(t)
	raw := wechatCallbackBodyForTest(t, apiV3Key, "TRANSACTION.SUCCESS", "wx-gojet-app", "1900000109", "gj_gojet_123", "420000000000000001", "CNY", 1234, fixedNow)

	for _, tc := range []struct {
		name      string
		timestamp int64
		serial    string
		probe     bool
	}{
		{name: "stale", timestamp: fixedNow.Add(-301 * time.Second).Unix(), serial: serial},
		{name: "future", timestamp: fixedNow.Add(301 * time.Second).Unix(), serial: serial},
		{name: "unknown serial", timestamp: fixedNow.Unix(), serial: "WECHAT-SERIAL-UNKNOWN"},
		{name: "signature probe", timestamp: fixedNow.Unix(), serial: serial, probe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeWeChatIntentResolver{intent: wechatIntentForTest()}
			verifier := wechatVerifierForTest(t, store, key, serial, apiV3Key, fixedNow)
			r := wechatRequestForTest(t, key, tc.serial, tc.timestamp, raw)
			if tc.probe {
				r.Header.Set("Wechatpay-Signature", "WECHATPAY/SIGNTEST/blocked")
			}
			_, err := verifier.VerifyAndNormalize(r, billing.ProviderWeChat)
			if !errors.Is(err, billing.ErrCallbackUnauthorized) || store.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, store.calls)
			}
		})
	}
}

func TestWeChatDecryptedIdentityAndBindingFailuresFailClosed(t *testing.T) {
	fixedNow := time.Date(2026, 9, 9, 2, 0, 0, 0, time.UTC)
	apiV3Key := "0123456789abcdef0123456789abcdef"
	serial := "WECHAT-SERIAL-001"
	key := wechatTestPrivateKey(t)

	cases := []struct {
		name     string
		appID    string
		mchID    string
		amount   int64
		currency string
		intent   billing.ProviderIntent
		storeErr error
		want     error
		calls    int
	}{
		{name: "appid", appID: "wx-wrong", mchID: "1900000109", amount: 1234, currency: "CNY", intent: wechatIntentForTest(), want: billing.ErrCallbackUnauthorized, calls: 0},
		{name: "mchid", appID: "wx-gojet-app", mchID: "1900009999", amount: 1234, currency: "CNY", intent: wechatIntentForTest(), want: billing.ErrCallbackUnauthorized, calls: 0},
		{name: "bound amount", appID: "wx-gojet-app", mchID: "1900000109", amount: 1234, currency: "CNY", intent: func() billing.ProviderIntent { v := wechatIntentForTest(); v.SettlementAmountUnits = 9999; return v }(), want: billing.ErrCallbackUnauthorized, calls: 1},
		{name: "bound currency", appID: "wx-gojet-app", mchID: "1900000109", amount: 1234, currency: "CNY", intent: func() billing.ProviderIntent { v := wechatIntentForTest(); v.SettlementAsset = "USD"; return v }(), want: billing.ErrCallbackUnauthorized, calls: 1},
		{name: "missing binding", appID: "wx-gojet-app", mchID: "1900000109", amount: 1234, currency: "CNY", storeErr: billing.ErrNotFound, want: billing.ErrCallbackUnauthorized, calls: 1},
		{name: "expired binding", appID: "wx-gojet-app", mchID: "1900000109", amount: 1234, currency: "CNY", storeErr: billing.ErrConflict, want: billing.ErrCallbackUnauthorized, calls: 1},
		{name: "db unavailable", appID: "wx-gojet-app", mchID: "1900000109", amount: 1234, currency: "CNY", storeErr: errors.New("database unavailable"), want: billing.ErrCallbackUnavailable, calls: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeWeChatIntentResolver{intent: tc.intent, err: tc.storeErr}
			verifier := wechatVerifierForTest(t, store, key, serial, apiV3Key, fixedNow)
			raw := wechatCallbackBodyForTest(t, apiV3Key, "TRANSACTION.SUCCESS", tc.appID, tc.mchID, "gj_gojet_123", "420000000000000001", tc.currency, tc.amount, fixedNow)
			_, err := verifier.VerifyAndNormalize(wechatRequestForTest(t, key, serial, fixedNow.Unix(), raw), billing.ProviderWeChat)
			if !errors.Is(err, tc.want) || store.calls != tc.calls {
				t.Fatalf("err=%v want=%v calls=%d wantCalls=%d", err, tc.want, store.calls, tc.calls)
			}
		})
	}
}

func TestBuildProductionWeChatCallbackVerifierFailsClosedOnIncompleteEnabledConfig(t *testing.T) {
	t.Setenv("GOJET_BILLING_WECHAT_ENABLED", "1")
	t.Setenv("GOJET_BILLING_WECHAT_APPID", "wx-gojet-app")
	t.Setenv("GOJET_BILLING_WECHAT_MCHID", "1900000109")
	t.Setenv("GOJET_BILLING_WECHAT_API_V3_KEY", "short")
	t.Setenv("GOJET_BILLING_WECHAT_PLATFORM_KEYS_JSON", "{}")
	if _, _, err := buildProductionWeChatCallbackVerifier(&fakeWeChatIntentResolver{}); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildProductionWeChatCallbackVerifierAcceptsCompleteConfigAndRotationMap(t *testing.T) {
	key1 := wechatTestPrivateKey(t)
	key2 := wechatTestPrivateKey(t)
	rawKeys, err := json.Marshal(map[string]string{
		"WECHAT-SERIAL-CURRENT":  wechatPublicKeyPEM(t, &key1.PublicKey),
		"WECHAT-SERIAL-PREVIOUS": wechatPublicKeyPEM(t, &key2.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOJET_BILLING_WECHAT_ENABLED", "1")
	t.Setenv("GOJET_BILLING_WECHAT_APPID", "wx-gojet-app")
	t.Setenv("GOJET_BILLING_WECHAT_MCHID", "1900000109")
	t.Setenv("GOJET_BILLING_WECHAT_API_V3_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("GOJET_BILLING_WECHAT_PLATFORM_KEYS_JSON", string(rawKeys))

	verifier, enabled, err := buildProductionWeChatCallbackVerifier(&fakeWeChatIntentResolver{})
	if err != nil || !enabled || verifier.appID != "wx-gojet-app" || verifier.mchID != "1900000109" || len(verifier.apiV3Key) != 32 || len(verifier.platformKeys) != 2 {
		t.Fatalf("enabled=%v keys=%d err=%v", enabled, len(verifier.platformKeys), err)
	}
}

func TestParseWeChatPlatformKeysRejectsDuplicateOrInvalidConfig(t *testing.T) {
	key := wechatTestPrivateKey(t)
	pemValue := wechatPublicKeyPEM(t, &key.PublicKey)
	encodedPEM, err := json.Marshal(pemValue)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := fmt.Sprintf(`{"SERIAL":%s,"SERIAL":%s}`, encodedPEM, encodedPEM)
	if _, err := parseWeChatPlatformKeysJSON(duplicate); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("duplicate err=%v", err)
	}
	if _, err := parseWeChatPlatformKeysJSON(`{"bad serial!":"not pem"}`); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("invalid err=%v", err)
	}
}

func TestWeChatPlatformKeyJSONHelperProducesAcceptedConfig(t *testing.T) {
	key := wechatTestPrivateKey(t)
	raw := wechatPlatformKeysJSON(t, "WECHAT-SERIAL-001", &key.PublicKey)
	keys, err := parseWeChatPlatformKeysJSON(raw)
	if err != nil || len(keys) != 1 || keys["WECHAT-SERIAL-001"] == nil {
		t.Fatalf("keys=%d err=%v", len(keys), err)
	}
}
