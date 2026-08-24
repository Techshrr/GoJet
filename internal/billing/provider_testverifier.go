package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidCallbackSignature = errors.New("invalid callback signature")

// DeterministicTestVerifier is CI-only evidence infrastructure. It is not a
// production implementation of any provider signature protocol.
type DeterministicTestVerifier struct {
	Secrets map[Provider][]byte
}

func (v DeterministicTestVerifier) Sign(provider Provider, eventID, transactionID string, body []byte) (string, error) {
	if !IsFrozenProvider(provider) {
		return "", ErrInvalidProvider
	}
	secret := v.Secrets[provider]
	if len(secret) < 16 || strings.TrimSpace(eventID) == "" || strings.TrimSpace(transactionID) == "" {
		return "", ErrInvalidCallbackSignature
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(provider))
	mac.Write([]byte("\n"))
	mac.Write([]byte(eventID))
	mac.Write([]byte("\n"))
	mac.Write([]byte(transactionID))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func (v DeterministicTestVerifier) Verify(provider Provider, eventID, transactionID string, body []byte, signature string) error {
	expected, err := v.Sign(provider, eventID, transactionID, body)
	if err != nil {
		return err
	}
	actual, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return ErrInvalidCallbackSignature
	}
	expectedBytes, _ := hex.DecodeString(expected)
	if !hmac.Equal(actual, expectedBytes) {
		return ErrInvalidCallbackSignature
	}
	return nil
}
