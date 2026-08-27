package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/redis/go-redis/v9"
)

const trustAdminCSRFTTL = 10 * time.Minute

type trustAdminPermissionAuthorizer struct {
	testAuth      bool
	securityActor string
	domainActor   string
}

func (a trustAdminPermissionAuthorizer) Authorize(_ context.Context, actorID, permission string) error {
	actorID = strings.TrimSpace(actorID)
	if !a.testAuth || actorID == "" {
		return trust.ErrUnauthorized
	}
	switch permission {
	case trust.SecurityManagePermission:
		if actorID == a.securityActor {
			return nil
		}
	case trust.DomainsRiskManagePermission:
		if actorID == a.domainActor {
			return nil
		}
	}
	return trust.ErrUnauthorized
}

type deterministicDomainRiskProvider struct {
	name    string
	outcome trust.ProviderOutcome
}

func (p deterministicDomainRiskProvider) Observe(_ context.Context, _ string) (trust.ProviderObservation, error) {
	return trust.ProviderObservation{
		Provider:   p.name,
		Outcome:    p.outcome,
		SignalCode: "test-" + string(p.outcome),
		Evidence:   map[string]any{"fixture": "server-side-deterministic"},
		ObservedAt: time.Now().UTC().Truncate(time.Microsecond),
	}, nil
}

type trustAdminHTTPHandler struct {
	store      *trust.Store
	domainRisk *trust.DomainRiskService
	authStore  *authn.Store
	csrf       *authn.CSRFManager
	origins    *authn.OriginPolicy
	authorizer trust.PermissionAuthorizer
}

func buildTrustAdminHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
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
	domainRisk, err := buildAdminDomainRiskService(store, testAuth)
	if err != nil {
		return nil, false, err
	}
	h := &trustAdminHTTPHandler{
		store:      store,
		domainRisk: domainRisk,
		authStore:  authn.NewStore(db),
		csrf:       csrf,
		origins:    originPolicy,
		authorizer: authorizer,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/destination-risks", h.handleDestinationRiskList)
	mux.HandleFunc("GET /api/admin/destination-risks/{riskId}", h.handleDestinationRiskDetail)
	mux.HandleFunc("POST /api/admin/destination-risks/{riskId}/rescan", h.handleDestinationRiskRescan)
	mux.HandleFunc("POST /api/admin/destination-risks/{riskId}/override", h.handleDestinationRiskOverride)
	mux.HandleFunc("GET /api/admin/domain-risks", h.handleDomainRiskList)
	mux.HandleFunc("GET /api/admin/domain-risks/{domainId}", h.handleDomainRiskDetail)
	mux.HandleFunc("POST /api/admin/domain-risks/{domainId}/revalidate", h.handleDomainRiskRevalidate)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyTrustAdminHeaders(w.Header())
		mux.ServeHTTP(w, r)
	}), true, nil
}

func buildAdminDomainRiskService(store *trust.Store, testAuth bool) (*trust.DomainRiskService, error) {
	providerName := strings.TrimSpace(os.Getenv("GOJET_DOMAIN_RISK_PROVIDER_NAME"))
	if providerName == "" {
		providerName = strings.TrimSpace(os.Getenv("GOJET_RISK_PROVIDER_NAME"))
	}
	policyVersion := strings.TrimSpace(os.Getenv("GOJET_DOMAIN_RISK_POLICY_VERSION"))
	if policyVersion == "" {
		policyVersion = strings.TrimSpace(os.Getenv("GOJET_RISK_POLICY_VERSION"))
	}
	if providerName == "" || policyVersion == "" {
		return nil, nil
	}
	policy := trust.DomainRiskPolicy{
		Version:           policyVersion,
		RequiredProviders: []string{providerName},
		AllowTTL:          durationEnvValue("GOJET_DOMAIN_RISK_ALLOW_TTL", 15*time.Minute, time.Minute, 24*time.Hour),
		RevalidateAfter:   durationEnvValue("GOJET_DOMAIN_RISK_REVALIDATE_AFTER", 10*time.Minute, time.Minute, 24*time.Hour),
		RetryAfter:        durationEnvValue("GOJET_DOMAIN_RISK_RETRY_AFTER", 2*time.Minute, time.Second, 24*time.Hour),
	}
	if !policy.Validate() {
		return nil, trust.ErrInvalid
	}

	if testAuth {
		switch strings.TrimSpace(os.Getenv("GOJET_TEST_TRUST_DOMAIN_PROVIDER_OUTCOME")) {
		case "allow":
			return trust.NewDomainRiskService(store, policy, deterministicDomainRiskProvider{name: providerName, outcome: trust.ProviderAllow})
		case "review":
			return trust.NewDomainRiskService(store, policy, deterministicDomainRiskProvider{name: providerName, outcome: trust.ProviderReview})
		case "block":
			return trust.NewDomainRiskService(store, policy, deterministicDomainRiskProvider{name: providerName, outcome: trust.ProviderBlock})
		case "unknown":
			return trust.NewDomainRiskService(store, policy, deterministicDomainRiskProvider{name: providerName, outcome: trust.ProviderUnknown})
		case "unavailable":
			return trust.NewDomainRiskService(store, policy, deterministicDomainRiskProvider{name: providerName, outcome: trust.ProviderUnavailable})
		case "":
		default:
			return nil, trust.ErrInvalid
		}
	}

	endpoint := strings.TrimSpace(os.Getenv("GOJET_DOMAIN_RISK_PROVIDER_ENDPOINT"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("GOJET_RISK_PROVIDER_ENDPOINT"))
	}
	if endpoint == "" {
		return nil, nil
	}
	provider := trust.SemanticProviderClient{
		Name:       providerName,
		Endpoint:   endpoint,
		HTTPClient: trust.NewInspectionHTTPClient(nil, nil),
	}
	return trust.NewDomainRiskService(store, policy, provider)
}

func durationEnvValue(name string, fallback, minimum, maximum time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0
	}
	return parsed
}

func mountTrustAdminRoutes(root *http.ServeMux, handler http.Handler) {
	for _, pattern := range []string{
		"GET /api/admin/destination-risks",
		"GET /api/admin/destination-risks/{riskId}",
		"POST /api/admin/destination-risks/{riskId}/rescan",
		"POST /api/admin/destination-risks/{riskId}/override",
		"GET /api/admin/domain-risks",
		"GET /api/admin/domain-risks/{domainId}",
		"POST /api/admin/domain-risks/{domainId}/revalidate",
	} {
		root.Handle(pattern, handler)
	}
}

func (h *trustAdminHTTPHandler) authorizedSession(w http.ResponseWriter, r *http.Request, permission string) (authn.Session, bool) {
	session, err := authn.AuthenticateRequest(r.Context(), h.authStore, r, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, false
	}
	if err := h.authorizer.Authorize(r.Context(), session.UserID, permission); err != nil {
		writeTrustAdminError(w, trust.ErrUnauthorized)
		return authn.Session{}, false
	}
	return session, true
}

func (h *trustAdminHTTPHandler) mutationSession(w http.ResponseWriter, r *http.Request, permission string) (authn.Session, bool) {
	now := time.Now().UTC()
	session, err := authn.AuthenticateRequest(r.Context(), h.authStore, r, now)
	if err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, false
	}
	if _, err := authn.AuthorizeUnsafeMutation(r.Context(), r, session, h.origins, h.csrf, now); err != nil {
		writeAuthServiceError(w, err, false)
		return authn.Session{}, false
	}
	if err := h.authorizer.Authorize(r.Context(), session.UserID, permission); err != nil {
		writeTrustAdminError(w, trust.ErrUnauthorized)
		return authn.Session{}, false
	}
	return session, true
}

func (h *trustAdminHTTPHandler) handleDestinationRiskList(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authorizedSession(w, r, trust.SecurityManagePermission)
	if !ok {
		return
	}
	limit, ok := trustAdminLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.ListAdminDestinationRisks(r.Context(), limit)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	csrfToken, err := h.csrf.Issue(session.ID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"items": items, "csrf_token": csrfToken})
}

func (h *trustAdminHTTPHandler) handleDestinationRiskDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizedSession(w, r, trust.SecurityManagePermission); !ok {
		return
	}
	riskID, ok := trustAdminID(w, r.PathValue("riskId"))
	if !ok {
		return
	}
	item, err := h.store.GetAdminDestinationRisk(r.Context(), riskID)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"risk": item})
}

func (h *trustAdminHTTPHandler) handleDestinationRiskRescan(w http.ResponseWriter, r *http.Request) {
	session, ok := h.mutationSession(w, r, trust.SecurityManagePermission)
	if !ok {
		return
	}
	riskID, ok := trustAdminID(w, r.PathValue("riskId"))
	if !ok {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeTrustAdminError(w, trust.ErrInvalid)
		return
	}
	result, err := h.store.AdminRescanDestinationRisk(r.Context(), trust.AdminDestinationRescanInput{
		RiskID:         riskID,
		ActorID:        session.UserID,
		CorrelationID:  correlationID,
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	}, h.authorizer)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusAccepted, map[string]any{
		"risk_id": result.Scan.ID,
		"created": result.Created,
		"status":  result.Scan.Status,
	})
}

func (h *trustAdminHTTPHandler) handleDestinationRiskOverride(w http.ResponseWriter, r *http.Request) {
	session, ok := h.mutationSession(w, r, trust.SecurityManagePermission)
	if !ok {
		return
	}
	riskID, ok := trustAdminID(w, r.PathValue("riskId"))
	if !ok {
		return
	}
	var body struct {
		Decision  trust.DecisionState `json:"decision"`
		Reason    string              `json:"reason"`
		ExpiresAt time.Time           `json:"expires_at"`
	}
	if !decodeAuthJSON(w, r, &body) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeTrustAdminError(w, trust.ErrInvalid)
		return
	}
	override, err := h.store.AdminOverrideDestinationRisk(r.Context(), trust.AdminDestinationOverrideInput{
		RiskID:        riskID,
		Decision:      body.Decision,
		Reason:        body.Reason,
		ActorID:       session.UserID,
		CorrelationID: correlationID,
		ExpiresAt:     body.ExpiresAt,
	}, h.authorizer, time.Now().UTC())
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{
		"override_id": override.ID,
		"decision":    override.Decision,
		"expires_at":  override.ExpiresAt,
	})
}

func (h *trustAdminHTTPHandler) handleDomainRiskList(w http.ResponseWriter, r *http.Request) {
	session, ok := h.authorizedSession(w, r, trust.DomainsRiskManagePermission)
	if !ok {
		return
	}
	limit, ok := trustAdminLimit(w, r)
	if !ok {
		return
	}
	items, err := h.store.ListAdminDomainRisks(r.Context(), limit)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	csrfToken, err := h.csrf.Issue(session.ID, time.Now().UTC())
	if err != nil {
		writeAuthServiceError(w, err, false)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"items": items, "csrf_token": csrfToken})
}

func (h *trustAdminHTTPHandler) handleDomainRiskDetail(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorizedSession(w, r, trust.DomainsRiskManagePermission); !ok {
		return
	}
	domainID, ok := trustAdminID(w, r.PathValue("domainId"))
	if !ok {
		return
	}
	item, err := h.store.GetAdminDomainRisk(r.Context(), domainID)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"domain_risk": item})
}

func (h *trustAdminHTTPHandler) handleDomainRiskRevalidate(w http.ResponseWriter, r *http.Request) {
	session, ok := h.mutationSession(w, r, trust.DomainsRiskManagePermission)
	if !ok {
		return
	}
	if h.domainRisk == nil {
		writeTrustAdminUnavailable(w)
		return
	}
	domainID, ok := trustAdminID(w, r.PathValue("domainId"))
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if !decodeAuthJSON(w, r, &body) {
		return
	}
	correlationID, err := requestCorrelation(r)
	if err != nil {
		writeTrustAdminError(w, trust.ErrInvalid)
		return
	}
	result, err := h.domainRisk.AdminRevalidateDomainRisk(r.Context(), trust.AdminDomainRevalidateInput{
		DomainID:       domainID,
		ActorID:        session.UserID,
		Reason:         body.Reason,
		CorrelationID:  correlationID,
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
		Now:            time.Now().UTC(),
	}, h.authorizer)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	item, err := h.store.GetAdminDomainRisk(r.Context(), result.Evaluation.DomainID)
	if err != nil {
		writeTrustAdminError(w, err)
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]any{"domain_risk": item, "created": result.Created})
}

func trustAdminID(w http.ResponseWriter, raw string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64)
	if err != nil || value == 0 {
		writeTrustAdminError(w, trust.ErrInvalid)
		return 0, false
	}
	return value, true
}

func trustAdminLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 100, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > 500 {
		writeTrustAdminError(w, trust.ErrInvalid)
		return 0, false
	}
	return value, true
}

func applyTrustAdminHeaders(header http.Header) {
	authn.ApplyPrivateAuthHeaders(header)
	header.Set("Cache-Control", "no-store, max-age=0")
	header.Set("Pragma", "no-cache")
	header.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
}

func writeTrustAdminError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "trust_internal"
	switch {
	case errors.Is(err, trust.ErrInvalid):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, trust.ErrUnauthorized):
		status, code = http.StatusForbidden, "forbidden"
	case errors.Is(err, trust.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, trust.ErrConflict), errors.Is(err, trust.ErrStaleFingerprint):
		status, code = http.StatusConflict, "state_conflict"
	case errors.Is(err, trust.ErrRateLimited):
		status, code = http.StatusTooManyRequests, "rate_limited"
	case errors.Is(err, trust.ErrVerification):
		status, code = http.StatusServiceUnavailable, "verification_unavailable"
	}
	writeAuthJSON(w, status, map[string]any{"error": code})
}

func writeTrustAdminUnavailable(w http.ResponseWriter) {
	writeAuthJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "risk_provider_unavailable"})
}
