package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

// The external provider is a protocol fixture; sessions, CSRF replay store,
// state, binding, unbinding and audit use real production code and databases.
type bindingProviderFixture struct{ calls int }

func (a *bindingProviderFixture) Exchange(_ context.Context, input authn.OAuthProviderExchangeRequest) (authn.OAuthProviderClaim, error) {
	a.calls++
	if input.Provider != "google" || input.Code != "p20-binding-code" || input.PKCEVerifier == "" {
		return authn.OAuthProviderClaim{}, authn.ErrForbidden
	}
	return authn.OAuthProviderClaim{Subject: "p20-binding-fixture-subject", DisplayName: "Binding fixture"}, nil
}

func TestOAuthBrowserBindingLifecycle(t *testing.T) {
	if os.Getenv("GOJET_P20_OAUTH_BROWSER_PROBE") != "1" {
		t.Skip("requires isolated OAuth administrator probe database; external provider is a fixture")
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal("binding integration setup failed")
		}
	}
	ctx := context.Background()
	runtime, err := adminfixture.Open()
	must(err)
	defer runtime.Close()
	now := time.Now().UTC()
	user, err := runnerutil.ActivateUser(ctx, runtime.DB, "binding@p20.test", "Binding fixture", now)
	must(err)
	owner, err := runnerutil.CreateSession(ctx, runtime.DB, user.ID, "p20-binding-owner", time.Hour)
	must(err)
	other, err := runnerutil.CreateSession(ctx, runtime.DB, user.ID, "p20-binding-other-session", time.Hour)
	must(err)
	crypto, err := authn.NewOAuthCrypto("p20-fixture", bytes.Repeat([]byte{0x37}, 32))
	must(err)
	oauth, err := authn.NewOAuthService(runtime.DB, crypto, 10*time.Minute)
	must(err)
	replay, err := authn.NewRedisDigestReplayStore(runtime.Redis, "p20:binding:csrf", time.Hour)
	must(err)
	csrf, err := authn.NewCSRFManager(bytes.Repeat([]byte{0x23}, 32), 10*time.Minute, replay)
	must(err)
	const origin = "https://site.p20.test"
	origins, err := authn.NewOriginPolicy(origin)
	must(err)
	adapter := &bindingProviderFixture{}
	h := &accountHTTPHandler{store: authn.NewStore(runtime.DB), oauth: oauth, csrf: csrf, origins: origins, oauthAdapter: adapter}
	inner := http.NewServeMux()
	h.registerConnectedAccountRoutes(inner)
	root := http.NewServeMux()
	mountAccountRoutes(root, inner)
	authHandler := &authHTTPHandler{store: h.store, oauth: oauth}
	authInner := http.NewServeMux()
	authHandler.registerOAuthBrowserRoutes(authInner)
	mountAuthRoutes(root, authInner)
	token := func(session authn.SessionSecret) string {
		value, err := csrf.Issue(session.Session.ID, time.Now().UTC())
		must(err)
		return value
	}
	request := func(method, path string, session authn.SessionSecret, csrfToken string, stateCookie *http.Cookie, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		must(err)
		r := httptest.NewRequest(method, origin+path, bytes.NewReader(raw))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set(authn.CSRFHeaderName, csrfToken)
		r.AddCookie(&http.Cookie{Name: authn.SessionCookieName, Value: session.Token})
		if stateCookie != nil {
			r.AddCookie(stateCookie)
		}
		w := httptest.NewRecorder()
		root.ServeHTTP(w, r)
		return w
	}
	const accountPath = "/api/me/connected-accounts/google"
	start := request("POST", accountPath+"/start", owner, token(owner), nil, nil)
	if start.Code != 200 || len(start.Result().Cookies()) != 1 {
		t.Fatal("binding start failed")
	}
	var started struct {
		URL string `json:"authorization_url"`
	}
	must(json.Unmarshal(start.Body.Bytes(), &started))
	location, err := url.Parse(started.URL)
	must(err)
	state := location.Query().Get("state")
	cookie := start.Result().Cookies()[0]
	body := map[string]string{"state": state, "code": "p20-binding-code"}
	callbackPath := "/api/public/auth/google/callback?" + url.Values{"state": {state}, "code": {"p20-binding-code"}}.Encode()
	if result := request("GET", callbackPath, other, "", cookie, nil); result.Code != 403 {
		t.Fatal("different session reached binding dispatch")
	}
	preflight := request("GET", callbackPath, owner, "", cookie, nil)
	var dispatch map[string]string
	must(json.Unmarshal(preflight.Body.Bytes(), &dispatch))
	if preflight.Code != 200 || dispatch["status"] != "binding_required" || adapter.calls != 0 {
		t.Fatal("binding was consumed by login handoff dispatch")
	}
	for _, attempt := range []struct {
		session authn.SessionSecret
		csrf    string
		cookie  *http.Cookie
	}{
		{other, token(other), cookie},
		{owner, "", cookie},
		{owner, token(owner), nil},
	} {
		if got := request("POST", accountPath+"/complete", attempt.session, attempt.csrf, attempt.cookie, body); got.Code != 403 || adapter.calls != 0 {
			t.Fatal("unauthorized completion reached provider exchange")
		}
	}
	usedCSRF := token(owner)
	completed := request("POST", accountPath+"/complete", owner, usedCSRF, cookie, body)
	if completed.Code != 200 || adapter.calls != 1 {
		t.Fatalf("binding completion failed with status %d", completed.Code)
	}
	cookies := completed.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != oauthBrowserCookie("google") || cookies[0].MaxAge != -1 {
		t.Fatal("binding must clear state without replacing login session")
	}
	if got := request("POST", accountPath+"/complete", owner, token(owner), cookie, body); got.Code != 403 || adapter.calls != 1 {
		t.Fatal("consumed binding state replay accepted")
	}
	accounts, err := oauth.ListConnectedAccounts(ctx, owner.Session, time.Now().UTC())
	must(err)
	if len(accounts) != 1 || accounts[0].UserID != user.ID || accounts[0].Provider != "google" {
		t.Fatal("binding not persisted under initiating user")
	}
	if got := request("DELETE", accountPath, owner, usedCSRF, nil, nil); got.Code != 409 {
		t.Fatal("one-time CSRF reused for unbinding")
	}
	if got := request("DELETE", accountPath, owner, token(owner), nil, nil); got.Code != 200 {
		t.Fatal("fresh-authority unbinding failed")
	}
	accounts, err = oauth.ListConnectedAccounts(ctx, owner.Session, time.Now().UTC())
	must(err)
	if len(accounts) != 0 {
		t.Fatal("unbinding did not remove identity")
	}
	var audits, handoffs int
	must(runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action IN ('auth.oauth.connected','auth.oauth.disconnected')", user.ID).Scan(&audits))
	must(runtime.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM oauth_handoffs WHERE user_id=?", user.ID).Scan(&handoffs))
	if audits != 2 || handoffs != 0 {
		t.Fatal("binding audit or handoff isolation failed")
	}
}
