package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

type SupportAuditResult string

const (
	AuditSuccess  SupportAuditResult = "success"
	AuditDenied   SupportAuditResult = "denied"
	AuditConflict SupportAuditResult = "conflict"
	AuditFailed   SupportAuditResult = "failed"
)

type SupportAuditInput struct {
	WorkspaceID   string
	ActorID       string
	Action        string
	ResourceType  string
	ResourceID    string
	CorrelationID string
	Result        SupportAuditResult
	Metadata      map[string]string
}

type SupportAuditWriter interface {
	RecordSupportAudit(ctx context.Context, input SupportAuditInput) error
}

type supportAuditExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var supportAuditASCIIPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]*$`)

var supportAuditMetadataAllowlist = map[string]struct{}{
	"status":          {},
	"previous_status": {},
	"message_kind":    {},
	"scan_status":     {},
	"template_key":    {},
	"attempt_number":  {},
	"error_code":      {},
	"domain_status":   {},
	"domain_source":   {},
	"grant_authority": {},
	"enabled":         {},
	"version":         {},
	"created":         {},
}

func (s *Store) RecordSupportAudit(ctx context.Context, input SupportAuditInput) error {
	if s == nil || s.db == nil {
		return ErrInvalidInput
	}
	return recordSupportAuditExecer(ctx, s.db, input)
}

func (s *MySQLMailStore) RecordSupportAudit(ctx context.Context, input SupportAuditInput) error {
	if s == nil || s.db == nil {
		return ErrInvalidInput
	}
	return recordSupportAuditExecer(ctx, s.db, input)
}

func recordSupportAuditExecer(ctx context.Context, execer supportAuditExecer, input SupportAuditInput) error {
	if execer == nil {
		return ErrInvalidInput
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Action = strings.TrimSpace(input.Action)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if input.ActorID == "" || len(input.ActorID) > 128 || !validAuditASCII(input.Action, 96) ||
		!validAuditASCII(input.ResourceType, 64) || !validAuditASCII(input.ResourceID, 128) ||
		!validAuditASCII(input.CorrelationID, 128) {
		return ErrInvalidInput
	}
	if input.WorkspaceID != "" && !validAuditASCII(input.WorkspaceID, 64) {
		return ErrInvalidInput
	}
	switch input.Result {
	case AuditSuccess, AuditDenied, AuditConflict, AuditFailed:
	default:
		return ErrInvalidInput
	}
	metadata := make(map[string]string, len(input.Metadata))
	for key, value := range input.Metadata {
		if _, ok := supportAuditMetadataAllowlist[key]; !ok {
			return ErrInvalidInput
		}
		value = strings.TrimSpace(value)
		if len(value) > 160 || strings.ContainsAny(value, "\r\n\x00") {
			return ErrInvalidInput
		}
		metadata[key] = value
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var workspaceValue any
	if input.WorkspaceID != "" {
		workspaceValue = input.WorkspaceID
	}
	_, err = execer.ExecContext(ctx, `
INSERT INTO support_audit_events
(workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json,created_at)
VALUES (?,?,?,?,?,NULL,?,?,?,CURRENT_TIMESTAMP(6))
ON DUPLICATE KEY UPDATE id=id`,
		workspaceValue, input.ActorID, input.Action, input.ResourceType, input.ResourceID,
		input.CorrelationID, string(input.Result), encoded)
	return err
}

func validAuditASCII(value string, max int) bool {
	return value != "" && len(value) <= max && supportAuditASCIIPattern.MatchString(value)
}

type supportCorrelationContextKey struct{}
type supportAuditActorContextKey struct{}

func WithSupportCorrelation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlation := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validAuditASCII(correlation, 128) {
			generated, err := newOpaqueID("req")
			if err != nil {
				writeSupportJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "server_error", "message": "Request could not be completed."}})
				return
			}
			correlation = generated
		}
		clone := r.Clone(context.WithValue(r.Context(), supportCorrelationContextKey{}, correlation))
		clone.Header = r.Header.Clone()
		clone.Header.Set("X-Request-ID", correlation)
		next.ServeHTTP(w, clone)
	})
}

func SupportAuditCorrelation(ctx context.Context) string {
	value, _ := ctx.Value(supportCorrelationContextKey{}).(string)
	return strings.TrimSpace(value)
}

func WithSupportAuditActor(ctx context.Context, actorID string) context.Context {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return ctx
	}
	return context.WithValue(ctx, supportAuditActorContextKey{}, actorID)
}

func SupportAuditActor(ctx context.Context) string {
	value, _ := ctx.Value(supportAuditActorContextKey{}).(string)
	return strings.TrimSpace(value)
}

func auditCorrelation(ctx context.Context, fallback string) string {
	if value := SupportAuditCorrelation(ctx); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

type AuditedSupportStore struct {
	inner SupportStore
	audit SupportAuditWriter
}

func NewAuditedSupportStore(inner SupportStore, audit SupportAuditWriter) (*AuditedSupportStore, error) {
	if inner == nil || audit == nil {
		return nil, ErrInvalidInput
	}
	return &AuditedSupportStore{inner: inner, audit: audit}, nil
}

func (s *AuditedSupportStore) CreatePublicContact(ctx context.Context, input CreatePublicContactInput) (Ticket, bool, error) {
	ticket, created, err := s.inner.CreatePublicContact(ctx, input)
	if err != nil {
		return ticket, created, err
	}
	actor := ticket.PublicContactID
	if actor == "" {
		actor = "public-contact"
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		ActorID: actor, Action: "ticket_created", ResourceType: "ticket", ResourceID: ticket.ID,
		CorrelationID: auditCorrelation(ctx, input.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "created": strconv.FormatBool(created)},
	})
	return ticket, created, err
}

func (s *AuditedSupportStore) CreateWorkspaceTicket(ctx context.Context, input CreateWorkspaceTicketInput) (Ticket, bool, error) {
	ticket, created, err := s.inner.CreateWorkspaceTicket(ctx, input)
	if err != nil {
		return ticket, created, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: input.RequesterUserID, Action: "ticket_created", ResourceType: "ticket", ResourceID: ticket.ID,
		CorrelationID: auditCorrelation(ctx, input.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "created": strconv.FormatBool(created)},
	})
	return ticket, created, err
}

func (s *AuditedSupportStore) ListRequesterTickets(ctx context.Context, workspaceID, requesterUserID string, limit int) ([]Ticket, error) {
	return s.inner.ListRequesterTickets(ctx, workspaceID, requesterUserID, limit)
}

func (s *AuditedSupportStore) GetTicket(ctx context.Context, ticketID string) (Ticket, error) {
	return s.inner.GetTicket(ctx, ticketID)
}

func (s *AuditedSupportStore) ReplyRequester(ctx context.Context, input ReplyTicketInput) (Ticket, TicketMessage, bool, error) {
	ticket, message, created, err := s.inner.ReplyRequester(ctx, input)
	if err != nil {
		return ticket, message, created, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: input.ActorID, Action: "ticket_requester_reply", ResourceType: "ticket_message", ResourceID: message.ID,
		CorrelationID: auditCorrelation(ctx, input.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "message_kind": string(message.Kind), "created": strconv.FormatBool(created)},
	})
	return ticket, message, created, err
}

func (s *AuditedSupportStore) CloseRequesterTicket(ctx context.Context, ticketID, requesterUserID string) (Ticket, bool, error) {
	ticket, changed, err := s.inner.CloseRequesterTicket(ctx, ticketID, requesterUserID)
	if err != nil {
		return ticket, changed, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: requesterUserID, Action: "ticket_closed", ResourceType: "ticket", ResourceID: ticket.ID,
		CorrelationID: auditCorrelation(ctx, ticket.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "version": strconv.FormatUint(ticket.Version, 10), "created": strconv.FormatBool(changed)},
	})
	return ticket, changed, err
}

type AuditedAdminTicketStore struct {
	inner AdminTicketStore
	audit SupportAuditWriter
}

func NewAuditedAdminTicketStore(inner AdminTicketStore, audit SupportAuditWriter) (*AuditedAdminTicketStore, error) {
	if inner == nil || audit == nil {
		return nil, ErrInvalidInput
	}
	return &AuditedAdminTicketStore{inner: inner, audit: audit}, nil
}

func (s *AuditedAdminTicketStore) ListAdminTickets(ctx context.Context, limit int) ([]Ticket, error) {
	return s.inner.ListAdminTickets(ctx, limit)
}
func (s *AuditedAdminTicketStore) GetTicket(ctx context.Context, ticketID string) (Ticket, error) {
	return s.inner.GetTicket(ctx, ticketID)
}
func (s *AuditedAdminTicketStore) ListTicketMessages(ctx context.Context, ticketID string, includeInternal bool) ([]TicketMessage, error) {
	return s.inner.ListTicketMessages(ctx, ticketID, includeInternal)
}
func (s *AuditedAdminTicketStore) AddAdminMessage(ctx context.Context, input AdminMessageInput) (Ticket, TicketMessage, bool, error) {
	ticket, message, created, err := s.inner.AddAdminMessage(ctx, input)
	if err != nil {
		return ticket, message, created, err
	}
	action := "admin_ticket_support_reply"
	if message.Kind == MessageInternalNote {
		action = "admin_ticket_internal_note"
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: input.ActorID, Action: action, ResourceType: "ticket_message", ResourceID: message.ID,
		CorrelationID: auditCorrelation(ctx, input.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "message_kind": string(message.Kind), "created": strconv.FormatBool(created)},
	})
	return ticket, message, created, err
}
func (s *AuditedAdminTicketStore) CloseAdminTicket(ctx context.Context, ticketID string, expectedVersion uint64) (Ticket, bool, error) {
	ticket, changed, err := s.inner.CloseAdminTicket(ctx, ticketID, expectedVersion)
	if err != nil {
		return ticket, changed, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: SupportAuditActor(ctx), Action: "admin_ticket_closed", ResourceType: "ticket", ResourceID: ticket.ID,
		CorrelationID: auditCorrelation(ctx, ticket.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"status": string(ticket.Status), "version": strconv.FormatUint(ticket.Version, 10), "created": strconv.FormatBool(changed)},
	})
	return ticket, changed, err
}

type AuditedAdminMailStore struct {
	inner AdminMailStore
	audit SupportAuditWriter
}

func NewAuditedAdminMailStore(inner AdminMailStore, audit SupportAuditWriter) (*AuditedAdminMailStore, error) {
	if inner == nil || audit == nil {
		return nil, ErrInvalidInput
	}
	return &AuditedAdminMailStore{inner: inner, audit: audit}, nil
}

func (s *AuditedAdminMailStore) ListAdminMailQueue(ctx context.Context, limit int) ([]AdminMailQueueItem, error) {
	return s.inner.ListAdminMailQueue(ctx, limit)
}
func (s *AuditedAdminMailStore) ListAdminMailTemplates(ctx context.Context) ([]AdminMailTemplateView, error) {
	return s.inner.ListAdminMailTemplates(ctx)
}
func (s *AuditedAdminMailStore) GetAdminMailSettings(ctx context.Context) (AdminMailSettings, error) {
	return s.inner.GetAdminMailSettings(ctx)
}
func (s *AuditedAdminMailStore) UpdateAdminMailSettings(ctx context.Context, expectedVersion uint64, enabled bool) (AdminMailSettings, error) {
	settings, err := s.inner.UpdateAdminMailSettings(ctx, expectedVersion, enabled)
	if err != nil {
		return settings, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		ActorID: SupportAuditActor(ctx), Action: "admin_mail_settings_updated", ResourceType: "mail_settings", ResourceID: "primary",
		CorrelationID: SupportAuditCorrelation(ctx), Result: AuditSuccess,
		Metadata: map[string]string{"enabled": strconv.FormatBool(settings.Enabled), "version": strconv.FormatUint(settings.Version, 10)},
	})
	return settings, err
}
func (s *AuditedAdminMailStore) EnqueueAdminTestMail(ctx context.Context, input AdminMailTestInput) (AdminMailQueueItem, bool, error) {
	job, created, err := s.inner.EnqueueAdminTestMail(ctx, input)
	if err != nil {
		return job, created, err
	}
	err = s.audit.RecordSupportAudit(ctx, SupportAuditInput{
		ActorID: input.ActorID, Action: "admin_mail_test_queued", ResourceType: "mail_job", ResourceID: job.ID,
		CorrelationID: auditCorrelation(ctx, input.CorrelationID), Result: AuditSuccess,
		Metadata: map[string]string{"template_key": job.TemplateKey, "status": string(job.Status), "created": strconv.FormatBool(created)},
	})
	return job, created, err
}

type DomainRequestAuditRecorder interface {
	RecordDomainRequestAudit(ctx context.Context, ticket Ticket, request domains.AccessRequest) error
}

type AuditedDomainAccessProjector struct {
	inner DomainAccessRequestProjector
	audit SupportAuditWriter
}

func NewAuditedDomainAccessProjector(inner DomainAccessRequestProjector, audit SupportAuditWriter) (*AuditedDomainAccessProjector, error) {
	if inner == nil || audit == nil {
		return nil, ErrInvalidInput
	}
	return &AuditedDomainAccessProjector{inner: inner, audit: audit}, nil
}

func (p *AuditedDomainAccessProjector) ResolveEntitlement(ctx context.Context, workspaceID string, now time.Time) (domains.ResolvedEntitlement, error) {
	return p.inner.ResolveEntitlement(ctx, workspaceID, now)
}
func (p *AuditedDomainAccessProjector) ProjectAccessRequest(ctx context.Context, input domains.AccessRequestInput) (domains.AccessRequest, error) {
	return p.inner.ProjectAccessRequest(ctx, input)
}
func (p *AuditedDomainAccessProjector) RecordDomainRequestAudit(ctx context.Context, ticket Ticket, request domains.AccessRequest) error {
	if request.WorkspaceID != ticket.WorkspaceID || request.SupportTicketID != ticket.ID || request.SubmittedAt.IsZero() {
		return ErrInvalidInput
	}
	return p.audit.RecordSupportAudit(ctx, SupportAuditInput{
		WorkspaceID: ticket.WorkspaceID, ActorID: ticket.RequesterUserID, Action: "domain_request_linked", ResourceType: "domain_request", ResourceID: request.SupportTicketID,
		CorrelationID: ticket.CorrelationID, Result: AuditSuccess,
		Metadata: map[string]string{"domain_status": "requested", "domain_source": "none", "grant_authority": "NONE"},
	})
}
