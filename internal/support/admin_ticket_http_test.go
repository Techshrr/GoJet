package support

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type fixedAdminPrincipalResolver struct {
	principal RequestPrincipal
	err       error
}

func (r fixedAdminPrincipalResolver) ResolvePrincipal(_ *http.Request) (RequestPrincipal, error) {
	return r.principal, r.err
}

type recordingAdminPermissionResolver struct {
	allowed bool
	err     error
	seen    []string
}

func (r *recordingAdminPermissionResolver) HasPermission(_ context.Context, _ RequestPrincipal, permission string) (bool, error) {
	r.seen = append(r.seen, permission)
	return r.allowed, r.err
}

type fakeAdminTicketStore struct {
	ticket       Ticket
	message      TicketMessage
	listCalls    int
	replyCalls   int
	closeCalls   int
	replyCreated bool
	closeChanged bool
}

func (s *fakeAdminTicketStore) ListAdminTickets(_ context.Context, _ int) ([]Ticket, error) {
	s.listCalls++
	return []Ticket{s.ticket}, nil
}

func (s *fakeAdminTicketStore) GetTicket(_ context.Context, _ string) (Ticket, error) {
	return s.ticket, nil
}

func (s *fakeAdminTicketStore) ListTicketMessages(_ context.Context, _ string, _ bool) ([]TicketMessage, error) {
	return []TicketMessage{s.message}, nil
}

func (s *fakeAdminTicketStore) AddAdminMessage(_ context.Context, input AdminMessageInput) (Ticket, TicketMessage, bool, error) {
	s.replyCalls++
	message := s.message
	message.Kind = input.Kind
	message.ActorType = ActorSupport
	message.ActorID = input.ActorID
	message.Body = input.Body
	message.CorrelationID = input.CorrelationID
	return s.ticket, message, s.replyCreated, nil
}

func (s *fakeAdminTicketStore) CloseAdminTicket(_ context.Context, _ string, _ uint64) (Ticket, bool, error) {
	s.closeCalls++
	return s.ticket, s.closeChanged, nil
}

type recordingSupportNotificationProducer struct {
	inputs []workspace.NotificationInput
	err    error
}

func (p *recordingSupportNotificationProducer) ProduceNotification(_ context.Context, input workspace.NotificationInput) (workspace.Notification, bool, error) {
	p.inputs = append(p.inputs, input)
	if p.err != nil {
		return workspace.Notification{}, false, p.err
	}
	return workspace.Notification{}, true, nil
}

func adminFixture() (Ticket, TicketMessage) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	ticket := Ticket{
		ID: "tkt-1", WorkspaceID: "ws-1", RequesterUserID: "user-1", Category: "general", Subject: "Help",
		Status: TicketOpen, CreatedAt: now, UpdatedAt: now, Version: 1, CorrelationID: "corr-1",
	}
	message := TicketMessage{
		ID: "msg-1", TicketID: ticket.ID, ActorType: ActorSupport, ActorID: "admin-1", Kind: MessageSupportReply,
		Body: "reply", CreatedAt: now, CorrelationID: "corr-2",
	}
	return ticket, message
}

func TestAdminTicketPermissionAuthorityFailsClosed(t *testing.T) {
	ticket, message := adminFixture()
	store := &fakeAdminTicketStore{ticket: ticket, message: message}
	notifications := &recordingSupportNotificationProducer{}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, nil, notifications)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/admin/support/tickets", nil)
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if store.listCalls != 0 {
		t.Fatalf("store calls=%d", store.listCalls)
	}
}

func TestAdminTicketPermissionDeniedBeforeStore(t *testing.T) {
	ticket, message := adminFixture()
	store := &fakeAdminTicketStore{ticket: ticket, message: message}
	permissions := &recordingAdminPermissionResolver{allowed: false}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions, &recordingSupportNotificationProducer{})
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/support/tickets", nil))
	if res.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if store.listCalls != 0 {
		t.Fatalf("store calls=%d", store.listCalls)
	}
	if len(permissions.seen) != 1 || permissions.seen[0] != TicketsManagePermission {
		t.Fatalf("permissions=%v", permissions.seen)
	}
}

func TestAdminInternalNoteNeverProducesRequesterNotification(t *testing.T) {
	ticket, message := adminFixture()
	store := &fakeAdminTicketStore{ticket: ticket, message: message, replyCreated: true}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	notifications := &recordingSupportNotificationProducer{}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions, notifications)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/support/tickets/tkt-1/replies", strings.NewReader(`{"kind":"internal_note","message":"private note"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "note-1")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if store.replyCalls != 1 {
		t.Fatalf("reply calls=%d", store.replyCalls)
	}
	if len(notifications.inputs) != 0 {
		t.Fatalf("internal note produced notifications=%v", notifications.inputs)
	}
}

func TestAdminSupportReplyReplayRepairsNotificationThroughP12Dedupe(t *testing.T) {
	ticket, message := adminFixture()
	store := &fakeAdminTicketStore{ticket: ticket, message: message, replyCreated: false}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	notifications := &recordingSupportNotificationProducer{}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions, notifications)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/support/tickets/tkt-1/replies", strings.NewReader(`{"kind":"support_reply","message":"reply"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "reply-1")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if len(notifications.inputs) != 1 || notifications.inputs[0].EventKey != "ticket_reply_received" {
		t.Fatalf("notifications=%v", notifications.inputs)
	}
}

func TestAdminCloseReplayRepairsNotificationThroughP12Dedupe(t *testing.T) {
	ticket, message := adminFixture()
	closedAt := ticket.UpdatedAt.Add(time.Second)
	ticket.Status = TicketClosedStatus
	ticket.ClosedAt = &closedAt
	ticket.UpdatedAt = closedAt
	ticket.Version = 2
	store := &fakeAdminTicketStore{ticket: ticket, message: message, closeChanged: false}
	permissions := &recordingAdminPermissionResolver{allowed: true}
	notifications := &recordingSupportNotificationProducer{}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions, notifications)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/support/tickets/tkt-1", strings.NewReader(`{"action":"close","expected_version":1}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	if len(notifications.inputs) != 1 || notifications.inputs[0].EventKey != "ticket_closed" {
		t.Fatalf("notifications=%v", notifications.inputs)
	}
}

func TestAdminPermissionBackendErrorFailsClosed(t *testing.T) {
	ticket, message := adminFixture()
	store := &fakeAdminTicketStore{ticket: ticket, message: message}
	permissions := &recordingAdminPermissionResolver{allowed: true, err: errors.New("permission backend unavailable")}
	api, err := NewAdminAPI(store, fixedAdminPrincipalResolver{principal: RequestPrincipal{UserID: "admin-1"}}, permissions, &recordingSupportNotificationProducer{})
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	api.Handler().ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/support/tickets", nil))
	if res.Code != http.StatusServiceUnavailable || store.listCalls != 0 {
		t.Fatalf("status=%d store calls=%d", res.Code, store.listCalls)
	}
}
