package main

import (
	"bytes"
	"context"
	"encoding/json"
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

const (
	cryptoTestTxID      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cryptoTestRecipient = "41abcdefabcdefabcdefabcdefabcdefabcdefabcd"
)

type cryptoStoreFixture struct {
	intent        billing.ProviderIntent
	order         billing.Order
	resolveErr    error
	orderErr      error
	resolveCalls  int
	orderCalls    int
	expectedRef   string
	lastResolveAt time.Time
}

func (s *cryptoStoreFixture) ResolveCryptoProviderIntentByProviderReference(_ context.Context, ref string, now time.Time) (billing.ProviderIntent, error) {
	s.resolveCalls++
	s.lastResolveAt = now
	if s.resolveErr != nil {
		return billing.ProviderIntent{}, s.resolveErr
	}
	if ref != s.expectedRef {
		return billing.ProviderIntent{}, billing.ErrNotFound
	}
	return s.intent, nil
}

func (s *cryptoStoreFixture) GetOrder(_ context.Context, workspaceID, orderID string) (billing.Order, error) {
	s.orderCalls++
	if s.orderErr != nil {
		return billing.Order{}, s.orderErr
	}
	if s.order.WorkspaceID != workspaceID || s.order.ID != orderID {
		return billing.Order{}, billing.ErrNotFound
	}
	return s.order, nil
}

type cryptoHTTPFixture struct {
	status      int
	body        []byte
	err         error
	calls       int
	lastURL     string
	lastAPIKey  string
	lastRequest []byte
}

func (f *cryptoHTTPFixture) Do(req *http.Request) (*http.Response, error) {
	f.calls++
	f.lastURL = req.URL.String()
	f.lastAPIKey = req.Header.Get("TRON-PRO-API-KEY")
	body, _ := io.ReadAll(req.Body)
	f.lastRequest = body
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

func TestBuildProductionCryptoCallbackVerifier(t *testing.T) {
	store := cryptoAuthorityStore()
	t.Setenv("GOJET_BILLING_CRYPTO_ENABLED", "0")
	if _, enabled, err := buildProductionCryptoCallbackVerifier(store); err != nil || enabled {
		t.Fatalf("disabled builder enabled=%v err=%v", enabled, err)
	}

	t.Setenv("GOJET_BILLING_CRYPTO_ENABLED", "1")
	t.Setenv("GOJET_BILLING_CRYPTO_TRONGRID_API_KEY", "")
	if _, enabled, err := buildProductionCryptoCallbackVerifier(store); !errors.Is(err, billing.ErrCallbackUnavailable) || enabled {
		t.Fatalf("missing key enabled=%v err=%v", enabled, err)
	}

	t.Setenv("GOJET_BILLING_CRYPTO_TRONGRID_API_KEY", "ci-trongrid-api-key")
	verifier, enabled, err := buildProductionCryptoCallbackVerifier(store)
	if err != nil || !enabled {
		t.Fatalf("builder enabled=%v err=%v", enabled, err)
	}
	if verifier.apiBase != tronGridAPIBase || verifier.apiKey != "ci-trongrid-api-key" {
		t.Fatalf("unexpected verifier config: %+v", verifier)
	}
	if _, ok := verifier.client.(*http.Client); !ok {
		t.Fatalf("production client type=%T", verifier.client)
	}
}

func TestCryptoCallbackVerifierPaid(t *testing.T) {
	store := cryptoAuthorityStore()
	fixture := &cryptoHTTPFixture{body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 1, tronUSDTContractLogAddress, "SUCCESS", "")}
	verificationTime := time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC)
	verifier := cryptoCallbackVerifier{
		apiBase: tronGridAPIBase,
		apiKey:  "ci-trongrid-api-key",
		client:  fixture,
		store:   store,
		now:     func() time.Time { return verificationTime },
	}

	cmd, err := verifier.VerifyAndNormalize(cryptoTriggerRequest(cryptoTestTxID), billing.ProviderCrypto)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Provider != billing.ProviderCrypto || cmd.ProviderEventID != cryptoTestTxID || cmd.ProviderTransactionID != cryptoTestTxID || cmd.OrderID != "ord_crypto_ci" || cmd.EventType != "TRC20_TRANSFER_SOLIDIFIED" || cmd.Outcome != billing.TransactionPaid || cmd.Money != (billing.Money{Currency: "USD", AmountMinor: 1234}) || cmd.ReceivedAt != time.UnixMilli(1788967800000).UTC() || cmd.CorrelationID == "" {
		t.Fatalf("unexpected command: %+v", cmd)
	}
	if store.resolveCalls != 1 || store.orderCalls != 1 || !store.lastResolveAt.Equal(verificationTime) {
		t.Fatalf("store calls resolve=%d order=%d at=%s", store.resolveCalls, store.orderCalls, store.lastResolveAt)
	}
	if fixture.calls != 1 || fixture.lastURL != tronGridAPIBase+tronGridSolidifiedReceiptPath || fixture.lastAPIKey != "ci-trongrid-api-key" {
		t.Fatalf("fixture calls=%d url=%q key=%q", fixture.calls, fixture.lastURL, fixture.lastAPIKey)
	}
	var lookup struct {
		Value string `json:"value"`
	}
	if json.Unmarshal(fixture.lastRequest, &lookup) != nil || lookup.Value != cryptoTestTxID {
		t.Fatalf("unexpected lookup body: %s", fixture.lastRequest)
	}
	ack := verifier.SuccessAcknowledgement(billing.ProviderCrypto)
	if ack.StatusCode != http.StatusOK || ack.ContentType != "" || len(ack.Body) != 0 {
		t.Fatalf("unexpected crypto ack: %+v", ack)
	}
}

func TestCryptoCallbackTriggerStrictness(t *testing.T) {
	valid := `{"txid":"` + cryptoTestTxID + `"}`
	for name, raw := range map[string]string{
		"empty":         `{}`,
		"unknown":       `{"txid":"` + cryptoTestTxID + `","order_id":"ord"}`,
		"duplicate":     `{"txid":"` + cryptoTestTxID + `","txid":"` + cryptoTestTxID + `"}`,
		"uppercase":     `{"txid":"` + strings.ToUpper(cryptoTestTxID) + `"}`,
		"short":         `{"txid":"abcd"}`,
		"wrong_type":    `{"txid":123}`,
		"trailing":      valid + `{}`,
		"top_level_arr": `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			if txid, ok := parseCryptoTrigger([]byte(raw)); ok || txid != "" {
				t.Fatalf("trigger accepted txid=%q raw=%s", txid, raw)
			}
		})
	}
	if txid, ok := parseCryptoTrigger([]byte(valid)); !ok || txid != cryptoTestTxID {
		t.Fatalf("valid trigger rejected txid=%q ok=%v", txid, ok)
	}
}

func TestCryptoCallbackVerifierFailClosed(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		err  error
	}{
		{name: "empty receipt unavailable", body: []byte(`{}`), err: billing.ErrCallbackUnavailable},
		{name: "failed receipt", body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 1, tronUSDTContractLogAddress, "FAILED", ""), err: billing.ErrCallbackUnauthorized},
		{name: "explicit top-level failure", body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 1, tronUSDTContractLogAddress, "SUCCESS", "FAILED"), err: billing.ErrCallbackUnauthorized},
		{name: "wrong contract", body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 1, "0000000000000000000000000000000000000000", "SUCCESS", ""), err: billing.ErrCallbackUnauthorized},
		{name: "wrong amount", body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 99990000, 1, tronUSDTContractLogAddress, "SUCCESS", ""), err: billing.ErrCallbackUnauthorized},
		{name: "multiple matching transfers", body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 2, tronUSDTContractLogAddress, "SUCCESS", ""), err: billing.ErrCallbackUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := cryptoAuthorityStore()
			fixture := &cryptoHTTPFixture{body: test.body}
			verifier := cryptoCallbackVerifier{apiBase: tronGridAPIBase, apiKey: "ci-trongrid-api-key", client: fixture, store: store, now: time.Now}
			if _, err := verifier.VerifyAndNormalize(cryptoTriggerRequest(cryptoTestTxID), billing.ProviderCrypto); !errors.Is(err, test.err) {
				t.Fatalf("err=%v want=%v", err, test.err)
			}
		})
	}
}

func TestCryptoCallbackVerifierDependencyFailures(t *testing.T) {
	store := cryptoAuthorityStore()
	fixture := &cryptoHTTPFixture{status: http.StatusServiceUnavailable, body: []byte(`{"error":"busy"}`)}
	verifier := cryptoCallbackVerifier{apiBase: tronGridAPIBase, apiKey: "ci-trongrid-api-key", client: fixture, store: store, now: time.Now}
	if _, err := verifier.VerifyAndNormalize(cryptoTriggerRequest(cryptoTestTxID), billing.ProviderCrypto); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("HTTP dependency err=%v", err)
	}

	store = cryptoAuthorityStore()
	store.resolveErr = errors.New("database unavailable")
	fixture = &cryptoHTTPFixture{body: cryptoReceiptFixture(cryptoTestTxID, cryptoTestRecipient, 12340000, 1, tronUSDTContractLogAddress, "SUCCESS", "")}
	verifier = cryptoCallbackVerifier{apiBase: tronGridAPIBase, apiKey: "ci-trongrid-api-key", client: fixture, store: store, now: time.Now}
	if _, err := verifier.VerifyAndNormalize(cryptoTriggerRequest(cryptoTestTxID), billing.ProviderCrypto); !errors.Is(err, billing.ErrCallbackUnavailable) {
		t.Fatalf("store dependency err=%v", err)
	}
}

func TestParseUSDTTransferCandidate(t *testing.T) {
	log := cryptoTransferLog(cryptoTestRecipient, 12340000, tronUSDTContractLogAddress)
	recipient, amount, ok := parseUSDTTransferCandidate(log)
	if !ok || recipient != cryptoTestRecipient || amount != 12340000 {
		t.Fatalf("recipient=%q amount=%d ok=%v", recipient, amount, ok)
	}
	log.Data = strings.Repeat("f", 64)
	if _, _, ok := parseUSDTTransferCandidate(log); ok {
		t.Fatal("uint256 larger than int64 accepted")
	}
}

func cryptoAuthorityStore() *cryptoStoreFixture {
	scale := uint8(6)
	return &cryptoStoreFixture{
		expectedRef: cryptoTestRecipient,
		intent: billing.ProviderIntent{
			ID:                    "pint_crypto_ci",
			WorkspaceID:           "ws_crypto_ci",
			OrderID:               "ord_crypto_ci",
			Provider:              billing.ProviderCrypto,
			MerchantReference:     "gj_crypto_ci",
			ProviderReference:     cryptoTestRecipient,
			SettlementAssetKind:   billing.SettlementAssetToken,
			SettlementAsset:       cryptoUSDTAsset,
			SettlementAmountUnits: 12340000,
			SettlementScale:       &scale,
			Status:                billing.ProviderIntentActive,
		},
		order: billing.Order{
			ID:          "ord_crypto_ci",
			WorkspaceID: "ws_crypto_ci",
			Money:       billing.Money{Currency: "USD", AmountMinor: 1234},
			Status:      billing.OrderPending,
		},
	}
}

func cryptoTriggerRequest(txid string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/crypto", strings.NewReader(`{"txid":"`+txid+`"}`))
}

func cryptoReceiptFixture(txid, recipient string, amount int64, copies int, contract, receiptResult, topResult string) []byte {
	logs := make([]tronEventLog, 0, copies)
	for i := 0; i < copies; i++ {
		logs = append(logs, cryptoTransferLog(recipient, amount, contract))
	}
	payload := map[string]any{
		"id":             txid,
		"blockTimeStamp": int64(1788967800000),
		"receipt":        map[string]any{"result": receiptResult},
		"log":            logs,
	}
	if topResult != "" {
		payload["result"] = topResult
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

func cryptoTransferLog(recipient string, amount int64, contract string) tronEventLog {
	return tronEventLog{
		Address: contract,
		Topics: []string{
			tronTransferTopic,
			strings.Repeat("0", 64),
			strings.Repeat("0", 24) + recipient[2:],
		},
		Data: fmt.Sprintf("%064x", amount),
	}
}
