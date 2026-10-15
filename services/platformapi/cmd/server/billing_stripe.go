package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

const (
	maxProductionCallbackBodyBytes = 1 << 20
	stripeSignatureTolerance       = 300 * time.Second
)

type stripeProviderIntentResolver interface {
	ResolveProviderIntentByProviderReference(context.Context, billing.Provider, string, time.Time) (billing.ProviderIntent, error)
}

type productionBillingCallbackDispatcher struct {
	adapters map[billing.Provider]billing.CallbackRequestVerifier
}

func buildProductionBillingCallbackDispatcher(store stripeProviderIntentResolver) (billing.CallbackRequestVerifier, error) {
	dispatcher := productionBillingCallbackDispatcher{adapters: map[billing.Provider]billing.CallbackRequestVerifier{}}
	stripeVerifier, enabled, err := buildProductionStripeCallbackVerifier(store)
	if err != nil {
		return nil, err
	}
	if enabled {
		dispatcher.adapters[billing.ProviderStripe] = stripeVerifier
	}
	return dispatcher, nil
}

func (d productionBillingCallbackDispatcher) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	adapter, ok := d.adapters[provider]
	if !ok || adapter == nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	return adapter.VerifyAndNormalize(r, provider)
}

func (d productionBillingCallbackDispatcher) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	adapter, ok := d.adapters[provider]
	if !ok || adapter == nil {
		return billing.CallbackAcknowledgement{}
	}
	ackProvider, ok := adapter.(billing.CallbackSuccessAcknowledgementProvider)
	if !ok {
		return billing.CallbackAcknowledgement{}
	}
	return ackProvider.SuccessAcknowledgement(provider)
}

type stripeCallbackVerifier struct {
	webhookSecret string
	livemode      bool
	store         stripeProviderIntentResolver
	now           func() time.Time
}

type stripeWebhookEvent struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Type     string `json:"type"`
	Livemode bool   `json:"livemode"`
	Created  int64  `json:"created"`
	Data     struct {
		Object json.RawMessage `json:"object"`
	} `json:"data"`
}

type stripePaymentIntent struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
}

func buildProductionStripeCallbackVerifier(store stripeProviderIntentResolver) (stripeCallbackVerifier, bool, error) {
	if os.Getenv("GOJET_BILLING_STRIPE_ENABLED") != "1" {
		return stripeCallbackVerifier{}, false, nil
	}
	secretKey := os.Getenv("GOJET_BILLING_STRIPE_SECRET_KEY")
	webhookSecret := os.Getenv("GOJET_BILLING_STRIPE_WEBHOOK_SECRET")
	livemodeRaw := os.Getenv("GOJET_BILLING_STRIPE_LIVEMODE")
	if store == nil || secretKey == "" || strings.TrimSpace(secretKey) != secretKey || webhookSecret == "" || strings.TrimSpace(webhookSecret) != webhookSecret || !strings.HasPrefix(webhookSecret, "whsec_") || (livemodeRaw != "0" && livemodeRaw != "1") {
		return stripeCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	return stripeCallbackVerifier{
		webhookSecret: webhookSecret,
		livemode:      livemodeRaw == "1",
		store:         store,
		now:           time.Now,
	}, true, nil
}

func (v stripeCallbackVerifier) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	if r == nil || r.Method != http.MethodPost || provider != billing.ProviderStripe || v.store == nil || v.webhookSecret == "" {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProductionCallbackBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductionCallbackBodyBytes {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	now := time.Now
	if v.now != nil {
		now = v.now
	}
	receivedAt := now().UTC()
	if !verifyStripeSignature(raw, r.Header.Get("Stripe-Signature"), v.webhookSecret, receivedAt) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	var event stripeWebhookEvent
	if err := json.Unmarshal(raw, &event); err != nil || !canonicalStripeIdentifier(event.ID, "evt_") || event.Object != "event" || event.Type == "" || len(event.Type) > 96 || event.Livemode != v.livemode || event.Created <= 0 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	var outcome billing.TransactionStatus
	switch event.Type {
	case "payment_intent.succeeded":
		outcome = billing.TransactionPaid
	case "payment_intent.payment_failed":
		outcome = billing.TransactionFailed
	default:
		return billing.CallbackCommand{}, billing.ErrCallbackIgnored
	}

	var paymentIntent stripePaymentIntent
	if err := json.Unmarshal(event.Data.Object, &paymentIntent); err != nil || paymentIntent.Object != "payment_intent" || !canonicalStripeIdentifier(paymentIntent.ID, "pi_") || paymentIntent.Amount <= 0 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	currency, ok := normalizeStripeCurrency(paymentIntent.Currency)
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	intent, err := v.store.ResolveProviderIntentByProviderReference(r.Context(), billing.ProviderStripe, paymentIntent.ID, receivedAt)
	if err != nil {
		switch {
		case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrConflict), errors.Is(err, billing.ErrInvalidInput):
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		default:
			return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
		}
	}
	if intent.Provider != billing.ProviderStripe || intent.ProviderReference != paymentIntent.ID || intent.SettlementAssetKind != billing.SettlementAssetFiat || intent.SettlementAsset != currency || intent.SettlementAmountUnits != paymentIntent.Amount || intent.SettlementScale != nil || intent.OrderID == "" {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	return billing.CallbackCommand{
		Provider:              billing.ProviderStripe,
		ProviderEventID:       event.ID,
		ProviderTransactionID: paymentIntent.ID,
		OrderID:               intent.OrderID,
		EventType:             event.Type,
		Outcome:               outcome,
		Money: billing.Money{
			Currency:    currency,
			AmountMinor: paymentIntent.Amount,
		},
		ReceivedAt:    receivedAt,
		CorrelationID: stripeCorrelationID(event.ID, paymentIntent.ID, intent.OrderID),
	}, nil
}

func (v stripeCallbackVerifier) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	if provider != billing.ProviderStripe {
		return billing.CallbackAcknowledgement{}
	}
	return billing.CallbackAcknowledgement{StatusCode: http.StatusOK}
}

func verifyStripeSignature(raw []byte, header, secret string, now time.Time) bool {
	timestampRaw, timestamp, signatures, ok := parseStripeSignatureHeader(header)
	if !ok || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestampRaw))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(raw)
	expected := mac.Sum(nil)
	matched := false
	for _, signature := range signatures {
		if hmac.Equal(expected, signature) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	nowUnix := now.UTC().Unix()
	tolerance := int64(stripeSignatureTolerance / time.Second)
	return timestamp >= nowUnix-tolerance && timestamp <= nowUnix+tolerance
}

func parseStripeSignatureHeader(header string) (string, int64, [][]byte, bool) {
	var timestampRaw string
	var timestamp int64
	signatures := make([][]byte, 0, 2)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		key, value, found := strings.Cut(part, "=")
		if !found || value == "" {
			continue
		}
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed <= 0 || (timestampRaw != "" && timestampRaw != value) {
				return "", 0, nil, false
			}
			timestampRaw = value
			timestamp = parsed
		case "v1":
			decoded, err := hex.DecodeString(value)
			if err == nil && len(decoded) == sha256.Size {
				signatures = append(signatures, decoded)
			}
		}
	}
	return timestampRaw, timestamp, signatures, timestampRaw != "" && len(signatures) > 0
}

func canonicalStripeIdentifier(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > 191 {
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

func normalizeStripeCurrency(value string) (string, bool) {
	if len(value) != 3 {
		return "", false
	}
	for _, ch := range value {
		if ch < 'a' || ch > 'z' {
			return "", false
		}
	}
	return strings.ToUpper(value), true
}

func stripeCorrelationID(eventID, paymentIntentID, orderID string) string {
	sum := sha256.Sum256([]byte("stripe\n" + eventID + "\n" + paymentIntentID + "\n" + orderID))
	return "stripe:" + hex.EncodeToString(sum[:])
}
