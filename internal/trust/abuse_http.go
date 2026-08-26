package trust

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type PublicAbuseAPI struct {
	service *AbuseService
}

func NewPublicAbuseAPI(service *AbuseService) (*PublicAbuseAPI, error) {
	if service == nil {
		return nil, ErrInvalid
	}
	return &PublicAbuseAPI{service: service}, nil
}

func (a *PublicAbuseAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/public/abuse-reports", a.submit)
	return abuseSecurityHeaders(mux)
}

type publicAbuseRequest struct {
	ResourceType   AbuseResourceType `json:"resource_type"`
	Hostname       string            `json:"hostname"`
	Code           string            `json:"code"`
	Category       AbuseCategory     `json:"category"`
	Details        string            `json:"details"`
	TurnstileToken string            `json:"turnstile_token"`
}

func (a *PublicAbuseAPI) submit(w http.ResponseWriter, r *http.Request) {
	var input publicAbuseRequest
	if !decodeAbuseJSON(w, r, &input) {
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		writeAbuseError(w, http.StatusBadRequest, "idempotency_required", "A request idempotency key is required.")
		return
	}
	correlationID, err := newAbuseCorrelationID()
	if err != nil {
		writeAbuseError(w, http.StatusServiceUnavailable, "intake_unavailable", "Abuse reporting is temporarily unavailable.")
		return
	}
	result, err := a.service.Submit(r.Context(), SubmitAbuseInput{
		ResourceType:   input.ResourceType,
		Hostname:       input.Hostname,
		Code:           input.Code,
		Category:       input.Category,
		Details:        input.Details,
		TurnstileToken: input.TurnstileToken,
		IdempotencyKey: idempotencyKey,
		CorrelationID:  correlationID,
		RemoteAddr:     r.RemoteAddr,
	})
	if err != nil {
		writeAbuseServiceError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeAbuseJSON(w, status, map[string]any{
		"status":         "received",
		"report_id":      result.Report.PublicID,
		"created":        result.Created,
		"correlation_id": result.Report.CorrelationID,
	})
}

func abuseSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func decodeAbuseJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeAbuseError(w, http.StatusBadRequest, "invalid_json", "Invalid request body.")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeAbuseError(w, http.StatusBadRequest, "invalid_json", "Invalid request body.")
		return false
	}
	return true
}

func writeAbuseServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		writeAbuseError(w, http.StatusBadRequest, "invalid_request", "Invalid abuse report.")
	case errors.Is(err, ErrVerification):
		writeAbuseError(w, http.StatusBadRequest, "verification_failed", "Verification failed.")
	case errors.Is(err, ErrRateLimited):
		writeAbuseError(w, http.StatusTooManyRequests, "rate_limited", "Abuse report rate limit exceeded.")
	case errors.Is(err, ErrNotFound):
		writeAbuseError(w, http.StatusNotFound, "resource_unavailable", "The reported resource is unavailable.")
	case errors.Is(err, ErrConflict):
		writeAbuseError(w, http.StatusConflict, "idempotency_conflict", "The request conflicts with an earlier submission.")
	default:
		writeAbuseError(w, http.StatusServiceUnavailable, "intake_unavailable", "Abuse reporting is temporarily unavailable.")
	}
}

func writeAbuseError(w http.ResponseWriter, status int, code, message string) {
	writeAbuseJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeAbuseJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newAbuseCorrelationID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "p16_" + hex.EncodeToString(buf), nil
}
