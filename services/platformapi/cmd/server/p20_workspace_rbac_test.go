package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	qrcodes "github.com/Techshrr/GoJet/internal/qr"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

// Fixture provisioning is explicit; every request uses a durable P15 session and
// the production principal resolver. No test-header authentication is enabled.
func TestP20WorkspaceRBACDurableSessions(t *testing.T) {
	if os.Getenv("GOJET_P20_RBAC_PROBE") != "1" {
		t.Skip("requires isolated migrated MySQL and Redis")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()
	key := bytes.Repeat([]byte{0x37}, 32)
	const origin = "https://workspace.p20.test"
	t.Setenv("GOJET_WORKSPACE_ENABLED", "1")
	t.Setenv("GOJET_AUTH_ALLOWED_ORIGIN", origin)
	t.Setenv("GOJET_AUTH_CSRF_KEY_HEX", hex.EncodeToString(key))
	handler, enabled, err := buildWorkspaceHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !enabled {
		t.Fatalf("production workspace handler unavailable: %v", err)
	}
	t.Setenv("GOJET_TEXT_ENABLED", "1")
	t.Setenv("GOJET_TEXT_WORKSPACE_QUOTA", "100")
	t.Setenv("GOJET_TEXT_PUBLIC_AUTH_SECRET", strings.Repeat("t", 32))
	t.Setenv("GOJET_BIO_ENABLED", "1")
	t.Setenv("GOJET_BIO_WORKSPACE_QUOTA", "100")
	textHandler, textEnabled, err := buildTextHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !textEnabled {
		t.Fatalf("production Text handler unavailable: %v", err)
	}
	bioHandler, bioEnabled, err := buildBioHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !bioEnabled {
		t.Fatalf("production Bio handler unavailable: %v", err)
	}
	domainStore := domains.NewMySQLStore(runtime.DB)
	linkStore := links.NewMySQLStoreWithCustomDomainAuthority(runtime.DB, domainStore)
	linkAuthority, err := buildLinksSessionAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal(err)
	}
	domainAuthority, err := buildDomainsSessionAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal(err)
	}
	qrAuthority, err := buildQRSessionAuthority(runtime.DB, runtime.Redis)
	if err != nil {
		t.Fatal(err)
	}
	linkHandler := links.NewAPIWithActorResolver(linkStore, linkAuthority.resolve).Handler()
	domainHandler := domains.NewWorkspaceDomainsAPIWithActorResolver(domainStore, domainAuthority.resolve).Handler()
	qrHandler := qrcodes.NewAPIWithActorResolver(qrcodes.NewStore(runtime.DB, 100), linkStore, links.NewRedisRiskStore(runtime.Redis), qrAuthority.resolve).Handler()
	t.Setenv("GOJET_FILES_ENABLED", "1")
	t.Setenv("GOJET_FILE_WORKSPACE_MAX_FILES", "100")
	t.Setenv("GOJET_FILE_WORKSPACE_MAX_BYTES", "1048576")
	t.Setenv("GOJET_FILE_MAX_UPLOAD_BYTES", "1024")
	t.Setenv("GOJET_FILE_STORAGE_ROOT", t.TempDir())
	t.Setenv("GOJET_FILE_PUBLIC_AUTH_SECRET", strings.Repeat("f", 32))
	t.Setenv("GOJET_FILE_TYPE_ALLOWLIST", "txt=text/plain")
	fileHandler, fileEnabled, err := buildFilesHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !fileEnabled {
		t.Fatalf("production Files handler unavailable: %v", err)
	}
	t.Setenv("GOJET_BILLING_ENABLED", "1")
	billingHandler, billingEnabled, err := buildBillingHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !billingEnabled {
		t.Fatalf("production Billing handler unavailable: %v", err)
	}
	t.Setenv("GOJET_SUPPORT_ENABLED", "1")
	// This read-only Support matrix never calls the external verifier.
	t.Setenv("GOJET_TURNSTILE_SECRET", "rbac-unused-verifier-secret")
	supportHandler, supportEnabled, err := buildSupportHandler(runtime.DB, runtime.Redis, false)
	if err != nil || !supportEnabled {
		t.Fatalf("production Support handler unavailable: %v", err)
	}
	replay, err := authn.NewRedisDigestReplayStore(runtime.Redis, "auth:csrf:account", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := authn.NewCSRFManager(key, 10*time.Minute, replay)
	if err != nil {
		t.Fatal(err)
	}
	users := map[string]authn.User{}
	sessions := map[string]authn.SessionSecret{}
	for _, role := range []string{"owner", "admin", "member", "viewer", "outsider"} {
		user, err := runnerutil.ActivateUser(ctx, runtime.DB, role+"-p20-rbac@example.test", role, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		users[role] = user
		session, err := runnerutil.CreateSession(ctx, runtime.DB, user.ID, "p20-rbac-"+role, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		sessions[role] = session
	}
	store := workspace.NewStore(runtime.DB)
	owner := users["owner"]
	ws, membership, err := store.CreateWorkspace(ctx, workspace.Principal{UserID: owner.ID, Email: owner.Email, DisplayName: owner.DisplayName}, "P20 RBAC tenant")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"admin", "member", "viewer"} {
		user := users[role]
		_, err := runtime.DB.ExecContext(ctx, "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (?,?,?,?,?)", ws.ID, user.ID, user.Email, user.DisplayName, role)
		if err != nil {
			t.Fatal(err)
		}
	}
	outsider := users["outsider"]
	foreign, _, err := store.CreateWorkspace(ctx, workspace.Principal{UserID: outsider.ID, Email: outsider.Email, DisplayName: outsider.DisplayName}, "Foreign tenant")
	if err != nil {
		t.Fatal(err)
	}
	call := func(role, method, path, body string) int {
		t.Helper()
		req := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		// A forged test principal must never override the authenticated user.
		req.Header.Set("X-GoJet-Test-Actor", owner.ID)
		if role != "anonymous" {
			session := sessions[role]
			req.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: session.Token})
			if method != "GET" {
				token, err := csrf.Issue(session.Session.ID, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("X-CSRF-Token", token)
			}
		}
		response := httptest.NewRecorder()
		switch {
		case strings.Contains(path, "/files"):
			fileHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/billing"):
			billingHandler.ServeHTTP(response, req)
		case strings.HasPrefix(path, "/api/support/"):
			supportHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/links"):
			linkHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/domains"):
			domainHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/qr-codes"):
			qrHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/text-shares"):
			textHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/bio-pages"):
			bioHandler.ServeHTTP(response, req)
		default:
			handler.ServeHTTP(response, req)
		}
		return response.Code
	}
	for _, resource := range []string{"organization", "members", "campaigns", "tags", "folders", "notifications", "text-shares", "bio-pages", "links", "domains", "qr-codes", "files"} {
		t.Run("read-"+resource, func(t *testing.T) {
			path := "/api/workspaces/" + ws.ID + "/" + resource
			for _, role := range []string{"owner", "admin", "member", "viewer"} {
				if code := call(role, "GET", path, ""); code != 200 {
					t.Fatalf("%s read status %d", role, code)
				}
				if code := call(role, "GET", "/api/workspaces/"+foreign.ID+"/"+resource, ""); code != 403 {
					t.Fatalf("cross-tenant %s status %d", role, code)
				}
			}
			if code := call("anonymous", "GET", path, ""); code != 401 {
				t.Fatalf("anonymous status %d", code)
			}
		})
	}
	for _, role := range []string{"owner", "admin", "member", "viewer", "outsider"} {
		t.Run("organization-write-"+role, func(t *testing.T) {
			before, err := store.GetOrganization(ctx, ws.ID)
			if err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"name":"changed-%s","description":"RBAC probe","expected_version":%d}`, role, before.Version)
			want := 403
			if role == "owner" || role == "admin" {
				want = 200
			} else if role == "outsider" {
				want = 403
			}
			if code := call(role, "PATCH", "/api/workspaces/"+ws.ID+"/organization", body); code != want {
				t.Fatalf("write status %d, want %d", code, want)
			}
			after, err := store.GetOrganization(ctx, ws.ID)
			if err != nil {
				t.Fatal(err)
			}
			if want == 200 {
				if after.Version != before.Version+1 || after.Name != "changed-"+role {
					t.Fatal("authorized mutation not durably committed")
				}
			} else if after.Version != before.Version || after.Name != before.Name {
				t.Fatal("denied mutation changed durable organization")
			}
		})
	}
	for _, resource := range []string{"campaigns", "tags", "folders"} {
		for _, role := range []string{"owner", "admin", "member", "viewer", "outsider"} {
			t.Run("create-"+resource+"-"+role, func(t *testing.T) {
				var before, after int
				query := "SELECT COUNT(*) FROM workspace_" + resource + " WHERE workspace_id=?"
				if err := runtime.DB.QueryRowContext(ctx, query, ws.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				want := 201
				if role == "viewer" || role == "outsider" {
					want = 403
				}
				body := fmt.Sprintf(`{"name":"rbac-%s-%s"}`, resource, role)
				if code := call(role, "POST", "/api/workspaces/"+ws.ID+"/"+resource, body); code != want {
					t.Fatalf("create status %d, want %d", code, want)
				}
				if err := runtime.DB.QueryRowContext(ctx, query, ws.ID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if want == 201 {
					before++
				}
				if after != before {
					t.Fatal("durable row count disagrees with authorization")
				}
			})
		}
	}
	for _, resource := range []string{"text-shares", "bio-pages"} {
		for _, role := range []string{"owner", "admin", "member", "viewer", "outsider"} {
			t.Run("product-create-"+resource+"-"+role, func(t *testing.T) {
				table := "text_shares"
				body := `{"title":"RBAC text","content":"private fixture","visibility":"public","change_reason":"RBAC test"}`
				if resource == "bio-pages" {
					table = "bio_pages"
					body = `{"title":"RBAC bio","bio":"fixture","links":[],"change_reason":"RBAC test"}`
				}
				var before, after int
				query := "SELECT COUNT(*) FROM " + table + " WHERE workspace_id=?"
				if err := runtime.DB.QueryRowContext(ctx, query, ws.ID).Scan(&before); err != nil {
					t.Fatal(err)
				}
				want := 201
				if role == "viewer" || role == "outsider" {
					want = 403
				}
				if code := call(role, "POST", "/api/workspaces/"+ws.ID+"/"+resource, body); code != want {
					t.Fatalf("product create status %d, want %d", code, want)
				}
				if err := runtime.DB.QueryRowContext(ctx, query, ws.ID).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if want == 201 {
					before++
				}
				if after != before {
					t.Fatal("product mutation disagrees with durable authorization")
				}
			})
		}
	}
	for _, resource := range []string{"links", "domains", "qr-codes", "files"} {
		for _, role := range []string{"viewer", "outsider"} {
			t.Run("deny-create-"+resource+"-"+role, func(t *testing.T) {
				// Authorization must reject before even parsing an invalid payload.
				if code := call(role, "POST", "/api/workspaces/"+ws.ID+"/"+resource, "{"); code != 403 {
					t.Fatalf("unauthorized mutation status %d, want 403", code)
				}
			})
		}
	}
	for _, role := range []string{"owner", "admin", "member", "viewer", "outsider", "anonymous"} {
		t.Run("billing-summary-"+role, func(t *testing.T) {
			want := 403
			if role == "owner" || role == "admin" {
				want = 200
			} else if role == "anonymous" {
				want = 401
			}
			if code := call(role, "GET", "/api/workspaces/"+ws.ID+"/billing", ""); code != want {
				t.Fatalf("billing status %d, want %d", code, want)
			}
		})
		t.Run("support-list-"+role, func(t *testing.T) {
			want := 200
			if role == "outsider" {
				want = 403
			} else if role == "anonymous" {
				want = 401
			}
			if code := call(role, "GET", "/api/support/tickets?workspace_id="+ws.ID, ""); code != want {
				t.Fatalf("support status %d, want %d", code, want)
			}
		})
	}
	memberPath := fmt.Sprintf("/api/workspaces/%s/members/%d", ws.ID, membership.ID)
	for _, method := range []string{"PATCH", "DELETE"} {
		body := `{"reason":"last owner probe"}`
		if method == "PATCH" {
			body = `{"role":"member","reason":"last owner probe"}`
		}
		if code := call("owner", method, memberPath, body); code != 409 {
			t.Fatalf("last-owner %s status %d", method, code)
		}
		if code := call("admin", method, memberPath, body); code != 403 {
			t.Fatalf("admin owner bypass %s status %d", method, code)
		}
	}
	current, err := store.GetMembership(ctx, ws.ID, owner.ID)
	if err != nil || current.Role != "owner" {
		t.Fatal("last owner not preserved")
	}
}

func TestP20AdministratorPermissionBoundary(t *testing.T) {
	if os.Getenv("GOJET_P20_RBAC_PROBE") != "1" {
		t.Skip("requires isolated migrated MySQL and Redis")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()
	service, err := adminfixture.NewService(runtime, "p20-rbac-admin", 10)
	if err != nil {
		t.Fatal(err)
	}
	const email = "p20-limited-admin@example.test"
	const password = "P20-fixture-only-Administrator-987!"
	now := time.Now().UTC().Add(-10 * time.Second)
	_, err = adminfixture.Bootstrap(ctx, service, email, password, []string{adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		t.Fatal(err)
	}
	_, adminSession, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, email, password, now)
	if err != nil {
		t.Fatal(err)
	}
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		t.Fatal(err)
	}
	user, err := runnerutil.ActivateUser(ctx, runtime.DB, "p20-admin-boundary-user@example.test", "Workspace user", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	userSession, err := runnerutil.CreateSession(ctx, runtime.DB, user.ID, "p20-admin-boundary", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/admin/overview", "/api/admin/administrators", "/api/admin/users", "/api/admin/workspaces"} {
		for _, identity := range []string{"anonymous", "workspace", "limited-admin"} {
			t.Run(identity+path, func(t *testing.T) {
				req := httptest.NewRequest("GET", adminfixture.AllowedOrigin+path, nil)
				req.Header.Set("X-GoJet-Test-Actor", "forged-root")
				req.Header.Set("X-GoJet-Test-Admin-Permissions", "admins.manage,users.manage,workspaces.manage")
				want := 401
				if identity == "workspace" {
					req.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: userSession.Token})
				} else if identity == "limited-admin" {
					req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: adminSession.Token})
					want = 403
					if path == "/api/admin/overview" {
						want = 200
					}
				}
				response := httptest.NewRecorder()
				api.Handler().ServeHTTP(response, req)
				if response.Code != want {
					t.Fatalf("administrator boundary status %d, want %d", response.Code, want)
				}
				if strings.Contains(response.Body.String(), adminSession.Token) || strings.Contains(response.Body.String(), userSession.Token) {
					t.Fatal("session secret reflected")
				}
			})
		}
	}
}
