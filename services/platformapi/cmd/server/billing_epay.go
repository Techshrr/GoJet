package main

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/billing"
)

const maxEpayCallbackQueryBytes = 16 << 10

type epayCallbackStore interface {
	ApplyAuthenticatedCallback(context.Context, billing.CallbackCommand) (billing.CallbackResult, error)
}

type epayCallbackVerifier struct {
	pid string
	key string
	now func() time.Time
}

type epayVerifiedCallback struct {
	command billing.CallbackCommand
	settle  bool
}

type epayCallbackHandler struct {
	enabled  bool
	store    epayCallbackStore
	verifier epayCallbackVerifier
}

func buildProductionEpayCallbackHandler(store epayCallbackStore) (http.Handler, error) {
	enabled := os.Getenv("GOJET_BILLING_EPAY_ENABLED") == "1"
	pid := strings.TrimSpace(os.Getenv("GOJET_BILLING_EPAY_PID"))
	key := os.Getenv("GOJET_BILLING_EPAY_KEY")
	if enabled && (pid == "" || strings.TrimSpace(key) == "") {
		return nil, billing.ErrCallbackUnavailable
	}
	return epayCallbackHandler{
		enabled: enabled,
		store:   store,
		verifier: epayCallbackVerifier{
			pid: pid,
			key: key,
			now: time.Now,
		},
	}, nil
}

func (h epayCallbackHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if !h.enabled {
		writeEpayAck(w, http.StatusServiceUnavailable, false)
		return
	}
	verified, err := h.verifier.Verify(r)
	if err != nil {
		writeEpayAck(w, http.StatusUnauthorized, false)
		return
	}
	// Rainbow Epay V1 defines only TRADE_SUCCESS as successful payment
	// authority. Authenticated non-success notifications are acknowledged so
	// they cannot mutate Billing state or create an endless provider retry loop.
	if !verified.settle {
		writeEpayAck(w, http.StatusOK, true)
		return
	}
	if h.store == nil {
		writeEpayAck(w, http.StatusServiceUnavailable, false)
		return
	}
	if _, err := h.store.ApplyAuthenticatedCallback(r.Context(), verified.command); err != nil {
		switch {
		case errors.Is(err, billing.ErrInvalidInput), errors.Is(err, billing.ErrInvalidMoney):
			writeEpayAck(w, http.StatusBadRequest, false)
		case errors.Is(err, billing.ErrConflict):
			writeEpayAck(w, http.StatusConflict, false)
		default:
			writeEpayAck(w, http.StatusInternalServerError, false)
		}
		return
	}
	writeEpayAck(w, http.StatusOK, true)
}

func writeEpayAck(w http.ResponseWriter, status int, success bool) {
	w.WriteHeader(status)
	if success {
		_, _ = w.Write([]byte("success"))
		return
	}
	_, _ = w.Write([]byte("fail"))
}

func (v epayCallbackVerifier) Verify(r *http.Request) (epayVerifiedCallback, error) {
	if r == nil || r.Method != http.MethodGet || v.pid == "" || strings.TrimSpace(v.key) == "" || len(r.URL.RawQuery) == 0 || len(r.URL.RawQuery) > maxEpayCallbackQueryBytes {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}
	params := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) != 1 {
			return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
		}
		params[key] = items[0]
	}

	required := []string{"pid", "trade_no", "out_trade_no", "type", "name", "money", "trade_status", "sign", "sign_type"}
	for _, key := range required {
		if strings.TrimSpace(params[key]) == "" {
			return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
		}
	}
	if params["pid"] != v.pid || !strings.EqualFold(strings.TrimSpace(params["sign_type"]), "MD5") {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}

	expected := epayMD5Signature(params, v.key)
	actual := strings.ToLower(strings.TrimSpace(params["sign"]))
	if len(actual) != len(expected) || subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}

	if strings.TrimSpace(params["trade_status"]) != "TRADE_SUCCESS" {
		return epayVerifiedCallback{settle: false}, nil
	}

	tradeNo := strings.TrimSpace(params["trade_no"])
	orderID := strings.TrimSpace(params["out_trade_no"])
	if len(tradeNo) > 191 || len(orderID) > 191 {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}
	amountMinor, err := parseEpayCNYMinor(params["money"])
	if err != nil {
		return epayVerifiedCallback{}, billing.ErrCallbackUnauthorized
	}
	now := time.Now
	if v.now != nil {
		now = v.now
	}
	return epayVerifiedCallback{
		settle: true,
		command: billing.CallbackCommand{
			Provider:              billing.ProviderEpay,
			ProviderEventID:       tradeNo,
			ProviderTransactionID: tradeNo,
			OrderID:               orderID,
			EventType:             "payment.paid",
			Outcome:               billing.TransactionPaid,
			Money:                 billing.Money{Currency: "CNY", AmountMinor: amountMinor},
			ReceivedAt:            now().UTC(),
			CorrelationID:         epayCorrelationID(tradeNo, orderID),
		},
	}, nil
}

func epayMD5Signature(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for name, value := range params {
		if name == "sign" || name == "sign_type" || value == "" {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for i, name := range keys {
		if i > 0 {
			canonical.WriteByte('&')
		}
		canonical.WriteString(name)
		canonical.WriteByte('=')
		canonical.WriteString(params[name])
	}
	canonical.WriteString(key)
	sum := md5.Sum([]byte(canonical.String())) // #nosec G401 -- protocol-mandated Rainbow Epay V1 signature.
	return hex.EncodeToString(sum[:])
}

func parseEpayCNYMinor(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, billing.ErrInvalidMoney
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 16 {
		return 0, billing.ErrInvalidMoney
	}
	if !allASCIIDigits(parts[0]) {
		return 0, billing.ErrInvalidMoney
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 || !allASCIIDigits(parts[1]) {
			return 0, billing.ErrInvalidMoney
		}
		fraction = parts[1]
		if len(fraction) == 1 {
			fraction += "0"
		}
	}
	major, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, billing.ErrInvalidMoney
	}
	minor, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil || major > (math.MaxInt64-minor)/100 {
		return 0, billing.ErrInvalidMoney
	}
	amount := major*100 + minor
	if amount <= 0 {
		return 0, billing.ErrInvalidMoney
	}
	return amount, nil
}

func allASCIIDigits(value string) bool {
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return value != ""
}

func epayCorrelationID(tradeNo, orderID string) string {
	sum := sha256.Sum256([]byte("epay\n" + tradeNo + "\n" + orderID))
	return "epay:" + hex.EncodeToString(sum[:])
}
