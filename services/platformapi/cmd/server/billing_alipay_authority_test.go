package main

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
	_ "github.com/go-sql-driver/mysql"
)

const (
	p20AlipayOrderID       = "ord_alipay_ci"
	p20AlipayPaidNotifyID  = "notify_alipay_ci_paid"
	p20AlipayFinishNotifyID = "notify_alipay_ci_finished"
	p20AlipayTradeNo       = "2026090922001400010000000001"
	p20AlipayAppID         = "2026000000000001"
	p20AlipaySellerID      = "2088000000000001"
)

type p20AlipayRuntimeEvidence struct {
	PaidStatus          int            `json:"paid_status"`
	DuplicateStatus     int            `json:"duplicate_status"`
	FinishedStatus      int            `json:"finished_status"`
	RefundIgnoredStatus int            `json:"refund_ignored_status"`
	BadAmountStatus     int            `json:"bad_amount_status"`
	BadSignatureStatus  int            `json:"bad_signature_status"`
	ProductionBuilder   bool           `json:"production_builder"`
	ProviderIntentMade  bool           `json:"provider_intent_created"`
	LiveAlipayNetwork   bool           `json:"live_alipay_network"`
	RSA2Fixture         bool           `json:"rsa2_signature_fixture"`
	Durable             map[string]any `json:"durable"`
}

func TestP20D016AlipayCallbackAuthority(t *testing.T) {
	if os.Getenv("GOJET_P20_D016_ALIPAY_AUTHORITY") != "1" {
		t.Skip("P20-D016 Alipay authority is CI-only")
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
	now := time.Now().UTC().Truncate(time.Second)
	expires := now.Add(24 * time.Hour)
	intent, created, err := store.CreateProviderIntent(ctx, billing.CreateProviderIntentInput{
		WorkspaceID: "ws_alipay_ci",
		OrderID:     p20AlipayOrderID,
		Provider:    billing.ProviderAlipay,
		ExpiresAt:   &expires,
		Now:         now,
	})
	if err != nil || !created || intent.Provider != billing.ProviderAlipay || intent.OrderID != p20AlipayOrderID || intent.SettlementAsset != "CNY" || intent.SettlementAmountUnits != 1234 || intent.MerchantReference == "" {
		t.Fatalf("provider intent created=%v intent=%+v err=%v", created, intent, err)
	}

	privateKey, publicPEM := p20AlipayAuthorityKey(t)
	keysRaw, err := json.Marshal(map[string]string{"ci-current": publicPEM})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOJET_BILLING_ALIPAY_ENABLED", "1")
	t.Setenv("GOJET_BILLING_ALIPAY_APP_ID", p20AlipayAppID)
	t.Setenv("GOJET_BILLING_ALIPAY_SELLER_ID", p20AlipaySellerID)
	t.Setenv("GOJET_BILLING_ALIPAY_PUBLIC_KEYS_JSON", string(keysRaw))
	verifier, enabled, err := buildProductionAlipayCallbackVerifier(store)
	if err != nil || !enabled {
		t.Fatalf("production builder enabled=%v err=%v", enabled, err)
	}
	api := billing.NewAPI(store, nil, nil, verifier).Handler()
	notifyTime := now.In(alipayChinaTime).Format(alipayNotifyTimeLayout)

	paid := p20AlipayFields(p20AlipayPaidNotifyID, "TRADE_SUCCESS", intent.MerchantReference, "12.34", notifyTime)
	paidStatus := p20AlipayAuthorityRequest(t, api, privateKey, paid, true)
	if paidStatus != http.StatusOK {
		t.Fatalf("paid status=%d", paidStatus)
	}
	duplicateStatus := p20AlipayAuthorityRequest(t, api, privateKey, paid, true)
	if duplicateStatus != http.StatusOK {
		t.Fatalf("duplicate status=%d", duplicateStatus)
	}
	finished := p20AlipayFields(p20AlipayFinishNotifyID, "TRADE_FINISHED", intent.MerchantReference, "12.34", notifyTime)
	finishedStatus := p20AlipayAuthorityRequest(t, api, privateKey, finished, true)
	if finishedStatus != http.StatusOK {
		t.Fatalf("finished status=%d", finishedStatus)
	}
	refundIgnored := p20AlipayFields("notify_alipay_ci_closed", "TRADE_CLOSED", intent.MerchantReference, "12.34", notifyTime)
	refundIgnored["refund_fee"] = "12.34"
	refundIgnored["out_biz_no"] = "refund_ci_ignored"
	refundIgnoredStatus := p20AlipayAuthorityRequest(t, api, privateKey, refundIgnored, true)
	if refundIgnoredStatus != http.StatusOK {
		t.Fatalf("refund ignored status=%d", refundIgnoredStatus)
	}
	badAmount := p20AlipayFields("notify_alipay_ci_bad_amount", "TRADE_SUCCESS", intent.MerchantReference, "99.99", notifyTime)
	badAmountStatus := p20AlipayAuthorityRequest(t, api, privateKey, badAmount, true)
	if badAmountStatus != http.StatusUnauthorized {
		t.Fatalf("bad amount status=%d", badAmountStatus)
	}
	badSignature := p20AlipayFields("notify_alipay_ci_bad_signature", "TRADE_SUCCESS", intent.MerchantReference, "12.34", notifyTime)
	badSignatureStatus := p20AlipayAuthorityRequest(t, api, privateKey, badSignature, false)
	if badSignatureStatus != http.StatusUnauthorized {
		t.Fatalf("bad signature status=%d", badSignatureStatus)
	}

	durable := p20AlipayDurableEvidence(t, db, intent.ID)
	evidence := p20AlipayRuntimeEvidence{
		PaidStatus:          paidStatus,
		DuplicateStatus:     duplicateStatus,
		FinishedStatus:      finishedStatus,
		RefundIgnoredStatus: refundIgnoredStatus,
		BadAmountStatus:     badAmountStatus,
		BadSignatureStatus:  badSignatureStatus,
		ProductionBuilder:   true,
		ProviderIntentMade:  true,
		LiveAlipayNetwork:   false,
		RSA2Fixture:         true,
		Durable:             durable,
	}
	if path := os.Getenv("GOJET_P20_D016_ALIPAY_RUNTIME_JSON"); path != "" {
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

func p20AlipayAuthorityKey(t *testing.T) (*rsa.PrivateKey, string) {
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

func p20AlipayFields(notifyID, status, merchantReference, amount, notifyTime string) map[string]string {
	return map[string]string{
		"notify_time":  notifyTime,
		"notify_type":  "trade_status_sync",
		"notify_id":    notifyID,
		"sign_type":    "RSA2",
		"trade_no":     p20AlipayTradeNo,
		"app_id":       p20AlipayAppID,
		"out_trade_no": merchantReference,
		"seller_id":    p20AlipaySellerID,
		"trade_status": status,
		"total_amount": amount,
		"subject":      "GoJet CI",
	}
}

func p20AlipayAuthorityRequest(t *testing.T, handler http.Handler, privateKey *rsa.PrivateKey, fields map[string]string, validSignature bool) int {
	t.Helper()
	copyFields := make(map[string]string, len(fields)+1)
	for k, v := range fields {
		copyFields[k] = v
	}
	content, ok := buildAlipaySignatureContent(copyFields)
	if !ok {
		t.Fatal("could not build Alipay authority signing content")
	}
	digest := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if !validSignature {
		signature[0] ^= 0xff
	}
	copyFields["sign"] = base64.StdEncoding.EncodeToString(signature)
	values := make(url.Values, len(copyFields))
	for k, v := range copyFields {
		values.Set(k, v)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/alipay", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	result := w.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode == http.StatusOK {
		if result.Header.Get("Content-Type") != alipaySuccessContentType || string(body) != "success" {
			t.Fatalf("Alipay success ACK status=%d content-type=%q body=%q", result.StatusCode, result.Header.Get("Content-Type"), string(body))
		}
	}
	return result.StatusCode
}

func p20AlipayDurableEvidence(t *testing.T, db *sql.DB, intentID string) map[string]any {
	t.Helper()
	ctx := context.Background()
	var orderStatus, invoiceStatus string
	var paidAt bool
	if err := db.QueryRowContext(ctx, `SELECT status FROM billing_orders WHERE id=?`, p20AlipayOrderID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status,paid_at IS NOT NULL FROM billing_invoices WHERE id='inv_alipay_ci'`).Scan(&invoiceStatus, &paidAt); err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{
		"transaction_count":            `SELECT COUNT(*) FROM billing_transactions WHERE provider='alipay' AND provider_transaction_id='2026090922001400010000000001' AND order_id='ord_alipay_ci' AND currency='CNY' AND amount_minor=1234 AND status='paid'`,
		"callback_event_count":         `SELECT COUNT(*) FROM payment_callback_events WHERE provider='alipay' AND status='processed'`,
		"paid_event_count":             `SELECT COUNT(*) FROM payment_callback_events WHERE provider='alipay' AND provider_event_id IN ('notify_alipay_ci_paid','notify_alipay_ci_finished') AND provider_transaction_id='2026090922001400010000000001' AND status='processed'`,
		"active_subscription_count":    `SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id='ws_alipay_ci' AND plan_id=900004 AND status='active'`,
		"active_entitlement_count":     `SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id='ws_alipay_ci' AND capability='custom_domains' AND source_type='billing' AND limit_value=5 AND revoked_at IS NULL`,
		"active_domain_source_count":   `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id='ws_alipay_ci' AND source='plan' AND source_key='p13:billing' AND status='active' AND domain_limit=5`,
		"payment_notification_count":   `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='ws_alipay_ci' AND category='billing' AND event_key='payment_succeeded' AND resource_id='ord_alipay_ci'`,
		"active_provider_intent_count": fmt.Sprintf(`SELECT COUNT(*) FROM billing_provider_intents WHERE id='%s' AND provider='alipay' AND settlement_asset_kind='fiat' AND settlement_asset='CNY' AND settlement_amount_units=1234 AND settlement_scale IS NULL AND status='active'`, intentID),
		"refunded_order_count":         `SELECT COUNT(*) FROM billing_orders WHERE id='ord_alipay_ci' AND status='refunded'`,
		"refunded_transaction_count":   `SELECT COUNT(*) FROM billing_transactions WHERE provider='alipay' AND status='refunded'`,
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
	for _, name := range []string{"transaction_count", "active_subscription_count", "active_entitlement_count", "active_domain_source_count", "payment_notification_count", "active_provider_intent_count"} {
		if counts[name] != 1 {
			t.Fatalf("%s=%d", name, counts[name])
		}
	}
	if counts["callback_event_count"] != 2 || counts["paid_event_count"] != 2 || counts["refunded_order_count"] != 0 || counts["refunded_transaction_count"] != 0 {
		t.Fatalf("callback/refund counts=%v", counts)
	}
	return map[string]any{
		"order_status":                orderStatus,
		"invoice_status":              invoiceStatus,
		"invoice_paid_at_present":     paidAt,
		"transaction_count":           counts["transaction_count"],
		"callback_event_count":        counts["callback_event_count"],
		"paid_event_count":            counts["paid_event_count"],
		"active_subscription_count":   counts["active_subscription_count"],
		"active_entitlement_count":    counts["active_entitlement_count"],
		"active_domain_source_count":  counts["active_domain_source_count"],
		"payment_notification_count":  counts["payment_notification_count"],
		"active_provider_intent_count": counts["active_provider_intent_count"],
		"refunded_order_count":        counts["refunded_order_count"],
		"refunded_transaction_count":  counts["refunded_transaction_count"],
	}
}
