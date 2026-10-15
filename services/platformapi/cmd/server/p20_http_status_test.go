package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func TestP20HTTPStatusExpiryContexts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		mutation bool
		status   int
		code     string
	}{
		{"csrf-expired", authn.ErrExpired, true, 419, "csrf_expired"},
		{"grant-expired", authn.ErrExpired, false, 410, "expired_token"},
		{"grant-revoked", authn.ErrRevoked, false, 410, "revoked_token"},
		{"forbidden", authn.ErrForbidden, true, 403, "forbidden"},
		{"unauthenticated", authn.ErrUnauthorized, false, 401, "invalid_credentials"},
		{"rate-limited", authn.ErrRateLimited, false, 429, "rate_limited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			if tc.mutation {
				writeAuthMutationError(w, tc.err)
			} else {
				writeAuthServiceError(w, tc.err, false)
			}
			if w.Code != tc.status || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("wrong status/error classification: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

// Real durable session, real Redis one-time authority, and the production
// profile handler. The clock of token issuance changes; no sleeps or API mocks.
func TestP20HTTPStatusDurableCSRF(t *testing.T) {
	if os.Getenv("GOJET_P20_RBAC_PROBE") != "1" {
		t.Skip("requires isolated migrated MySQL and Redis")
	}
	runtime, err := adminfixture.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	ctx := context.Background()
	user, err := runnerutil.ActivateUser(ctx, runtime.DB, "p20-status@example.test", "Original", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := runnerutil.CreateSession(ctx, runtime.DB, user.ID, "p20-status", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := authn.NewRedisDigestReplayStore(runtime.Redis, "p20:t030:csrf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := authn.NewCSRFManager(bytes.Repeat([]byte{0x48}, 32), 10*time.Minute, replay)
	if err != nil {
		t.Fatal(err)
	}
	const origin = "https://account.p20.test"
	origins, err := authn.NewOriginPolicy(origin)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := authn.NewAccountService(runtime.DB)
	if err != nil {
		t.Fatal(err)
	}
	h := &accountHTTPHandler{store: authn.NewStore(runtime.DB), accounts: accounts, csrf: csrf, origins: origins}
	issue := func(at time.Time) string {
		t.Helper()
		token, err := csrf.Issue(session.Session.ID, at)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	call := func(cookie, token, requestOrigin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPatch, origin+"/api/me/profile", strings.NewReader(`{"display_name":"Updated"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", requestOrigin)
		r.Header.Set(authn.CSRFHeaderName, token)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: cookie})
		}
		w := httptest.NewRecorder()
		h.handleProfile(w, r)
		return w
	}
	for _, tc := range []struct {
		name, cookie, token, origin string
		status                      int
	}{
		{"expired-csrf", session.Token, issue(time.Now().Add(-11 * time.Minute)), origin, 419},
		{"missing-csrf", session.Token, "", origin, 403},
		{"invalid-csrf", session.Token, "invalid", origin, 403},
		{"invalid-origin", session.Token, issue(time.Now()), "https://foreign.example", 403},
		{"missing-session", "", issue(time.Now()), origin, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := call(tc.cookie, tc.token, tc.origin)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d", w.Code, tc.status)
			}
			current, err := h.store.GetUserByID(ctx, user.ID)
			if err != nil || current.DisplayName != "Original" {
				t.Fatal("rejected request mutated profile")
			}
			if tc.status == 419 && !strings.Contains(w.Body.String(), `"code":"csrf_expired"`) {
				t.Fatal("missing contextual error")
			}
		})
	}
	for _, scope := range []string{"admin-oauth-expired", "admin-trust-expired"} {
		t.Run(scope, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, origin+"/api/admin/probe", nil)
			r.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: session.Token})
			r.Header.Set("Origin", origin)
			r.Header.Set(authn.CSRFHeaderName, issue(time.Now().Add(-11*time.Minute)))
			w := httptest.NewRecorder()
			if scope == "admin-oauth-expired" {
				admin := &adminOAuthHTTPHandler{store: h.store, csrf: csrf, origins: origins}
				if _, _, ok := admin.mutationAuthority(w, r); ok {
					t.Fatal("expired CSRF authorized admin mutation")
				}
			} else {
				admin := &trustAdminHTTPHandler{authStore: h.store, csrf: csrf, origins: origins}
				if _, ok := admin.mutationSession(w, r, "settings.manage"); ok {
					t.Fatal("expired CSRF authorized trust mutation")
				}
			}
			if w.Code != 419 {
				t.Fatalf("admin expired CSRF status=%d", w.Code)
			}
		})
	}
	t.Run("refresh-and-retry", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, origin+"/api/me", nil)
		r.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: session.Token})
		w := httptest.NewRecorder()
		h.handleMe(w, r)
		var me struct {
			CSRF string `json:"csrf_token"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.CSRF == "" {
			t.Fatal("session refresh failed")
		}
		updated := call(session.Token, me.CSRF, origin)
		if updated.Code != 200 {
			t.Fatalf("fresh token rejected: %d", updated.Code)
		}
		current, err := h.store.GetUserByID(ctx, user.ID)
		if err != nil || current.DisplayName != "Updated" {
			t.Fatal("successful mutation not durable")
		}
		if repeated := call(session.Token, me.CSRF, origin); repeated.Code != 409 {
			t.Fatalf("replayed token status=%d", repeated.Code)
		}
	})
	t.Run("database-unavailable", func(t *testing.T) {
		if err := runtime.DB.Close(); err != nil {
			t.Fatal(err)
		}
		w := call(session.Token, issue(time.Now()), origin)
		if w.Code != 500 || !strings.Contains(w.Body.String(), `"code":"internal_error"`) {
			t.Fatalf("unavailable database disguised as success: %d", w.Code)
		}
	})
}
