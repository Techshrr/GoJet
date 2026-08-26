package auth

import (
	"context"
	"database/sql"
	"time"
)

const (
	OAuthIntentLogin    = "login"
	OAuthIntentRegister = "register"
	OAuthIntentBind     = "bind"
	SettingsManage      = "settings.manage"
)

type SettingsPermissionAuthorizer interface {
	Authorize(ctx context.Context, actorID, permission string) error
}

type OAuthProviderConfig struct {
	Provider         string   `json:"provider"`
	Enabled          bool     `json:"enabled"`
	Configured       bool     `json:"configured"`
	ClientID         string   `json:"client_id"`
	AuthorizationURL string   `json:"authorization_url"`
	TokenURL         string   `json:"token_url"`
	UserInfoURL      string   `json:"userinfo_url"`
	RedirectURI      string   `json:"redirect_uri"`
	Scopes           []string `json:"scopes"`
	SecretConfigured bool     `json:"secret_configured"`
	Version          uint64   `json:"version"`
}

type OAuthProviderUpdate struct {
	Provider         string
	Enabled          bool
	ClientID         string
	ClientSecret     string
	AuthorizationURL string
	TokenURL         string
	UserInfoURL      string
	RedirectURI      string
	Scopes           []string
}

type OAuthStartInput struct {
	Provider            string
	Intent              string
	InitiatingUserID    string
	InitiatingSessionID string
	CorrelationID       string
	MutationAuthority   *UnsafeMutationAuthority
}

type OAuthStartResult struct {
	StateID          string
	Provider         string
	Intent           string
	AuthorizationURL string
	State            string
	PKCEVerifier     string
	ExpiresAt        time.Time
}

type OAuthProviderExchangeRequest struct {
	Provider     string
	Code         string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	PKCEVerifier string
}

type OAuthProviderClaim struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

type OAuthProviderAdapter interface {
	Exchange(ctx context.Context, request OAuthProviderExchangeRequest) (OAuthProviderClaim, error)
}

type OAuthCallbackInput struct {
	Provider      string
	State         string
	Code          string
	CorrelationID string
}

type OAuthCallbackResult struct {
	StateID               string
	Provider              string
	Intent                string
	InitiatingUserID      string
	InitiatingSessionID   string
	ProviderSubject       string
	ProviderEmail         string
	ProviderEmailVerified bool
	DisplayName           string
}

type OAuthService struct {
	db       *sql.DB
	crypto   *OAuthCrypto
	stateTTL time.Duration
}

func NewOAuthService(db *sql.DB, crypto *OAuthCrypto, stateTTL time.Duration) (*OAuthService, error) {
	if db == nil || crypto == nil || crypto.KeyID() == "" || stateTTL < 2*time.Minute || stateTTL > 30*time.Minute {
		return nil, ErrInvalid
	}
	return &OAuthService{db: db, crypto: crypto, stateTTL: stateTTL}, nil
}
