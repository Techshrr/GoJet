package logging_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Techshrr/GoJet/internal/logging"
)

func TestHTTPPreservesSemanticsAndHashesUntrustedIdentifiers(t *testing.T) {
	var logs bytes.Buffer
	request := httptest.NewRequest("POST", "/private-secret-canary?token=secret-canary", strings.NewReader("secret-canary"))
	request.Header.Set("X-Request-ID", "secret-canary")
	request.Header.Set("X-Correlation-ID", "secret-canary")
	request.Header.Set("Authorization", "Bearer secret-canary")
	response := httptest.NewRecorder()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if w != response || r != request || r.Header.Get("X-Request-ID") != "secret-canary" {
			t.Fatal("HTTP identity/semantics changed")
		}
		w.Header().Set("X-Test", "preserved")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("denied"))
	})
	logging.HTTP(logging.NewLocal(&logs, "platformapi"), handler).ServeHTTP(response, request)
	if response.Code != 403 || response.Body.String() != "denied" || response.Header().Get("X-Test") != "preserved" || strings.Contains(logs.String(), "secret-canary") {
		t.Fatal("response changed or request material leaked")
	}
	var row map[string]any
	if err := json.Unmarshal(logs.Bytes(), &row); err != nil {
		t.Fatal(err)
	}
	if row["correlation_fingerprint"] != logging.Fingerprint("correlation", "secret-canary") || row["request_fingerprint"] != logging.Fingerprint("request", "secret-canary") {
		t.Fatal("correlation fingerprints missing")
	}
}
