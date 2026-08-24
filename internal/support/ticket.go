package support

import (
	"strings"
	"time"
)

func NewWorkspaceTicket(id, workspaceID, requesterUserID, category, subject, correlationID string, now time.Time) (Ticket, error) {
	now = now.UTC()
	ticket := Ticket{
		ID:              strings.TrimSpace(id),
		WorkspaceID:     strings.TrimSpace(workspaceID),
		RequesterUserID: strings.TrimSpace(requesterUserID),
		Category:        strings.TrimSpace(category),
		Subject:         strings.TrimSpace(subject),
		Status:          TicketOpen,
		CreatedAt:       now,
		UpdatedAt:       now,
		Version:         1,
		CorrelationID:   strings.TrimSpace(correlationID),
	}
	if err := ticket.Validate(); err != nil {
		return Ticket{}, err
	}
	return ticket, nil
}

func ApplyTicketMessage(ticket Ticket, message TicketMessage, now time.Time) (Ticket, error) {
	if err := ticket.Validate(); err != nil {
		return Ticket{}, err
	}
	if err := message.Validate(); err != nil || message.TicketID != ticket.ID {
		return Ticket{}, ErrInvalidInput
	}
	if ticket.Status == TicketClosedStatus {
		return Ticket{}, ErrTicketClosed
	}

	now = now.UTC()
	if now.Before(ticket.UpdatedAt) || message.CreatedAt.Before(ticket.CreatedAt) {
		return Ticket{}, ErrInvalidInput
	}

	next := ticket
	switch message.Kind {
	case MessageRequesterReply:
		next.Status = TicketAwaitingSupport
	case MessageSupportReply:
		next.Status = TicketAwaitingUser
	case MessageInternalNote:
		// Internal notes are deliberately state-neutral and never become requester-visible replies.
	default:
		return Ticket{}, ErrInvalidTransition
	}
	next.Version++
	next.UpdatedAt = now
	if err := next.Validate(); err != nil {
		return Ticket{}, err
	}
	return next, nil
}

func CloseTicket(ticket Ticket, now time.Time) (Ticket, error) {
	if err := ticket.Validate(); err != nil {
		return Ticket{}, err
	}
	if ticket.Status == TicketClosedStatus {
		return ticket, nil
	}
	now = now.UTC()
	if now.Before(ticket.UpdatedAt) {
		return Ticket{}, ErrInvalidInput
	}
	next := ticket
	next.Status = TicketClosedStatus
	next.ClosedAt = &now
	next.UpdatedAt = now
	next.Version++
	if err := next.Validate(); err != nil {
		return Ticket{}, err
	}
	return next, nil
}

// ProjectDomainAccessRequest exposes only request semantics. It has no grant path and intentionally
// carries no entitlement source capable of authorizing a custom domain.
func ProjectDomainAccessRequest(ticket Ticket) (DomainRequestProjection, error) {
	if err := ticket.Validate(); err != nil {
		return DomainRequestProjection{}, err
	}
	if ticket.Category != CustomDomainAccessCategory || ticket.WorkspaceID == "" {
		return DomainRequestProjection{}, ErrInvalidInput
	}
	return DomainRequestProjection{
		WorkspaceID:     ticket.WorkspaceID,
		SupportTicketID: ticket.ID,
		Status:          "requested",
		Source:          "none",
		GrantAuthority:  "NONE",
	}, nil
}
