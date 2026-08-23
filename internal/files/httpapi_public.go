package files

import (
	"errors"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

type publicPageData struct {
	State        string
	Headline     string
	Message      string
	Slug         string
	ShowForm     bool
	ShowDownload bool
	DownloadURL  string
}

var publicFilePageTemplate = template.Must(template.New("public-file").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Headline}} · GoJet</title></head>
<body><main aria-labelledby="file-state-heading"><section role="status" aria-live="polite" data-file-state="{{.State}}">
<h1 id="file-state-heading">{{.Headline}}</h1><p>{{.Message}}</p>
{{if .ShowForm}}<form method="post" action="/f/{{.Slug}}"><label for="file-password">Password</label>
<input id="file-password" name="password" type="password" autocomplete="current-password" required>
<button type="submit">Continue</button></form>{{end}}
{{if .ShowDownload}}<p><a href="{{.DownloadURL}}">Download file</a></p>{{end}}
</section></main></body></html>`))

func (a *API) publicPageGet(w http.ResponseWriter, r *http.Request) {
	a.renderPublicPage(w, r, http.StatusOK, false)
}

func (a *API) publicPagePost(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	resource, passwordHash, err := a.store.GetBySlug(r.Context(), slug)
	if err != nil {
		a.writePublicLookupError(w, err)
		return
	}
	now := time.Now().UTC()
	state, status := publicFileState(resource, now)
	if state != "available" {
		a.renderPublicResourceState(w, status, slug, state, false)
		return
	}
	if passwordHash == "" {
		http.Redirect(w, r, "/f/"+url.PathEscape(slug), http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_form", "Password form is invalid.")
		return
	}
	password := r.PostForm.Get("password")
	if !links.VerifyLinkPassword(passwordHash, password) {
		clearPublicAuthCookie(w, !a.testAuthEnabled || requestIsSecure(r))
		a.renderPublicResourceState(w, http.StatusForbidden, slug, "password-required", true)
		return
	}
	if err := a.setPublicAuthCookie(w, r, slug, passwordHash, now); err != nil {
		writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
		return
	}
	http.Redirect(w, r, "/f/"+url.PathEscape(slug), http.StatusSeeOther)
}

func (a *API) renderPublicPage(w http.ResponseWriter, r *http.Request, defaultStatus int, passwordFailure bool) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	resource, passwordHash, err := a.store.GetBySlug(r.Context(), slug)
	if err != nil {
		a.writePublicLookupError(w, err)
		return
	}
	now := time.Now().UTC()
	state, status := publicFileState(resource, now)
	if status == 0 {
		status = defaultStatus
	}
	if state == "available" && passwordHash != "" && !a.requestHasPublicAuth(r, slug, passwordHash, now) {
		state = "password-required"
		if passwordFailure {
			status = http.StatusForbidden
		}
	}
	a.renderPublicResourceState(w, status, slug, state, state == "password-required")
}

func publicFileState(resource Resource, now time.Time) (string, int) {
	if resource.ExpiresAt != nil && !now.Before(resource.ExpiresAt.UTC()) {
		return "expired", http.StatusGone
	}
	if resource.DownloadLimit != nil && resource.DownloadCount >= *resource.DownloadLimit {
		return "download-limit", http.StatusGone
	}
	if resource.ScanState == ScanBlocked || resource.ScanState == ScanError {
		return "blocked", http.StatusOK
	}
	if resource.ScanState == ScanQuarantined || resource.ScanState == ScanScanning {
		return "scan-pending", http.StatusOK
	}
	if resource.ScanState != ScanSafe {
		return "blocked", http.StatusOK
	}
	if !resource.Published {
		return "removed", http.StatusOK
	}
	return "available", http.StatusOK
}

func (a *API) renderPublicResourceState(w http.ResponseWriter, status int, slug, state string, showForm bool) {
	data := publicPageData{State: state, Slug: url.PathEscape(slug), ShowForm: showForm}
	switch state {
	case "available":
		data.Headline = "File available"
		data.Message = "This file passed its current security scan and is available to download."
		data.ShowDownload = true
		data.DownloadURL = "/api/public/files/" + url.PathEscape(slug)
	case "password-required":
		data.Headline = "Password required"
		data.Message = "Enter the file password to continue."
	case "scan-pending":
		data.Headline = "Security scan in progress"
		data.Message = "This file remains private until its security scan completes."
	case "blocked":
		data.Headline = "File blocked"
		data.Message = "This file is not available for distribution."
	case "expired":
		data.Headline = "File expired"
		data.Message = "This file is no longer available."
	case "download-limit":
		data.Headline = "Download limit reached"
		data.Message = "This file has reached its download limit."
	default:
		data.State = "removed"
		data.Headline = "File removed"
		data.Message = "This file is not currently shared."
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = publicFilePageTemplate.Execute(w, data)
}

func (a *API) writePublicLookupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeFileAPIError(w, http.StatusNotFound, "not_found", "File share not found.")
	case errors.Is(err, ErrDeleted):
		a.renderPublicResourceState(w, http.StatusGone, "", "removed", false)
	default:
		writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func (a *API) publicDownload(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	resource, passwordHash, err := a.store.GetBySlug(r.Context(), slug)
	if err != nil {
		a.writePublicBinaryError(w, err)
		return
	}
	now := time.Now().UTC()
	if !resource.Published || resource.ScanState != ScanSafe {
		a.writePublicBinaryError(w, ErrNotSafe)
		return
	}
	if resource.ExpiresAt != nil && !now.Before(resource.ExpiresAt.UTC()) {
		a.writePublicBinaryError(w, ErrExpired)
		return
	}
	authorizedPasswordHash := ""
	if passwordHash != "" {
		if !a.requestHasPublicAuth(r, slug, passwordHash, now) {
			a.writePublicBinaryError(w, ErrPasswordRequired)
			return
		}
		authorizedPasswordHash = passwordHash
	}
	file, err := a.storage.OpenPublished(resource.StorageKey)
	if err != nil {
		writeFileAPIError(w, http.StatusServiceUnavailable, "storage_unavailable", "The file bytes are unavailable.")
		return
	}
	defer file.Close()

	reserved, err := a.store.ReservePublicDownload(r.Context(), slug, now, authorizedPasswordHash)
	if err != nil {
		a.writePublicBinaryError(w, err)
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": reserved.OriginalName})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", reserved.DetectedMIME)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.FormatUint(reserved.SizeBytes, 10))
	http.ServeContent(w, r, "file", reserved.UpdatedAt, file)
}

func (a *API) writePublicBinaryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeFileAPIError(w, http.StatusNotFound, "not_found", "File share not found.")
	case errors.Is(err, ErrDeleted), errors.Is(err, ErrExpired), errors.Is(err, ErrDownloadLimit):
		writeFileAPIError(w, http.StatusGone, "gone", "File share is no longer available.")
	case errors.Is(err, ErrNotSafe), errors.Is(err, ErrPasswordRequired):
		writeFileAPIError(w, http.StatusForbidden, "forbidden", "File bytes are not authorized.")
	default:
		writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}
