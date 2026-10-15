package domains

import (
	"errors"
	"net/http"
)

var (
	ErrAuthenticationRequired    = errors.New("domains authentication required")
	ErrForbidden                 = errors.New("domains workspace access forbidden")
	ErrAuthenticationUnavailable = errors.New("domains authentication dependency unavailable")
)

// Actor is the server-authoritative identity and Workspace role accepted by
// the Domains API after the authentication/session boundary has been resolved.
type Actor struct {
	ActorID string
	Role    string
}

// ActorResolver resolves a real request and path Workspace into the identity
// authority used by Domains. Production resolvers must not derive authority
// from X-GoJet-Test-* headers; those headers remain isolated to test mode.
type ActorResolver func(*http.Request, string) (Actor, error)
