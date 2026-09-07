package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	adminfixture "github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

type p17AdminAuthority struct {
	CookieHeader    string
	CSRFToken       string
	Origin          string
	AdministratorID string
}

func establishRealP17AdminAuthority(ctx context.Context, apiBase, suffix string) (p17AdminAuthority, error) {
	runtime, err := adminfixture.Open()
	if err != nil {
		return p17AdminAuthority{}, err
	}
	defer runtime.Close()

	service, err := adminfixture.NewService(runtime, "p20-t020-"+suffix, 20)
	if err != nil {
		return p17AdminAuthority{}, err
	}
	now := time.Now().UTC()
	email := "p20-t020-admin-" + suffix + "@example.test"
	password := "P20-T020-" + suffix + "!AdminFixture"
	administrator, err := adminfixture.Bootstrap(ctx, service, email, password, []string{
		adminaccess.PermissionTicketsManage,
		adminaccess.PermissionMailManage,
	}, now)
	if err != nil {
		return p17AdminAuthority{}, err
	}
	principal, session, _, err := adminfixture.LoginAndConfirmMFA(ctx, service, email, password, now.Add(time.Second))
	if err != nil {
		return p17AdminAuthority{}, err
	}
	if principal.Administrator.ID != administrator.ID || !principal.Has(adminaccess.PermissionTicketsManage) || !principal.Has(adminaccess.PermissionMailManage) {
		return p17AdminAuthority{}, fmt.Errorf("P17 administrator permission principal mismatch")
	}

	cookieHeader := "gojet_admin_session=" + session.Token
	current, err := requestRaw(ctx, apiBase, http.MethodGet, "/api/admin/auth/session", nil, map[string]string{"Cookie": cookieHeader})
	if err != nil {
		return p17AdminAuthority{}, err
	}
	if current.Status != http.StatusOK {
		return p17AdminAuthority{}, fmt.Errorf("production P17 Admin session status=%d", current.Status)
	}
	administratorID := nestedString(current.Body, "administrator", "id")
	csrf := stringValue(current.Body["csrf_token"])
	if administratorID != administrator.ID || csrf == "" {
		return p17AdminAuthority{}, fmt.Errorf("production P17 Admin session identity/CSRF mismatch")
	}
	rawPermissions, ok := current.Body["permissions"].([]any)
	if !ok {
		return p17AdminAuthority{}, fmt.Errorf("production P17 Admin permission list missing")
	}
	seen := map[string]bool{}
	for _, item := range rawPermissions {
		if value, ok := item.(string); ok {
			seen[strings.TrimSpace(value)] = true
		}
	}
	if !seen[adminaccess.PermissionTicketsManage] || !seen[adminaccess.PermissionMailManage] {
		return p17AdminAuthority{}, fmt.Errorf("production P17 Admin required permissions missing")
	}
	return p17AdminAuthority{
		CookieHeader:    cookieHeader,
		CSRFToken:       csrf,
		Origin:          adminfixture.AllowedOrigin,
		AdministratorID: administratorID,
	}, nil
}

func adminUnsafeHeaders(authority p17AdminAuthority, correlation string) map[string]string {
	return map[string]string{
		"Cookie":           authority.CookieHeader,
		"Origin":           authority.Origin,
		"X-CSRF-Token":     authority.CSRFToken,
		"X-Request-ID":     correlation,
		"X-Correlation-ID": correlation,
	}
}
