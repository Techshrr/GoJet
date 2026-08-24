package support

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type recordingSupportStore struct {
	publicCreateCalls       int
	ticketCreateCalls       int
	ticket                  Ticket
	messages                []TicketMessage
	messageVisibilityChecks []bool
}

func (s *recordingSupportStore) CreatePublicContact(_ context.Context, _ CreatePublicContactInput) (Ticket, bool, error) {
	s.publicCreateCalls++
	return Ticket{}, true, nil
}

func (s *recordingSupportStore) CreateWorkspaceTicket(_ context.Context, _ CreateWorkspaceTicketInput) (Ticket, bool, error) {
	s.ticketCreateCalls++
	return Ticket{}, true, nil
}

func (s *recordingSupportStore) ListRequesterTickets(_ context.Context, _, _ string, _ int) ([]Ticket, error) {
	return nil, nil
}

func (s *recordingSupportStore) GetTicket(_ context.Context, ticketID string) (Ticket, error) {
	if s.ticket.ID != "" && s.ticket.ID == ticketID {
		return s.ticket, nil
	}
	return Ticket{}, ErrSupportNotFound
}

func (s *recordingSupportStore) ListTicketMessages(_ context.Context, _ string, includeInternal bool) ([]TicketMessage, error) {
	s.messageVisibilityChecks = append(s.messageVisibilityChecks, includeInternal)
	if includeInternal {
		return append([]TicketMessage(nil), s.messages...), nil
	}
	items := make([]TicketMessage, 0, len(s.messages))
	for _, message := range s.messages {
		if message.Kind != MessageInternalNote {
			items = append(items, message)
		}
	}
	return items, nil
}

func (s *recordingSupportStore) ReplyRequester(_ context.Context, _ ReplyTicketInput) (Ticket, TicketMessage, bool, error) {
	return Ticket{}, TicketMessage{}, false, nil
}

func (s *recordingSupportStore) CloseRequesterTicket(_ context.Context, _, _ string) (Ticket, bool, error) {
	return Ticket{}, false, nil
}

type fixedPrincipalResolver struct {
	principal RequestPrincipal
	err       error
}

func (r fixedPrincipalResolver) ResolvePrincipal(_ *http.Request) (RequestPrincipal, error) {
	return r.principal, r.err
}

type allowAllRateLimiter struct{}

func (allowAllRateLimiter) AllowSubmission(_ context.Context, _ SubmissionSurface, _ string) (bool, error) {
	return true, nil
}

func newSecurityOrderingAPI(t *testing.T, store SupportStore, verifier TurnstileVerifier, replay TurnstileReplayStore) *API {
	t.Helper()
	memberships := &fakeMembershipResolver{membership: workspace.Membership{WorkspaceID: "ws-1", UserID: "user-1", Role: workspace.RoleMember}}
	domains := &fakeDomainRequestProjector{}
	notifications := &fakeSupportNotificationProducer{}
	api, err := NewAPI(
		store,
		memberships,
		domains,
		notifications,
		fixedPrincipalResolver{principal: RequestPrincipal{UserID: "user-1", Email: "user@example.test"}},
		verifier,
		replay,
		allowAllRateLimiter{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return api
}

func TestPublicContactInvalidTurnstileHasZeroStoreMutation(t *testing.T) {
	store := &recordingSupportStore{}
	verifier := &recordingVerifier{ok: false}
	replay := &recordingReplayStore{claim: true}
	api := newSecurityOrderingAPI(t, store, verifier, replay)

	req := httptest.NewRequest(http.MethodPost, "/api/public/contact", strings.NewReader(`{"email":"user@example.test","name":"User","subject":"Help","message":"Please help","turnstile_token":"bad-token"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.publicCreateCalls != 0 || store.ticketCreateCalls != 0 {
		t.Fatalf("invalid Turnstile reached durable store: public=%d ticket=%d", store.publicCreateCalls, store.ticketCreateCalls)
	}
	if replay.digest != ([32]byte{}) {
		t.Fatal("failed provider verification reached replay mutation")
	}
}

func TestWorkspaceTicketInvalidTurnstileHasZeroStoreMutation(t *testing.T) {
	store := &recordingSupportStore{}
	verifier := &recordingVerifier{ok: false}
	replay := &recordingReplayStore{claim: true}
	api := newSecurityOrderingAPI(t, store, verifier, replay)

	req := httptest.NewRequest(http.MethodPost, "/api/support/tickets", strings.NewReader(`{"workspace_id":"ws-1","category":"general","subject":"Help","message":"Please help","turnstile_token":"bad-token"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.publicCreateCalls != 0 || store.ticketCreateCalls != 0 {
		t.Fatalf("invalid Turnstile reached durable store: public=%d ticket=%d", store.publicCreateCalls, store.ticketCreateCalls)
	}
}

func TestWorkspaceTicketReplayedTurnstileHasZeroStoreMutation(t *testing.T) {
	store := &recordingSupportStore{}
	verifier := &recordingVerifier{ok: true}
	replay := &recordingReplayStore{claim: false}
	api := newSecurityOrderingAPI(t, store, verifier, replay)

	req := httptest.NewRequest(http.MethodPost, "/api/support/tickets", strings.NewReader(`{"workspace_id":"ws-1","category":"general","subject":"Help","message":"Please help","turnstile_token":"valid-but-replayed"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if store.ticketCreateCalls != 0 {
		t.Fatalf("replayed Turnstile reached durable store: %d", store.ticketCreateCalls)
	}
}

func TestRequesterTicketDetailExcludesInternalNotes(t *testing.T) {
	now := time.Now().UTC()
	store := &recordingSupportStore{
		ticket: Ticket{ID: "tkt-detail", WorkspaceID: "ws-1", RequesterUserID: "user-1", Category: "general", Subject: "Help", Status: TicketAwaitingUser, CreatedAt: now, UpdatedAt: now, Version: 2, CorrelationID: "corr-detail"},
		messages: []TicketMessage{
			{ID: "msg-requester", TicketID: "tkt-detail", ActorType: ActorRequester, ActorID: "user-1", Kind: MessageRequesterReply, Body: "Visible requester message", CreatedAt: now, CorrelationID: "corr-1"},
			{ID: "msg-internal", TicketID: "tkt-detail", ActorType: ActorSupport, ActorID: "support-1", Kind: MessageInternalNote, Body: "Secret internal note", CreatedAt: now, CorrelationID: "corr-2"},
			{ID: "msg-support", TicketID: "tkt-detail", ActorType: ActorSupport, ActorID: "support-1", Kind: MessageSupportReply, Body: "Visible support reply", CreatedAt: now, CorrelationID: "corr-3"},
		},
	}
	api := newSecurityOrderingAPI(t, store, &recordingVerifier{ok: true}, &recordingReplayStore{claim: true})
	req := httptest.NewRequest(http.MethodGet, "/api/support/tickets/tkt-detail", nil)
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(store.messageVisibilityChecks) != 1 || store.messageVisibilityChecks[0] {
		t.Fatalf("requester detail did not request server-filtered messages: %+v", store.messageVisibilityChecks)
	}
	var body struct {
		Ticket   Ticket          `json:"ticket"`
		Messages []TicketMessage `json:"messages"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("requester-visible messages=%d body=%s", len(body.Messages), recorder.Body.String())
	}
	for _, message := range body.Messages {
		if message.Kind == MessageInternalNote || strings.Contains(message.Body, "Secret internal note") {
			t.Fatalf("internal note leaked: %+v", message)
		}
	}
}

func TestDeterministicTurnstileVerifierMatchesDigestOnly(t *testing.T) {
	verifier, err := NewDeterministicTurnstileVerifier("ci-known-token")
	if err != nil {
		t.Fatal(err)
	}
	valid, err := verifier.Verify(context.Background(), "ci-known-token")
	if err != nil || !valid.Success {
		t.Fatalf("valid token result=%+v err=%v", valid, err)
	}
	invalid, err := verifier.Verify(context.Background(), "different-token")
	if err != nil || invalid.Success {
		t.Fatalf("invalid token result=%+v err=%v", invalid, err)
	}
}
