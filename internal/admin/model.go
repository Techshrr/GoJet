package admin

import "time"

const (
	PermissionPlatformRead              = "platform.read"
	PermissionAdminsManage              = "admins.manage"
	PermissionUsersManage               = "users.manage"
	PermissionWorkspacesManage          = "workspaces.manage"
	PermissionLinksManage               = "links.manage"
	PermissionDomainsManage             = "domains.manage"
	PermissionDomainsRiskManage         = "domains.risk.manage"
	PermissionDomainsEntitlementsManage = "domains.entitlements.manage"
	PermissionSecurityManage            = "security.manage"
	PermissionFilesManage               = "files.manage"
	PermissionTicketsManage             = "tickets.manage"
	PermissionOperationsManage          = "operations.manage"
	PermissionBillingManage             = "billing.manage"
	PermissionMailManage                = "mail.manage"
	PermissionSettingsManage            = "settings.manage"
	PermissionContentManage             = "content.manage"
)

var PermissionCatalog = []string{
	PermissionPlatformRead,
	PermissionAdminsManage,
	PermissionUsersManage,
	PermissionWorkspacesManage,
	PermissionLinksManage,
	PermissionDomainsManage,
	PermissionDomainsRiskManage,
	PermissionDomainsEntitlementsManage,
	PermissionSecurityManage,
	PermissionFilesManage,
	PermissionTicketsManage,
	PermissionOperationsManage,
	PermissionBillingManage,
	PermissionMailManage,
	PermissionSettingsManage,
	PermissionContentManage,
}

var permissionSet = func() map[string]struct{} {
	m := make(map[string]struct{}, len(PermissionCatalog))
	for _, p := range PermissionCatalog {
		m[p] = struct{}{}
	}
	return m
}()

func ValidPermission(permission string) bool {
	_, ok := permissionSet[permission]
	return ok
}

type Administrator struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	MFAEnabled  bool      `json:"mfa_enabled"`
	Version     uint64    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Permissions []string  `json:"permissions"`
	Version     uint64    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Session struct {
	ID              string     `json:"id"`
	AdministratorID string     `json:"administrator_id"`
	Status          string     `json:"status"`
	ExpiresAt       time.Time  `json:"expires_at"`
	MFAVerifiedAt   *time.Time `json:"mfa_verified_at,omitempty"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	CreatedAt       time.Time  `json:"created_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
}

type Principal struct {
	Administrator Administrator
	Session       Session
	Permissions   map[string]struct{}
	CSRFHash      [32]byte
}

func (p Principal) Has(permission string) bool {
	if !ValidPermission(permission) {
		return false
	}
	_, ok := p.Permissions[permission]
	return ok
}

type SessionSecret struct {
	Session   Session `json:"session"`
	Token     string  `json:"session_token"`
	CSRFToken string  `json:"csrf_token"`
}

type AuditEvent struct {
	ID           uint64         `json:"id"`
	ActorKind    string         `json:"actor_kind"`
	ActorID      string         `json:"actor_id"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	Result       string         `json:"result"`
	RequestID    string         `json:"request_id"`
	Reason       string         `json:"reason,omitempty"`
	Before       map[string]any `json:"before,omitempty"`
	After        map[string]any `json:"after,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type MutationAuthority struct {
	Reason         string
	CorrelationID  string
	IdempotencyKey string
}
