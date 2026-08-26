package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
)

const authHTTPBodyLimit = 16 << 10

type authHTTPHandler struct {
	db           *sql.DB
	store        *authn.Store
	login        *authn.PasswordLoginService
	registration *authn.RegistrationService
	password     *authn.PasswordService
	verification *authn.VerificationService
	emailCode    *authn.EmailCodeService
	recovery     *authn.PasswordRecoveryService
	oauth        *authn.OAuthService
	social       *authn.SocialRegistrationService
	grantKey     securetoken.Key
	testAuth     bool
}

type publicOAuthProvider struct {
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

type deterministicOAuthAdapter struct{}

func (deterministicOAuthAdapter) Exchange(_ context.Context, request authn.OAuthProviderExchangeRequest) (authn.OAuthProviderClaim, error) {
	if strings.TrimSpace(request.Code) == "p15-browser-provider-error" {
		return authn.OAuthProviderClaim{}, authn.ErrForbidden
	}
	if strings.TrimSpace(request.Code) == "" || !authn.ValidProvider(request.Provider) {
		return authn.OAuthProviderClaim{}, authn.ErrInvalid
	}
	digest := sha256.Sum256([]byte(request.Provider + "\x00" + request.Code))
	return authn.OAuthProviderClaim{
		Subject:       "p15-local-" + hex.EncodeToString(digest[:12]),
		Email:         request.Provider + "-p15@example.test",
		EmailVerified: true,
		DisplayName:   "P15 Local OAuth",
	}, nil
}

func buildAuthHandler(db *sql.DB, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_AUTH_ENABLED") != "1" {
		return nil, false, nil
	}
	grantKey, err := loadSecureTokenKey("GOJET_AUTH_GRANT_KEY_ID", "GOJET_AUTH_GRANT_KEY_HEX")
	if err != nil {
		return nil, false, err
	}
	oauthKeyID := strings.TrimSpace(os.Getenv("GOJET_OAUTH_KEY_ID"))
	oauthKeyBytes, err := decodeExactHexKey(os.Getenv("GOJET_OAUTH_KEY_HEX"))
	if err != nil || oauthKeyID == "" {
		return nil, false, authn.ErrInvalid
	}
	oauthCrypto, err := authn.NewOAuthCrypto(oauthKeyID, oauthKeyBytes)
	if err != nil {
		return nil, false, err
	}
	oauthService, err := authn.NewOAuthService(db, oauthCrypto, 10*time.Minute)
	if err != nil {
		return nil, false, err
	}
	registration, err := authn.NewRegistrationService(db, grantKey)
	if err != nil {
		return nil, false, err
	}
	password, err := authn.NewPasswordService(db)
	if err != nil {
		return nil, false, err
	}
	verification, err := authn.NewVerificationService(db)
	if err != nil {
		return nil, false, err
	}
	emailCode, err := authn.NewEmailCodeService(db, grantKey, 0)
	if err != nil {
		return nil, false, err
	}
	recovery, err := authn.NewPasswordRecoveryService(db, grantKey)
	if err != nil {
		return nil, false, err
	}
	login, err := authn.NewPasswordLoginService(db, 0)
	if err != nil {
		return nil, false, err
	}
	social, err := authn.NewSocialRegistrationService(db, grantKey, 0)
	if err != nil {
		return nil, false, err
	}
	h := &authHTTPHandler{
		db:           db,
		store:        authn.NewStore(db),
		login:        login,
		registration: registration,
		password:     password,
		verification: verification,
		emailCode:    emailCode,
		recovery:     recovery,
		oauth:        oauthService,
		social:       social,
		grantKey:     grantKey,
		testAuth:     testAuth,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", h.handlePasswordLogin)
	mux.HandleFunc("POST /api/public/login-email-code", h.handleLoginEmailCode)
	mux.HandleFunc("GET /api/public/auth/providers", h.handleProviders)
	mux.HandleFunc("POST /api/auth/register", h.handleRegister)
	mux.HandleFunc("POST /api/public/email-code", h.handleVerificationResend)
	mux.HandleFunc("POST /api/public/register-email-code", h.handleVerifyEmail)
	mux.HandleFunc("POST /api/auth/verifyemail", h.handleVerifyEmail)
	mux.HandleFunc("POST /api/mail/verification", h.handleVerificationResend)
	mux.HandleFunc("POST /api/auth/forgotpassword", h.handleForgotPassword)
	mux.HandleFunc("POST /api/auth/resetpassword", h.handleResetPassword)
	mux.HandleFunc("GET /api/public/auth/{provider}/callback", h.handleOAuthCallback)
	mux.HandleFunc("POST /api/public/auth/handoff", h.handleOAuthHandoff)
	mux.HandleFunc("GET /api/public/auth/social-registration", h.handleSocialRegistrationState)
	mux.HandleFunc("POST /api/public/auth/social-registration/complete", h.handleSocialRegistrationComplete)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authn.ApplyPrivateAuthHeaders(w.Header())
		mux.ServeHTTP(w, r)
	}), true, nil
}

func mountAuthRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"POST /api/auth/login",
		"POST /api/public/login-email-code",
		"GET /api/public/auth/providers",
		"POST /api/auth/register",
		"POST /api/public/email-code",
		"POST /api/public/register-email-code",
		"POST /api/auth/verifyemail",
		"POST /api/mail/verification",
		"POST /api/auth/forgotpassword",
		"POST /api/auth/resetpassword",
		"GET /api/public/auth/{provider}/callback",
		"POST /api/public/auth/handoff",
		"GET /api/public/auth/social-registration",
		"POST /api/public/auth/social-registration/complete",
	} {
		root.Handle(pattern, handler)
	}
}

func loadSecureTokenKey(idEnv, hexEnv string) (securetoken.Key, error) {
	id := strings.TrimSpace(os.Getenv(idEnv))
	key, err := decodeExactHexKey(os.Getenv(hexEnv))
	if err != nil || id == "" {
		return securetoken.Key{}, authn.ErrInvalid
	}
	return securetoken.NewKey(id, key)
}

func decodeExactHexKey(raw string) ([]byte, error) {
	value, err := hex.DecodeString(strings.TrimSpace(raw))
	if err != nil || len(value) != 32 {
		return nil, authn.ErrInvalid
	}
	return value, nil
}

func decodeAuthJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, authHTTPBodyLimit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeAuthProblem(w, http.StatusBadRequest, "invalid_request", "The request could not be processed.")
		return false
	}
	return true
}

func authCorrelation(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		if len(raw) > 128 {
			return "", authn.ErrInvalid
		}
		for _, r := range raw {
			if r < 0x21 || r > 0x7e {
				return "", authn.ErrInvalid
			}
		}
		return raw, nil
	}
	secret, err := authn.NewOpaqueSecret("req_", 16)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

func writeAuthJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAuthProblem(w http.ResponseWriter, status int, code, message string) {
	writeAuthJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeAuthServiceError(w http.ResponseWriter, err error, tokenContext bool) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := "The authentication service could not complete the request."
	switch {
	case errors.Is(err, authn.ErrInvalid):
		status, code, message = http.StatusBadRequest, "invalid_request", "The request could not be processed."
	case errors.Is(err, authn.ErrUnauthorized):
		status = http.StatusUnauthorized
		if tokenContext {
			code, message = "invalid_token", "The authentication link or code is not valid."
		} else {
			code, message = "invalid_credentials", "The supplied credentials are not valid."
		}
	case errors.Is(err, authn.ErrVerificationRequired):
		status, code, message = http.StatusForbidden, "verification_required", "Email verification is required."
	case errors.Is(err, authn.ErrLocked):
		status, code, message = http.StatusLocked, "account_locked", "This account is temporarily locked."
	case errors.Is(err, authn.ErrRateLimited):
		status, code, message = http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again later."
		w.Header().Set("Retry-After", "60")
	case errors.Is(err, authn.ErrExpired):
		status, code, message = http.StatusGone, "expired_token", "The authentication link or code has expired."
	case errors.Is(err, authn.ErrReplay):
		status, code, message = http.StatusConflict, "reused_token", "The authentication link or code has already been used."
	case errors.Is(err, authn.ErrRevoked):
		status, code, message = http.StatusGone, "revoked_token", "The authentication link or code is no longer valid."
	case errors.Is(err, authn.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "The requested authentication state conflicts with an existing account."
	case errors.Is(err, authn.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "The authentication request was denied."
	case errors.Is(err, authn.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "The authentication resource was not found."
	}
	writeAuthProblem(w, status, code, message)
}

func setAuthSessionCookie(w http.ResponseWriter, secret authn.SessionSecret) error {
	cookie, err := authn.NewSessionCookie(secret.Token, secret.Session.ExpiresAt)
	if err != nil {
		return err
	}
	http.SetCookie(w, cookie)
	return nil
}

func (h *authHTTPHandler) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email         string `json:"email"`
		Password      string `json:"password"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	secret, err := h.login.LoginPassword(r.Context(), authn.PasswordLoginInput{Email: input.Email, Password: input.Password, CorrelationID: correlationID})
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := setAuthSessionCookie(w, secret); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "authenticated", "expires_at": secret.Session.ExpiresAt})
}

func (h *authHTTPHandler) handleLoginEmailCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email         string `json:"email"`
		Code          string `json:"code"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, input.Code != "")
		return
	}
	if strings.TrimSpace(input.Code) != "" && strings.TrimSpace(input.Email) == "" {
		secret, consumeErr := h.emailCode.ConsumeLoginCode(r.Context(), input.Code, correlationID)
		if consumeErr != nil {
			writeAuthServiceError(w, consumeErr, true)
			return
		}
		if err := setAuthSessionCookie(w, secret); err != nil {
			writeAuthServiceError(w, err, true)
			return
		}
		writeAuthJSON(w, http.StatusOK, map[string]any{"status": "authenticated", "expires_at": secret.Session.ExpiresAt})
		return
	}
	if strings.TrimSpace(input.Email) == "" || strings.TrimSpace(input.Code) != "" {
		writeAuthServiceError(w, authn.ErrInvalid, false)
		return
	}
	if err := h.emailCode.RequestLoginCode(r.Context(), input.Email, correlationID); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusAccepted, map[string]string{"status": "code_sent"})
}

func (h *authHTTPHandler) handleProviders(w http.ResponseWriter, r *http.Request) {
	configs, err := h.oauth.ListProviderConfigs(r.Context())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	providers := make([]publicOAuthProvider, 0, len(configs))
	for _, cfg := range configs {
		providers = append(providers, publicOAuthProvider{Provider: cfg.Provider, Enabled: cfg.Enabled && cfg.Configured})
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

func (h *authHTTPHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email         string `json:"email"`
		DisplayName   string `json:"display_name"`
		Password      string `json:"password"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	if input.Password == "" || len(input.Password) > 1024 || !utf8.ValidString(input.Password) {
		writeAuthServiceError(w, authn.ErrInvalid, false)
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	result, err := h.registration.Register(r.Context(), authn.RegistrationInput{Email: input.Email, DisplayName: input.DisplayName, CorrelationID: correlationID})
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.password.SetInitialPassword(r.Context(), result.User.ID, input.Password, correlationID+"-password"); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusAccepted, map[string]any{"status": "verification_required", "expires_at": result.Grant.ExpiresAt})
}

func (h *authHTTPHandler) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code          string `json:"code"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	result, err := h.verification.VerifyEmail(r.Context(), authn.EmailVerificationInput{Code: input.Code, CorrelationID: correlationID})
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "verified", "verified_at": result.VerifiedAt})
}

func (h *authHTTPHandler) handleVerificationResend(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email         string `json:"email"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.reissueVerification(r.Context(), input.Email, correlationID); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusAccepted, map[string]string{"status": "verification_sent"})
}

func (h *authHTTPHandler) reissueVerification(ctx context.Context, rawEmail, correlationID string) error {
	normalized, err := authn.NormalizeEmail(rawEmail)
	if err != nil {
		return authn.ErrInvalid
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID, email, status string
	var verifiedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,email,status,email_verified_at FROM auth_users WHERE email_normalized=? FOR UPDATE`, normalized).
		Scan(&userID, &email, &status, &verifiedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if status != authn.UserStatusPendingVerification || verifiedAt.Valid {
		return tx.Commit()
	}
	var recent int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_one_time_grants
WHERE user_id=? AND purpose='email_verification' AND consumed_at IS NULL AND invalidated_at IS NULL AND created_at>=?`,
		userID, now.Add(-60*time.Second)).Scan(&recent); err != nil {
		return err
	}
	if recent > 0 {
		return authn.ErrRateLimited
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_one_time_grants SET invalidated_at=?
WHERE user_id=? AND purpose='email_verification' AND consumed_at IS NULL AND invalidated_at IS NULL`, now, userID); err != nil {
		return err
	}
	grantSecret, err := authn.NewOpaqueSecret("grt_", 18)
	if err != nil {
		return err
	}
	grantID := grantSecret.Value
	code, err := h.grantKey.Derive("gvc_", "email_verification", grantID)
	if err != nil {
		return err
	}
	hash := securetoken.Hash(code)
	expiresAt := now.Add(authn.EmailVerificationTTL)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_one_time_grants
(id,purpose,user_id,email_normalized,token_hash,token_key_id,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,correlation_id,created_at)
VALUES (?,'email_verification',?,?,?,?,0,8,?,NULL,NULL,?,?)`,
		grantID, userID, normalized, hash[:], h.grantKey.ID(), expiresAt, correlationID, now); err != nil {
		return err
	}
	if err := support.EnqueueMailTx(ctx, tx, support.MailEnqueueInput{
		TemplateKey:    "auth-email-verification",
		Locale:         "en",
		RecipientKind:  "auth_user",
		RecipientValue: email,
		ResourceType:   "auth_one_time_grant",
		ResourceID:     grantID,
	}, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.email_verification.resent','auth_one_time_grant',?,'success',?,JSON_OBJECT('purpose','email_verification'),?)`,
		userID, grantID, correlationID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (h *authHTTPHandler) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email         string `json:"email"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	if err := h.recovery.RequestReset(r.Context(), input.Email, correlationID); err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusAccepted, map[string]string{"status": "submitted"})
}

func (h *authHTTPHandler) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Token         string `json:"token"`
		Password      string `json:"password"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	if err := h.recovery.ResetPassword(r.Context(), input.Token, input.Password, correlationID); err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "password_reset"})
}

func (h *authHTTPHandler) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	provider := strings.TrimSpace(r.PathValue("provider"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if !authn.ValidProvider(provider) || !strings.HasPrefix(state, "gos_") || code == "" {
		writeAuthProblem(w, http.StatusBadRequest, "state_error", "The provider callback could not be validated.")
		return
	}
	correlationID, err := authCorrelation(r.Header.Get("X-GoJet-Correlation-ID"))
	if err != nil {
		writeAuthProblem(w, http.StatusBadRequest, "state_error", "The provider callback could not be validated.")
		return
	}
	if !h.testAuth {
		writeAuthProblem(w, http.StatusServiceUnavailable, "provider_error", "The identity provider could not complete the request.")
		return
	}
	callback, err := h.oauth.Callback(r.Context(), deterministicOAuthAdapter{}, authn.OAuthCallbackInput{Provider: provider, State: state, Code: code, CorrelationID: correlationID}, time.Now().UTC())
	if err != nil {
		if errors.Is(err, authn.ErrForbidden) || errors.Is(err, authn.ErrExpired) || errors.Is(err, authn.ErrReplay) {
			writeAuthProblem(w, http.StatusBadRequest, "state_error", "The provider callback could not be validated.")
			return
		}
		writeAuthProblem(w, http.StatusBadGateway, "provider_error", "The identity provider could not complete the request.")
		return
	}
	handoff, err := h.oauth.CreateBrowserHandoff(r.Context(), callback, correlationID+"-handoff", time.Now().UTC())
	if err != nil {
		writeAuthProblem(w, http.StatusBadGateway, "provider_error", "The identity provider could not complete the request.")
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "handoff_ready", "handoff_code": handoff.Code, "expires_at": handoff.ExpiresAt})
}

func (h *authHTTPHandler) handleOAuthHandoff(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code          string `json:"code"`
		CorrelationID string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	result, err := h.oauth.ExchangeBrowserHandoff(r.Context(), input.Code, correlationID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	if result.Session != nil {
		if err := setAuthSessionCookie(w, *result.Session); err != nil {
			writeAuthServiceError(w, err, true)
			return
		}
		writeAuthJSON(w, http.StatusOK, map[string]any{"status": "authenticated", "expires_at": result.ExpiresAt})
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "registration_required", "registration_code": result.SocialRegistrationCode, "expires_at": result.ExpiresAt})
}

func (h *authHTTPHandler) handleSocialRegistrationState(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state, err := h.social.GetState(r.Context(), code, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"provider":                    state.Provider,
		"email":                       state.Email,
		"provider_email_verified":     state.ProviderEmailVerified,
		"requires_email_verification": state.RequiresEmailVerification,
		"display_name":                state.DisplayName,
		"expires_at":                  state.ExpiresAt,
	})
}

func (h *authHTTPHandler) handleSocialRegistrationComplete(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code             string `json:"code"`
		Email            string `json:"email"`
		VerificationCode string `json:"verification_code"`
		CorrelationID    string `json:"correlation_id"`
	}
	if !decodeAuthJSON(w, r, &input) {
		return
	}
	correlationID, err := authCorrelation(input.CorrelationID)
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	state, err := h.social.GetState(r.Context(), input.Code, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	if state.RequiresEmailVerification && strings.TrimSpace(input.VerificationCode) == "" {
		email := strings.TrimSpace(input.Email)
		if email == "" {
			email = strings.TrimSpace(state.Email)
		}
		if email == "" {
			writeAuthProblem(w, http.StatusBadRequest, "email_required", "An email address is required to finish registration.")
			return
		}
		grant, issueErr := h.social.RequestEmailVerification(r.Context(), input.Code, email, correlationID, time.Now().UTC())
		if issueErr != nil {
			writeAuthServiceError(w, issueErr, true)
			return
		}
		writeAuthJSON(w, http.StatusAccepted, map[string]any{"status": "verification_required", "expires_at": grant.ExpiresAt})
		return
	}
	secret, err := h.social.Complete(r.Context(), authn.CompleteSocialRegistrationInput{SocialCode: input.Code, VerificationCode: input.VerificationCode, CorrelationID: correlationID}, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	if err := setAuthSessionCookie(w, secret); err != nil {
		writeAuthServiceError(w, err, true)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"status": "authenticated", "expires_at": secret.Session.ExpiresAt})
}
