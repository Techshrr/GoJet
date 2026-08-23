package bio

import (
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"
)

const publicBioCSP = "default-src 'none'; style-src 'sha256-4mHm9XgrwzqzLQqYyIEoiapa2ff3CH3jrgNc3S71f+Q='; base-uri 'none'; frame-ancestors 'none'; form-action 'none'"

type publicLinkData struct {
	Position   uint
	Label      string
	RiskStatus string
	Href       string
	Navigable  bool
}

type publicPageData struct {
	State    string
	Headline string
	Message  string
	Title    string
	Bio      string
	Links    []publicLinkData
}

type publicAPIPage struct {
	Slug   string          `json:"slug"`
	Title  string          `json:"title"`
	Bio    string          `json:"bio"`
	Status string          `json:"status"`
	Links  []publicAPILink `json:"links"`
}

type publicAPILink struct {
	ID         uint64 `json:"id"`
	Position   uint   `json:"position"`
	Label      string `json:"label"`
	RiskStatus string `json:"risk_status"`
	URL        string `json:"url,omitempty"`
}

var publicBioTemplate = template.Must(template.New("public-bio").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex,nofollow"><title>{{.Headline}} · GoJet</title>
<style>*,*::before,*::after{box-sizing:border-box}body{overflow-wrap:anywhere}main{max-width:44rem;margin:0 auto;padding:1rem}ul{padding:0;list-style:none}li{margin:.75rem 0}a,span{display:block;max-width:100%;overflow-wrap:anywhere}</style></head>
<body><main aria-labelledby="bio-state-heading" data-bio-state="{{.State}}">
<h1 id="bio-state-heading">{{.Headline}}</h1><p>{{.Message}}</p>
{{if .Title}}<section aria-labelledby="bio-title"><h2 id="bio-title">{{.Title}}</h2>{{if .Bio}}<p>{{.Bio}}</p>{{end}}
{{if .Links}}<ul aria-label="Bio links">{{range .Links}}<li>{{if .Navigable}}<a href="{{.Href}}" rel="ugc nofollow">{{.Label}}</a>{{else}}<span data-risk-status="{{.RiskStatus}}">{{.Label}} — unavailable</span>{{end}}</li>{{end}}</ul>{{end}}</section>{{end}}
</main></body></html>`))

func (a *API) publicPage(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	page, status, err := a.publicResource(r, slug)
	if err != nil {
		a.renderPublicError(w, err)
		return
	}
	data := publicPageData{
		State:    page.Status,
		Headline: "Bio page",
		Message:  "This Bio page is published.",
		Title:    page.Title,
		Bio:      page.Bio,
		Links:    publicHTMLLinks(page, status == "published"),
	}
	if status == "paused" {
		data.Headline = "Bio page paused"
		data.Message = "This Bio page is temporarily paused."
	}
	a.executePublicPage(w, http.StatusOK, data)
}

func (a *API) publicAPI(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.PathValue("slug"))
	page, status, err := a.publicResource(r, slug)
	if err != nil {
		a.writePublicAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, publicAPIPage{
		Slug:   page.Slug,
		Title:  page.Title,
		Bio:    page.Bio,
		Status: status,
		Links:  publicJSONLinks(page, status == "published"),
	})
}

func (a *API) publicResource(r *http.Request, slug string) (Page, string, error) {
	page, err := a.store.GetPublic(r.Context(), slug)
	if err != nil {
		return Page{}, "", err
	}
	if page.DeletedAt != nil {
		return Page{}, "", ErrDeleted
	}
	switch page.Status {
	case "draft":
		// Draft existence is intentionally not disclosed on the public surface.
		return Page{}, "", ErrNotFound
	case "paused":
		return page, "paused", nil
	case "published":
		refreshed, err := a.refreshRisk(r.Context(), page, time.Now().UTC())
		if err != nil {
			return Page{}, "", err
		}
		return refreshed, "published", nil
	default:
		return Page{}, "", ErrNotFound
	}
}

func publicHTMLLinks(page Page, navigable bool) []publicLinkData {
	result := make([]publicLinkData, 0, len(page.Links))
	for _, child := range page.Links {
		item := publicLinkData{
			Position:   child.Position,
			Label:      child.Label,
			RiskStatus: child.RiskStatus,
		}
		if navigable && child.RiskStatus == "allowed" {
			item.Href = child.DestinationURL
			item.Navigable = true
		}
		result = append(result, item)
	}
	return result
}

func publicJSONLinks(page Page, navigable bool) []publicAPILink {
	result := make([]publicAPILink, 0, len(page.Links))
	for _, child := range page.Links {
		item := publicAPILink{
			ID:         child.ID,
			Position:   child.Position,
			Label:      child.Label,
			RiskStatus: child.RiskStatus,
		}
		if navigable && child.RiskStatus == "allowed" {
			item.URL = child.DestinationURL
		}
		result = append(result, item)
	}
	return result
}

func (a *API) renderPublicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		a.executePublicPage(w, http.StatusNotFound, publicPageData{State: "not-found", Headline: "Bio not found", Message: "This Bio page does not exist."})
	case errors.Is(err, ErrDeleted):
		a.executePublicPage(w, http.StatusGone, publicPageData{State: "removed", Headline: "Bio removed", Message: "This Bio page is no longer available."})
	default:
		a.executePublicPage(w, http.StatusInternalServerError, publicPageData{State: "error", Headline: "Bio unavailable", Message: "The request could not be completed."})
	}
}

func (a *API) executePublicPage(w http.ResponseWriter, status int, data publicPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", publicBioCSP)
	w.WriteHeader(status)
	_ = publicBioTemplate.Execute(w, data)
}

func (a *API) writePublicAPIError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "Bio page not found.")
	case errors.Is(err, ErrDeleted):
		writeAPIError(w, http.StatusGone, "gone", "Bio page is no longer available.")
	default:
		writeAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}
