package support

import (
	"context"
	"errors"
	"strings"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
)

// ErrTicketUnavailable deliberately collapses foreign Workspace, former-member,
// public-contact and wrong-requester cases so requester APIs do not disclose existence.
var ErrTicketUnavailable = errors.New("support ticket unavailable")

type WorkspaceMembershipResolver interface {
	GetMembership(ctx context.Context, workspaceID, userID string) (workspace.Membership, error)
}

type DomainAccessRequestProjector interface {
	ProjectAccessRequest(ctx context.Context, input domains.AccessRequestInput) (domains.AccessRequest, error)
}

// AuthorizeRequesterTicket re-resolves current P12 Workspace membership and then
// requires ownership by the authenticated requester. Admin authority is intentionally
// excluded; P14 consumes tickets.manage separately on the Admin surface.
func AuthorizeRequesterTicket(ctx context.Context, memberships WorkspaceMembershipResolver, ticket Ticket, workspaceID, userID string) error {
	if memberships == nil {
		return ErrInvalidInput
	}
	if err := ticket.Validate(); err != nil {
		return err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	userID = strings.TrimSpace(userID)
	if workspaceID == "" || userID == "" {
		return ErrTicketUnavailable
	}
	if ticket.PublicContactID != "" || ticket.WorkspaceID != workspaceID || ticket.RequesterUserID != userID {
		return ErrTicketUnavailable
	}

	membership, err := memberships.GetMembership(ctx, workspaceID, userID)
	if err != nil {
		if errors.Is(err, workspace.ErrForbidden) || errors.Is(err, workspace.ErrNotFound) {
			return ErrTicketUnavailable
		}
		return err
	}
	if membership.WorkspaceID != workspaceID || membership.UserID != userID {
		return ErrTicketUnavailable
	}
	return nil
}

// ProjectTicketDomainAccess uses only P06's request projection interface. The
// interface deliberately exposes no plan/manual-approval/domain mutation method,
// keeping a P14 ticket incapable of becoming grant authority by construction.
func ProjectTicketDomainAccess(ctx context.Context, projector DomainAccessRequestProjector, ticket Ticket) (domains.AccessRequest, error) {
	if projector == nil {
		return domains.AccessRequest{}, ErrInvalidInput
	}
	projection, err := ProjectDomainAccessRequest(ticket)
	if err != nil {
		return domains.AccessRequest{}, err
	}
	request, err := projector.ProjectAccessRequest(ctx, domains.AccessRequestInput{
		WorkspaceID:     projection.WorkspaceID,
		SupportTicketID: projection.SupportTicketID,
		SubmittedAt:     ticket.CreatedAt.UTC(),
		CorrelationID:   ticket.CorrelationID,
	})
	if err != nil {
		return domains.AccessRequest{}, err
	}
	if request.WorkspaceID != projection.WorkspaceID || request.SupportTicketID != projection.SupportTicketID || request.SubmittedAt.IsZero() {
		return domains.AccessRequest{}, ErrInvalidInput
	}
	return request, nil
}
