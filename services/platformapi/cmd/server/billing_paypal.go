package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

const (
	maxPayPalVerificationResponseBytes = 64 << 10
	maxPayPalAccessTokenBytes          = 4 << 10
)

type paypalProviderIntentResolver interface {
	ResolveProviderIntentByProviderReference(context.Context, billing.Provider, string, time.Time) (billing.ProviderIntent, error)
}

type paypalHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type paypalCallbackVerifier struct {
	apiBase      string
	clientID     string
	clientSecret string
	webhookID    string
	client       paypalHTTPDoer
	store        paypalProviderIntentResolver
	now          func() time.Time
}

type paypalWebhookHeaders struct {
	AuthAlgo         string
	CertURL          string
	TransmissionID   string
	TransmissionSig  string
	TransmissionTime string
}

type paypalWebhookEvent struct {
	ID         string          `json:"id"`
	EventType  string          `json:"event_type"`
	CreateTime string          `json:"create_time"`
	Resource   json.RawMessage `json:"resource"`
}

type paypalCapture struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Amount struct {
		CurrencyCode string `json:"currency_code"`
		Value        string `json:"value"`
	} `json:"amount"`
	SupplementaryData struct {
		RelatedIDs struct {
			OrderID string `json:"order_id"`
		} `json:"related_ids"`
	} `json:"supplementary_data"`
}

type paypalAccessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type paypalVerificationResponse struct {
	VerificationStatus string `json:"verification_status"`
}

func buildProductionPayPalCallbackVerifier(store paypalProviderIntentResolver) (paypalCallbackVerifier, bool, error) {
	if os.Getenv("GOJET_BILLING_PAYPAL_ENABLED") != "1" {
		return paypalCallbackVerifier{}, false, nil
	}
	apiBase, apiHost, ok := paypalAPIOrigin(os.Getenv("GOJET_BILLING_PAYPAL_ENV"))
	if !ok {
		return paypalCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	clientID := os.Getenv("GOJET_BILLING_PAYPAL_CLIENT_ID")
	clientSecret := os.Getenv("GOJET_BILLING_PAYPAL_CLIENT_SECRET")
	webhookID := os.Getenv("GOJET_BILLING_PAYPAL_WEBHOOK_ID")
	if store == nil || !validPayPalCredential(clientID, 1024) || !validPayPalCredential(clientSecret, 1024) || !canonicalPayPalIdentifier(webhookID, 50) {
		return paypalCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	return paypalCallbackVerifier{
		apiBase:      apiBase,
		clientID:     clientID,
		clientSecret: clientSecret,
		webhookID:    webhookID,
		client:       newProductionPayPalHTTPClient(apiHost),
		store:        store,
		now:          time.Now,
	}, true, nil
}

func (v paypalCallbackVerifier) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	if r == nil || r.Method != http.MethodPost || provider != billing.ProviderPayPal || v.store == nil || v.client == nil || v.apiBase == "" || v.clientID == "" || v.clientSecret == "" || v.webhookID == "" {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProductionCallbackBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductionCallbackBodyBytes || !json.Valid(raw) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	headers, ok := extractPayPalWebhookHeaders(r)
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	verified, err := v.verifyWithPayPal(r.Context(), headers, raw)
	if err != nil {
		return billing.CallbackCommand{}, err
	}
	if !verified {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	var event paypalWebhookEvent
	if err := json.Unmarshal(raw, &event); err != nil || !canonicalPayPalIdentifier(event.ID, 191) || event.EventType == "" || len(event.EventType) > 96 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	eventTime, err := time.Parse(time.RFC3339Nano, event.CreateTime)
	if err != nil || eventTime.IsZero() {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	now := time.Now
	if v.now != nil {
		now = v.now
	}
	verificationTime := now().UTC()

	var outcome billing.TransactionStatus
	var expectedCaptureStatus string
	switch event.EventType {
	case "PAYMENT.CAPTURE.COMPLETED":
		outcome = billing.TransactionPaid
		expectedCaptureStatus = "COMPLETED"
	case "PAYMENT.CAPTURE.DENIED":
		outcome = billing.TransactionFailed
		expectedCaptureStatus = "DENIED"
	default:
		// Refund and non-terminal payment events remain payment-side settlement-ignore.
		// P20-DR001 has not frozen PayPal refund settlement semantics.
		return billing.CallbackCommand{}, billing.ErrCallbackIgnored
	}

	var capture paypalCapture
	if len(event.Resource) == 0 || json.Unmarshal(event.Resource, &capture) != nil || !canonicalPayPalIdentifier(capture.ID, 191) || capture.Status != expectedCaptureStatus {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	payPalOrderID := capture.SupplementaryData.RelatedIDs.OrderID
	if !canonicalPayPalIdentifier(payPalOrderID, 191) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	amountMinor, currency, ok := parsePayPalAmountMinor(capture.Amount.CurrencyCode, capture.Amount.Value)
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	intent, err := v.store.ResolveProviderIntentByProviderReference(r.Context(), billing.ProviderPayPal, payPalOrderID, verificationTime)
	if err != nil {
		switch {
		case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrConflict), errors.Is(err, billing.ErrInvalidInput):
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		default:
			return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
		}
	}
	if intent.Provider != billing.ProviderPayPal || intent.ProviderReference != payPalOrderID || intent.SettlementAssetKind != billing.SettlementAssetFiat || intent.SettlementAsset != currency || intent.SettlementAmountUnits != amountMinor || intent.SettlementScale != nil || intent.OrderID == "" {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	return billing.CallbackCommand{
		Provider:              billing.ProviderPayPal,
		ProviderEventID:       event.ID,
		ProviderTransactionID: capture.ID,
		OrderID:               intent.OrderID,
		EventType:             event.EventType,
		Outcome:               outcome,
		Money: billing.Money{
			Currency:    currency,
			AmountMinor: amountMinor,
		},
		ReceivedAt:    eventTime.UTC(),
		CorrelationID: paypalCorrelationID(event.ID, capture.ID, intent.OrderID),
	}, nil
}

func (v paypalCallbackVerifier) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	if provider != billing.ProviderPayPal {
		return billing.CallbackAcknowledgement{}
	}
	return billing.CallbackAcknowledgement{StatusCode: http.StatusOK}
}

func (v paypalCallbackVerifier) verifyWithPayPal(ctx context.Context, headers paypalWebhookHeaders, raw []byte) (bool, error) {
	token, err := v.fetchPayPalAccessToken(ctx)
	if err != nil {
		return false, err
	}
	payload, err := buildPayPalVerificationPayload(headers, v.webhookID, raw)
	if err != nil {
		return false, billing.ErrCallbackUnauthorized
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.apiBase+"/v1/notifications/verify-webhook-signature", bytes.NewReader(payload))
	if err != nil {
		return false, billing.ErrCallbackUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.client.Do(req)
	if err != nil {
		return false, billing.ErrCallbackUnavailable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayPalVerificationResponseBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxPayPalVerificationResponseBytes || resp.StatusCode != http.StatusOK {
		return false, billing.ErrCallbackUnavailable
	}
	var verification paypalVerificationResponse
	if json.Unmarshal(body, &verification) != nil {
		return false, billing.ErrCallbackUnavailable
	}
	switch verification.VerificationStatus {
	case "SUCCESS":
		return true, nil
	case "FAILURE":
		return false, nil
	default:
		return false, billing.ErrCallbackUnavailable
	}
}

func (v paypalCallbackVerifier) fetchPayPalAccessToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.apiBase+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return "", billing.ErrCallbackUnavailable
	}
	req.SetBasicAuth(v.clientID, v.clientSecret)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := v.client.Do(req)
	if err != nil {
		return "", billing.ErrCallbackUnavailable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayPalVerificationResponseBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxPayPalVerificationResponseBytes || resp.StatusCode != http.StatusOK {
		return "", billing.ErrCallbackUnavailable
	}
	var token paypalAccessTokenResponse
	if json.Unmarshal(body, &token) != nil || !strings.EqualFold(token.TokenType, "Bearer") || !validPayPalCredential(token.AccessToken, maxPayPalAccessTokenBytes) {
		return "", billing.ErrCallbackUnavailable
	}
	return token.AccessToken, nil
}

func extractPayPalWebhookHeaders(r *http.Request) (paypalWebhookHeaders, bool) {
	var headers paypalWebhookHeaders
	var ok bool
	if headers.AuthAlgo, ok = singlePayPalHeader(r, "PayPal-Auth-Algo", 100); !ok {
		return paypalWebhookHeaders{}, false
	}
	if headers.CertURL, ok = singlePayPalHeader(r, "PayPal-Cert-Url", 500); !ok {
		return paypalWebhookHeaders{}, false
	}
	if headers.TransmissionID, ok = singlePayPalHeader(r, "PayPal-Transmission-Id", 50); !ok {
		return paypalWebhookHeaders{}, false
	}
	if headers.TransmissionSig, ok = singlePayPalHeader(r, "PayPal-Transmission-Sig", 500); !ok {
		return paypalWebhookHeaders{}, false
	}
	if headers.TransmissionTime, ok = singlePayPalHeader(r, "PayPal-Transmission-Time", 100); !ok {
		return paypalWebhookHeaders{}, false
	}
	return headers, true
}

func singlePayPalHeader(r *http.Request, name string, maxLen int) (string, bool) {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	value := values[0]
	return value, value != "" && len(value) <= maxLen && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
}

func buildPayPalVerificationPayload(headers paypalWebhookHeaders, webhookID string, raw []byte) ([]byte, error) {
	if !json.Valid(raw) || !canonicalPayPalIdentifier(webhookID, 50) {
		return nil, billing.ErrCallbackUnauthorized
	}
	fields := []struct {
		name  string
		value string
	}{
		{name: "auth_algo", value: headers.AuthAlgo},
		{name: "cert_url", value: headers.CertURL},
		{name: "transmission_id", value: headers.TransmissionID},
		{name: "transmission_sig", value: headers.TransmissionSig},
		{name: "transmission_time", value: headers.TransmissionTime},
		{name: "webhook_id", value: webhookID},
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for i, field := range fields {
		if field.value == "" {
			return nil, billing.ErrCallbackUnauthorized
		}
		if i > 0 {
			out.WriteByte(',')
		}
		name, _ := json.Marshal(field.name)
		value, _ := json.Marshal(field.value)
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteString(`,"webhook_event":`)
	out.Write(raw)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func paypalAPIOrigin(environment string) (string, string, bool) {
	switch environment {
	case "live":
		return "https://api-m.paypal.com", "api-m.paypal.com", true
	case "sandbox":
		return "https://api-m.sandbox.paypal.com", "api-m.sandbox.paypal.com", true
	default:
		return "", "", false
	}
}

func newProductionPayPalHTTPClient(expectedHost string) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host != expectedHost || port != "443" {
			return nil, errors.New("paypal outbound destination rejected")
		}
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, expectedHost)
		if err != nil || len(resolved) == 0 {
			return nil, errors.New("paypal outbound resolution failed")
		}
		var lastErr error
		for _, item := range resolved {
			ip := item.IP
			if !publicPayPalDestinationIP(ip) || (strings.HasSuffix(network, "4") && ip.To4() == nil) || (strings.HasSuffix(network, "6") && (ip.To16() == nil || ip.To4() != nil)) {
				continue
			}
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("paypal outbound destination rejected")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func publicPayPalDestinationIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}

func validPayPalCredential(value string, maxLen int) bool {
	return value != "" && len(value) <= maxLen && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
}

func canonicalPayPalIdentifier(value string, maxLen int) bool {
	if len(value) == 0 || len(value) > maxLen || value != strings.TrimSpace(value) {
		return false
	}
	for _, ch := range value {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '_', ch == '-', ch == '.', ch == ':':
		default:
			return false
		}
	}
	return true
}

var paypalCurrencyScale = map[string]uint8{
	"AUD": 2, "BRL": 2, "CAD": 2, "CNY": 2, "CZK": 2, "DKK": 2,
	"EUR": 2, "HKD": 2, "HUF": 0, "ILS": 2, "JPY": 0, "MYR": 2,
	"MXN": 2, "TWD": 0, "NZD": 2, "NOK": 2, "PHP": 2, "PLN": 2,
	"GBP": 2, "RUB": 2, "SGD": 2, "SEK": 2, "CHF": 2, "THB": 2,
	"USD": 2,
}

func parsePayPalAmountMinor(currency, value string) (int64, string, bool) {
	if currency != strings.TrimSpace(currency) || len(currency) != 3 {
		return 0, "", false
	}
	scale, ok := paypalCurrencyScale[currency]
	if !ok || value == "" || value != strings.TrimSpace(value) || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, "", false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 16 || !allASCIIDigits(parts[0]) {
		return 0, "", false
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || !allASCIIDigits(fraction) {
			return 0, "", false
		}
	}
	if scale == 0 {
		if fraction != "" {
			return 0, "", false
		}
	} else {
		if len(fraction) > int(scale) {
			return 0, "", false
		}
		for len(fraction) < int(scale) {
			fraction += "0"
		}
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", false
	}
	multiplier := int64(1)
	for i := uint8(0); i < scale; i++ {
		multiplier *= 10
	}
	minor := int64(0)
	if fraction != "" {
		minor, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, "", false
		}
	}
	if major > (math.MaxInt64-minor)/multiplier {
		return 0, "", false
	}
	amount := major*multiplier + minor
	if amount <= 0 {
		return 0, "", false
	}
	return amount, currency, true
}

func paypalCorrelationID(eventID, captureID, orderID string) string {
	sum := sha256.Sum256([]byte("paypal\n" + eventID + "\n" + captureID + "\n" + orderID))
	return "paypal:" + hex.EncodeToString(sum[:])
}
