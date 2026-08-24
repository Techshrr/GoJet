package support

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const TicketsManagePermission = "tickets.manage"

type AdminPermissionResolver interface {
	HasPermission(ctx context.Context, principal RequestPrincipal, permission string) (bool, error)
}

type AdminTicketStore interface {
	ListAdminTickets(ctx context.Context, limit int) ([]Ticket, error)
	GetTicket(ctx context.Context, ticketID string) (Ticket, error)
	ListTicketMessages(ctx context.Context, ticketID string, includeInternal bool) ([]TicketMessage, error)
	AddAdminMessage(ctx context.Context, input AdminMessageInput) (Ticket, TicketMessage, bool, error)
	CloseAdminTicket(ctx context.Context, ticketID string, expectedVersion uint64) (Ticket, bool, error)
}

type AdminAPI struct {
	store         AdminTicketStore
	principals    PrincipalResolver
	permissions   AdminPermissionResolver
	notifications SupportNotificationProducer
}

func NewAdminAPI(store AdminTicketStore, principals PrincipalResolver, permissions AdminPermissionResolver, notifications SupportNotificationProducer) (*AdminAPI, error) {
	if store == nil || principals == nil || notifications == nil {
		return nil, ErrInvalidInput
	}
	return &AdminAPI{store: store, principals: principals, permissions: permissions, notifications: notifications}, nil
}

func (a *AdminAPI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/admin/support/tickets", a.listTickets)
	mux.HandleFunc("GET /api/admin/support/tickets/{ticketId}", a.getTicket)
	mux.HandleFunc("POST /api/admin/support/tickets/{ticketId}/replies", a.replyTicket)
	mux.HandleFunc("PATCH /api/admin/support/tickets/{ticketId}", a.patchTicket)
	return supportSecurityHeaders(mux)
}

func (a *AdminAPI) listTickets(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r, TicketsManagePermission); !ok {
		return
	}
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeSupportError(w, r, http.StatusBadRequest, "invalid_limit", "Invalid list limit.")
			return
		}
		limit = parsed
	}
	items, err := a.store.ListAdminTickets(r.Context(), limit)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *AdminAPI) getTicket(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.adminActor(w, r, TicketsManagePermission); !ok {
		return
	}
	ticket, err := a.store.GetTicket(r.Context(), r.PathValue("ticketId"))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	messages, err := a.store.ListTicketMessages(r.Context(), ticket.ID, true)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"ticket": ticket, "messages": messages})
}

type adminReplyRequest struct {
	Kind    MessageKind `json:"kind"`
	Message string      `json:"message"`
}

func (a *AdminAPI) replyTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r, TicketsManagePermission)
	if !ok {
		return
	}
	var body adminReplyRequest
	if !decodeSupportJSON(w, r, &body) {
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" || (body.Kind != MessageSupportReply && body.Kind != MessageInternalNote) {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid support reply.")
		return
	}
	hash, err := adminOperationIdempotencyHash("ticket-message", r.PathValue("ticketId"), principal.UserID, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	storeCtx := WithSupportAuditActor(r.Context(), principal.UserID)
	ticket, message, created, err := a.store.AddAdminMessage(storeCtx, AdminMessageInput{
		TicketID: r.PathValue("ticketId"), ActorID: principal.UserID, Kind: body.Kind, Body: body.Message,
		CorrelationID: supportRequestCorrelationID(r), IdempotencyKeyHash: hash,
	})
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if body.Kind == MessageSupportReply {
		// Run the P12 dedupe producer on both first-write and idempotent replay.
		// If the first notification attempt failed after the durable message/mail
		// transaction committed, replay repairs the notification without creating
		// a second support message or mail job.
		if _, _, err := ProduceSupportNotification(r.Context(), a.notifications, ticket, "ticket_reply_received", message.ID); err != nil {
			writeSupportStoreError(w, r, err)
			return
		}
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeSupportJSON(w, status, map[string]any{"ticket": ticket, "message": message, "created": created})
}

type adminPatchTicketRequest struct {
	Action          string `json:"action"`
	ExpectedVersion uint64 `json:"expected_version"`
}

func (a *AdminAPI) patchTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.adminActor(w, r, TicketsManagePermission)
	if !ok {
		return
	}
	var body adminPatchTicketRequest
	if !decodeSupportJSON(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Action) != "close" || body.ExpectedVersion == 0 {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid ticket mutation.")
		return
	}
	storeCtx := WithSupportAuditActor(r.Context(), principal.UserID)
	ticket, changed, err := a.store.CloseAdminTicket(storeCtx, r.PathValue("ticketId"), body.ExpectedVersion)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	// As with replies, run the dedupe producer on an idempotent close replay so
	// a transient P12 producer failure cannot permanently suppress the event.
	if _, _, err := ProduceSupportNotification(r.Context(), a.notifications, ticket, "ticket_closed", strconv.FormatUint(ticket.Version, 10)); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"ticket": ticket, "changed": changed})
}

func (a *AdminAPI) adminActor(w http.ResponseWriter, r *http.Request, permission string) (RequestPrincipal, bool) {
	if a == nil || a.principals == nil || a.permissions == nil {
		writeSupportError(w, r, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	principal, err := a.principals.ResolvePrincipal(r)
	if err != nil || strings.TrimSpace(principal.UserID) == "" {
		if errors.Is(err, ErrAuthenticationUnavailable) {
			writeSupportError(w, r, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		} else {
			writeSupportError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		}
		return RequestPrincipal{}, false
	}
	allowed, err := a.permissions.HasPermission(r.Context(), principal, permission)
	if err != nil {
		writeSupportError(w, r, http.StatusServiceUnavailable, "admin_permission_unavailable", "Admin permission authority is unavailable.")
		return RequestPrincipal{}, false
	}
	if !allowed {
		writeSupportError(w, r, http.StatusForbidden, "forbidden", "Admin support permission denied.")
		return RequestPrincipal{}, false
	}
	return principal, true
}

func adminOperationIdempotencyHash(operation, resourceID, actorID, rawKey string) ([32]byte, error) {
	operation = strings.TrimSpace(operation)
	resourceID = strings.TrimSpace(resourceID)
	actorID = strings.TrimSpace(actorID)
	rawKey = strings.TrimSpace(rawKey)
	if operation == "" || resourceID == "" || actorID == "" {
		return [32]byte{}, ErrInvalidInput
	}
	if rawKey == "" {
		generated, err := newOpaqueID("idem")
		if err != nil {
			return [32]byte{}, err
		}
		rawKey = generated
	}
	if len(rawKey) > 512 {
		return [32]byte{}, ErrInvalidInput
	}
	h := sha256.New()
	writeHashPart(h, "gojet:p14:admin-idempotency:v1")
	writeHashPart(h, operation)
	writeHashPart(h, resourceID)
	writeHashPart(h, actorID)
	writeHashPart(h, rawKey)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
