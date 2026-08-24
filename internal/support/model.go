package support

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidInput      = errors.New("invalid support input")
	ErrInvalidTransition = errors.New("invalid support state transition")
	ErrTicketClosed      = errors.New("ticket is closed")
	ErrAttachmentBlocked = errors.New("attachment is not clean")
	ErrTurnstileRejected = errors.New("turnstile verification rejected")
	ErrTurnstileReplay   = errors.New("turnstile token replayed")
	ErrTemplateVariable  = errors.New("invalid mail template variable")
	ErrSensitiveVariable = errors.New("sensitive mail template variable")
	ErrMailState         = errors.New("invalid mail job state")
	ErrMailClaim         = errors.New("invalid mail job claim")
)

type TicketStatus string

const (
	TicketOpen            TicketStatus = "open"
	TicketAwaitingUser    TicketStatus = "awaiting_user"
	TicketAwaitingSupport TicketStatus = "awaiting_support"
	TicketClosedStatus    TicketStatus = "closed"
)

type MessageKind string

const (
	MessageRequesterReply MessageKind = "requester_reply"
	MessageSupportReply   MessageKind = "support_reply"
	MessageInternalNote   MessageKind = "internal_note"
)

type ActorType string

const (
	ActorRequester ActorType = "requester"
	ActorSupport   ActorType = "support"
)

const CustomDomainAccessCategory = "custom-domain-access"

type Ticket struct {
	ID              string       `json:"id"`
	WorkspaceID     string       `json:"workspace_id,omitempty"`
	RequesterUserID string       `json:"requester_user_id,omitempty"`
	PublicContactID string       `json:"public_contact_id,omitempty"`
	Category        string       `json:"category"`
	Subject         string       `json:"subject"`
	Status          TicketStatus `json:"status"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
	ClosedAt        *time.Time   `json:"closed_at,omitempty"`
	Version         uint64       `json:"version"`
	CorrelationID   string       `json:"correlation_id"`
}

type TicketMessage struct {
	ID                 string      `json:"id"`
	TicketID           string      `json:"ticket_id"`
	ActorType          ActorType   `json:"actor_type"`
	ActorID            string      `json:"actor_id"`
	Kind               MessageKind `json:"kind"`
	Body               string      `json:"body"`
	IdempotencyKeyHash [32]byte    `json:"-"`
	CreatedAt          time.Time   `json:"created_at"`
	CorrelationID      string      `json:"correlation_id"`
}

type DomainRequestProjection struct {
	WorkspaceID     string `json:"workspace_id"`
	SupportTicketID string `json:"support_ticket_id"`
	Status          string `json:"status"`
	Source          string `json:"source"`
	GrantAuthority  string `json:"grant_authority"`
}

func (t Ticket) Validate() error {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Category) == "" || strings.TrimSpace(t.Subject) == "" || strings.TrimSpace(t.CorrelationID) == "" || t.CreatedAt.IsZero() || t.UpdatedAt.IsZero() || t.Version == 0 {
		return ErrInvalidInput
	}
	workspaceScoped := strings.TrimSpace(t.WorkspaceID) != "" || strings.TrimSpace(t.RequesterUserID) != ""
	publicScoped := strings.TrimSpace(t.PublicContactID) != ""
	if workspaceScoped == publicScoped {
		return ErrInvalidInput
	}
	if workspaceScoped && (strings.TrimSpace(t.WorkspaceID) == "" || strings.TrimSpace(t.RequesterUserID) == "") {
		return ErrInvalidInput
	}
	switch t.Status {
	case TicketOpen, TicketAwaitingUser, TicketAwaitingSupport, TicketClosedStatus:
	default:
		return ErrInvalidInput
	}
	if t.Status == TicketClosedStatus {
		if t.ClosedAt == nil || t.ClosedAt.IsZero() || t.ClosedAt.Before(t.CreatedAt) {
			return ErrInvalidInput
		}
	} else if t.ClosedAt != nil {
		return ErrInvalidInput
	}
	if t.UpdatedAt.Before(t.CreatedAt) {
		return ErrInvalidInput
	}
	return nil
}

func (m TicketMessage) Validate() error {
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.TicketID) == "" || strings.TrimSpace(m.ActorID) == "" || strings.TrimSpace(m.Body) == "" || strings.TrimSpace(m.CorrelationID) == "" || m.CreatedAt.IsZero() {
		return ErrInvalidInput
	}
	switch m.Kind {
	case MessageRequesterReply:
		if m.ActorType != ActorRequester {
			return ErrInvalidInput
		}
	case MessageSupportReply, MessageInternalNote:
		if m.ActorType != ActorSupport {
			return ErrInvalidInput
		}
	default:
		return ErrInvalidInput
	}
	return nil
}
