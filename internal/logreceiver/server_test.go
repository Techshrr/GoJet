package logreceiver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/logging"
	"github.com/Techshrr/GoJet/internal/logreceiver"
)

func TestRealSocketRoundTripAndRestart(t *testing.T) {
	dir := t.TempDir(); os.Chmod(dir, 0700)
	socket := filepath.Join(dir, "receiver.sock")
	for attempt := 0; attempt < 2; attempt++ {
		listener, err := logreceiver.Open(socket)
		if err != nil { t.Fatal(err) }
		if other, err := logreceiver.Open(socket); err == nil { other.Close(); t.Fatal("occupied socket replaced") }
		info, _ := os.Stat(socket)
		if info.Mode().Perm() != 0600 { t.Fatal("socket not private") }
		ctx, cancel := context.WithCancel(context.Background())
		var received, local bytes.Buffer
		done := make(chan error, 1)
		go func() { done <- logreceiver.Serve(ctx, listener, &received) }()
		t.Setenv("GOJET_LOG_SOCKET", socket)
		logging.New(&local, "platformapi").Info("resource updated", "request_id", "request-123", "workspace_id", "workspace-456", "event_id", "event-789", "access_token", "secret-canary")
		if strings.Contains(local.String(), "forwarding unavailable") { t.Fatal("forwarding failed") }
		if err := logging.SendLocal(socket, []byte("{\"type\":\"health\"}\n")); err != nil { t.Fatal(err) }
		cancel()
		if err := <-done; err != nil { t.Fatal(err) }
		var row map[string]any
		if err := json.Unmarshal(received.Bytes(), &row); err != nil { t.Fatal(err) }
		if row["request_id"] != "request-123" || row["workspace_id"] != "workspace-456" || row["event_id"] != "event-789" || row["service_id"] != "platformapi" { t.Fatal("correlation changed") }
		if strings.Contains(received.String(), "secret-canary") { t.Fatal("secret leaked") }
	}
}

type brokenWriter struct{}
func (brokenWriter) Write(p []byte) (int, error) { return 0, errors.New("output unavailable") }

func TestReceiveRejectsInvalidRecordsAndRedacts(t *testing.T) {
	valid := `{"service_id":"mailworker","msg":"delivery failed","time":"2026-10-12T08:00:00Z","level":"ERROR","error":"secret-canary","nested":{"token":"secret-canary"},"event_id":"mail-123"}`
	var out bytes.Buffer
	if err := logreceiver.Receive(context.Background(), &out, []byte(valid)); err != nil { t.Fatal(err) }
	if strings.Contains(out.String(), "secret-canary") || !strings.Contains(out.String(), "mail-123") { t.Fatal("unsafe receive") }
	if err := logreceiver.Receive(context.Background(), brokenWriter{}, []byte(valid)); err == nil { t.Fatal("output failure ignored") }
	for _, raw := range []string{"invalid", `{}`, strings.Replace(valid, "mailworker", "unknown", 1), strings.Replace(valid, "2026-10-12T08:00:00Z", "bad", 1), strings.Replace(valid, "ERROR", "bad", 1), strings.Repeat("x", logging.MaxRecordBytes+1)} {
		out.Reset()
		if err := logreceiver.Receive(context.Background(), &out, []byte(raw)); err == nil || out.Len() != 0 { t.Fatal("invalid record accepted") }
	}
}

func TestUnavailableReceiverRetainsLocalRecord(t *testing.T) {
	t.Setenv("GOJET_LOG_SOCKET", filepath.Join(t.TempDir(), "absent.sock"))
	var out bytes.Buffer
	start := time.Now()
	logger := logging.New(&out, "fileworker")
	logger.Error("file scan failed", "file_id", "file-123")
	logger.Error("file scan failed", "file_id", "file-456")
	if time.Since(start) > time.Second || strings.Count(out.String(), "forwarding unavailable") != 1 || !strings.Contains(out.String(), "file-123") || !strings.Contains(out.String(), "file-456") { t.Fatal("fallback lost records, hung, or flooded warnings") }
}

func TestUnsafeDirectoryAndExistingPathRejected(t *testing.T) {
	dir := t.TempDir(); os.Chmod(dir, 0755)
	if l, err := logreceiver.Open(filepath.Join(dir, "sock")); err == nil { l.Close(); t.Fatal("public directory accepted") }
	os.Chmod(dir, 0700)
	path := filepath.Join(dir, "sock"); os.WriteFile(path, []byte("retained"), 0600)
	if l, err := logreceiver.Open(path); err == nil { l.Close(); t.Fatal("existing file replaced") }
	b, _ := os.ReadFile(path); if string(b) != "retained" { t.Fatal("existing file changed") }
}
