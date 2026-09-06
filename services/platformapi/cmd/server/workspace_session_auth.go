package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

const workspaceCSRFTTL = 10 * time.Minute

type workspaceSessionAuthority struct {
	authenticate    func(context.Context, *http.Request, time.Time) (authn.Session, error)
	getUser         func(context.Context, string) (authn.User, error)
	authorizeUnsafe func(context.Context, *http.Request, authn.Session, time.Time) error
}

func buildWorkspaceSessionAuthority(db *sql.DB, redisClient *redis.Client) (*workspaceSessionAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, authn.ErrInvalid
	}
	csrfKey, err := decodeExactHexKey(os.Getenv("GOJET_AUTH_CSRF_KEY_HEX"))
	if err != nil {
		return nil, err
	}
	rawOrigins := strings.TrimSpace(os.Getenv("GOJET_AUTH_ALLOWED_ORIGIN"))
	if rawOrigins == "" {
		return nil, authn.ErrInvalid
	}
	origins := make([]string, 0, 2)
	for _, raw := range strings.Split(rawOrigins, ",") {
		if value := strings.TrimSpace(raw); value != "" {
			origins = append(origins, value)
		}
	}
	originPolicy, err := authn.NewOriginPolicy(origins...)
	if err != nil {
		return nil, err
	}
	// Reuse the account/session replay namespace so one P15 CSRF token cannot be
	// consumed once by Account and again by Workspace.
	replay, err := authn.NewRedisDigestReplayStore(redisClient, "auth:csrf:account", time.Hour)
	if err != nil {
		return nil, err
	}
	csrf, err := authn.NewCSRFManager(csrfKey, workspaceCSRFTTL, replay)
	if err != nil {
		return nil, err
	}
	store := authn.NewStore(db)
	return &workspaceSessionAuthority{
		authenticate: func(ctx context.Context, request *http.Request, now time.Time) (authn.Session, error) {
			return authn.AuthenticateRequest(ctx, store, request, now)
		},
		getUser: store.GetUserByID,
		authorizeUnsafe: func(ctx context.Context, request *http.Request, session authn.Session, now time.Time) error {
			return authn.AuthorizeUnsafeRequest(ctx, request, session, originPolicy, csrf, now)
		},
	}, nil
}

func (a *workspaceSessionAuthority) resolve(request *http.Request) (workspace.Principal, error) {
	if a == nil || a.authenticate == nil || a.getUser == nil || a.authorizeUnsafe == nil || request == nil {
		return workspace.Principal{}, workspace.ErrAuthenticationUnavailable
	}
	now := time.Now().UTC()
	session, err := a.authenticate(request.Context(), request, now)
	if err != nil {
		return workspace.Principal{}, workspaceSessionAuthenticationError(err)
	}
	if workspaceUnsafeMethod(request.Method) {
		if err := a.authorizeUnsafe(request.Context(), request, session, now); err != nil {
			return workspace.Principal{}, workspaceUnsafeAuthenticationError(err)
		}
	}
	user, err := a.getUser(request.Context(), session.UserID)
	if err != nil {
		if errors.Is(err, authn.ErrNotFound) {
			return workspace.Principal{}, workspace.ErrAuthenticationRequired
		}
		return workspace.Principal{}, workspace.ErrAuthenticationUnavailable
	}
	if user.Status != authn.UserStatusActive || user.EmailVerifiedAt == nil || strings.TrimSpace(user.ID) == "" || strings.TrimSpace(user.Email) == "" {
		return workspace.Principal{}, workspace.ErrAuthenticationRequired
	}
	return workspace.Principal{
		UserID:      user.ID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
	}, nil
}

func workspaceUnsafeMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func workspaceSessionAuthenticationError(err error) error {
	switch {
	case errors.Is(err, authn.ErrUnauthorized), errors.Is(err, authn.ErrRevoked), errors.Is(err, authn.ErrExpired), errors.Is(err, authn.ErrLocked):
		return workspace.ErrAuthenticationRequired
	default:
		return workspace.ErrAuthenticationUnavailable
	}
}

func workspaceUnsafeAuthenticationError(err error) error {
	switch {
	case errors.Is(err, authn.ErrForbidden), errors.Is(err, authn.ErrReplay), errors.Is(err, authn.ErrExpired), errors.Is(err, authn.ErrUnauthorized), errors.Is(err, authn.ErrRevoked):
		return workspace.ErrForbidden
	default:
		return workspace.ErrAuthenticationUnavailable
	}
}
