package main

import (
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/redis/go-redis/v9"
)

type trustAbuseAdminHTTPHandler struct {
	base   *trustAdminHTTPHandler
	store  *trust.Store
	action *trust.AbuseActionService
}

func buildTrustAbuseAdminHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_TRUST_ADMIN_ENABLED") != "1" {
		return nil, false, nil
	}
	if db == nil || redisClient == nil {
		return nil, false, trust.ErrInvalid
	}

	csrfKey, err := decodeExactHexKey(os.Getenv("GOJET_AUTH_CSRF_KEY_HEX"))
	if err != nil {
		return nil, false, err
	}
	rawOrigins := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	if rawOrigins == "" {
		return nil, false, trust.ErrInvalid
	}
	origins := make([]string, 0, 2)
	for _, raw := range strings.Split(rawOrigins, ",") {
		if value := strings.TrimSpace(raw); value != "" {
			origins = append(origins, value)
		}
	}
	originPolicy, err := authn.NewOriginPolicy(origins...)
	if err != nil {
		return nil, false, err
	}
	replay, err := authn.NewRedisDigestReplayStore(redisClient, "auth:csrf:p16-trust-admin", time.Hour)
	if err != nil {
		return nil, false, err
	}
	csrf, err := authn.NewCSRFManager(csrfKey, trustAdminCSRFTTL, replay)
	if err != nil {
		return nil, false, err
	}

	securityActor := strings.TrimSpace(os.Getenv("GOJET_TEST_TRUST_SECURITY_ADMIN_ACTOR"))
	domainActor := strings.TrimSpace(os.Getenv("GOJET_TEST_TRUST_DOMAIN_ADMIN_ACTOR"))
	if (securityActor != "" || domainActor != "") && !testAuth {
		return nil, false, trust.ErrInvalid
	}
	authorizer := trustAdminPermissionAuthorizer{
		testAuth:      testAuth,
		securityActor: securityActor,
		domainActor:   domainActor,
	}
	store := trust.NewStore(db)
	base := &trustAdminHTTPHandler{
		store:      store,
		authStore:  authn.NewStore(db),
		csrf:       csrf,
		origins:    originPolicy,
		authorizer: authorizer,
	}

	var actionService *trust.AbuseActionService
	policyVersion := strings.TrimSpace(os.Getenv("GOJET_RISK_POLICY_VERSION"))
	if policyVersion != "" {
		runtimeTTL := durationEnvValue("GOJET_TRUST_ABUSE_RUNTIME_TTL", 10*time.Minute, time.Minute, 24*time.Hour)
		if runtimeTTL <= 0 {
			return nil, false, trust.ErrInvalid
		}
		actionService, err = trust.NewAbuseActionService(store, links.NewRedisRiskStore(redisClient), policyVersion, runtimeTTL)
		if err != nil {
			return nil, false, err
		}
	}

	h := &trustAbuseAdminHTTPHandler{base: base, store: store, action: actionService}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/abuse", h.handleList)
	mux.HandleFunc("GET /api/admin/abuse/{reportId}", h.handleDetail)
	mux.HandleFunc("POST /api/admin/abuse/{reportId}/actions", h.handleAction)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyTrustAdminHeaders(w.Header())
		mux.ServeHTTP(w, r)
	}), true, nil
}

func mountTrustAbuseAdminRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"GET /api/admin/abuse",
		"GET /api/admin/abuse/{reportId}",
		"POST /api/admin/abuse/{reportId}/actions",
	} {
		root.Handle(pattern, handler)
	}
}

func (h *trustAbuseAdminHTTPHandler) handleList(w http.ResponseWriter, r *http.Request) {
	session, ok := h.base.authorizedSession(w, r, trust.SecurityManagePermission)
	if !ok {
		return
	}
	limit, ok := trustAdminLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.ListAdminAbuseReports(r.Context(), limit)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	csrfToken, err := h.base.csrf.Issue(session.ID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"items": items, "csrf_token": csrfToken})
}

func (h *trustAbuseAdminHTTPHandler) handleDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.base.authorizedSession(w, r, trust.SecurityManagePermission); !ok {
		return
	}
	reportID, ok := trustAdminID(w, r.PathValue("reportId"))
	if !ok {
		return
	}
	item, err := h.store.GetAdminAbuseReport(r.Context(), reportID)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"report": item})
}

func (h *trustAbuseAdminHTTPHandler) handleAction(w http.ResponseWriter, r *http.Request) {
	session, ok := h.base.mutationSession(w, r, trust.SecurityManagePermission)
	if !ok {
		return
	}
	reportID, ok := trustAdminID(w, r.PathValue("reportId"))
	if !ok {
		return
	}
	var body struct {
		Action           string `json:"action"`
		ExpectedVersion  uint64 `json:"expected_version"`
		ExactFingerprint string `json:"exact_fingerprint"`
		Reason           string `json:"reason"`
	}
	if !decodeAuthJSON(w, r, &body) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeTrustAdminError(w, trust.ErrInvalid)
		return
	}
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	action := strings.TrimSpace(body.Action)
	changed := false

	switch action {
	case "investigate", "resolve", "dismiss":
		status := trust.AbuseInvestigating
		if action == "resolve" {
			status = trust.AbuseResolved
		} else if action == "dismiss" {
			status = trust.AbuseDismissed
		}
		result, err := h.store.TransitionAbuseReport(r.Context(), trust.AbuseAdminTransitionInput{
			ReportID:        reportID,
			ExpectedVersion: body.ExpectedVersion,
			ToStatus:        status,
			Reason:          body.Reason,
			ActorID:         session.UserID,
			CorrelationID:   correlationID,
			IdempotencyKey:  idempotencyKey,
		}, h.base.authorizer)
		if err != nil {
			writeTrustAdminError(w, err)
			return
		}
		changed = result.Changed
	case "block", "suspend", "restore":
		if h.action == nil {
			writeTrustAdminUnavailable(w)
			return
		}
		resourceAction := trust.AbuseActionBlock
		if action == "suspend" {
			resourceAction = trust.AbuseActionSuspend
		} else if action == "restore" {
			resourceAction = trust.AbuseActionRestore
		}
		result, err := h.action.Apply(r.Context(), trust.AbuseResourceActionInput{
			ReportID:          reportID,
			Action:            resourceAction,
			ExactFingerprint:  strings.TrimSpace(body.ExactFingerprint),
			Reason:            body.Reason,
			ActorID:           session.UserID,
			CorrelationID:     correlationID,
			IdempotencyKey:    idempotencyKey,
			Now:               time.Now().UTC(),
		}, h.base.authorizer)
		if err != nil {
			writeTrustAdminError(w, err)
			return
		}
		changed = result.Changed
	default:
		writeTrustAdminError(w, trust.ErrInvalid)
		return
	}

	item, err := h.store.GetAdminAbuseReport(r.Context(), reportID)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"report": item, "action": action, "changed": changed})
}
