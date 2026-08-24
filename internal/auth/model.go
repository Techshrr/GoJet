package auth

import "time"

const (
	UserStatusPendingVerification = "pending_verification"
	UserStatusActive              = "active"
	UserStatusLocked              = "locked"
	UserStatusDisabled            = "disabled"

	SessionStatusActive  = "active"
	SessionStatusRevoked = "revoked"
	SessionStatusExpired = "expired"

	ProviderGoogle   = "google"
	ProviderFacebook = "facebook"
	ProviderGitHub   = "github"
	ProviderQQ       = "qq"
	ProviderWeChat   = "wechat"
	ProviderRainbow  = "rainbow"
)

var Providers = [...]string{
	ProviderGoogle,
	ProviderFacebook,
	ProviderGitHub,
	ProviderQQ,
	ProviderWeChat,
	ProviderRainbow,
}

type User struct {
	ID                string     `json:"id"`
	Email             string     `json:"email"`
	EmailNormalized   string     `json:"-"`
	DisplayName       string     `json:"display_name"`
	Status            string     `json:"status"`
	EmailVerifiedAt   *time.Time `json:"email_verified_at,omitempty"`
	PasswordChangedAt *time.Time `json:"password_changed_at,omitempty"`
	Version           uint64     `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type Session struct {
	ID            string     `json:"id"`
	UserID        string     `json:"user_id"`
	Status        string     `json:"status"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	LastSeenAt    time.Time  `json:"last_seen_at"`
	CorrelationID string     `json:"correlation_id"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type OAuthIdentity struct {
	ID                    string    `json:"id"`
	UserID                string    `json:"user_id"`
	Provider              string    `json:"provider"`
	ProviderEmail         string    `json:"provider_email,omitempty"`
	ProviderEmailVerified bool      `json:"provider_email_verified"`
	DisplayName           string    `json:"display_name,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type SessionSecret struct {
	Session   Session
	Token     string
	CSRFToken string
}

type CreateUserInput struct {
	Email       string
	DisplayName string
}

type BindOAuthIdentityInput struct {
	UserID                string
	Provider              string
	ProviderSubject       string
	ProviderEmail         string
	ProviderEmailVerified bool
	DisplayName           string
}
