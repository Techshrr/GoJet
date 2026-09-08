package main

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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
	wechatSignatureTolerance         = 300 * time.Second
	maxWeChatPlatformKeysConfigBytes = 256 << 10
	wechatSignatureTypeRSA2048       = "WECHATPAY2-SHA256-RSA2048"
)

type wechatProviderIntentResolver interface {
	ResolveProviderIntentByMerchantReference(context.Context, billing.Provider, string, time.Time) (billing.ProviderIntent, error)
}

type wechatCallbackVerifier struct {
	appID        string
	mchID        string
	apiV3Key     []byte
	platformKeys map[string]*rsa.PublicKey
	store        wechatProviderIntentResolver
	now          func() time.Time
}

type wechatCallbackEnvelope struct {
	ID           string                  `json:"id"`
	CreateTime   string                  `json:"create_time"`
	ResourceType string                  `json:"resource_type"`
	EventType    string                  `json:"event_type"`
	Summary      string                  `json:"summary"`
	Resource     wechatEncryptedResource `json:"resource"`
}

type wechatEncryptedResource struct {
	Algorithm      string `json:"algorithm"`
	Ciphertext     string `json:"ciphertext"`
	AssociatedData string `json:"associated_data"`
	Nonce          string `json:"nonce"`
	OriginalType   string `json:"original_type"`
}

type wechatPaymentTransaction struct {
	AppID         string `json:"appid"`
	MchID         string `json:"mchid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	TradeState    string `json:"trade_state"`
	SuccessTime   string `json:"success_time"`
	Amount        struct {
		Total    int64  `json:"total"`
		Currency string `json:"currency"`
	} `json:"amount"`
}

func buildProductionWeChatCallbackVerifier(store wechatProviderIntentResolver) (wechatCallbackVerifier, bool, error) {
	if os.Getenv("GOJET_BILLING_WECHAT_ENABLED") != "1" {
		return wechatCallbackVerifier{}, false, nil
	}
	appID := os.Getenv("GOJET_BILLING_WECHAT_APPID")
	mchID := os.Getenv("GOJET_BILLING_WECHAT_MCHID")
	apiV3Key := os.Getenv("GOJET_BILLING_WECHAT_API_V3_KEY")
	platformKeysRaw := os.Getenv("GOJET_BILLING_WECHAT_PLATFORM_KEYS_JSON")
	if store == nil || appID == "" || strings.TrimSpace(appID) != appID || mchID == "" || strings.TrimSpace(mchID) != mchID || len(apiV3Key) != 32 || strings.TrimSpace(apiV3Key) != apiV3Key || platformKeysRaw == "" || len(platformKeysRaw) > maxWeChatPlatformKeysConfigBytes {
		return wechatCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	platformKeys, err := parseWeChatPlatformKeysJSON(platformKeysRaw)
	if err != nil || len(platformKeys) == 0 {
		return wechatCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	return wechatCallbackVerifier{
		appID:        appID,
		mchID:        mchID,
		apiV3Key:     []byte(apiV3Key),
		platformKeys: platformKeys,
		store:        store,
		now:          time.Now,
	}, true, nil
}

func (v wechatCallbackVerifier) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	if r == nil || r.Method != http.MethodPost || provider != billing.ProviderWeChat || v.store == nil || v.appID == "" || v.mchID == "" || len(v.apiV3Key) != 32 || len(v.platformKeys) == 0 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProductionCallbackBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductionCallbackBodyBytes {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	timestampRaw, ok := singleWeChatHeader(r, "Wechatpay-Timestamp")
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	nonce, ok := singleWeChatHeader(r, "Wechatpay-Nonce")
	if !ok || len(nonce) > 128 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	serial, ok := singleWeChatHeader(r, "Wechatpay-Serial")
	if !ok || !canonicalWeChatIdentifier(serial, 128) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	signature, ok := singleWeChatHeader(r, "Wechatpay-Signature")
	if !ok || strings.HasPrefix(signature, "WECHATPAY/SIGNTEST/") {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if values := r.Header.Values("Wechatpay-Signature-Type"); len(values) > 1 || (len(values) == 1 && strings.TrimSpace(values[0]) != "" && strings.TrimSpace(values[0]) != wechatSignatureTypeRSA2048) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	now := time.Now
	if v.now != nil {
		now = v.now
	}
	verificationTime := now().UTC()
	if !verifyWeChatSignature(raw, timestampRaw, nonce, signature, serial, v.platformKeys, verificationTime) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	var envelope wechatCallbackEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || !canonicalWeChatIdentifier(envelope.ID, 191) || envelope.EventType == "" || len(envelope.EventType) > 96 {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	// The payment-side authority intentionally ignores authenticated refund,
	// processing and unrelated events. Refund settlement remains separately
	// frozen under P20-DR001 and must not collapse partial refunds into P13's
	// terminal whole-order refunded state.
	if envelope.EventType != "TRANSACTION.SUCCESS" {
		return billing.CallbackCommand{}, billing.ErrCallbackIgnored
	}

	eventTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(envelope.CreateTime))
	if err != nil || eventTime.IsZero() {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	plaintext, err := decryptWeChatResource(envelope.Resource, v.apiV3Key)
	if err != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	var transaction wechatPaymentTransaction
	if err := json.Unmarshal(plaintext, &transaction); err != nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if transaction.AppID != v.appID || transaction.MchID != v.mchID {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	if transaction.TradeState != "SUCCESS" {
		return billing.CallbackCommand{}, billing.ErrCallbackIgnored
	}
	if !canonicalWeChatMerchantReference(transaction.OutTradeNo) || !canonicalWeChatIdentifier(transaction.TransactionID, 64) || transaction.Amount.Total <= 0 || !canonicalUpperISOCurrency(transaction.Amount.Currency) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	intent, err := v.store.ResolveProviderIntentByMerchantReference(r.Context(), billing.ProviderWeChat, transaction.OutTradeNo, verificationTime)
	if err != nil {
		switch {
		case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrConflict), errors.Is(err, billing.ErrInvalidInput):
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		default:
			return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
		}
	}
	if intent.Provider != billing.ProviderWeChat || intent.MerchantReference != transaction.OutTradeNo || intent.SettlementAssetKind != billing.SettlementAssetFiat || intent.SettlementAsset != transaction.Amount.Currency || intent.SettlementAmountUnits != transaction.Amount.Total || intent.SettlementScale != nil || intent.OrderID == "" || (intent.ProviderReference != "" && intent.ProviderReference != transaction.TransactionID) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}

	return billing.CallbackCommand{
		Provider:              billing.ProviderWeChat,
		ProviderEventID:       envelope.ID,
		ProviderTransactionID: transaction.TransactionID,
		OrderID:               intent.OrderID,
		EventType:             envelope.EventType,
		Outcome:               billing.TransactionPaid,
		Money: billing.Money{
			Currency:    transaction.Amount.Currency,
			AmountMinor: transaction.Amount.Total,
		},
		ReceivedAt:    eventTime.UTC(),
		CorrelationID: wechatCorrelationID(envelope.ID, transaction.TransactionID, intent.OrderID),
	}, nil
}

func (v wechatCallbackVerifier) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	if provider != billing.ProviderWeChat {
		return billing.CallbackAcknowledgement{}
	}
	return billing.CallbackAcknowledgement{StatusCode: http.StatusNoContent}
}

func singleWeChatHeader(r *http.Request, name string) (string, bool) {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	return value, value != ""
}

func verifyWeChatSignature(raw []byte, timestampRaw, nonce, signature, serial string, platformKeys map[string]*rsa.PublicKey, now time.Time) bool {
	timestamp, err := strconv.ParseInt(timestampRaw, 10, 64)
	if err != nil || timestamp <= 0 || nonce == "" || signature == "" || serial == "" {
		return false
	}
	tolerance := int64(wechatSignatureTolerance / time.Second)
	nowUnix := now.UTC().Unix()
	if timestamp < nowUnix-tolerance || timestamp > nowUnix+tolerance {
		return false
	}
	publicKey := platformKeys[serial]
	if publicKey == nil {
		return false
	}
	decodedSignature, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(decodedSignature) == 0 {
		return false
	}
	message := timestampRaw + "\n" + nonce + "\n" + string(raw) + "\n"
	digest := sha256.Sum256([]byte(message))
	return rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], decodedSignature) == nil
}

func decryptWeChatResource(resource wechatEncryptedResource, apiV3Key []byte) ([]byte, error) {
	if resource.Algorithm != "AEAD_AES_256_GCM" || len(apiV3Key) != 32 || resource.Nonce == "" || resource.Ciphertext == "" {
		return nil, billing.ErrCallbackUnauthorized
	}
	block, err := aes.NewCipher(apiV3Key)
	if err != nil {
		return nil, billing.ErrCallbackUnauthorized
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(resource.Nonce) != gcm.NonceSize() {
		return nil, billing.ErrCallbackUnauthorized
	}
	ciphertext, err := base64.StdEncoding.DecodeString(resource.Ciphertext)
	if err != nil || len(ciphertext) < gcm.Overhead() {
		return nil, billing.ErrCallbackUnauthorized
	}
	plaintext, err := gcm.Open(nil, []byte(resource.Nonce), ciphertext, []byte(resource.AssociatedData))
	if err != nil || len(plaintext) == 0 || len(plaintext) > maxProductionCallbackBodyBytes {
		return nil, billing.ErrCallbackUnauthorized
	}
	return plaintext, nil
}

func parseWeChatPlatformKeysJSON(raw string) (map[string]*rsa.PublicKey, error) {
	if raw == "" || len(raw) > maxWeChatPlatformKeysConfigBytes {
		return nil, billing.ErrCallbackUnavailable
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, billing.ErrCallbackUnavailable
	}
	keys := make(map[string]*rsa.PublicKey)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		serial, ok := keyToken.(string)
		if !ok || !canonicalWeChatIdentifier(serial, 128) {
			return nil, billing.ErrCallbackUnavailable
		}
		if _, exists := keys[serial]; exists {
			return nil, billing.ErrCallbackUnavailable
		}
		var pemValue string
		if err := decoder.Decode(&pemValue); err != nil || strings.TrimSpace(pemValue) == "" {
			return nil, billing.ErrCallbackUnavailable
		}
		publicKey, err := parseWeChatRSAPublicKey(pemValue)
		if err != nil {
			return nil, billing.ErrCallbackUnavailable
		}
		keys[serial] = publicKey
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return nil, billing.ErrCallbackUnavailable
	}
	if decoder.More() {
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

func parseWeChatRSAPublicKey(value string) (*rsa.PublicKey, error) {
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

func canonicalWeChatMerchantReference(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, ch := range value {
		if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func canonicalWeChatIdentifier(value string, maxLen int) bool {
	if len(value) == 0 || len(value) > maxLen {
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

func canonicalUpperISOCurrency(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, ch := range value {
		if ch < 'A' || ch > 'Z' {
			return false
		}
	}
	return true
}

func wechatCorrelationID(eventID, transactionID, orderID string) string {
	sum := sha256.Sum256([]byte("wechat\n" + eventID + "\n" + transactionID + "\n" + orderID))
	return "wechat:" + hex.EncodeToString(sum[:])
}
