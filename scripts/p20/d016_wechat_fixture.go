package main

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strconv"
	"time"
)

func main() {
	if len(os.Args) != 6 {
		panic("usage: d016_wechat_fixture EVENT_ID EVENT_TYPE AMOUNT TIMESTAMP_OFFSET_SECONDS PREFIX")
	}
	eventID := os.Args[1]
	eventType := os.Args[2]
	amount := mustInt64(os.Args[3])
	timestampOffsetSeconds := mustInt64(os.Args[4])
	prefix := os.Args[5]

	apiV3Key := []byte(os.Getenv("GOJET_BILLING_WECHAT_API_V3_KEY"))
	if len(apiV3Key) != 32 {
		panic("GOJET_BILLING_WECHAT_API_V3_KEY must be 32 bytes")
	}
	privateKey := readPrivateKey("/tmp/wechat-platform-private.pem")

	eventTime := time.Now().UTC().Truncate(time.Second)
	transaction := map[string]any{
		"appid":          os.Getenv("GOJET_BILLING_WECHAT_APPID"),
		"mchid":          os.Getenv("GOJET_BILLING_WECHAT_MCHID"),
		"out_trade_no":   "gj_wechat_ci",
		"transaction_id": "420000000000000002",
		"trade_state":    "SUCCESS",
		"success_time":   eventTime.Format(time.RFC3339),
		"amount": map[string]any{
			"total":    amount,
			"currency": "CNY",
		},
	}
	plaintext := mustJSON(transaction)

	resourceNonce := []byte("0123456789ab")
	associatedData := []byte("transaction")
	block, err := aes.NewCipher(apiV3Key)
	must(err)
	gcm, err := cipher.NewGCM(block)
	must(err)
	ciphertext := gcm.Seal(nil, resourceNonce, plaintext, associatedData)

	envelope := map[string]any{
		"id":            eventID,
		"create_time":   eventTime.Format(time.RFC3339Nano),
		"resource_type": "encrypt-resource",
		"event_type":    eventType,
		"summary":       "gojet-ci",
		"resource": map[string]any{
			"algorithm":       "AEAD_AES_256_GCM",
			"ciphertext":      base64.StdEncoding.EncodeToString(ciphertext),
			"associated_data": string(associatedData),
			"nonce":           string(resourceNonce),
			"original_type":   "transaction",
		},
	}
	raw := mustJSON(envelope)

	timestamp := time.Now().UTC().Add(time.Duration(timestampOffsetSeconds) * time.Second).Unix()
	headerNonce := "gojet-ci-nonce"
	message := fmt.Sprintf("%d\n%s\n%s\n", timestamp, headerNonce, raw)
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	must(err)

	must(os.WriteFile(prefix+".json", raw, 0o600))
	headers := fmt.Sprintf(
		"WECHAT_TS=%d\nWECHAT_NONCE=%s\nWECHAT_SIGNATURE=%s\n",
		timestamp,
		headerNonce,
		base64.StdEncoding.EncodeToString(signature),
	)
	must(os.WriteFile(prefix+".env", []byte(headers), 0o600))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func mustInt64(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	must(err)
	return parsed
}

func mustJSON(value any) []byte {
	raw, err := json.Marshal(value)
	must(err)
	return raw
}

func readPrivateKey(path string) *rsa.PrivateKey {
	raw, err := os.ReadFile(path)
	must(err)
	block, rest := pem.Decode(raw)
	if block == nil || len(rest) != 0 {
		panic("invalid private key PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	must(err)
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		panic("private key is not RSA")
	}
	return privateKey
}
