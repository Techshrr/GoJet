package admin

import (
	"net/http"
	"strings"
	"time"
)

func (a *HTTPAPI) DomainEntitlementHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/domain-entitlements", a.domainEntitlements)
	mux.HandleFunc("GET /api/admin/domain-entitlements/{workspaceId}", a.domainEntitlement)
	mux.HandleFunc("POST /api/admin/domain-entitlements/{workspaceId}/decisions", a.domainEntitlementDecision)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminHeaders(w.Header())
		mux.ServeHTTP(w, r)
	})
}

func (a *HTTPAPI) domainEntitlements(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.service.ListDomainEntitlements(r.Context(), p, a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *HTTPAPI) domainEntitlement(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.service.GetDomainEntitlement(r.Context(), p, r.PathValue("workspaceId"), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"item": item})
}

func (a *HTTPAPI) domainEntitlementDecision(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Action                           string  `json:"action"`
		DomainLimit                      *uint32 `json:"domain_limit"`
		StartsAt                         string  `json:"starts_at"`
		ExpiresAt                        string  `json:"expires_at"`
		SupportTicketID                  string  `json:"support_ticket_id"`
		UserVisibleCategory              string  `json:"user_visible_category"`
		EffectiveAt                      string  `json:"effective_at"`
		Scope                            string  `json:"scope"`
		Confirmation                     string  `json:"confirmation"`
		ExistingLinkImpact               string  `json:"existing_link_impact"`
		CurrentSecurityOwnershipEvidence string  `json:"current_security_ownership_evidence"`
		Reason                           string  `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	input := DomainEntitlementDecisionInput{
		Action:                           strings.TrimSpace(body.Action),
		DomainLimit:                      body.DomainLimit,
		SupportTicketID:                  strings.TrimSpace(body.SupportTicketID),
		UserVisibleCategory:              strings.TrimSpace(body.UserVisibleCategory),
		Scope:                            strings.TrimSpace(body.Scope),
		Confirmation:                     strings.TrimSpace(body.Confirmation),
		ExistingLinkImpact:               strings.TrimSpace(body.ExistingLinkImpact),
		CurrentSecurityOwnershipEvidence: strings.TrimSpace(body.CurrentSecurityOwnershipEvidence),
	}
	var err error
	if input.StartsAt, err = parseOptionalAdminTime(body.StartsAt); err != nil {
		writeError(w, ErrInvalid)
		return
	}
	if input.ExpiresAt, err = parseOptionalAdminTime(body.ExpiresAt); err != nil {
		writeError(w, ErrInvalid)
		return
	}
	if input.EffectiveAt, err = parseOptionalAdminTime(body.EffectiveAt); err != nil {
		writeError(w, ErrInvalid)
		return
	}
	result, replayed, err := a.service.DecideDomainEntitlement(
		r.Context(),
		p,
		r.PathValue("workspaceId"),
		input,
		authority(r, body.Reason),
		a.now(),
	)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": result, "replayed": replayed})
}

func parseOptionalAdminTime(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	value = value.UTC().Truncate(time.Microsecond)
	return &value, nil
}
