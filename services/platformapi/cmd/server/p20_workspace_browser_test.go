package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func TestP20WorkspaceProductionBrowser(t *testing.T) {
	if os.Getenv("GOJET_P20_BROWSER_PROBE") != "1" {
		t.Skip("requires migrated database and production browser build")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "frontend/apps/workspace/dist-p12-unauth")
	mux := http.NewServeMux()
	server := httptest.NewTLSServer(mux)
	defer server.Close()
	t.Setenv("GOJET_AUTH_ALLOWED_ORIGIN", server.URL)
	t.Setenv("GOJET_AUTH_CSRF_KEY_HEX", strings.Repeat("37", 32))
	t.Setenv("GOJET_OAUTH_KEY_HEX", strings.Repeat("38", 32))
	t.Setenv("GOJET_OAUTH_KEY_ID", "p20-browser")
	t.Setenv("GOJET_ACCOUNT_ENABLED", "1")
	t.Setenv("GOJET_WORKSPACE_ENABLED", "1")
	account, enabled, err := buildAccountHandler(runtime.DB, runtime.Redis)
	if err != nil || !enabled {
		t.Fatalf("account: %v", err)
	}
	handler, enabled, err := buildWorkspaceHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !enabled {
		t.Fatalf("workspace: %v", err)
	}
	mux.Handle("/api/me", account)
	mux.Handle("/api/me/", account)
	mux.Handle("/api/workspaces", handler)
	mux.Handle("/api/workspaces/", handler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	})
	ctx := context.Background()
	users := map[string]authn.User{}
	tokens := map[string]string{}
	for _, role := range []string{"owner", "admin", "member", "viewer"} {
		u, e := runnerutil.ActivateUser(ctx, runtime.DB, "browser-"+role+"@p20.test", role, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		users[role] = u
		s, e := runnerutil.CreateSession(ctx, runtime.DB, u.ID, "p20-browser-"+role, time.Hour)
		if e != nil {
			t.Fatal(e)
		}
		tokens[role] = s.Token
	}
	owner := users["owner"]
	ws, _, err := workspace.NewStore(runtime.DB).CreateWorkspace(ctx, workspace.Principal{UserID: owner.ID, Email: owner.Email, DisplayName: owner.DisplayName}, "P20 Browser Workspace")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member", "viewer"} {
		u := users[role]
		if _, err := runtime.DB.ExecContext(ctx, "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (?,?,?,?,?)", ws.ID, u.ID, u.Email, u.DisplayName, role); err != nil {
			t.Fatal(err)
		}
	}
	// Administrator authority is separate from every Workspace role.
	adminMux := http.NewServeMux()
	adminServer := httptest.NewTLSServer(adminMux)
	defer adminServer.Close()
	t.Setenv("GOJET_ADMIN_ACCESS_ENABLED", "1")
	t.Setenv("GOJET_ADMIN_TOTP_KEY_ID", "p20-mail-browser")
	t.Setenv("GOJET_ADMIN_TOTP_KEY_HEX", strings.Repeat("6d", 32))
	t.Setenv("GOJET_ADMIN_ALLOWED_ORIGIN", adminServer.URL)
	service, _, _, err := buildAdminAccessService(runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal(err)
	}
	const adminEmail = "browser-limited@p20.test"
	const adminPassword = "P20-browser-fixture-only-Administrator-987!"
	now := time.Now().UTC().Add(-10 * time.Second)
	if _, err := adminfixture.Bootstrap(ctx, service, adminEmail, adminPassword, []string{adminaccess.PermissionPlatformRead, adminaccess.PermissionMailManage}, now); err != nil {
		t.Fatal(err)
	}
	_, adminSession, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, adminEmail, adminPassword, now)
	if err != nil {
		t.Fatal(err)
	}
	adminAPI, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		t.Fatal(err)
	}
	adminDist := filepath.Join(root, "frontend/apps/admin/dist-p20-rbac")
	operations, err := buildAdminOperationsGovernance(service, runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal(err)
	}
	adminMux.Handle("/api/admin/operations/", adminAPI.ExtendedGovernanceHandler(operations))
	adminMux.Handle("/api/admin/", adminAPI.Handler())
	adminMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.FileServer(http.Dir(adminDist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(adminDist, "index.html"))
	})
	store, err := support.NewStore(runtime.DB)
	if err != nil {
		t.Fatal(err)
	}
	audited, err := support.NewAuditedAdminMailStore(store, store)
	if err != nil {
		t.Fatal(err)
	}
	authority := newSupportAdminAuthority(service)
	mailAPI, err := support.NewAdminMailAPI(audited, authority, authority)
	if err != nil {
		t.Fatal(err)
	}
	adminMux.Handle("/api/admin/mail/", support.WithSupportCorrelation(mailAPI.Handler()))
	payload, err := json.Marshal(map[string]any{"origin": server.URL, "tokens": tokens, "workspace": ws.ID, "adminOrigin": adminServer.URL, "adminToken": adminSession.Token})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", "scripts/p20/workspace-production-browser.mjs")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "P20_BROWSER_HANDOFF="+string(payload))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("browser failed: %v\n%s", err, output)
	}
	var templateVersion, templateAudits int
	if err := runtime.DB.QueryRowContext(ctx, "SELECT MAX(version) FROM mail_templates WHERE template_key='mail-test' AND locale='en'").Scan(&templateVersion); err != nil || templateVersion != 2 {
		t.Fatalf("browser template version: %d %v", templateVersion, err)
	}
	if err := runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM support_audit_events WHERE action='admin_mail_template_updated' AND resource_id='mail-test:en'").Scan(&templateAudits); err != nil || templateAudits != 1 {
		t.Fatalf("browser template audit: %d %v", templateAudits, err)
	}
	var name string
	if err := runtime.DB.QueryRowContext(ctx, "SELECT name FROM workspaces WHERE id=?", ws.ID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "P20 browser admin saved" {
		t.Fatal("browser mutation not durable")
	}
}
