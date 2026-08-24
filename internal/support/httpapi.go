package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

var (
	ErrAuthenticationUnavailable = errors.New("support authentication unavailable")
	ErrAuthenticationRequired    = errors.New("support authentication required")
	ErrRateLimited               = errors.New("support rate limited")
)

type RequestPrincipal struct {
	UserID      string
	Email       string
	DisplayName string
}

type PrincipalResolver interface {
	ResolvePrincipal(req *http.Request) (RequestPrincipal, error)
}

type SubmissionRateLimiter interface {
	AllowSubmission(ctx context.Context, surface SubmissionSurface, remoteAddr string) (bool, error)
}

type SupportStore interface {
	CreatePublicContact(ctx context.Context, input CreatePublicContactInput) (Ticket, bool, error)
	CreateWorkspaceTicket(ctx context.Context, input CreateWorkspaceTicketInput) (Ticket, bool, error)
	ListRequesterTickets(ctx context.Context, workspaceID, requesterUserID string, limit int) ([]Ticket, error)
	GetTicket(ctx context.Context, ticketID string) (Ticket, error)
	ReplyRequester(ctx context.Context, input ReplyTicketInput) (Ticket, TicketMessage, bool, error)
	CloseRequesterTicket(ctx context.Context, ticketID, requesterUserID string) (Ticket, bool, error)
}

type API struct {
	store         SupportStore
	memberships   WorkspaceMembershipResolver
	domainProject DomainAccessRequestProjector
	notifications SupportNotificationProducer
	principal     PrincipalResolver
	turnstile     TurnstileVerifier
	replay        TurnstileReplayStore
	rate          SubmissionRateLimiter
}

func NewAPI(store SupportStore, memberships WorkspaceMembershipResolver, domainProject DomainAccessRequestProjector, notifications SupportNotificationProducer, principal PrincipalResolver, turnstile TurnstileVerifier, replay TurnstileReplayStore, rate SubmissionRateLimiter) (*API, error) {
	if store == nil || memberships == nil || domainProject == nil || notifications == nil || principal == nil || turnstile == nil || replay == nil || rate == nil {
		return nil, ErrInvalidInput
	}
	return &API{store: store, memberships: memberships, domainProject: domainProject, notifications: notifications, principal: principal, turnstile: turnstile, replay: replay, rate: rate}, nil
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/public/contact", a.createPublicContact)
	mux.HandleFunc("GET /api/support/tickets", a.listTickets)
	mux.HandleFunc("POST /api/support/tickets", a.createTicket)
	mux.HandleFunc("GET /api/support/tickets/{ticketId}", a.getTicket)
	mux.HandleFunc("POST /api/support/tickets/{ticketId}/replies", a.replyTicket)
	mux.HandleFunc("POST /api/support/tickets/{ticketId}/close", a.closeTicket)
	return supportSecurityHeaders(mux)
}

func supportSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type publicContactRequest struct {
	Email          string `json:"email"`
	Name           string `json:"name"`
	Subject        string `json:"subject"`
	Message        string `json:"message"`
	TurnstileToken string `json:"turnstile_token"`
}

func (a *API) createPublicContact(w http.ResponseWriter, r *http.Request) {
	var input publicContactRequest
	if !decodeSupportJSON(w, r, &input) {
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Message = strings.TrimSpace(input.Message)
	if input.Email == "" || input.Name == "" || input.Subject == "" || input.Message == "" {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid contact submission.")
		return
	}
	if !a.allowSubmission(w, r, SubmissionPublicContact) || !a.verifyTurnstile(w, r, input.TurnstileToken) {
		return
	}
	hash, err := a.idempotencyHash(r, SubmissionPublicContact, "public")
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	ticket, created, err := a.store.CreatePublicContact(r.Context(), CreatePublicContactInput{
		Email: input.Email, Name: input.Name, Subject: input.Subject, Message: input.Message,
		CorrelationID: supportRequestCorrelationID(r), IdempotencyKeyHash: hash,
	})
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeSupportJSON(w, status, map[string]any{"status": "received", "ticket_id": ticket.ID, "created": created, "correlation_id": supportRequestCorrelationID(r)})
}

type ticketCreateRequest struct {
	WorkspaceID    string `json:"workspace_id"`
	Category       string `json:"category"`
	Subject        string `json:"subject"`
	Message        string `json:"message"`
	TurnstileToken string `json:"turnstile_token"`
}

func (a *API) createTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.resolvePrincipal(w, r)
	if !ok {
		return
	}
	var input ticketCreateRequest
	if !decodeSupportJSON(w, r, &input) {
		return
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.Category = strings.TrimSpace(input.Category)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Message = strings.TrimSpace(input.Message)
	if input.WorkspaceID == "" || input.Category == "" || input.Subject == "" || input.Message == "" {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid support request.")
		return
	}
	membership, err := a.memberships.GetMembership(r.Context(), input.WorkspaceID, principal.UserID)
	if err != nil || membership.WorkspaceID != input.WorkspaceID || membership.UserID != principal.UserID {
		writeSupportError(w, r, http.StatusForbidden, "forbidden", "Support access denied.")
		return
	}
	if !a.allowSubmission(w, r, SubmissionTicketCreate) || !a.verifyTurnstile(w, r, input.TurnstileToken) {
		return
	}
	hash, err := a.idempotencyHash(r, SubmissionTicketCreate, input.WorkspaceID+":"+principal.UserID)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	ticket, created, err := a.store.CreateWorkspaceTicket(r.Context(), CreateWorkspaceTicketInput{
		WorkspaceID: input.WorkspaceID, RequesterUserID: principal.UserID, Category: input.Category,
		Subject: input.Subject, Body: input.Message, CorrelationID: supportRequestCorrelationID(r), IdempotencyKeyHash: hash,
	})
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if ticket.Category == CustomDomainAccessCategory {
		if _, err := ProjectTicketDomainAccess(r.Context(), a.domainProject, ticket); err != nil {
			writeSupportStoreError(w, r, err)
			return
		}
	}
	if _, _, err := ProduceSupportNotification(r.Context(), a.notifications, ticket, "ticket_created", strconv.FormatUint(ticket.Version, 10)); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeSupportJSON(w, status, map[string]any{"ticket": ticket, "created": created})
}

func (a *API) listTickets(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.resolvePrincipal(w, r)
	if !ok {
		return
	}
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_workspace", "Workspace is required.")
		return
	}
	membership, err := a.memberships.GetMembership(r.Context(), workspaceID, principal.UserID)
	if err != nil || membership.WorkspaceID != workspaceID || membership.UserID != principal.UserID {
		writeSupportError(w, r, http.StatusForbidden, "forbidden", "Support access denied.")
		return
	}
	tickets, err := a.store.ListRequesterTickets(r.Context(), workspaceID, principal.UserID, 50)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"items": tickets})
}

func (a *API) getTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.resolvePrincipal(w, r)
	if !ok {
		return
	}
	ticket, err := a.store.GetTicket(r.Context(), r.PathValue("ticketId"))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if err := AuthorizeRequesterTicket(r.Context(), a.memberships, ticket, ticket.WorkspaceID, principal.UserID); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"ticket": ticket})
}

type ticketReplyRequest struct {
	Message string `json:"message"`
}

func (a *API) replyTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.resolvePrincipal(w, r)
	if !ok {
		return
	}
	ticket, err := a.store.GetTicket(r.Context(), r.PathValue("ticketId"))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if err := AuthorizeRequesterTicket(r.Context(), a.memberships, ticket, ticket.WorkspaceID, principal.UserID); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	var input ticketReplyRequest
	if !decodeSupportJSON(w, r, &input) {
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	if input.Message == "" {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Reply is required.")
		return
	}
	if !a.allowSubmission(w, r, SubmissionTicketReply) {
		return
	}
	hash, err := a.idempotencyHash(r, SubmissionTicketReply, ticket.ID+":"+principal.UserID)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	next, message, created, err := a.store.ReplyRequester(r.Context(), ReplyTicketInput{TicketID: ticket.ID, ActorID: principal.UserID, Body: input.Message, CorrelationID: supportRequestCorrelationID(r), IdempotencyKeyHash: hash})
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if _, _, err := ProduceSupportNotification(r.Context(), a.notifications, next, "ticket_reply_sent", message.ID); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeSupportJSON(w, status, map[string]any{"ticket": next, "message_id": message.ID, "created": created})
}

func (a *API) closeTicket(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.resolvePrincipal(w, r)
	if !ok {
		return
	}
	ticket, err := a.store.GetTicket(r.Context(), r.PathValue("ticketId"))
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if err := AuthorizeRequesterTicket(r.Context(), a.memberships, ticket, ticket.WorkspaceID, principal.UserID); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	next, changed, err := a.store.CloseRequesterTicket(r.Context(), ticket.ID, principal.UserID)
	if err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	if _, _, err := ProduceSupportNotification(r.Context(), a.notifications, next, "ticket_closed", strconv.FormatUint(next.Version, 10)); err != nil {
		writeSupportStoreError(w, r, err)
		return
	}
	writeSupportJSON(w, http.StatusOK, map[string]any{"ticket": next, "changed": changed})
}

func (a *API) allowSubmission(w http.ResponseWriter, r *http.Request, surface SubmissionSurface) bool {
	allowed, err := a.rate.AllowSubmission(r.Context(), surface, r.RemoteAddr)
	if err != nil || !allowed {
		writeSupportError(w, r, http.StatusTooManyRequests, "rate_limited", "Submission rate limit exceeded.")
		return false
	}
	return true
}

func (a *API) verifyTurnstile(w http.ResponseWriter, r *http.Request, rawToken string) bool {
	if err := VerifyProtectedSubmission(r.Context(), rawToken, a.turnstile, a.replay); err != nil {
		writeSupportError(w, r, http.StatusBadRequest, "turnstile_rejected", "Verification failed.")
		return false
	}
	return true
}

func (a *API) idempotencyHash(r *http.Request, surface SubmissionSurface, scope string) ([32]byte, error) {
	raw := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if raw == "" {
		generated, err := newOpaqueID("idem")
		if err != nil {
			return [32]byte{}, err
		}
		raw = generated
	}
	return SubmissionIdempotencyHash(surface, scope, raw)
}

func (a *API) resolvePrincipal(w http.ResponseWriter, r *http.Request) (RequestPrincipal, bool) {
	principal, err := a.principal.ResolvePrincipal(r)
	if err != nil {
		if errors.Is(err, ErrAuthenticationRequired) {
			writeSupportError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		} else {
			writeSupportError(w, r, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
		}
		return RequestPrincipal{}, false
	}
	if strings.TrimSpace(principal.UserID) == "" {
		writeSupportError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
		return RequestPrincipal{}, false
	}
	return principal, true
}

func decodeSupportJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeSupportError(w, r, http.StatusBadRequest, "invalid_json", "Invalid request body.")
		return false
	}
	return true
}

func writeSupportJSON(w http.ResponseWriter, status int, value any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSupportError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	writeSupportJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": message, "correlation_id": supportRequestCorrelationID(r)}})
}

func writeSupportStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeSupportError(w, r, http.StatusBadRequest, "invalid_request", "Invalid request.")
	case errors.Is(err, ErrTicketClosed):
		writeSupportError(w, r, http.StatusConflict, "ticket_closed", "Ticket is closed.")
	case errors.Is(err, ErrSupportConflict):
		writeSupportError(w, r, http.StatusConflict, "conflict", "Resource changed; retry safely.")
	case errors.Is(err, ErrTicketUnavailable), errors.Is(err, ErrSupportNotFound), errors.Is(err, workspace.ErrNotFound), errors.Is(err, workspace.ErrForbidden):
		writeSupportError(w, r, http.StatusNotFound, "not_found", "Support resource not found.")
	case errors.Is(err, ErrRateLimited):
		writeSupportError(w, r, http.StatusTooManyRequests, "rate_limited", "Submission rate limit exceeded.")
	default:
		writeSupportError(w, r, http.StatusInternalServerError, "server_error", "Request could not be completed.")
	}
}

func supportRequestCorrelationID(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("X-Request-ID"))
	if value != "" && len(value) <= 128 {
		return value
	}
	return fmt.Sprintf("p14-%d", time.Now().UTC().UnixNano())
}
