package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
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
	p20PayPalOrderID            = "ord_paypal_ci"
	p20PayPalProviderOrderID    = "5O190127TN364715T"
	p20PayPalCaptureID          = "CAPTURE-PAYPAL-CI-001"
	p20PayPalPaidEventID        = "WH-P20-PAYPAL-CI-PAID"
	p20PayPalWebhookID          = "WH-P20-PAYPAL-CI-001"
	p20PayPalSandboxAPIOrigin   = "https://api-m.sandbox.paypal.com"
	p20PayPalVerificationFailed = "fixture-verification-failure"
)

type p20PayPalAuthorityFixture struct {
	clientID     string
	clientSecret string
	webhookID    string
	expectedRaw  []byte
	tokenCalls   int
	verifyCalls  int
}

type p20PayPalRuntimeEvidence struct {
	PaidStatus                int            `json:"paid_status"`
	DuplicateStatus           int            `json:"duplicate_status"`
	RefundIgnoredStatus       int            `json:"refund_ignored_status"`
	BadAmountStatus           int            `json:"bad_amount_status"`
	VerificationFailureStatus int            `json:"verification_failure_status"`
	OAuthCalls                int            `json:"oauth_calls"`
	VerificationCalls         int            `json:"verification_calls"`
	FixedAPIOrigin            string         `json:"fixed_api_origin"`
	ProductionBuilder         bool           `json:"production_builder"`
	LivePayPalNetwork         bool           `json:"live_paypal_network"`
	ProviderPostbackFixture   bool           `json:"provider_postback_fixture"`
	Durable                   map[string]any `json:"durable"`
}

func (f *p20PayPalAuthorityFixture) Do(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, errors.New("nil PayPal fixture request")
	}
	if req.Method != http.MethodPost || req.URL.Scheme != "https" || req.URL.Host != "api-m.sandbox.paypal.com" {
		return nil, fmt.Errorf("unexpected PayPal fixture destination %s %s", req.Method, req.URL.String())
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	switch req.URL.Path {
	case "/v1/oauth2/token":
		f.tokenCalls++
		user, password, ok := req.BasicAuth()
		if !ok || user != f.clientID || password != f.clientSecret {
			return nil, errors.New("PayPal OAuth client credentials mismatch")
		}
		if string(body) != "grant_type=client_credentials" || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			return nil, errors.New("PayPal OAuth request contract mismatch")
		}
		return p20PayPalFixtureResponse(http.StatusOK, `{"access_token":"p20-paypal-ci-access-token","token_type":"Bearer"}`), nil
	case "/v1/notifications/verify-webhook-signature":
		f.verifyCalls++
		if req.Header.Get("Authorization") != "Bearer p20-paypal-ci-access-token" || req.Header.Get("Content-Type") != "application/json" {
			return nil, errors.New("PayPal verification authorization contract mismatch")
		}
		var payload struct {
			AuthAlgo         string          `json:"auth_algo"`
			CertURL          string          `json:"cert_url"`
			TransmissionID   string          `json:"transmission_id"`
			TransmissionSig  string          `json:"transmission_sig"`
			TransmissionTime string          `json:"transmission_time"`
			WebhookID        string          `json:"webhook_id"`
			WebhookEvent     json.RawMessage `json:"webhook_event"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode PayPal verification payload: %w", err)
		}
		if payload.AuthAlgo != "SHA256withRSA" || payload.CertURL != "https://api.paypal.com/v1/notifications/certs/CERT-P20-CI" || payload.TransmissionID == "" || payload.TransmissionTime == "" || payload.WebhookID != f.webhookID {
			return nil, errors.New("PayPal verification postback authority fields mismatch")
		}
		if !bytes.Equal(payload.WebhookEvent, f.expectedRaw) {
			return nil, errors.New("PayPal verification postback did not preserve raw webhook event bytes")
		}
		result := "SUCCESS"
		if payload.TransmissionSig == p20PayPalVerificationFailed {
			result = "FAILURE"
		} else if payload.TransmissionSig != "fixture-verification-success" {
			return nil, errors.New("unexpected PayPal fixture verification signature")
		}
		return p20PayPalFixtureResponse(http.StatusOK, `{"verification_status":"`+result+`"}`), nil
	default:
		return nil, fmt.Errorf("unexpected PayPal fixture path %q", req.URL.Path)
	}
}

func p20PayPalFixtureResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestP20D016PayPalCallbackAuthority(t *testing.T) {
	if os.Getenv("GOJET_P20_D016_PAYPAL_AUTHORITY") != "1" {
		t.Skip("P20-D016 PayPal authority is CI-only")
	}
	if os.Getenv("GOJET_BILLING_PAYPAL_ENV") != "sandbox" {
		t.Fatal("authority requires fixed PayPal sandbox environment")
	}
	clientID := os.Getenv("GOJET_BILLING_PAYPAL_CLIENT_ID")
	clientSecret := os.Getenv("GOJET_BILLING_PAYPAL_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" || os.Getenv("GOJET_BILLING_PAYPAL_WEBHOOK_ID") != p20PayPalWebhookID {
		t.Fatal("authority PayPal credentials/webhook configuration missing")
	}

	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	store := billing.NewStore(db)
	verifier, enabled, err := buildProductionPayPalCallbackVerifier(store)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || verifier.apiBase != p20PayPalSandboxAPIOrigin {
		t.Fatalf("production builder enabled=%v apiBase=%q", enabled, verifier.apiBase)
	}
	if _, ok := verifier.client.(*http.Client); !ok {
		t.Fatalf("production builder client=%T", verifier.client)
	}

	fixture := &p20PayPalAuthorityFixture{
		clientID:     clientID,
		clientSecret: clientSecret,
		webhookID:    p20PayPalWebhookID,
	}
	// CI deliberately replaces only the outbound PayPal network dependency after
	// the production builder has fixed the official sandbox origin and built the
	// hardened production HTTP client. This is provider-postback fixture authority,
	// not a claim of live PayPal network authority.
	verifier.client = fixture
	api := billing.NewAPI(store, nil, nil, verifier).Handler()

	paidRaw := p20PayPalAuthorityEvent(p20PayPalPaidEventID, "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "12.34")
	paidStatus := p20PayPalAuthorityRequest(t, api, fixture, paidRaw, "fixture-verification-success")
	if paidStatus != http.StatusOK {
		t.Fatalf("paid status=%d", paidStatus)
	}
	duplicateStatus := p20PayPalAuthorityRequest(t, api, fixture, paidRaw, "fixture-verification-success")
	if duplicateStatus != http.StatusOK {
		t.Fatalf("duplicate status=%d", duplicateStatus)
	}

	refundRaw := p20PayPalAuthorityEvent("WH-P20-PAYPAL-CI-REFUND-IGNORED", "PAYMENT.CAPTURE.REFUNDED", "REFUNDED", "12.34")
	refundIgnoredStatus := p20PayPalAuthorityRequest(t, api, fixture, refundRaw, "fixture-verification-success")
	if refundIgnoredStatus != http.StatusOK {
		t.Fatalf("refund ignored status=%d", refundIgnoredStatus)
	}

	badAmountRaw := p20PayPalAuthorityEvent("WH-P20-PAYPAL-CI-BAD-AMOUNT", "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "99.99")
	badAmountStatus := p20PayPalAuthorityRequest(t, api, fixture, badAmountRaw, "fixture-verification-success")
	if badAmountStatus != http.StatusUnauthorized {
		t.Fatalf("bad amount status=%d", badAmountStatus)
	}

	verificationFailureRaw := p20PayPalAuthorityEvent("WH-P20-PAYPAL-CI-VERIFY-FAIL", "PAYMENT.CAPTURE.COMPLETED", "COMPLETED", "12.34")
	verificationFailureStatus := p20PayPalAuthorityRequest(t, api, fixture, verificationFailureRaw, p20PayPalVerificationFailed)
	if verificationFailureStatus != http.StatusUnauthorized {
		t.Fatalf("verification failure status=%d", verificationFailureStatus)
	}
	if fixture.tokenCalls != 5 || fixture.verifyCalls != 5 {
		t.Fatalf("PayPal postback calls oauth=%d verify=%d", fixture.tokenCalls, fixture.verifyCalls)
	}

	durable := p20PayPalDurableEvidence(t, db)
	evidence := p20PayPalRuntimeEvidence{
		PaidStatus:                paidStatus,
		DuplicateStatus:           duplicateStatus,
		RefundIgnoredStatus:       refundIgnoredStatus,
		BadAmountStatus:           badAmountStatus,
		VerificationFailureStatus: verificationFailureStatus,
		OAuthCalls:                fixture.tokenCalls,
		VerificationCalls:         fixture.verifyCalls,
		FixedAPIOrigin:            verifier.apiBase,
		ProductionBuilder:         true,
		LivePayPalNetwork:         false,
		ProviderPostbackFixture:   true,
		Durable:                   durable,
	}
	if path := os.Getenv("GOJET_P20_D016_PAYPAL_RUNTIME_JSON"); path != "" {
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

func p20PayPalAuthorityRequest(t *testing.T, handler http.Handler, fixture *p20PayPalAuthorityFixture, raw []byte, transmissionSig string) int {
	t.Helper()
	fixture.expectedRaw = append(fixture.expectedRaw[:0], raw...)
	r := httptest.NewRequest(http.MethodPost, "/api/payments/callbacks/paypal", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("PayPal-Auth-Algo", "SHA256withRSA")
	r.Header.Set("PayPal-Cert-Url", "https://api.paypal.com/v1/notifications/certs/CERT-P20-CI")
	r.Header.Set("PayPal-Transmission-Id", fmt.Sprintf("p20-paypal-ci-%d", fixture.verifyCalls+1))
	r.Header.Set("PayPal-Transmission-Sig", transmissionSig)
	r.Header.Set("PayPal-Transmission-Time", time.Now().UTC().Format(time.RFC3339))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	result := w.Result()
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode == http.StatusOK {
		if len(body) != 0 || result.Header.Get("Content-Type") != "" {
			t.Fatalf("PayPal success ACK status=%d content-type=%q body=%q", result.StatusCode, result.Header.Get("Content-Type"), string(body))
		}
	}
	return result.StatusCode
}

func p20PayPalAuthorityEvent(eventID, eventType, captureStatus, amount string) []byte {
	resource := map[string]any{
		"id":     p20PayPalCaptureID,
		"status": captureStatus,
		"amount": map[string]any{
			"currency_code": "USD",
			"value":         amount,
		},
		"supplementary_data": map[string]any{
			"related_ids": map[string]any{
				"order_id": p20PayPalProviderOrderID,
			},
		},
	}
	event := map[string]any{
		"id":          eventID,
		"event_type":  eventType,
		"create_time": time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano),
		"resource":    resource,
	}
	raw, err := json.Marshal(event)
	if err != nil {
		panic(err)
	}
	return raw
}

func p20PayPalDurableEvidence(t *testing.T, db *sql.DB) map[string]any {
	t.Helper()
	ctx := t.Context()
	var orderStatus, invoiceStatus string
	var paidAt bool
	if err := db.QueryRowContext(ctx, `SELECT status FROM billing_orders WHERE id=?`, p20PayPalOrderID).Scan(&orderStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT status,paid_at IS NOT NULL FROM billing_invoices WHERE id='inv_paypal_ci'`).Scan(&invoiceStatus, &paidAt); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	queries := map[string]string{
		"transaction_count":             `SELECT COUNT(*) FROM billing_transactions WHERE provider='paypal' AND provider_transaction_id='CAPTURE-PAYPAL-CI-001' AND order_id='ord_paypal_ci' AND currency='USD' AND amount_minor=1234 AND status='paid'`,
		"callback_event_count":          `SELECT COUNT(*) FROM payment_callback_events WHERE provider='paypal' AND provider_event_id='WH-P20-PAYPAL-CI-PAID' AND status='processed'`,
		"active_subscription_count":     `SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id='ws_paypal_ci' AND plan_id=900003 AND status='active'`,
		"active_entitlement_count":      `SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id='ws_paypal_ci' AND capability='custom_domains' AND source_type='billing' AND limit_value=5 AND revoked_at IS NULL`,
		"active_domain_source_count":    `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id='ws_paypal_ci' AND source='plan' AND source_key='p13:billing' AND status='active' AND domain_limit=5`,
		"payment_notification_count":    `SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id='ws_paypal_ci' AND category='billing' AND event_key='payment_succeeded' AND resource_id='ord_paypal_ci'`,
		"active_provider_intent_count":  `SELECT COUNT(*) FROM billing_provider_intents WHERE id='pint_paypal_ci' AND provider='paypal' AND provider_reference='5O190127TN364715T' AND settlement_asset_kind='fiat' AND settlement_asset='USD' AND settlement_amount_units=1234 AND settlement_scale IS NULL AND status='active'`,
		"refunded_order_count":          `SELECT COUNT(*) FROM billing_orders WHERE id='ord_paypal_ci' AND status='refunded'`,
		"refunded_transaction_count":    `SELECT COUNT(*) FROM billing_transactions WHERE provider='paypal' AND status='refunded'`,
		"non_paid_callback_event_count": `SELECT COUNT(*) FROM payment_callback_events WHERE provider='paypal' AND provider_event_id<>'WH-P20-PAYPAL-CI-PAID'`,
	}
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
	for _, name := range []string{"transaction_count", "callback_event_count", "active_subscription_count", "active_entitlement_count", "active_domain_source_count", "payment_notification_count", "active_provider_intent_count"} {
		if counts[name] != 1 {
			t.Fatalf("%s=%d", name, counts[name])
		}
	}
	for _, name := range []string{"refunded_order_count", "refunded_transaction_count", "non_paid_callback_event_count"} {
		if counts[name] != 0 {
			t.Fatalf("%s=%d", name, counts[name])
		}
	}
	return map[string]any{
		"order_status":                  orderStatus,
		"invoice_status":                invoiceStatus,
		"invoice_paid_at_present":       paidAt,
		"transaction_count":             counts["transaction_count"],
		"callback_event_count":          counts["callback_event_count"],
		"active_subscription_count":     counts["active_subscription_count"],
		"active_entitlement_count":      counts["active_entitlement_count"],
		"active_domain_source_count":    counts["active_domain_source_count"],
		"payment_notification_count":    counts["payment_notification_count"],
		"active_provider_intent_count":  counts["active_provider_intent_count"],
		"refunded_order_count":          counts["refunded_order_count"],
		"refunded_transaction_count":    counts["refunded_transaction_count"],
		"non_paid_callback_event_count": counts["non_paid_callback_event_count"],
	}
}
