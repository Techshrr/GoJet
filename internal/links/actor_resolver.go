package links

import (
	"errors"
	"net/http"
)

var (
	ErrAuthenticationRequired    = errors.New("links authentication required")
	ErrForbidden                 = errors.New("links workspace access forbidden")
	ErrAuthenticationUnavailable = errors.New("links authentication dependency unavailable")
)

// Actor is the server-authoritative identity and Workspace role accepted by the
// Links API after the authentication/session boundary has been resolved.
type Actor struct {
	ActorID string
	Role    string
}

// ActorResolver resolves a real request and path Workspace into the identity
// authority used by Links. Production resolvers must not derive authority from
// X-GoJet-Test-* headers; those headers remain isolated to NewAPI test mode.
type ActorResolver func(*http.Request, string) (Actor, error)
