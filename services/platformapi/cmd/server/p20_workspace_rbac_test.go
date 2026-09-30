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

	authn "github.com/Techshrr/GoJet/internal/auth"
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
		case strings.Contains(path, "/text-shares"):
			textHandler.ServeHTTP(response, req)
		case strings.Contains(path, "/bio-pages"):
			bioHandler.ServeHTTP(response, req)
		default:
			handler.ServeHTTP(response, req)
		}
		return response.Code
	}
	for _, resource := range []string{"organization", "members", "campaigns", "tags", "folders", "notifications", "text-shares", "bio-pages"} {
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
