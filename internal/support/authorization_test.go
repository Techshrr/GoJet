package support

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
)

type fakeMembershipResolver struct {
	membership workspace.Membership
	err        error
	calls      int
}

func (f *fakeMembershipResolver) GetMembership(_ context.Context, workspaceID, userID string) (workspace.Membership, error) {
	f.calls++
	if f.err != nil {
		return workspace.Membership{}, f.err
	}
	return f.membership, nil
}

type fakeDomainRequestProjector struct {
	resolved     domains.ResolvedEntitlement
	resolveErr   error
	input        domains.AccessRequestInput
	resolveCalls int
	calls        int
	err          error
}

func (f *fakeDomainRequestProjector) ResolveEntitlement(_ context.Context, _ string, _ time.Time) (domains.ResolvedEntitlement, error) {
	f.resolveCalls++
	if f.resolveErr != nil {
		return domains.ResolvedEntitlement{}, f.resolveErr
	}
	return f.resolved, nil
}

func (f *fakeDomainRequestProjector) ProjectAccessRequest(_ context.Context, input domains.AccessRequestInput) (domains.AccessRequest, error) {
	f.calls++
	f.input = input
	if f.err != nil {
		return domains.AccessRequest{}, f.err
	}
	return domains.AccessRequest{WorkspaceID: input.WorkspaceID, SupportTicketID: input.SupportTicketID, SubmittedAt: input.SubmittedAt}, nil
}

func TestAuthorizeRequesterTicketRequiresCurrentP12MembershipAndOwnership(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-auth-1", "ws-1", "user-1", "general", "Help", "corr-auth-1", now)
	if err != nil {
		t.Fatal(err)
	}

	resolver := &fakeMembershipResolver{membership: workspace.Membership{WorkspaceID: "ws-1", UserID: "user-1", Role: workspace.RoleMember}}
	if err := AuthorizeRequesterTicket(context.Background(), resolver, ticket, "ws-1", "user-1"); err != nil {
		t.Fatalf("current requester rejected: %v", err)
	}
	if resolver.calls != 1 {
		t.Fatalf("membership resolutions=%d", resolver.calls)
	}

	former := &fakeMembershipResolver{err: workspace.ErrForbidden}
	if err := AuthorizeRequesterTicket(context.Background(), former, ticket, "ws-1", "user-1"); !errors.Is(err, ErrTicketUnavailable) {
		t.Fatalf("former member error=%v", err)
	}

	otherRequester := &fakeMembershipResolver{membership: workspace.Membership{WorkspaceID: "ws-1", UserID: "user-2", Role: workspace.RoleMember}}
	if err := AuthorizeRequesterTicket(context.Background(), otherRequester, ticket, "ws-1", "user-2"); !errors.Is(err, ErrTicketUnavailable) {
		t.Fatalf("wrong requester error=%v", err)
	}
	if otherRequester.calls != 0 {
		t.Fatalf("foreign requester triggered membership lookup: %d", otherRequester.calls)
	}

	foreignWorkspace := &fakeMembershipResolver{membership: workspace.Membership{WorkspaceID: "ws-2", UserID: "user-1", Role: workspace.RoleMember}}
	if err := AuthorizeRequesterTicket(context.Background(), foreignWorkspace, ticket, "ws-2", "user-1"); !errors.Is(err, ErrTicketUnavailable) {
		t.Fatalf("foreign workspace error=%v", err)
	}
	if foreignWorkspace.calls != 0 {
		t.Fatalf("foreign workspace triggered membership lookup: %d", foreignWorkspace.calls)
	}
}

func TestAuthorizeRequesterTicketPreservesOperationalMembershipErrors(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-auth-2", "ws-1", "user-1", "general", "Help", "corr-auth-2", now)
	if err != nil {
		t.Fatal(err)
	}
	backendErr := errors.New("membership backend unavailable")
	resolver := &fakeMembershipResolver{err: backendErr}
	if err := AuthorizeRequesterTicket(context.Background(), resolver, ticket, "ws-1", "user-1"); !errors.Is(err, backendErr) {
		t.Fatalf("operational error was hidden: %v", err)
	}
}

func TestProjectTicketDomainAccessUsesP06RequestAuthorityOnly(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-domain-request-1", "ws-1", "user-1", CustomDomainAccessCategory, "Request custom domain access", "corr-domain-request-1", now)
	if err != nil {
		t.Fatal(err)
	}
	projector := &fakeDomainRequestProjector{resolved: domains.ResolvedEntitlement{Source: domains.SourceNone, Status: domains.EntitlementExpired}}
	request, err := ProjectTicketDomainAccess(context.Background(), projector, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if projector.resolveCalls != 1 || projector.calls != 1 {
		t.Fatalf("resolve/project calls=%d/%d", projector.resolveCalls, projector.calls)
	}
	if projector.input.WorkspaceID != ticket.WorkspaceID || projector.input.SupportTicketID != ticket.ID || projector.input.CorrelationID != ticket.CorrelationID {
		t.Fatalf("projection input=%+v", projector.input)
	}
	if projector.input.RequestedDomainLimit != nil {
		t.Fatalf("P14 unexpectedly requested a domain limit: %+v", projector.input.RequestedDomainLimit)
	}
	if !projector.input.SubmittedAt.Equal(ticket.CreatedAt) {
		t.Fatalf("submitted_at=%s ticket.created_at=%s", projector.input.SubmittedAt, ticket.CreatedAt)
	}
	if request.WorkspaceID != ticket.WorkspaceID || request.SupportTicketID != ticket.ID {
		t.Fatalf("request=%+v", request)
	}
}

func TestProjectTicketDomainAccessSuppressesActiveIndependentEntitlement(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	for _, source := range []domains.EntitlementSourceKind{domains.SourcePlan, domains.SourceManualApproval} {
		t.Run(string(source), func(t *testing.T) {
			ticket, err := NewWorkspaceTicket("tkt-domain-active-"+string(source), "ws-1", "user-1", CustomDomainAccessCategory, "Request custom domain access", "corr-active-"+string(source), now)
			if err != nil {
				t.Fatal(err)
			}
			projector := &fakeDomainRequestProjector{resolved: domains.ResolvedEntitlement{Source: source, Status: domains.EntitlementActive, MutationAllowed: true}}
			request, err := ProjectTicketDomainAccess(context.Background(), projector, ticket)
			if err != nil {
				t.Fatal(err)
			}
			if request != (domains.AccessRequest{}) {
				t.Fatalf("active entitlement returned request=%+v", request)
			}
			if projector.resolveCalls != 1 || projector.calls != 0 {
				t.Fatalf("active entitlement resolve/project calls=%d/%d", projector.resolveCalls, projector.calls)
			}
		})
	}
}

func TestProjectTicketDomainAccessReplayIsWriteFree(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-domain-replay-1", "ws-1", "user-1", CustomDomainAccessCategory, "Request custom domain access", "corr-domain-replay-1", now)
	if err != nil {
		t.Fatal(err)
	}
	projector := &fakeDomainRequestProjector{resolved: domains.ResolvedEntitlement{Source: domains.SourceNone, Status: domains.EntitlementRequested, SupportTicketID: ticket.ID}}
	request, err := ProjectTicketDomainAccess(context.Background(), projector, ticket)
	if err != nil {
		t.Fatal(err)
	}
	if projector.resolveCalls != 1 || projector.calls != 0 {
		t.Fatalf("replay resolve/project calls=%d/%d", projector.resolveCalls, projector.calls)
	}
	if request.WorkspaceID != ticket.WorkspaceID || request.SupportTicketID != ticket.ID || !request.SubmittedAt.Equal(ticket.CreatedAt) {
		t.Fatalf("replay request=%+v", request)
	}
}

func TestProjectTicketDomainAccessRejectsNonRequestCategoryBeforeP06(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-general-1", "ws-1", "user-1", "general", "General question", "corr-general-1", now)
	if err != nil {
		t.Fatal(err)
	}
	projector := &fakeDomainRequestProjector{}
	if _, err := ProjectTicketDomainAccess(context.Background(), projector, ticket); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non-request category error=%v", err)
	}
	if projector.resolveCalls != 0 || projector.calls != 0 {
		t.Fatalf("non-request category reached P06 resolve/project: %d/%d", projector.resolveCalls, projector.calls)
	}
}
