package support

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTurnstileVerifyEndpoint = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileResponseLimit        = int64(64 * 1024)
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type TurnstileHTTPVerifier struct {
	secret   string
	endpoint string
	client   HTTPDoer
}

func NewTurnstileHTTPVerifier(secret string, client HTTPDoer) (*TurnstileHTTPVerifier, error) {
	return newTurnstileHTTPVerifier(secret, defaultTurnstileVerifyEndpoint, client)
}

func newTurnstileHTTPVerifier(secret, endpoint string, client HTTPDoer) (*TurnstileHTTPVerifier, error) {
	secret = strings.TrimSpace(secret)
	endpoint = strings.TrimSpace(endpoint)
	if secret == "" || endpoint == "" {
		return nil, ErrInvalidInput
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &TurnstileHTTPVerifier{secret: secret, endpoint: endpoint, client: client}, nil
}

// Verify sends the raw token only to the Turnstile verification endpoint. The
// provider response is reduced to the success bit and is never returned/logged
// as raw evidence by this adapter.
func (v *TurnstileHTTPVerifier) Verify(ctx context.Context, token string) (TurnstileVerification, error) {
	if v == nil || v.client == nil || strings.TrimSpace(v.secret) == "" {
		return TurnstileVerification{}, ErrInvalidInput
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return TurnstileVerification{}, ErrInvalidInput
	}

	form := url.Values{}
	form.Set("secret", v.secret)
	form.Set("response", token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TurnstileVerification{}, ErrTurnstileRejected
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	response, err := v.client.Do(req)
	if err != nil {
		return TurnstileVerification{}, ErrTurnstileRejected
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, turnstileResponseLimit))
		return TurnstileVerification{}, ErrTurnstileRejected
	}

	var payload struct {
		Success bool `json:"success"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, turnstileResponseLimit))
	if err := decoder.Decode(&payload); err != nil {
		return TurnstileVerification{}, ErrTurnstileRejected
	}
	return TurnstileVerification{Success: payload.Success}, nil
}
