package trust

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const maxProviderResponseBytes = 64 * 1024

type SemanticProviderClient struct {
	Name       string
	Endpoint   string
	HTTPClient *http.Client
}

type semanticProviderRequest struct {
	Target string `json:"target"`
}

type semanticProviderResponse struct {
	Complete   *bool          `json:"complete"`
	Verdict    string         `json:"verdict"`
	SignalCode string         `json:"signal_code"`
	Evidence   map[string]any `json:"evidence"`
}

// Observe sends one normalized target to a provider endpoint and always maps
// provider/runtime failure modes to an explicit non-allow observation. Only
// local configuration errors are returned as Go errors.
func (c SemanticProviderClient) Observe(ctx context.Context, target string) (ProviderObservation, error) {
	provider := strings.TrimSpace(c.Name)
	endpoint := strings.TrimSpace(c.Endpoint)
	if !validProviderName(provider) || endpoint == "" || c.HTTPClient == nil || strings.TrimSpace(target) == "" {
		return ProviderObservation{}, ErrInvalid
	}

	body, err := json.Marshal(semanticProviderRequest{Target: target})
	if err != nil {
		return ProviderObservation{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ProviderObservation{}, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	now := time.Now().UTC().Truncate(time.Microsecond)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return normalizedProviderFailure(provider, ProviderUnavailable, "provider-timeout", "timeout", now), nil
		}
		return normalizedProviderFailure(provider, ProviderUnavailable, "provider-transport-error", "transport", now), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return normalizedProviderFailure(provider, ProviderUnavailable, "provider-unavailable", "http-status", now), nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderResponseBytes+1))
	if err != nil {
		return normalizedProviderFailure(provider, ProviderUnavailable, "provider-read-error", "read", now), nil
	}
	if len(raw) > maxProviderResponseBytes {
		return normalizedProviderFailure(provider, ProviderUnknown, "provider-malformed", "response-too-large", now), nil
	}

	var decoded semanticProviderResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return normalizedProviderFailure(provider, ProviderUnknown, "provider-malformed", "invalid-json", now), nil
	}
	if decoded.Complete == nil || !*decoded.Complete {
		return normalizedProviderFailure(provider, ProviderUnknown, "provider-partial", "partial", now), nil
	}

	outcome, ok := normalizeProviderVerdict(decoded.Verdict)
	if !ok {
		return normalizedProviderFailure(provider, ProviderUnknown, "provider-malformed", "invalid-verdict", now), nil
	}
	signal := normalizeSignalCode(decoded.SignalCode)
	if signal == "" {
		signal = "provider-" + string(outcome)
	}
	evidence := SanitizeProviderEvidence(decoded.Evidence)
	return ProviderObservation{
		Provider:   provider,
		Outcome:    outcome,
		SignalCode: signal,
		Evidence:   evidence,
		ObservedAt: now,
	}, nil
}

func normalizedProviderFailure(provider string, outcome ProviderOutcome, signal, category string, observedAt time.Time) ProviderObservation {
	return ProviderObservation{
		Provider:   provider,
		Outcome:    outcome,
		SignalCode: signal,
		Evidence: map[string]any{
			"failure_category": category,
		},
		ObservedAt: observedAt,
	}
}

func normalizeProviderVerdict(raw string) (ProviderOutcome, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "allow":
		return ProviderAllow, true
	case "review":
		return ProviderReview, true
	case "block":
		return ProviderBlock, true
	default:
		return "", false
	}
}

func validProviderName(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func normalizeSignalCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			continue
		}
		return ""
	}
	return value
}

// SanitizeProviderEvidence strips common credential-bearing keys recursively
// and bounds text values before anything reaches durable storage or CI output.
func SanitizeProviderEvidence(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if providerEvidenceKeySensitive(key) {
			continue
		}
		out[key] = sanitizeProviderEvidenceValue(value, 0)
	}
	return out
}

func sanitizeProviderEvidenceValue(value any, depth int) any {
	if depth >= 6 {
		return "[truncated]"
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if providerEvidenceKeySensitive(key) {
				continue
			}
			out[key] = sanitizeProviderEvidenceValue(child, depth+1)
		}
		return out
	case []any:
		limit := len(typed)
		if limit > 32 {
			limit = 32
		}
		out := make([]any, 0, limit)
		for _, child := range typed[:limit] {
			out = append(out, sanitizeProviderEvidenceValue(child, depth+1))
		}
		return out
	case string:
		if len(typed) > 500 {
			return typed[:500]
		}
		return typed
	case nil, bool, float64, json.Number:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func providerEvidenceKeySensitive(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	sensitive := []string{
		"secret", "token", "password", "passwd", "authorization", "cookie",
		"credential", "api_key", "apikey", "private_key", "client_secret",
	}
	for _, marker := range sensitive {
		if normalized == marker || strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
