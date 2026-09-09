package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

const (
	tronGridAPIBase               = "https://api.trongrid.io"
	tronGridAPIHost               = "api.trongrid.io"
	tronGridSolidifiedReceiptPath = "/walletsolidity/gettransactioninfobyid"
	tronUSDTContractLogAddress    = "a614f803b6fd780986a42c78ec9c7f77e6ded13c"
	tronTransferTopic             = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	cryptoUSDTAsset               = "USDT_TRC20"
	cryptoUSDTScale               = uint8(6)
	maxTronReceiptResponseBytes   = 2 << 20
)

var cryptoTxIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var cryptoTopicPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type cryptoProviderIntentResolver interface {
	ResolveCryptoProviderIntentByProviderReference(context.Context, string, time.Time) (billing.ProviderIntent, error)
	GetOrder(context.Context, string, string) (billing.Order, error)
}

type cryptoHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type cryptoCallbackVerifier struct {
	apiBase string
	apiKey  string
	client  cryptoHTTPDoer
	store   cryptoProviderIntentResolver
	now     func() time.Time
}

type tronTransactionInfo struct {
	ID             string `json:"id"`
	BlockTimeStamp int64  `json:"blockTimeStamp"`
	Result         string `json:"result"`
	Receipt        struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Log []tronEventLog `json:"log"`
}

type tronEventLog struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

func buildProductionCryptoCallbackVerifier(store cryptoProviderIntentResolver) (cryptoCallbackVerifier, bool, error) {
	if os.Getenv("GOJET_BILLING_CRYPTO_ENABLED") != "1" {
		return cryptoCallbackVerifier{}, false, nil
	}
	apiKey := os.Getenv("GOJET_BILLING_CRYPTO_TRONGRID_API_KEY")
	if store == nil || !validCryptoAPIKey(apiKey) {
		return cryptoCallbackVerifier{}, false, billing.ErrCallbackUnavailable
	}
	return cryptoCallbackVerifier{
		apiBase: tronGridAPIBase,
		apiKey:  apiKey,
		client:  newProductionTronGridHTTPClient(tronGridAPIHost),
		store:   store,
		now:     time.Now,
	}, true, nil
}

func (v cryptoCallbackVerifier) VerifyAndNormalize(r *http.Request, provider billing.Provider) (billing.CallbackCommand, error) {
	if r == nil || r.Method != http.MethodPost || provider != billing.ProviderCrypto || v.store == nil || v.client == nil || v.apiBase != tronGridAPIBase || !validCryptoAPIKey(v.apiKey) {
		return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxProductionCallbackBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxProductionCallbackBodyBytes {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	txid, ok := parseCryptoTrigger(raw)
	if !ok {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	info, err := v.fetchSolidifiedTransactionInfo(r.Context(), txid)
	if err != nil {
		return billing.CallbackCommand{}, err
	}
	now := time.Now
	if v.now != nil {
		now = v.now
	}
	verificationTime := now().UTC()
	return v.normalizeSolidifiedUSDTTransfer(r.Context(), txid, info, verificationTime)
}

func (v cryptoCallbackVerifier) SuccessAcknowledgement(provider billing.Provider) billing.CallbackAcknowledgement {
	if provider != billing.ProviderCrypto {
		return billing.CallbackAcknowledgement{}
	}
	return billing.CallbackAcknowledgement{StatusCode: http.StatusOK}
}

func (v cryptoCallbackVerifier) fetchSolidifiedTransactionInfo(ctx context.Context, txid string) (tronTransactionInfo, error) {
	payload, err := json.Marshal(struct {
		Value string `json:"value"`
	}{Value: txid})
	if err != nil {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.apiBase+tronGridSolidifiedReceiptPath, bytes.NewReader(payload))
	if err != nil {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("TRON-PRO-API-KEY", v.apiKey)
	resp, err := v.client.Do(req)
	if err != nil {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTronReceiptResponseBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxTronReceiptResponseBytes || resp.StatusCode != http.StatusOK {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "{}" || trimmed == "null" {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	var info tronTransactionInfo
	if json.Unmarshal(body, &info) != nil || info.ID == "" || info.BlockTimeStamp <= 0 || info.Receipt.Result == "" {
		return tronTransactionInfo{}, billing.ErrCallbackUnavailable
	}
	if info.ID != txid || info.Receipt.Result != "SUCCESS" || (info.Result != "" && info.Result != "SUCCESS") {
		return tronTransactionInfo{}, billing.ErrCallbackUnauthorized
	}
	return info, nil
}

func (v cryptoCallbackVerifier) normalizeSolidifiedUSDTTransfer(ctx context.Context, txid string, info tronTransactionInfo, verificationTime time.Time) (billing.CallbackCommand, error) {
	var matched *billing.CallbackCommand
	for _, item := range info.Log {
		recipient, amount, ok := parseUSDTTransferCandidate(item)
		if !ok {
			continue
		}
		intent, err := v.store.ResolveCryptoProviderIntentByProviderReference(ctx, recipient, verificationTime)
		if err != nil {
			switch {
			case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrConflict), errors.Is(err, billing.ErrInvalidInput):
				continue
			default:
				return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
			}
		}
		if intent.Provider != billing.ProviderCrypto || intent.ProviderReference != recipient || intent.SettlementAssetKind != billing.SettlementAssetToken || intent.SettlementAsset != cryptoUSDTAsset || intent.SettlementScale == nil || *intent.SettlementScale != cryptoUSDTScale || intent.SettlementAmountUnits != amount || intent.WorkspaceID == "" || intent.OrderID == "" {
			continue
		}
		order, err := v.store.GetOrder(ctx, intent.WorkspaceID, intent.OrderID)
		if err != nil {
			switch {
			case errors.Is(err, billing.ErrNotFound), errors.Is(err, billing.ErrInvalidInput), errors.Is(err, billing.ErrConflict):
				return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
			default:
				return billing.CallbackCommand{}, billing.ErrCallbackUnavailable
			}
		}
		if order.ID != intent.OrderID || order.WorkspaceID != intent.WorkspaceID || order.Money.Validate(false) != nil {
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		}
		receivedAt := time.UnixMilli(info.BlockTimeStamp).UTC()
		candidate := billing.CallbackCommand{
			Provider:              billing.ProviderCrypto,
			ProviderEventID:       txid,
			ProviderTransactionID: txid,
			OrderID:               order.ID,
			EventType:             "TRC20_TRANSFER_SOLIDIFIED",
			Outcome:               billing.TransactionPaid,
			Money:                 order.Money,
			ReceivedAt:            receivedAt,
			CorrelationID:         cryptoCorrelationID(txid, order.ID),
		}
		if matched != nil {
			return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
		}
		matched = &candidate
	}
	if matched == nil {
		return billing.CallbackCommand{}, billing.ErrCallbackUnauthorized
	}
	return *matched, nil
}

func parseCryptoTrigger(raw []byte) (string, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", false
	}
	var txid string
	seen := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return "", false
		}
		key, ok := keyToken.(string)
		if !ok || key != "txid" || seen {
			return "", false
		}
		if err := decoder.Decode(&txid); err != nil {
			return "", false
		}
		seen = true
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen || !cryptoTxIDPattern.MatchString(txid) {
		return "", false
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", false
	}
	return txid, true
}

func parseUSDTTransferCandidate(item tronEventLog) (string, int64, bool) {
	if item.Address != tronUSDTContractLogAddress || len(item.Topics) != 3 || item.Topics[0] != tronTransferTopic || !cryptoTopicPattern.MatchString(item.Topics[2]) || !cryptoTopicPattern.MatchString(item.Data) {
		return "", 0, false
	}
	recipient := "41" + item.Topics[2][24:]
	if len(recipient) != 42 {
		return "", 0, false
	}
	decoded, err := hex.DecodeString(item.Data)
	if err != nil || len(decoded) != 32 {
		return "", 0, false
	}
	value := new(big.Int).SetBytes(decoded)
	if value.Sign() <= 0 || !value.IsInt64() {
		return "", 0, false
	}
	return recipient, value.Int64(), true
}

func cryptoCorrelationID(txid, orderID string) string {
	sum := sha256.Sum256([]byte(txid + "|" + orderID))
	return "crypto:" + hex.EncodeToString(sum[:])
}

func newProductionTronGridHTTPClient(expectedHost string) *http.Client {
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
			return nil, errors.New("trongrid outbound destination rejected")
		}
		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, expectedHost)
		if err != nil || len(resolved) == 0 {
			return nil, errors.New("trongrid outbound resolution failed")
		}
		var lastErr error
		for _, item := range resolved {
			ip := item.IP
			if !publicTronGridDestinationIP(ip) || (strings.HasSuffix(network, "4") && ip.To4() == nil) || (strings.HasSuffix(network, "6") && (ip.To16() == nil || ip.To4() != nil)) {
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
		return nil, errors.New("trongrid outbound destination rejected")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func publicTronGridDestinationIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsUnspecified()
}

func validCryptoAPIKey(value string) bool {
	return value != "" && len(value) <= 512 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n")
}
