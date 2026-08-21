package links

import (
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RedirectHandler struct {
	store                   *MySQLStore
	risk                    *RedisRiskStore
	trustTestRoutingHeaders bool
}

type safetyView struct {
	Title   string
	Message string
	Code    string
	Reason  string
}

type passwordView struct {
	Code    string
	Message string
}

var safetyTemplate = template.Must(template.New("link-safety").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>{{.Title}} · GoJet</title>
</head>
<body>
<main>
<h1>{{.Title}}</h1>
<p>{{.Message}}</p>
<p>Reference: <code>{{.Code}}</code></p>
</main>
</body>
</html>`))

var passwordTemplate = template.Must(template.New("link-password").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow">
<title>Password required · GoJet</title>
</head>
<body>
<main>
<h1>Password required</h1>
<p>This link is protected. Enter its password to continue.</p>
{{if .Message}}<p role="alert">{{.Message}}</p>{{end}}
<form method="post" action="">
<label for="gojet-link-password">Password</label>
<input id="gojet-link-password" name="password" type="password" minlength="8" maxlength="256" autocomplete="current-password" required>
<button type="submit">Continue</button>
</form>
<p>Reference: <code>{{.Code}}</code></p>
</main>
</body>
</html>`))

func NewRedirectHandler(store *MySQLStore, risk *RedisRiskStore, trustTestRoutingHeaders bool) http.Handler {
	handler := &RedirectHandler{store: store, risk: risk, trustTestRoutingHeaders: trustTestRoutingHeaders}
	mux := http.NewServeMux()
	// In Go's ServeMux, a GET method pattern also matches HEAD. Registering an
	// additional generic HEAD /{code} conflicts with the more specific
	// GET /healthz route, so GET registrations are the single HEAD authority.
	mux.HandleFunc("GET /healthz", handler.health)
	mux.HandleFunc("GET /{code}", handler.resolve)
	mux.HandleFunc("POST /{code}", handler.resolve)
	return redirectSecurityHeaders(mux)
}

func redirectSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (h *RedirectHandler) health(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := h.store.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if err := h.risk.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RedirectHandler) resolve(w http.ResponseWriter, r *http.Request) {
	host, err := normalizeRequestHost(r.Host)
	if err != nil {
		h.writeNotFound(w)
		return
	}
	code, err := normalizeCode(r.PathValue("code"))
	if err != nil {
		h.writeNotFound(w)
		return
	}

	link, err := h.store.GetByHostCode(r.Context(), host, code)
	if errors.Is(err, ErrNotFound) {
		h.writeNotFound(w)
		return
	}
	if err != nil {
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
		return
	}

	// Domain trust is a separate authority. Until P06 provides its resolver,
	// custom domains fail closed rather than inheriting official-host behavior.
	if link.DomainKind == "custom" {
		h.writeSafety(w, http.StatusServiceUnavailable, "domain-unavailable", code)
		return
	}
	if link.Status == "deleted" || link.DeletedAt != nil {
		h.writeSafety(w, http.StatusGone, "removed", code)
		return
	}
	if link.Status != "active" {
		h.writeSafety(w, http.StatusOK, "operational-unavailable", code)
		return
	}

	// Destination risk is the first content authority after link/domain state.
	// Password verification must never expose or bypass a non-allow decision.
	_, riskState, riskErr := h.risk.Resolve(r.Context(), link.ID, link.RiskFingerprint, time.Now().UTC())
	if riskErr != nil {
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
		return
	}
	if riskState != RiskAllow {
		h.writeRiskSafety(w, riskState, code)
		return
	}

	selected, err := SelectTarget(link, h.resolveContext(r))
	if err != nil {
		h.writeSafety(w, http.StatusOK, "pending", code)
		return
	}
	if err := VerifySelectedTargetIsFingerprintMember(link, selected.Destination); err != nil {
		h.writeSafety(w, http.StatusOK, "pending", code)
		return
	}
	finalDestination, err := ApplyUTM(selected.Destination, link.UTM)
	if err != nil {
		h.writeSafety(w, http.StatusOK, "pending", code)
		return
	}

	// Access control executes only after exact-current risk allow and target
	// selection. GET/HEAD never consumes click/one-time counters.
	if link.Access.PasswordHash != "" {
		if r.Method != http.MethodPost {
			h.writePasswordChallenge(w, http.StatusOK, code, "")
			return
		}
		if !h.verifyPasswordAttempt(w, r, link, code) {
			return
		}
	}

	claimed, claimState, err := h.store.ClaimRedirectAccess(
		r.Context(), link.ID, link.Version, link.RiskFingerprint, time.Now().UTC(),
	)
	if err != nil {
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
		return
	}
	if claimState != AccessClaimAllowed {
		h.writeClaimSafety(w, claimState, code)
		return
	}
	if claimed.Version != link.Version || claimed.RiskFingerprint != link.RiskFingerprint {
		h.writeSafety(w, http.StatusOK, "pending", code)
		return
	}

	// Exact-current risk allow and every applicable access control have now
	// succeeded. This is the only branch that may emit a customer Location.
	w.Header().Set("Location", finalDestination)
	w.WriteHeader(link.RedirectStatus)
}

func (h *RedirectHandler) verifyPasswordAttempt(w http.ResponseWriter, r *http.Request, link Link, code string) bool {
	allowed, err := h.risk.AllowPasswordAttempt(r.Context(), link.ID, r.RemoteAddr)
	if err != nil {
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "300")
		h.writePasswordChallenge(w, http.StatusTooManyRequests, code, "Too many password attempts. Try again later.")
		return false
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
		h.writePasswordChallenge(w, http.StatusBadRequest, code, "The password submission was invalid.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		h.writePasswordChallenge(w, http.StatusBadRequest, code, "The password submission was invalid.")
		return false
	}
	password := r.PostForm.Get("password")
	if !VerifyLinkPassword(link.Access.PasswordHash, password) {
		h.writePasswordChallenge(w, http.StatusUnauthorized, code, "The password was not accepted.")
		return false
	}
	if err := h.risk.ClearPasswordAttempts(r.Context(), link.ID, r.RemoteAddr); err != nil {
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
		return false
	}
	return true
}

func normalizeRequestHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidInput
	}
	host := raw
	if parsedHost, _, err := net.SplitHostPort(raw); err == nil {
		host = parsedHost
	}
	return normalizeHostname(host)
}

func (h *RedirectHandler) resolveContext(r *http.Request) ResolveContext {
	country := ""
	if h.trustTestRoutingHeaders {
		country = strings.TrimSpace(r.Header.Get("X-GoJet-Test-Country"))
	}
	language := firstLanguage(r.Header.Get("Accept-Language"))
	device := classifyDevice(r.UserAgent())
	sourceHostname := ""
	if ref := strings.TrimSpace(r.Referer()); ref != "" {
		if parsed, err := url.Parse(ref); err == nil {
			sourceHostname = strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
		}
	}
	seedHost := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		seedHost = host
	}
	return ResolveContext{
		Country:        country,
		Device:         device,
		Language:       language,
		SourceHostname: sourceHostname,
		ABSeed:         seedHost + "\n" + r.UserAgent() + "\n" + language,
	}
}

func firstLanguage(value string) string {
	if value == "" {
		return ""
	}
	first := strings.TrimSpace(strings.Split(value, ",")[0])
	if semicolon := strings.IndexByte(first, ';'); semicolon >= 0 {
		first = first[:semicolon]
	}
	return strings.TrimSpace(first)
}

func classifyDevice(userAgent string) string {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "ipad"), strings.Contains(ua, "tablet"):
		return "tablet"
	case strings.Contains(ua, "mobile"), strings.Contains(ua, "iphone"), strings.Contains(ua, "android"):
		return "mobile"
	default:
		return "desktop"
	}
}

func (h *RedirectHandler) writeRiskSafety(w http.ResponseWriter, state RiskState, code string) {
	switch state {
	case RiskBlock:
		h.writeSafety(w, http.StatusOK, "blocked", code)
	case RiskReview:
		h.writeSafety(w, http.StatusOK, "review", code)
	case RiskMissing:
		h.writeSafety(w, http.StatusOK, "pending", code)
	case RiskMalformed:
		h.writeSafety(w, http.StatusOK, "malformed", code)
	case RiskStale:
		h.writeSafety(w, http.StatusOK, "stale", code)
	default:
		h.writeSafety(w, http.StatusOK, "pending", code)
	}
}

func (h *RedirectHandler) writeClaimSafety(w http.ResponseWriter, state AccessClaimState, code string) {
	switch state {
	case AccessClaimExpired:
		h.writeSafety(w, http.StatusGone, "expired", code)
	case AccessClaimExhausted:
		h.writeSafety(w, http.StatusGone, "exhausted", code)
	case AccessClaimDeleted:
		h.writeSafety(w, http.StatusGone, "removed", code)
	case AccessClaimPaused:
		h.writeSafety(w, http.StatusOK, "operational-unavailable", code)
	case AccessClaimConflict:
		h.writeSafety(w, http.StatusOK, "pending", code)
	default:
		h.writeSafety(w, http.StatusServiceUnavailable, "operational-unavailable", code)
	}
}

func (h *RedirectHandler) writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("<!doctype html><html><head><meta name=\"robots\" content=\"noindex,nofollow\"><title>Not found · GoJet</title></head><body><main><h1>Link not found</h1></main></body></html>"))
}

func (h *RedirectHandler) writePasswordChallenge(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' http: https:; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return
	}
	_ = passwordTemplate.Execute(w, passwordView{Code: code, Message: message})
}

func (h *RedirectHandler) writeSafety(w http.ResponseWriter, status int, reason, code string) {
	title, message := safetyCopy(reason)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return
	}
	_ = safetyTemplate.Execute(w, safetyView{Title: title, Message: message, Code: code, Reason: reason})
}

func safetyCopy(reason string) (string, string) {
	switch reason {
	case "review":
		return "Link under review", "This link is not available while its destination is being reviewed."
	case "blocked":
		return "Link blocked", "This link is unavailable because the destination did not pass GoJet safety checks."
	case "malformed", "stale", "pending":
		return "Link temporarily unavailable", "This link is waiting for a current safety decision."
	case "expired":
		return "Link expired", "This link is no longer available."
	case "exhausted":
		return "Link limit reached", "This link is no longer available."
	case "removed":
		return "Link removed", "This link has been removed."
	case "domain-unavailable":
		return "Domain unavailable", "This link cannot be served because its domain is not currently eligible for routing."
	default:
		return "Link unavailable", "This link is temporarily unavailable."
	}
}

func (h *RedirectHandler) String() string {
	return fmt.Sprintf("RedirectHandler{testRoutingHeaders:%t}", h.trustTestRoutingHeaders)
}
