package logging_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Techshrr/GoJet/internal/files"
	"github.com/Techshrr/GoJet/internal/logging"
)

func TestActualQuarantineErrorDoesNotLeak(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private-path-canary")
	storage, err := files.NewNativeStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = storage.OpenQuarantine(strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), root) {
		t.Fatal("test did not exercise the real path-bearing storage error")
	}
	var original, safe bytes.Buffer
	slog.New(slog.NewJSONHandler(&original, nil)).Error("open quarantine bytes", "error", err)
	if !strings.Contains(original.String(), "private-path-canary") {
		t.Fatal("original leak was not reproduced")
	}
	logging.New(&safe, "fileworker").Error("open quarantine bytes", "file_id", "file-123", "attempt_id", "attempt-456", "error", err)
	if strings.Contains(safe.String(), "private-path-canary") || strings.Contains(safe.String(), root) {
		t.Fatal("sensitive path leaked")
	}
	var row map[string]any
	if err := json.Unmarshal(safe.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["service_id"] != "fileworker" || row["file_id"] != "file-123" || row["attempt_id"] != "attempt-456" || row["level"] != "ERROR" || row["error"] != "[redacted]" {
		t.Fatal("event identity/level/redaction lost")
	}
}

func TestSensitiveAttributesAndOpaqueObjects(t *testing.T) {
	keys := []string{"password", "session_token", "access_token", "oauth_secret", "webhook_secret", "payment_signature", "txt_record", "verification_material", "file_content", "path", "Authorization", "mysql_dsn"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			var out bytes.Buffer
			logging.New(&out, "platformapi").With(key, "secret-canary").Info("test boundary", "request_id", "request-123")
			if strings.Contains(out.String(), "secret-canary") || !strings.Contains(out.String(), "request-123") {
				t.Fatal("sensitive attribute leaked or correlation lost")
			}
		})
	}
	var out bytes.Buffer
	logging.New(&out, "platformapi").Info("opaque boundary", "details", map[string]any{"nested": "secret-canary"}, "cause", errors.New("secret-canary"))
	if strings.Contains(out.String(), "secret-canary") {
		t.Fatal("opaque payload leaked")
	}
}

func TestSensitiveGroupsAndBoundAttributes(t *testing.T) {
	var out bytes.Buffer
	logger := logging.New(&out, "platformapi")
	logger.WithGroup("credentials").With("value", "secret-canary").Info("group boundary", "other", "secret-canary")
	logger.Info("nested boundary", slog.Group("details", slog.String("password", "secret-canary")), "count", 7)
	if strings.Contains(out.String(), "secret-canary") {
		t.Fatal("nested sensitive values leaked")
	}
}
