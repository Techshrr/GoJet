package admin

import (
	"net/http"
	"strings"
	"time"
)

func (a *HTTPAPI) PlatformGovernanceHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/settings/{settingKey}", a.platformSetting)
	mux.HandleFunc("PUT /api/admin/settings/{settingKey}", a.updatePlatformSetting)
	mux.HandleFunc("GET /api/admin/bot-protection", a.botProtection)
	mux.HandleFunc("PUT /api/admin/bot-protection", a.updateBotProtection)
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
	writeJSON(w, http.StatusOK, map[string]any{"setting": item})
}

func (a *HTTPAPI) updatePlatformSetting(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Value           map[string]string `json:"value"`
		ExpectedVersion uint64            `json:"expected_version"`
		Reason          string            `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.UpdatePlatformSetting(r.Context(), p, r.PathValue("settingKey"), UpdatePlatformSettingInput{Value: body.Value, ExpectedVersion: body.ExpectedVersion}, authority(r, body.Reason), a.now())
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
	writeJSON(w, http.StatusOK, map[string]any{"bot_protection": item})
}

func (a *HTTPAPI) updateBotProtection(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		SiteKey         string `json:"site_key"`
		Secret          string `json:"secret"`
		Enabled         bool   `json:"enabled"`
		ProviderState   string `json:"provider_state"`
		ExpectedVersion uint64 `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.UpdateTurnstileConfig(r.Context(), p, UpdateTurnstileInput{SiteKey: body.SiteKey, Secret: body.Secret, Enabled: body.Enabled, ProviderState: body.ProviderState, ExpectedVersion: body.ExpectedVersion}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot_protection": item, "replayed": replayed})
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
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Hostname string `json:"hostname"`
		Reason   string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.CreateOfficialDomain(r.Context(), p, CreateOfficialDomainInput{Hostname: body.Hostname}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"official_domain": item, "replayed": replayed})
}

func (a *HTTPAPI) mutateOfficialDomain(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Action          string `json:"action"`
		ExpectedVersion uint64 `json:"expected_version"`
		Reason          string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.MutateOfficialDomain(r.Context(), p, r.PathValue("domainId"), OfficialDomainActionInput{Action: body.Action, ExpectedVersion: body.ExpectedVersion}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"official_domain": item, "replayed": replayed})
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
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Title       string `json:"title"`
		Summary     string `json:"summary"`
		Body        string `json:"body"`
		Scope       string `json:"scope"`
		WorkspaceID string `json:"workspace_id"`
		Reason      string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	item, replayed, err := a.service.CreateAnnouncement(r.Context(), p, CreateAnnouncementInput{Title: body.Title, Summary: body.Summary, Body: body.Body, Scope: body.Scope, WorkspaceID: body.WorkspaceID}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"announcement": item, "replayed": replayed})
}

func (a *HTTPAPI) mutateAnnouncement(w http.ResponseWriter, r *http.Request) {
	p, ok := a.mutationPrincipal(w, r)
	if !ok {
		return
	}
	var body struct {
		Action          string `json:"action"`
		ExpectedVersion uint64 `json:"expected_version"`
		ScheduledFor    string `json:"scheduled_for"`
		Reason          string `json:"reason"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	var scheduled *time.Time
	if strings.TrimSpace(body.ScheduledFor) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(body.ScheduledFor))
		if err != nil {
			writeError(w, ErrInvalid)
			return
		}
		scheduled = &parsed
	}
	item, replayed, err := a.service.MutateAnnouncement(r.Context(), p, r.PathValue("announcementId"), AnnouncementActionInput{Action: body.Action, ExpectedVersion: body.ExpectedVersion, ScheduledFor: scheduled}, authority(r, body.Reason), a.now())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"announcement": item, "replayed": replayed})
}
