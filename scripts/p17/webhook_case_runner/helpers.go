package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

var webhookFixtureKey = bytes.Repeat([]byte{0x71}, 32)

func newWebhookAuthority(r *adminfixture.Runtime, resolver trust.IPResolver, dialer trust.ContextDialer) (*adminaccess.WorkspaceWebhookAuthority, error) {
	cipher, err := adminaccess.NewSecretCipher("p17-webhook-fixture-v1", webhookFixtureKey)
	if err != nil {
		return nil, err
	}
	return adminaccess.NewWorkspaceWebhookAuthority(r.DB, r.Redis, cipher, resolver, dialer)
}

func seedWebhookWorkspace(ctx context.Context, db *sql.DB, workspaceID string, roles map[string]string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at) VALUES (?,?,'active',1,?,?,?)`, workspaceID, "P17 Webhook "+workspaceID, "fixture", now, now); err != nil {
		return err
	}
	for userID, role := range roles {
		email := strings.ToLower(userID) + "@p17.test"
		if _, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,?,?,?,?)`, workspaceID, userID, email, userID, role, now, now); err != nil {
			return err
		}
	}
	return nil
}

func scalarWebhook(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

type fixtureResolver struct {
	mu        sync.Mutex
	answers   map[string][][]net.IPAddr
	calls     map[string]int
	fallback  []net.IPAddr
}

func newFixtureResolver() *fixtureResolver {
	return &fixtureResolver{
		answers:  map[string][][]net.IPAddr{},
		calls:    map[string]int{},
		fallback: []net.IPAddr{{IP: net.ParseIP("1.1.1.1")}},
	}
}

func (r *fixtureResolver) set(host string, sequences ...[]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	converted := make([][]net.IPAddr, 0, len(sequences))
	for _, sequence := range sequences {
		items := make([]net.IPAddr, 0, len(sequence))
		for _, raw := range sequence {
			items = append(items, net.IPAddr{IP: net.ParseIP(raw)})
		}
		converted = append(converted, items)
	}
	r.answers[host] = converted
	r.calls[host] = 0
}

func (r *fixtureResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	sequences, ok := r.answers[host]
	if !ok || len(sequences) == 0 {
		return append([]net.IPAddr(nil), r.fallback...), nil
	}
	index := r.calls[host]
	r.calls[host] = index + 1
	if index >= len(sequences) {
		index = len(sequences) - 1
	}
	return append([]net.IPAddr(nil), sequences[index]...), nil
}

func (r *fixtureResolver) count(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[strings.ToLower(strings.TrimSuffix(host, "."))]
}

type localFixtureDialer struct {
	mu      sync.Mutex
	address string
	calls   int
}

func (d *localFixtureDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	d.mu.Lock()
	d.calls++
	address := d.address
	d.mu.Unlock()
	return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, network, address)
}

func (d *localFixtureDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type capturedWebhookRequest struct {
	DeliveryID    string
	IdempotencyID string
	EventType     string
	Timestamp     string
	Signature     string
	Body          []byte
}

type webhookReceiver struct {
	mu        sync.Mutex
	statuses  []int
	redirects []string
	requests  []capturedWebhookRequest
	server    *httptest.Server
}

func newWebhookReceiver(statuses ...int) *webhookReceiver {
	r := &webhookReceiver{statuses: append([]int(nil), statuses...)}
	if len(r.statuses) == 0 {
		r.statuses = []int{http.StatusNoContent}
	}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(req.Body, 512*1024))
		r.mu.Lock()
		index := len(r.requests)
		r.requests = append(r.requests, capturedWebhookRequest{
			DeliveryID: req.Header.Get("X-GoJet-Delivery"), IdempotencyID: req.Header.Get("X-GoJet-Idempotency-Key"),
			EventType: req.Header.Get("X-GoJet-Event"), Timestamp: req.Header.Get("X-GoJet-Timestamp"),
			Signature: req.Header.Get("X-GoJet-Signature"), Body: append([]byte(nil), body...),
		})
		redirect := ""
		if index < len(r.redirects) {
			redirect = r.redirects[index]
		}
		status := r.statuses[len(r.statuses)-1]
		if index < len(r.statuses) {
			status = r.statuses[index]
		}
		r.mu.Unlock()
		if redirect != "" {
			w.Header().Set("Location", redirect)
		}
		w.WriteHeader(status)
	}))
	return r
}

func (r *webhookReceiver) close() { r.server.Close() }

func (r *webhookReceiver) address() string {
	return strings.TrimPrefix(r.server.URL, "http://")
}

func (r *webhookReceiver) endpoint(host string) string {
	_, port, _ := net.SplitHostPort(r.address())
	return "http://" + host + ":" + port + "/hooks"
}

func (r *webhookReceiver) snapshot() []capturedWebhookRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]capturedWebhookRequest, len(r.requests))
	copy(out, r.requests)
	return out
}

func parseWebhookTimestamp(raw string) (time.Time, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(value, 0).UTC(), nil
}

func webhookAPIRequest(handler http.Handler, method, path, actor string, body any) (int, http.Header, map[string]any, string, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, "", err
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if actor != "" {
		req.Header.Set("X-GoJet-Test-Actor", actor)
		req.Header.Set("X-GoJet-Test-Email", strings.ToLower(actor)+"@p17.test")
	}
	req.Header.Set("X-Request-ID", "p17-webhook-http")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, rec.Header().Clone(), decoded, rec.Body.String(), nil
}

func webhookNoStoreNoIndex(header http.Header) bool {
	return strings.Contains(strings.ToLower(header.Get("Cache-Control")), "no-store") && strings.Contains(strings.ToLower(header.Get("X-Robots-Tag")), "noindex")
}

func requireWebhookAPI(authority *adminaccess.WorkspaceWebhookAuthority) (http.Handler, error) {
	api, err := adminaccess.NewWorkspaceWebhookHTTPAPI(authority, true)
	if err != nil {
		return nil, err
	}
	return api.Handler(), nil
}

func mustJSONRaw(value string) json.RawMessage { return json.RawMessage(value) }

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}
