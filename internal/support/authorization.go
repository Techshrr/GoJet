package support

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
)

// ErrTicketUnavailable deliberately collapses foreign Workspace, former-member,
// public-contact and wrong-requester cases so requester APIs do not disclose existence.
var ErrTicketUnavailable = errors.New("support ticket unavailable")

type WorkspaceMembershipResolver interface {
	GetMembership(ctx context.Context, workspaceID, userID string) (workspace.Membership, error)
}

// DomainAccessRequestProjector is intentionally capability-narrow: P14 may read
// current P06 entitlement state and project an AccessRequest, but it cannot call
// plan/manual-approval grant or custom-domain mutation methods through this port.
type DomainAccessRequestProjector interface {
	ResolveEntitlement(ctx context.Context, workspaceID string, now time.Time) (domains.ResolvedEntitlement, error)
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

// ProjectTicketDomainAccess uses only P06 request authority. Existing active plan
// or manual authority suppresses request projection. Replaying the same already-
// requested ticket is write-free and returns the deterministic linkage instead.
func ProjectTicketDomainAccess(ctx context.Context, projector DomainAccessRequestProjector, ticket Ticket) (domains.AccessRequest, error) {
	if projector == nil {
		return domains.AccessRequest{}, ErrInvalidInput
	}
	projection, err := ProjectDomainAccessRequest(ticket)
	if err != nil {
		return domains.AccessRequest{}, err
	}
	submittedAt := ticket.CreatedAt.UTC()
	resolved, err := projector.ResolveEntitlement(ctx, projection.WorkspaceID, submittedAt)
	if err != nil {
		return domains.AccessRequest{}, err
	}
	if resolved.Source != domains.SourceNone && resolved.Status == domains.EntitlementActive {
		return domains.AccessRequest{}, nil
	}
	if resolved.Source == domains.SourceNone && resolved.Status == domains.EntitlementRequested && resolved.SupportTicketID == projection.SupportTicketID {
		request := domains.AccessRequest{
			WorkspaceID:     projection.WorkspaceID,
			SupportTicketID: projection.SupportTicketID,
			SubmittedAt:     submittedAt,
		}
		if recorder, ok := projector.(DomainRequestAuditRecorder); ok {
			if err := recorder.RecordDomainRequestAudit(ctx, ticket, request); err != nil {
				return domains.AccessRequest{}, err
			}
		}
		return request, nil
	}

	request, err := projector.ProjectAccessRequest(ctx, domains.AccessRequestInput{
		WorkspaceID:     projection.WorkspaceID,
		SupportTicketID: projection.SupportTicketID,
		SubmittedAt:     submittedAt,
		CorrelationID:   ticket.CorrelationID,
	})
	if err != nil {
		return domains.AccessRequest{}, err
	}
	if request.WorkspaceID != projection.WorkspaceID || request.SupportTicketID != projection.SupportTicketID || request.SubmittedAt.IsZero() {
		return domains.AccessRequest{}, ErrInvalidInput
	}
	if recorder, ok := projector.(DomainRequestAuditRecorder); ok {
		if err := recorder.RecordDomainRequestAudit(ctx, ticket, request); err != nil {
			return domains.AccessRequest{}, err
		}
	}
	return request, nil
}
