package textshares

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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

const publicAuthTTL = 30 * time.Minute

type publicPageData struct {
	State        string
	Headline     string
	Message      string
	Slug         string
	Title        string
	Content      string
	ShowPassword bool
	ShowContent  bool
	ShowReveal   bool
	ShowDownload bool
	DownloadURL  string
	AbuseURL     string
}

var publicTextTemplate = template.Must(template.New("public-text").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow"><title>{{.Headline}} · GoJet</title></head>
<body><main aria-labelledby="text-state-heading"><section data-text-state="{{.State}}">
<h1 id="text-state-heading">{{.Headline}}</h1><p>{{.Message}}</p>
{{if .ShowPassword}}<form method="post" action="/t/{{.Slug}}"><label for="text-password">Password</label>
<input id="text-password" name="password" type="password" autocomplete="current-password" required>
<button type="submit">Continue</button></form>{{end}}
{{if .ShowContent}}<div aria-labelledby="text-title"><h2 id="text-title">{{.Title}}</h2><pre id="text-content">{{.Content}}</pre>
<form method="post" action="/api/public/text/{{.Slug}}"><button type="submit">Open plain text</button></form></div>{{end}}
{{if .ShowReveal}}<form method="post" action="/api/public/text/{{.Slug}}"><button type="submit">Reveal text once</button></form>{{end}}
{{if .ShowDownload}}<p><a href="{{.DownloadURL}}">Download text</a></p>{{end}}
<p><a href="{{.AbuseURL}}">Report abuse</a></p>
</section></main></body></html>`))

func (a *API) publicPageGet(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("download") == "1" {
		a.publicDownload(w, r)
		return
	}
	a.renderPublicPage(w, r, false)
}

func (a *API) publicPagePost(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	current, err := a.store.GetPublic(r.Context(), slug)
	if err != nil {
		a.renderPublicError(w, slug, err)
		return
	}
	if err := validatePublicLifecycle(current, time.Now().UTC()); err != nil {
		a.renderPublicError(w, slug, err)
		return
	}
	if current.PasswordHash == "" {
		http.Redirect(w, r, "/t/"+url.PathEscape(slug), http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		a.renderPublicState(w, http.StatusBadRequest, slug, "invalid", "Invalid request", "The password form could not be processed.", publicPageData{})
		return
	}
	password := r.PostForm.Get("password")
	if !links.VerifyLinkPassword(current.PasswordHash, password) {
		a.clearPublicAuthCookie(w, slug, requestIsSecure(r))
		a.renderPasswordPage(w, http.StatusForbidden, current, "The password was not accepted.")
		return
	}
	a.setPublicAuthCookie(w, r, current, time.Now().UTC())
	http.Redirect(w, r, "/t/"+url.PathEscape(slug), http.StatusSeeOther)
}

func (a *API) renderPublicPage(w http.ResponseWriter, r *http.Request, passwordFailure bool) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	current, err := a.store.GetPublic(r.Context(), slug)
	if err != nil {
		a.renderPublicError(w, slug, err)
		return
	}
	if err := validatePublicLifecycle(current, time.Now().UTC()); err != nil {
		a.renderPublicError(w, slug, err)
		return
	}
	if current.PasswordHash != "" && !a.requestHasPublicAuth(r, current, time.Now().UTC()) {
		status := http.StatusUnauthorized
		message := "Enter the Text password to continue."
		if passwordFailure {
			status = http.StatusForbidden
			message = "The password was not accepted."
		}
		a.renderPasswordPage(w, status, current, message)
		return
	}
	data := publicPageData{
		State:        "available",
		Headline:     "Text available",
		Message:      "This Text share is available.",
		Slug:         url.PathEscape(current.PublicSlug),
		Title:        current.Title,
		AbuseURL:     "/abuse/report",
		ShowDownload: true,
		DownloadURL:  "/t/" + url.PathEscape(current.PublicSlug) + "?download=1",
	}
	if current.OneTime {
		data.State = "one-time"
		data.Headline = "One-time text available"
		data.Message = "Revealing or downloading this Text share consumes it once."
		data.ShowReveal = true
	} else {
		data.Content = current.Content
		data.ShowContent = true
	}
	a.executePublicPage(w, http.StatusOK, data)
}

func (a *API) renderPasswordPage(w http.ResponseWriter, status int, current storedResource, message string) {
	data := publicPageData{
		State: "password-required", Headline: "Password required", Message: message,
		Slug: url.PathEscape(current.PublicSlug), ShowPassword: true, AbuseURL: "/abuse/report",
	}
	a.executePublicPage(w, status, data)
}

func (a *API) renderPublicError(w http.ResponseWriter, slug string, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.renderPublicState(w, http.StatusNotFound, slug, "not-found", "Text not found", "This Text share does not exist.", publicPageData{})
	case errors.Is(err, ErrPrivate):
		a.renderPublicState(w, http.StatusForbidden, slug, "private", "Text unavailable", "This Text share is private.", publicPageData{})
	case errors.Is(err, ErrDeleted):
		a.renderPublicState(w, http.StatusGone, slug, "removed", "Text removed", "This Text share is no longer available.", publicPageData{})
	case errors.Is(err, ErrExpired):
		a.renderPublicState(w, http.StatusGone, slug, "expired", "Text expired", "This Text share has expired.", publicPageData{})
	case errors.Is(err, ErrConsumed):
		a.renderPublicState(w, http.StatusGone, slug, "consumed", "Text consumed", "This one-time Text share has already been consumed.", publicPageData{})
	default:
		a.renderPublicState(w, http.StatusInternalServerError, slug, "error", "Text unavailable", "The request could not be completed.", publicPageData{})
	}
}

func (a *API) renderPublicState(w http.ResponseWriter, status int, slug, state, headline, message string, data publicPageData) {
	data.State = state
	data.Headline = headline
	data.Message = message
	data.Slug = url.PathEscape(slug)
	data.AbuseURL = "/abuse/report"
	a.executePublicPage(w, status, data)
}

func (a *API) executePublicPage(w http.ResponseWriter, status int, data publicPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = publicTextTemplate.Execute(w, data)
}

func (a *API) publicTextAction(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	current, err := a.authorizePublic(r, slug, time.Now().UTC())
	if err != nil {
		a.writePublicActionError(w, err)
		return
	}
	content, err := a.store.ConsumePublic(r.Context(), slug, time.Now().UTC())
	if err != nil {
		a.writePublicActionError(w, err)
		return
	}
	if current.ID != content.ID {
		a.writePublicActionError(w, ErrConflict)
		return
	}
	writePlainText(w, http.StatusOK, content.Content, false, content.Title)
}

func (a *API) publicDownload(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	current, err := a.authorizePublic(r, slug, time.Now().UTC())
	if err != nil {
		a.writePublicActionError(w, err)
		return
	}
	content, err := a.store.ConsumePublic(r.Context(), slug, time.Now().UTC())
	if err != nil {
		a.writePublicActionError(w, err)
		return
	}
	if current.ID != content.ID {
		a.writePublicActionError(w, ErrConflict)
		return
	}
	writePlainText(w, http.StatusOK, content.Content, true, content.Title)
}

func (a *API) authorizePublic(r *http.Request, slug string, now time.Time) (storedResource, error) {
	current, err := a.store.GetPublic(r.Context(), slug)
	if err != nil {
		return storedResource{}, err
	}
	if err := validatePublicLifecycle(current, now); err != nil {
		return storedResource{}, err
	}
	if current.PasswordHash != "" && !a.requestHasPublicAuth(r, current, now) {
		return storedResource{}, ErrPasswordRequired
	}
	return current, nil
}

func (a *API) writePublicActionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "Text share not found.")
	case errors.Is(err, ErrPrivate), errors.Is(err, ErrPasswordRequired):
		writeAPIError(w, http.StatusForbidden, "forbidden", "Text content is not authorized.")
	case errors.Is(err, ErrDeleted), errors.Is(err, ErrExpired), errors.Is(err, ErrConsumed):
		writeAPIError(w, http.StatusGone, "gone", "Text share is no longer available.")
	case errors.Is(err, ErrConflict):
		writeAPIError(w, http.StatusConflict, "state_conflict", "Text share state changed; retry against current state.")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func writePlainText(w http.ResponseWriter, status int, content string, attachment bool, title string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if attachment {
		filename := strings.TrimSpace(title)
		if filename == "" {
			filename = "gojet-text"
		}
		if len([]rune(filename)) > 80 {
			filename = string([]rune(filename)[:80])
		}
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename + ".txt"})
		if disposition == "" {
			disposition = "attachment"
		}
		w.Header().Set("Content-Disposition", disposition)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len([]byte(content))))
	w.WriteHeader(status)
	_, _ = w.Write([]byte(content))
}

func (a *API) setPublicAuthCookie(w http.ResponseWriter, r *http.Request, current storedResource, now time.Time) {
	expires := now.Add(publicAuthTTL)
	if current.ExpiresAt != nil && current.ExpiresAt.Before(expires) {
		expires = current.ExpiresAt.UTC()
	}
	payload := current.PublicSlug + "|" + strconv.FormatInt(expires.Unix(), 10) + "|" + passwordFingerprint(current.PasswordHash)
	signature := signPublicToken(a.publicAuthKey, payload)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + signature
	http.SetCookie(w, &http.Cookie{
		Name: cookieName(current.PublicSlug), Value: value, Path: "/", Expires: expires,
		MaxAge: max(1, int(expires.Sub(now).Seconds())), HttpOnly: true,
		Secure: requestIsSecure(r), SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) requestHasPublicAuth(r *http.Request, current storedResource, now time.Time) bool {
	cookie, err := r.Cookie(cookieName(current.PublicSlug))
	if err != nil || cookie.Value == "" {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	payload := string(raw)
	if !hmac.Equal([]byte(parts[1]), []byte(signPublicToken(a.publicAuthKey, payload))) {
		return false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 3 || fields[0] != current.PublicSlug || fields[2] != passwordFingerprint(current.PasswordHash) {
		return false
	}
	expiresUnix, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || !now.Before(time.Unix(expiresUnix, 0).UTC()) {
		return false
	}
	return true
}

func (a *API) clearPublicAuthCookie(w http.ResponseWriter, slug string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName(slug), Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
	})
}

func cookieName(slug string) string {
	digest := sha256.Sum256([]byte(slug))
	return "gojet_text_auth_" + hex.EncodeToString(digest[:6])
}

func passwordFingerprint(encoded string) string {
	digest := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(digest[:])
}

func signPublicToken(key []byte, payload string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
