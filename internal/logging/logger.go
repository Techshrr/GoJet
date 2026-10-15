// Package logging provides the service log boundary. Callers must use constant
// event messages and identifiers/scalar metrics, never user-controlled prose.
// It is not a replacement for the authoritative business audit store.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

const redacted = "[redacted]"

// New binds each record to its executable and prevents raw driver errors,
// filesystem paths and opaque payloads from being serialized into service logs.
func New(out io.Writer, service string) *slog.Logger {
	if socket := os.Getenv("GOJET_LOG_SOCKET"); socket != "" && service != "logreceiver" {
		out = &localForwarder{out: out, socket: socket, service: service}
	}
	return NewLocal(out, service)
}

// NewLocal emits sanitized records without forwarding; receiver output must use it.
func NewLocal(out io.Writer, service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{
		Level:       slog.LevelInfo,
		ReplaceAttr: safeAttribute,
	})).With("service_id", service)
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(key)
	for _, part := range []string{"password", "secret", "token", "credential", "authorization", "cookie", "private_key", "dsn", "quarantine", "file_content", "verification_material", "txt_record", "payment_signature"} {
		if strings.Contains(key, part) {
			return true
		}
	}
	return key == "error" || key == "err" || key == "path" || key == "raw" || key == "payload" || key == "body"
}

func safeAttribute(groups []string, a slog.Attr) slog.Attr {
	for _, group := range groups {
		if sensitiveKey(group) {
			return slog.String(a.Key, redacted)
		}
	}
	if sensitiveKey(a.Key) {
		return slog.String(a.Key, redacted)
	}
	if a.Value.Kind() == slog.KindAny {
		// Level is the JSON handler's built-in scalar, not arbitrary Stringer
		// output. Other objects may hide secrets in MarshalJSON/Error/String.
		if level, ok := a.Value.Any().(slog.Level); ok && a.Key == slog.LevelKey {
			return slog.String(a.Key, level.String())
		}
		return slog.String(a.Key, redacted)
	}
	return a
}
