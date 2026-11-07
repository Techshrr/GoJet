package main

import (
	"context"
	"database/sql"
	"encoding/json"
	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Only an explicitly disposable, freshly migrated CI database may run this test.
// HTTP builders and browser use native cookie auth, never test actor headers.
func TestP22NativeAdminIntegration(t *testing.T) {
	if os.Getenv("P22_DISPOSABLE_ADMIN_CI") != "1" {
		t.Skip("requires disposable native admin CI database")
	}
	db, err := sql.Open("mysql", os.Getenv("GOJET_MYSQL_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	redisClient := redis.NewClient(&redis.Options{Addr: os.Getenv("GOJET_REDIS_ADDR")})
	defer redisClient.Close()
	root := http.NewServeMux()
	server := httptest.NewTLSServer(root)
	defer server.Close()
	for k, v := range map[string]string{
		"GOJET_TEST_AUTH_ENABLED": "0", "GOJET_ADMIN_ACCESS_ENABLED": "1", "GOJET_BILLING_ENABLED": "1", "GOJET_SUPPORT_ENABLED": "1", "GOJET_TRUST_ADMIN_ENABLED": "1",
		"GOJET_ADMIN_ALLOWED_ORIGIN": server.URL, "GOJET_AUTH_ALLOWED_ORIGIN": server.URL,
		"GOJET_ADMIN_TOTP_KEY_ID": "p22-ci", "GOJET_ADMIN_TOTP_KEY_HEX": strings.Repeat("31", 32), "GOJET_AUTH_CSRF_KEY_HEX": strings.Repeat("42", 32),
		"GOJET_TURNSTILE_SECRET": "ci-not-called-no-submissions", "GOJET_ADMIN_LOGIN_RATE_LIMIT": "100",
	} {
		t.Setenv(k, v)
	}
	service, _, enabled, err := buildAdminAccessService(db, redisClient)
	if err != nil || !enabled {
		t.Fatalf("admin build: %v", err)
	}
	const email = "admin@example.test"
	const password = "Native-Admin-CI-Password-2026!"
	administrator, err := service.BootstrapAdministrator(context.Background(), email, "CI administrator", password, adminaccess.PermissionCatalog, "p22-bootstrap", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	adminHandler, enabled, err := buildAdminAccessHandler(db, redisClient)
	if err != nil || !enabled {
		t.Fatalf("admin handler: %v", err)
	}
	root.Handle("/api/admin/", adminHandler)
	billingHandler, enabled, err := buildBillingHandler(db, redisClient, false)
	if err != nil || !enabled {
		t.Fatalf("billing: %v", err)
	}
	mountBillingRoutes(root, billingHandler)
	supportHandler, enabled, err := buildSupportHandler(db, redisClient, false)
	if err != nil || !enabled {
		t.Fatalf("support: %v", err)
	}
	mountSupportRoutes(root, supportHandler)
	trustHandler, enabled, err := buildTrustAdminHandler(db, redisClient, false)
	if err != nil || !enabled {
		t.Fatalf("trust: %v", err)
	}
	mountTrustAdminRoutes(root, trustHandler)
	abuseHandler, enabled, err := buildTrustAbuseAdminHandler(db, redisClient, false)
	if err != nil || !enabled {
		t.Fatalf("abuse: %v", err)
	}
	mountTrustAbuseAdminRoutes(root, abuseHandler)
	repo, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(repo, "frontend/apps/admin/dist")
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	client := server.Client()
	call := func(method, path, body string, cookie *http.Cookie, csrf, origin string) (int, map[string]any, *http.Response) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.Header.Set("X-CSRF-Token", csrf)
		req.Header.Set("X-Correlation-ID", "p22-native-http")
		req.Header.Set("X-Request-Correlation-ID", "p22-native-http")
		req.Header.Set("Idempotency-Key", "p22-"+time.Now().Format("150405.000000000"))
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var data map[string]any
		_ = json.Unmarshal(raw, &data)
		return res.StatusCode, data, res
	}
	status, _, res := call("POST", "/api/admin/auth/login", `{"email":"`+email+`","password":"`+password+`"}`, nil, "", server.URL)
	if status != 200 {
		t.Fatalf("login=%d", status)
	}
	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == adminaccess.AdminSessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no administrator cookie")
	}
	reads := []string{"/api/admin/destination-risks", "/api/admin/domain-risks", "/api/admin/abuse", "/api/admin/support/tickets", "/api/admin/mail/queue", "/api/admin/mail/settings", "/api/admin/mail/templates", "/api/admin/plans", "/api/admin/payments", "/api/admin/fx", "/api/admin/settings/general", "/api/admin/settings/brand"}
	for _, path := range reads {
		status, data, _ := call("GET", path, "", cookie, "", server.URL)
		if status != 200 {
			t.Fatalf("native GET %s=%d %#v", path, status, data)
		}
		if strings.Contains(path, "/settings/general") || strings.Contains(path, "/settings/brand") {
			if data["setting"].(map[string]any)["version"] != float64(0) {
				t.Fatal("unset setting version must be zero")
			}
		}
		status, _, _ = call("GET", path, "", nil, "", server.URL)
		if status != 401 {
			t.Fatalf("anonymous %s=%d", path, status)
		}
		status, _, _ = call("GET", path, "", &http.Cookie{Name: "__Host-gojet_session", Value: cookie.Value}, "", server.URL)
		if status != 401 {
			t.Fatalf("workspace cookie %s=%d", path, status)
		}
	}
	// Native browser tests exercise the exact built application over TLS.
	if os.Getenv("P22_BROWSER_CI") == "1" {
		cmd := exec.Command("node", filepath.Join(repo, "scripts/p22/native-admin-browser.mjs"))
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "P22_ADMIN_ORIGIN="+server.URL, "P22_ADMIN_EMAIL="+email, "P22_ADMIN_PASSWORD="+password)
		out, err := cmd.CombinedOutput()
		t.Log(string(out))
		if err != nil {
			t.Fatalf("native browser: %v", err)
		}
	}
	// Removing exact permissions immediately denies the same live session.
	for _, pair := range [][2]string{{"security.manage", "/api/admin/destination-risks"}, {"domains.risk.manage", "/api/admin/domain-risks"}, {"tickets.manage", "/api/admin/support/tickets"}, {"mail.manage", "/api/admin/mail/queue"}, {"billing.manage", "/api/admin/plans"}} {
		_, err = db.Exec(`DELETE p FROM admin_role_permissions p JOIN admin_role_assignments a ON a.role_id=p.role_id WHERE a.administrator_id=? AND p.permission=?`, administrator.ID, pair[0])
		if err != nil {
			t.Fatal(err)
		}
		status, _, _ := call("GET", pair[1], "", cookie, "", server.URL)
		if status != 403 {
			t.Fatalf("revoked permission %s=%d", pair[0], status)
		}
		_, err = db.Exec(`INSERT INTO admin_role_permissions(role_id,permission) SELECT role_id,? FROM admin_role_assignments WHERE administrator_id=?`, pair[0], administrator.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	_, data, _ := call("GET", "/api/admin/auth/session", "", cookie, "", server.URL)
	csrf := data["csrf_token"].(string)
	for _, path := range []string{"/api/admin/destination-risks/1/rescan", "/api/admin/plans", "/api/admin/mail/test"} {
		for _, bad := range [][2]string{{"", server.URL}, {csrf, "https://untrusted.example"}} {
			status, _, _ := call("POST", path, "{}", cookie, bad[0], bad[1])
			if status != 401 && status != 403 {
				t.Fatalf("unsafe mutation %s=%d", path, status)
			}
		}
	}
	// Correct authority reaches resource validation, rather than an auth rejection.
	status, _, _ = call("POST", "/api/admin/destination-risks/999999/rescan", "{}", cookie, csrf, server.URL)
	if status != 404 {
		t.Fatalf("trust authorized mutation=%d", status)
	}
	status, _, _ = call("POST", "/api/admin/auth/logout", "{}", cookie, csrf, server.URL)
	if status != 204 {
		t.Fatalf("logout=%d", status)
	}
	for _, path := range reads {
		status, _, _ := call("GET", path, "", cookie, "", server.URL)
		if status != 401 {
			t.Fatalf("revoked session %s=%d", path, status)
		}
	}
}
