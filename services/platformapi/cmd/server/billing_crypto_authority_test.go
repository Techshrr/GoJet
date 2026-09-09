package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
	_ "github.com/go-sql-driver/mysql"
)

const (
	p20CryptoOrderID        = "ord_crypto_ci"
	p20CryptoIntentID       = "pint_crypto_ci"
	p20CryptoProviderRef    = "41abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	p20CryptoPaidTxID       = "1111111111111111111111111111111111111111111111111111111111111111"
	p20CryptoBadAmountTxID  = "2222222222222222222222222222222222222222222222222222222222222222"
	p20CryptoWrongTokenTxID = "3333333333333333333333333333333333333333333333333333333333333333"
	p20CryptoMultipleTxID   = "4444444444444444444444444444444444444444444444444444444444444444"
	p20CryptoPendingTxID    = "5555555555555555555555555555555555555555555555555555555555555555"
	p20CryptoAPIKey         = "p20-d016-crypto-fixture-api-key"
)

type p20CryptoRuntimeEvidence struct {
	PaidStatus                   int            `json:"paid_status"`
	DuplicateStatus              int            `json:"duplicate_status"`
	BadAmountStatus              int            `json:"bad_amount_status"`
	WrongTokenStatus             int            `json:"wrong_token_status"`
	MultipleTransferStatus       int            `json:"multiple_transfer_status"`
	UnsolidifiedStatus           int            `json:"unsolidified_status"`
	ProductionBuilder            bool           `json:"production_builder"`
	FixedTronOrigin              string         `json:"fixed_tron_origin"`
	LiveTronNetwork              bool           `json:"live_tron_network"`
	SolidifiedReceiptFixture     bool           `json:"solidified_receipt_fixture"`
	ServerPrecreatedTokenIntent  bool           `json:"server_precreated_token_intent_fixture"`
	GenericCryptoCreationEnabled bool           `json:"generic_crypto_intent_creation_enabled"`
	OriginalFiatMoneyPreserved   bool           `json:"original_fiat_money_preserved"`
	Durable                      map[string]any `json:"durable"`
}

type p20CryptoReceiptFixture struct {
	t              *testing.T
	apiKey         string
	providerRef    string
	calls          int
	requestsByTxID map[string]int
}

func (f *p20CryptoReceiptFixture) Do(req *http.Request) (*http.Response, error) {
	f.t.Helper()
	f.calls++
	if req.Method != http.MethodPost || req.URL.String() != tronGridAPIBase+tronGridSolidifiedReceiptPath {
		f.t.Fatalf("unexpected TronGrid request method=%s url=%s", req.Method, req.URL.String())
	}
	if req.Header.Get("TRON-PRO-API-KEY") != f.apiKey || req.Header.Get("Content-Type") != "application/json" || req.Header.Get("Accept") != "application/json" {
		f.t.Fatalf("unexpected TronGrid headers key=%q content-type=%q accept=%q", req.Header.Get("TRON-PRO-API-KEY"), req.Header.Get("Content-Type"), req.Header.Get("Accept"))
	}
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	var lookup map[string]any
	if json.Unmarshal(raw, &lookup) != nil || len(lookup) != 1 {
		f.t.Fatalf("unexpected TronGrid lookup body=%s", raw)
	}
	txid, ok := lookup["value"].(string)
	if !ok || !cryptoTxIDPattern.MatchString(txid) {
		f.t.Fatalf("unexpected TronGrid lookup txid=%v", lookup["value"])
	}
	f.requestsByTxID[txid]++

	var body []byte
	switch txid {
	case p20CryptoPaidTxID:
		body = p20CryptoSolidifiedReceipt(txid, f.providerRef, 12340000, tronUSDTContractLogAddress, 1)
	case p20CryptoBadAmountTxID:
		body = p20CryptoSolidifiedReceipt(txid, f.providerRef, 99990000, tronUSDTContractLogAddress, 1)
	case p20CryptoWrongTokenTxID:
		body = p20CryptoSolidifiedReceipt(txid, f.providerRef, 12340000, "0000000000000000000000000000000000000000", 1)
	case p20CryptoMultipleTxID:
		body = p20CryptoSolidifiedReceipt(txid, f.providerRef, 12340000, tronUSDTContractLogAddress, 2)
	case p20CryptoPendingTxID:
		body = []byte(`{}`)
	default:
		return nil, fmt.Errorf("unexpected fixture txid %q", txid)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func TestP20D016CryptoCallbackAuthority(t *testing.T) {
	if os.Getenv("GOJET_P20_D016_CRYPTO_AUTHORITY") != "1" {
		t.Skip("P20-D016 Crypto authority is CI-only")
	}
	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := billing.NewStore(db)

	t.Setenv("GOJET_BILLING_CRYPTO_ENABLED", "1")
	t.Setenv("GOJET_BILLING_CRYPTO_TRONGRID_API_KEY", p20CryptoAPIKey)
	verifier, enabled, err := buildProductionCryptoCallbackVerifier(store)
	if err != nil || !enabled {
		t.Fatalf("production builder enabled=%v err=%v", enabled, err)
	}
	if verifier.apiBase != tronGridAPIBase {
		t.Fatalf("production api base=%q", verifier.apiBase)
	}
	if _, ok := verifier.client.(*http.Client); !ok {
		t.Fatalf("production client type=%T", verifier.client)
	}

	// Keep the production builder, fixed Mainnet origin, API-key contract and
	// real Store. Replace only the outbound network dependency with a bounded
	// solidified-receipt fixture. This is not live TronGrid network authority.
	fixture := &p20CryptoReceiptFixture{
		t:              t,
		apiKey:         p20CryptoAPIKey,
		providerRef:    p20CryptoProviderRef,
		requestsByTxID: map[string]int{},
	}
	verifier.client = fixture
	verifier.now = func() time.Time { return time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC) }
	api := billing.NewAPI(store, nil, nil, verifier).Handler()

	paidStatus := p20CryptoAuthorityRequest(t, api, p20CryptoPaidTxID)
	if paidStatus != http.StatusOK {
		t.Fatalf("paid status=%d", paidStatus)
	}
	duplicateStatus := p20CryptoAuthorityRequest(t, api, p20CryptoPaidTxID)
	if duplicateStatus != http.StatusOK {
		t.Fatalf("duplicate status=%d", duplicateStatus)
	}
	badAmountStatus := p20CryptoAuthorityRequest(t, api, p20CryptoBadAmountTxID)
	if badAmountStatus != http.StatusUnauthorized {
		t.Fatalf("bad amount status=%d", badAmountStatus)
	}
	wrongTokenStatus := p20CryptoAuthorityRequest(t, api, p20CryptoWrongTokenTxID)
	if wrongTokenStatus != http.StatusUnauthorized {
		t.Fatalf("wrong token status=%d", wrongTokenStatus)
	}
	multipleStatus := p20CryptoAuthorityRequest(t, api, p20CryptoMultipleTxID)
	if multipleStatus != http.StatusUnauthorized {
		t.Fatalf("multiple transfer status=%d", multipleStatus)
	}
	unsolidifiedStatus := p20CryptoAuthorityRequest(t, api, p20CryptoPendingTxID)
	if unsolidifiedStatus != http.StatusServiceUnavailable {
		t.Fatalf("unsolidified status=%d", unsolidifiedStatus)
	}

	if fixture.calls != 6 || fixture.requestsByTxID[p20CryptoPaidTxID] != 2 {
		t.Fatalf("fixture calls=%d by-txid=%v", fixture.calls, fixture.requestsByTxID)
	}
	durable := p20CryptoDurableEvidence(t, db)
	evidence := p20CryptoRuntimeEvidence{
		PaidStatus:                   paidStatus,
		DuplicateStatus:              duplicateStatus,
		BadAmountStatus:              badAmountStatus,
		WrongTokenStatus:             wrongTokenStatus,
		MultipleTransferStatus:       multipleStatus,
		UnsolidifiedStatus:           unsolidifiedStatus,
		ProductionBuilder:            true,
		FixedTronOrigin:              tronGridAPIBase,
		LiveTronNetwork:              false,
		SolidifiedReceiptFixture:     true,
		ServerPrecreatedTokenIntent:  true,
		GenericCryptoCreationEnabled: false,
		OriginalFiatMoneyPreserved:   true,
		Durable:                      durable,
	}
	if path := os.Getenv("GOJET_P20_D016_CRYPTO_RUNTIME_JSON"); path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func p20CryptoAuthorityRequest(t *testing.T, handler http.Handler, txid string) int {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/crypto", strings.NewReader(`{"txid":"`+txid+`"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	result := w.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode == http.StatusOK && len(body) != 0 {
		t.Fatalf("Crypto success ACK body=%q", string(body))
	}
	return result.StatusCode
}

func p20CryptoSolidifiedReceipt(txid, recipient string, amount int64, contract string, copies int) []byte {
	logs := make([]tronEventLog, 0, copies)
	for i := 0; i < copies; i++ {
		logs = append(logs, tronEventLog{
			Address: contract,
			Topics: []string{
				tronTransferTopic,
				strings.Repeat("0", 64),
				strings.Repeat("0", 24) + recipient[2:],
			},
			Data: fmt.Sprintf("%064x", amount),
		})
	}
	payload := map[string]any{
		"id":             txid,
		"blockTimeStamp": int64(1788967800000),
		"receipt":        map[string]any{"result": "SUCCESS"},
		"log":            logs,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return raw
}

func p20CryptoDurableEvidence(t *testing.T, db *sql.DB) map[string]any {
	t.Helper()
	ctx := context.Background()
	var orderStatus, invoiceStatus string
	var paidAt bool
	if err := db.QueryRowContext(ctx, `SELECT status FROM billing_orders WHERE id=?`, p20CryptoOrderID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status,paid_at IS NOT NULL FROM billing_invoices WHERE id='inv_crypto_ci'`).Scan(&invoiceStatus, &paidAt); err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{
		"transaction_count":            `SELECT COUNT(*) FROM billing_transactions WHERE provider='crypto' AND provider_transaction_id='1111111111111111111111111111111111111111111111111111111111111111' AND order_id='ord_crypto_ci' AND currency='USD' AND amount_minor=1234 AND status='paid'`,
		"callback_event_count":         `SELECT COUNT(*) FROM payment_callback_events WHERE provider='crypto' AND provider_event_id='1111111111111111111111111111111111111111111111111111111111111111' AND status='processed'`,
		"all_crypto_callback_count":    `SELECT COUNT(*) FROM payment_callback_events WHERE provider='crypto'`,
		"active_subscription_count":    `SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id='ws_crypto_ci' AND plan_id=900005 AND status='active'`,
		"active_entitlement_count":     `SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id='ws_crypto_ci' AND capability='custom_domains' AND source_type='billing' AND limit_value=5 AND revoked_at IS NULL`,
		"active_domain_source_count":   `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id='ws_crypto_ci' AND source='plan' AND source_key='p13:billing' AND status='active' AND domain_limit=5`,
		"payment_notification_count":   `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='ws_crypto_ci' AND category='billing' AND event_key='payment_succeeded' AND resource_id='ord_crypto_ci'`,
		"active_provider_intent_count": `SELECT COUNT(*) FROM billing_provider_intents WHERE id='pint_crypto_ci' AND provider='crypto' AND provider_reference='41abcdefabcdefabcdefabcdefabcdefabcdefabcd' AND settlement_asset_kind='token' AND settlement_asset='USDTTRC20' AND settlement_amount_units=12340000 AND settlement_scale=6 AND status='active'`,
		"refunded_order_count":         `SELECT COUNT(*) FROM billing_orders WHERE id='ord_crypto_ci' AND status='refunded'`,
		"refunded_transaction_count":   `SELECT COUNT(*) FROM billing_transactions WHERE provider='crypto' AND status='refunded'`,
	}
	counts := map[string]int{}
	for name, query := range queries {
		var count int
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		counts[name] = count
	}
	if orderStatus != "paid" || invoiceStatus != "paid" || !paidAt {
		t.Fatalf("order=%s invoice=%s paidAt=%v", orderStatus, invoiceStatus, paidAt)
	}
	for _, name := range []string{"transaction_count", "callback_event_count", "all_crypto_callback_count", "active_subscription_count", "active_entitlement_count", "active_domain_source_count", "payment_notification_count", "active_provider_intent_count"} {
		if counts[name] != 1 {
			t.Fatalf("%s=%d", name, counts[name])
		}
	}
	if counts["refunded_order_count"] != 0 || counts["refunded_transaction_count"] != 0 {
		t.Fatalf("refund counts=%v", counts)
	}
	return map[string]any{
		"order_status":                 orderStatus,
		"invoice_status":               invoiceStatus,
		"invoice_paid_at_present":      paidAt,
		"transaction_count":            counts["transaction_count"],
		"callback_event_count":         counts["callback_event_count"],
		"all_crypto_callback_count":    counts["all_crypto_callback_count"],
		"active_subscription_count":    counts["active_subscription_count"],
		"active_entitlement_count":     counts["active_entitlement_count"],
		"active_domain_source_count":   counts["active_domain_source_count"],
		"payment_notification_count":   counts["payment_notification_count"],
		"active_provider_intent_count": counts["active_provider_intent_count"],
		"refunded_order_count":         counts["refunded_order_count"],
		"refunded_transaction_count":   counts["refunded_transaction_count"],
	}
}
