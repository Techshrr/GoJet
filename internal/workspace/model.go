package workspace

import "time"

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleViewer = "viewer"
)

type Principal struct {
	UserID      string `json:"user_id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Version   uint64    `json:"version"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Membership struct {
	ID          uint64    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joined_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Invitation struct {
	ID          uint64    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	ExpiresAt   time.Time `json:"expires_at"`
	InvitedBy   string    `json:"invited_by"`
	AcceptedBy  *string   `json:"accepted_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type InvitationInspection struct {
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expires_at"`
	AccountMatch  bool      `json:"account_match"`
}

type Organization struct {
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     uint64    `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Campaign struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Version     uint64    `json:"version"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Tag struct {
	ID             uint64    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalized_name"`
	Version        uint64    `json:"version"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Folder struct {
	ID             uint64    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"normalized_name"`
	Version        uint64    `json:"version"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type LinkOrganization struct {
	WorkspaceID string    `json:"workspace_id"`
	LinkID      uint64    `json:"link_id"`
	CampaignID  *string   `json:"campaign_id,omitempty"`
	FolderID    *uint64   `json:"folder_id,omitempty"`
	TagIDs      []uint64  `json:"tag_ids"`
	Version     uint64    `json:"version"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Notification struct {
	ID              uint64     `json:"id"`
	WorkspaceID     string     `json:"workspace_id"`
	RecipientUserID string     `json:"recipient_user_id"`
	Category        string     `json:"category"`
	EventKey        string     `json:"event_key"`
	DedupeKey       string     `json:"-"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	DeepLink        string     `json:"deep_link,omitempty"`
	ResourceType    string     `json:"resource_type,omitempty"`
	ResourceID      string     `json:"resource_id,omitempty"`
	ReadAt          *time.Time `json:"read_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type NotificationState struct {
	WorkspaceID   string     `json:"workspace_id"`
	Status        string     `json:"status"`
	DataThroughAt *time.Time `json:"data_through_at,omitempty"`
	StateReason   string     `json:"state_reason"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type AuditEvent struct {
	ID                   uint64    `json:"id"`
	WorkspaceID          string    `json:"workspace_id"`
	ActorID              string    `json:"actor_id"`
	Action               string    `json:"action"`
	ResourceType         string    `json:"resource_type"`
	ResourceID           string    `json:"resource_id"`
	Reason               *string   `json:"reason,omitempty"`
	RequestCorrelationID string    `json:"request_correlation_id"`
	Result               string    `json:"result"`
	MetadataJSON         string    `json:"metadata_json"`
	CreatedAt            time.Time `json:"created_at"`
}
