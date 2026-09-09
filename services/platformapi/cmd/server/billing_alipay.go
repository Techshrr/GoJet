package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

const (
	maxAlipayPublicKeysConfigBytes = 256 << 10
	alipayNotifyTimeLayout         = "2006-01-02 15:04:05"
	alipaySuccessContentType       = "text/plain; charset=utf-8"
)

var alipayChinaTime = time.FixedZone("Asia/Shanghai", 8*60*60)

type alipayProviderIntentResolver interface {
	ResolveProviderIntentByMerchantReference(context.Context, billing.Provider, string, time.Time) (billing.ProviderIntent, error)
}

type alipayCallbackVerifier struct {
	appID      string
	sellerID   string
	publicKeys map[string]*rsa.PublicKey
	store      alipayProviderIntentResolver
	now        func() time.Time
}

func buildProductionAlipayCallbackVerifier(store alipayProviderIntentResolver) (alipayCallbackVerifier, bool, error) {
	if os.Getenv("GOJET_BILLING_ALIPAY_ENABLED") != "1" {
		return alipayCallbackVerifier{}, false, nil
	}
	appID := os.Getenv("GOJET_BILLING_ALIPAY_APP_ID")
	sellerID := os.Getenv("GOJET_BILLING_ALIPAY_SELLER_ID")
	publicKeysRaw := os.Getenv("GOJET_BILLING_ALIPAY_PUBLIC_KEYS_JSON")
	if store == nil || !canonicalAlipayConfigValue(appID, 64) || !canonicalAlipayConfigValue(sellerID, 64) || publicKeysRaw == "" || len(publicKeysRaw) > maxAlipayPublicKeysConfigBytes {
		return alipayCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	publicKeys, err := parseAlipayPublicKeysJSON(publicKeysRaw)
	if err != nil || len(publicKeys) == 0 {
		return alipayCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	return alipayCallbackVerifier{
		appID:      appID,
		sellerID:   sellerID,
		publicKeys: publicKeys,
		store:      store,
		now:        time.Now,
	}, true, nil
}

func (v alipayCallbackVerifier) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	if r == nil || r.Method != http.MethodPost || provider != billing.ProviderAlipay || v.store == nil || v.appID == "" || v.sellerID == "" || len(v.publicKeys) == 0 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProductionCallbackBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductionCallbackBodyBytes {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	mediaType, mediaParams, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if charset, ok := mediaParams["charset"]; ok && !strings.EqualFold(charset, "UTF-8") {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	fields, err := parseAlipayNotificationForm(raw)
	if err != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if fields["notify_type"] != "trade_status_sync" || fields["sign_type"] != "RSA2" || !canonicalAlipayIdentifier(fields["notify_id"], 128) || !canonicalAlipayIdentifier(fields["trade_no"], 64) || !canonicalAlipayMerchantReference(fields["out_trade_no"]) || !canonicalAlipayIdentifier(fields["trade_status"], 32) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if fields["app_id"] != v.appID || fields["seller_id"] != v.sellerID {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if !verifyAlipayRSA2(fields, v.publicKeys) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	amountMinor, ok := parseAlipayAmountMinor(fields["total_amount"])
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	notifyTime, err := time.ParseInLocation(alipayNotifyTimeLayout, fields["notify_time"], alipayChinaTime)
	if err != nil || notifyTime.IsZero() {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	switch fields["trade_status"] {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		// Continue through the server-created provider-intent binding.
	default:
		return billing.CallbackCommand{}, billing.ErrCallbackIgnored
	}

	now := time.Now
	if v.now != nil {
		now = v.now
	}
	verificationTime := now().UTC()
	intent, err := v.store.ResolveProviderIntentByMerchantReference(r.Context(), billing.ProviderAlipay, fields["out_trade_no"], verificationTime)
	if err != nil {
		switch {
		case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrConflict), errors.Is(err, billing.ErrInvalidInput):
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		default:
			return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
		}
	}
	if intent.Provider != billing.ProviderAlipay || intent.MerchantReference != fields["out_trade_no"] || intent.SettlementAssetKind != billing.SettlementAssetFiat || intent.SettlementAsset != "CNY" || intent.SettlementAmountUnits != amountMinor || intent.SettlementScale != nil || intent.OrderID == "" || (intent.ProviderReference != "" && intent.ProviderReference != fields["trade_no"]) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	return billing.CallbackCommand{
		Provider:              billing.ProviderAlipay,
		ProviderEventID:       fields["notify_id"],
		ProviderTransactionID: fields["trade_no"],
		OrderID:               intent.OrderID,
		EventType:             fields["trade_status"],
		Outcome:               billing.TransactionPaid,
		Money: billing.Money{
			Currency:    "CNY",
			AmountMinor: amountMinor,
		},
		ReceivedAt:    notifyTime.UTC(),
		CorrelationID: alipayCorrelationID(fields["notify_id"], fields["trade_no"], intent.OrderID),
	}, nil
}

func (v alipayCallbackVerifier) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	if provider != billing.ProviderAlipay {
		return billing.CallbackAcknowledgement{}
	}
	return billing.CallbackAcknowledgement{
		StatusCode:  http.StatusOK,
		ContentType: alipaySuccessContentType,
		Body:        "success",
	}
}

func parseAlipayNotificationForm(raw []byte) (map[string]string, error) {
	values, err := url.ParseQuery(string(raw))
	if err != nil || len(values) == 0 {
		return nil, billing.ErrCallbackUnauthorized
	}
	fields := make(map[string]string, len(values))
	for key, items := range values {
		if !canonicalAlipayFormKey(key) || len(items) != 1 || strings.ContainsRune(items[0], '\x00') {
			return nil, billing.ErrCallbackUnauthorized
		}
		fields[key] = items[0]
	}
	return fields, nil
}

func verifyAlipayRSA2(fields map[string]string, publicKeys map[string]*rsa.PublicKey) bool {
	signatureRaw := fields["sign"]
	if signatureRaw == "" || len(signatureRaw) > 344 || fields["sign_type"] != "RSA2" || len(publicKeys) == 0 {
		return false
	}
	signature, err := base64.StdEncoding.DecodeString(signatureRaw)
	if err != nil || len(signature) == 0 {
		return false
	}
	content, ok := buildAlipaySignatureContent(fields)
	if !ok {
		return false
	}
	digest := sha256.Sum256([]byte(content))
	for _, publicKey := range publicKeys {
		if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature) == nil {
			return true
		}
	}
	return false
}

func buildAlipaySignatureContent(fields map[string]string) (string, bool) {
	if len(fields) == 0 {
		return "", false
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		if key == "sign" || key == "sign_type" {
			continue
		}
		if !canonicalAlipayFormKey(key) {
			return "", false
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return "", false
	}
	sort.Strings(keys)
	var builder strings.Builder
	for i, key := range keys {
		if i > 0 {
			builder.WriteByte('&')
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		builder.WriteString(fields[key])
	}
	return builder.String(), true
}

func parseAlipayPublicKeysJSON(raw string) (map[string]*rsa.PublicKey, error) {
	if raw == "" || len(raw) > maxAlipayPublicKeysConfigBytes {
		return nil, billing.ErrCallbackUnavailable
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, billing.ErrCallbackUnavailable
	}
	keys := make(map[string]*rsa.PublicKey)
	for decoder.More() {
		labelToken, err := decoder.Token()
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		label, ok := labelToken.(string)
		if !ok || !canonicalAlipayKeyLabel(label) {
			return nil, billing.ErrCallbackUnavailable
		}
		if _, exists := keys[label]; exists {
			return nil, billing.ErrCallbackUnavailable
		}
		var pemValue string
		if err := decoder.Decode(&pemValue); err != nil || strings.TrimSpace(pemValue) == "" {
			return nil, billing.ErrCallbackUnavailable
		}
		publicKey, err := parseAlipayRSAPublicKey(pemValue)
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		keys[label] = publicKey
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, billing.ErrCallbackUnavailable
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, billing.ErrCallbackUnavailable
	}
	if len(keys) == 0 {
		return nil, billing.ErrCallbackUnavailable
	}
	return keys, nil
}

func parseAlipayRSAPublicKey(value string) (*rsa.PublicKey, error) {
	block, rest := pem.Decode([]byte(value))
	if block == nil || strings.TrimSpace(string(rest)) != "" {
		return nil, billing.ErrCallbackUnavailable
	}
	var publicKey *rsa.PublicKey
	switch block.Type {
	case "PUBLIC KEY":
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		var ok bool
		publicKey, ok = parsed.(*rsa.PublicKey)
		if !ok {
			return nil, billing.ErrCallbackUnavailable
		}
	case "RSA PUBLIC KEY":
		parsed, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		publicKey = parsed
	case "CERTIFICATE":
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		var ok bool
		publicKey, ok = certificate.PublicKey.(*rsa.PublicKey)
		if !ok {
			return nil, billing.ErrCallbackUnavailable
		}
	default:
		return nil, billing.ErrCallbackUnavailable
	}
	if publicKey == nil || publicKey.N == nil || publicKey.N.BitLen() < 2048 || publicKey.E < 3 {
		return nil, billing.ErrCallbackUnavailable
	}
	return publicKey, nil
}

func parseAlipayAmountMinor(value string) (int64, bool) {
	if value == "" || len(value) > 11 || value != strings.TrimSpace(value) || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || !allASCIIDigits(parts[0]) {
		return 0, false
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || len(fraction) > 2 || !allASCIIDigits(fraction) {
			return 0, false
		}
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, false
	}
	minor := int64(0)
	if fraction != "" {
		minor, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, false
		}
	}
	if major > (math.MaxInt64-minor)/100 {
		return 0, false
	}
	amount := major*100 + minor
	return amount, amount > 0
}

func canonicalAlipayConfigValue(value string, maxLen int) bool {
	return value != "" && len(value) <= maxLen && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
}

func canonicalAlipayFormKey(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func canonicalAlipayKeyLabel(value string) bool {
	return canonicalAlipayIdentifier(value, 64)
}

func canonicalAlipayMerchantReference(value string) bool {
	return strings.HasPrefix(value, "gj_") && canonicalAlipayIdentifier(value, 64)
}

func canonicalAlipayIdentifier(value string, maxLen int) bool {
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

func alipayCorrelationID(eventID, transactionID, orderID string) string {
	sum := sha256.Sum256([]byte("alipay\n" + eventID + "\n" + transactionID + "\n" + orderID))
	return "alipay:" + hex.EncodeToString(sum[:])
}
