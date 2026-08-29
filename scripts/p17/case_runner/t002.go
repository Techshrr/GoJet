package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT002(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL 8.x, real Redis 7.x and production Admin HTTP handler proving login/session/TOTP/DB-lock/rate/Origin/CSRF and secret-safe authority")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t002", 10)
	if err != nil {
		return out, err
	}
	root, err := adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionAdminsManage, adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		return out, err
	}
	server, err := adminfixture.NewHTTPServer(service)
	if err != nil {
		return out, err
	}
	defer server.Close()

	badOrigin, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", "https://evil.p17.test", "", "", "", "p17-t002-bad-origin", map[string]any{"email": rootEmail, "password": rootPassword})
	if err != nil {
		return out, err
	}
	initial, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", "p17-t002-login-1", map[string]any{"email": rootEmail, "password": rootPassword})
	if err != nil {
		return out, err
	}
	if initial.Status != http.StatusOK || initial.Cookie == "" || adminfixture.CSRF(initial) == "" {
		return out, fmt.Errorf("initial login failed status=%d", initial.Status)
	}
	initialCookie := initial.Cookie
	initialCSRF := adminfixture.CSRF(initial)
	enroll, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/totp/enroll", adminfixture.AllowedOrigin, initialCookie, initialCSRF, "", "p17-t002-enroll", map[string]any{})
	if err != nil {
		return out, err
	}
	secret := adminfixture.Secret(enroll)
	if secret == "" {
		return out, fmt.Errorf("TOTP secret missing")
	}
	code, err := adminfixture.TOTPCode(secret, time.Now().UTC())
	if err != nil {
		return out, err
	}
	confirm, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/totp/confirm", adminfixture.AllowedOrigin, initialCookie, initialCSRF, "", "p17-t002-confirm", map[string]any{"code": code})
	if err != nil {
		return out, err
	}
	logoutInitial, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/logout", adminfixture.AllowedOrigin, initialCookie, initialCSRF, "", "p17-t002-logout-1", map[string]any{})
	if err != nil {
		return out, err
	}
	missingTOTP, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", "p17-t002-mfa-required", map[string]any{"email": rootEmail, "password": rootPassword})
	if err != nil {
		return out, err
	}
	wrongTOTP, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", "p17-t002-mfa-invalid", map[string]any{"email": rootEmail, "password": rootPassword, "totp": "000000"})
	if err != nil {
		return out, err
	}
	code, err = adminfixture.TOTPCode(secret, time.Now().UTC())
	if err != nil {
		return out, err
	}
	mfaLogin, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", "p17-t002-login-mfa", map[string]any{"email": rootEmail, "password": rootPassword, "totp": code})
	if err != nil {
		return out, err
	}
	if mfaLogin.Status != http.StatusOK || mfaLogin.Cookie == "" {
		return out, fmt.Errorf("MFA login failed status=%d", mfaLogin.Status)
	}
	sessionView, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/auth/session", "", mfaLogin.Cookie, "", "", "", nil)
	if err != nil {
		return out, err
	}
	rotatedCSRF := adminfixture.CSRF(sessionView)
	missingOriginMutation, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/logout", "", mfaLogin.Cookie, rotatedCSRF, "", "p17-t002-no-origin", map[string]any{})
	if err != nil {
		return out, err
	}
	missingCSRFMutation, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/logout", adminfixture.AllowedOrigin, mfaLogin.Cookie, "", "", "p17-t002-no-csrf", map[string]any{})
	if err != nil {
		return out, err
	}
	logoutMFA, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/logout", adminfixture.AllowedOrigin, mfaLogin.Cookie, rotatedCSRF, "", "p17-t002-logout-2", map[string]any{})
	if err != nil {
		return out, err
	}

	lockStatuses := make([]int, 0, 5)
	for i := 0; i < 5; i++ {
		attempt, reqErr := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", fmt.Sprintf("p17-t002-lock-%d", i), map[string]any{"email": rootEmail, "password": "definitely-wrong-password"})
		if reqErr != nil {
			return out, reqErr
		}
		lockStatuses = append(lockStatuses, attempt.Status)
	}
	code, err = adminfixture.TOTPCode(secret, time.Now().UTC())
	if err != nil {
		return out, err
	}
	locked, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", "p17-t002-locked", map[string]any{"email": rootEmail, "password": rootPassword, "totp": code})
	if err != nil {
		return out, err
	}

	var rateLast adminfixture.HTTPResult
	for i := 0; i < 11; i++ {
		rateLast, err = adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/auth/login", adminfixture.AllowedOrigin, "", "", "", fmt.Sprintf("p17-t002-rate-%d", i), map[string]any{"email": "unknown-rate@p17.test", "password": "not-a-user"})
		if err != nil {
			return out, err
		}
	}

	var passwordHash string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT password_hash FROM admin_credentials WHERE administrator_id=?`, root.ID).Scan(&passwordHash); err != nil {
		return out, err
	}
	var totpCipher []byte
	if err := runtime.DB.QueryRowContext(ctx, `SELECT secret_ciphertext FROM admin_totp_credentials WHERE administrator_id=?`, root.ID).Scan(&totpCipher); err != nil {
		return out, err
	}
	var tokenHash []byte
	if err := runtime.DB.QueryRowContext(ctx, `SELECT token_hash FROM admin_sessions WHERE administrator_id=? ORDER BY created_at DESC LIMIT 1`, root.ID).Scan(&tokenHash); err != nil {
		return out, err
	}
	expectedTokenHash := sha256.Sum256([]byte(mfaLogin.Cookie))
	var auditRaw string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(CONCAT(before_json,after_json,metadata_json) SEPARATOR ''),'') FROM admin_audit_events`).Scan(&auditRaw); err != nil {
		return out, err
	}
	auditCount, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.auth.login'`)
	if err != nil {
		return out, err
	}
	allLockUnauthorized := true
	for _, status := range lockStatuses {
		allLockUnauthorized = allLockUnauthorized && status == http.StatusUnauthorized
	}
	out.RecordCounts = map[string]int{"login_audit_events": auditCount, "lock_attempts": len(lockStatuses), "rate_attempts": 11}
	out.Checks = map[string]bool{
		"login_rejects_wrong_origin":                        badOrigin.Status == http.StatusForbidden,
		"session_cookie_and_csrf_are_server_issued":         initial.Status == http.StatusOK && initialCookie != "" && initialCSRF != "",
		"totp_enrollment_and_confirmation_succeed":          enroll.Status == http.StatusCreated && confirm.Status == http.StatusNoContent,
		"totp_is_required_after_enrollment":                 missingTOTP.Status == http.StatusPreconditionRequired,
		"invalid_totp_is_rejected":                          wrongTOTP.Status == http.StatusUnauthorized,
		"valid_totp_establishes_fresh_session":              mfaLogin.Status == http.StatusOK && sessionView.Status == http.StatusOK && rotatedCSRF != "",
		"unsafe_mutation_requires_origin":                   missingOriginMutation.Status == http.StatusForbidden,
		"unsafe_mutation_requires_csrf":                     missingCSRFMutation.Status == http.StatusForbidden,
		"valid_origin_and_csrf_allow_logout":                logoutInitial.Status == http.StatusNoContent && logoutMFA.Status == http.StatusNoContent,
		"five_bad_passwords_trigger_database_lock":          allLockUnauthorized && locked.Status == http.StatusLocked,
		"redis_rate_authority_fails_closed_after_limit":     rateLast.Status == http.StatusTooManyRequests,
		"password_is_persisted_only_as_kdf_hash":            strings.HasPrefix(passwordHash, "pbkdf2-sha256$600000$") && passwordHash != rootPassword && !strings.Contains(passwordHash, rootPassword),
		"totp_secret_is_encrypted_at_rest":                  !bytes.Contains(totpCipher, []byte(secret)),
		"session_secret_is_persisted_only_as_hash":          bytes.Equal(tokenHash, expectedTokenHash[:]) && !strings.Contains(mfaLogin.Raw, mfaLogin.Cookie),
		"audit_does_not_contain_raw_authentication_secrets": !strings.Contains(auditRaw, rootPassword) && !strings.Contains(auditRaw, secret) && !strings.Contains(auditRaw, mfaLogin.Cookie),
		"admin_http_is_private_no_store_and_noindex":        adminfixture.NoStoreNoIndex(initial) && adminfixture.NoStoreNoIndex(sessionView),
	}
	pass(&out)
	return out, nil
}
