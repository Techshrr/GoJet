package admin

import (
	"net/http"
	"strings"
)

func (a *HTTPAPI) PlatformGovernanceHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/settings/{settingKey}", a.platformSetting)
	mux.HandleFunc("PUT /api/admin/settings/{settingKey}", a.updatePlatformSetting)
	mux.HandleFunc("GET /api/admin/security/bot-protection", a.botProtection)
	mux.HandleFunc("PUT /api/admin/security/bot-protection", a.updateBotProtection)
	mux.HandleFunc("GET /api/admin/official-domains", a.officialDomains)
	mux.HandleFunc("POST /api/admin/official-domains", a.createOfficialDomain)
	mux.HandleFunc("POST /api/admin/official-domains/{domainId}/actions", a.mutateOfficialDomain)
	mux.HandleFunc("GET /api/admin/announcements", a.announcements)
	mux.HandleFunc("POST /api/admin/announcements", a.createAnnouncement)
	mux.HandleFunc("POST /api/admin/announcements/{announcementId}/actions", a.mutateAnnouncement)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { adminHeaders(w.Header()); mux.ServeHTTP(w, r) })
}

func (a *HTTPAPI) platformSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.service.GetPlatformSetting(r.Context(), p, r.PathValue("settingKey"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *HTTPAPI) updatePlatformSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input UpdatePlatformSettingInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.UpdatePlatformSetting(r.Context(), p, r.PathValue("settingKey"), input, mutationAuthority(r), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"setting": item, "replayed": replayed})
}

func (a *HTTPAPI) botProtection(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.service.GetTurnstileConfig(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *HTTPAPI) updateBotProtection(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input UpdateTurnstileInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.UpdateTurnstileConfig(r.Context(), p, input, mutationAuthority(r), a.now())
	input.Secret = ""
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": item, "replayed": replayed})
}

func (a *HTTPAPI) officialDomains(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	items, err := a.service.ListOfficialDomains(r.Context(), p)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *HTTPAPI) createOfficialDomain(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input CreateOfficialDomainInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.CreateOfficialDomain(r.Context(), p, input, mutationAuthority(r), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"domain": item, "replayed": replayed})
}

func (a *HTTPAPI) mutateOfficialDomain(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input OfficialDomainActionInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.MutateOfficialDomain(r.Context(), p, strings.TrimSpace(r.PathValue("domainId")), input, mutationAuthority(r), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain": item, "replayed": replayed})
}

func (a *HTTPAPI) announcements(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	limit, err := adminLimit(r)
	if err != nil {
		writeError(w, err)
		return
	}
	items, err := a.service.ListAnnouncements(r.Context(), p, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *HTTPAPI) createAnnouncement(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input CreateAnnouncementInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.CreateAnnouncement(r.Context(), p, input, mutationAuthority(r), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"announcement": item, "replayed": replayed})
}

func (a *HTTPAPI) mutateAnnouncement(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok || !a.mutationGuard(w, r, p) {
		return
	}
	var input AnnouncementActionInput
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, err)
		return
	}
	item, replayed, err := a.service.MutateAnnouncement(r.Context(), p, strings.TrimSpace(r.PathValue("announcementId")), input, mutationAuthority(r), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"announcement": item, "replayed": replayed})
}
