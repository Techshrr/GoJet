package auth

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// UnsafeMutationAuthority is an opaque, one-request proof that the current
// server session, Origin policy and one-time CSRF authority were all accepted.
// Its fields are intentionally unexported so callers cannot manufacture it.
type UnsafeMutationAuthority struct {
	sessionID string
	userID    string
	used      atomic.Bool
}

func AuthorizeUnsafeMutation(ctx context.Context, request *http.Request, session Session, origins *OriginPolicy, csrf *CSRFManager, now time.Time) (*UnsafeMutationAuthority, error) {
	if err := AuthorizeUnsafeRequest(ctx, request, session, origins, csrf, now); err != nil {
		return nil, err
	}
	return &UnsafeMutationAuthority{sessionID: session.ID, userID: session.UserID}, nil
}

func (a *UnsafeMutationAuthority) consumeFor(session Session) bool {
	return a != nil && a.sessionID != "" && a.userID != "" && a.sessionID == session.ID && a.userID == session.UserID && a.used.CompareAndSwap(false, true)
}
