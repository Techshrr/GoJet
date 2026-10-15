package logging

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

// Fingerprint correlates an identifier without writing externally supplied
// correlation headers or opaque business identifiers into operational logs.
func Fingerprint(kind, value string) string {
	digest := sha256.Sum256([]byte(kind + ":" + value))
	return hex.EncodeToString(digest[:])
}

// HTTP records completion without wrapping the ResponseWriter or changing
// request/response semantics. The event does not assert a successful mutation;
// durable outcome and actor authority remain in the corresponding audit store.
func HTTP(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		correlationID := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		next.ServeHTTP(w, r)
		attrs := []any{}
		if requestID != "" {
			attrs = append(attrs, "request_fingerprint", Fingerprint("request", requestID))
		}
		if correlationID != "" {
			attrs = append(attrs, "correlation_fingerprint", Fingerprint("correlation", correlationID))
		}
		logger.InfoContext(r.Context(), "http request finished", attrs...)
	})
}
