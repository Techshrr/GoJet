package support

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrSupportNotFound = errors.New("support resource not found")
	ErrSupportConflict = errors.New("support resource conflict")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &Store{db: db}, nil
}

type CreateWorkspaceTicketInput struct {
	WorkspaceID        string
	RequesterUserID    string
	Category           string
	Subject            string
	Body               string
	CorrelationID      string
	IdempotencyKeyHash [32]byte
}

type CreatePublicContactInput struct {
	Email              string
	Name               string
	Subject            string
	Message            string
	CorrelationID      string
	IdempotencyKeyHash [32]byte
}

type ReplyTicketInput struct {
	TicketID           string
	ActorID            string
	Body               string
	CorrelationID      string
	IdempotencyKeyHash [32]byte
}

func (s *Store) CreateWorkspaceTicket(ctx context.Context, input CreateWorkspaceTicketInput) (Ticket, bool, error) {
	if s == nil || s.db == nil || !validCreateTicketInput(input) {
		return Ticket{}, false, ErrInvalidInput
	}
	now := time.Now().UTC()
	ticketID, err := newOpaqueID("tkt")
	if err != nil {
		return Ticket{}, false, err
	}
	messageID, err := newOpaqueID("msg")
	if err != nil {
		return Ticket{}, false, err
	}
	ticket, err := NewWorkspaceTicket(ticketID, input.WorkspaceID, input.RequesterUserID, input.Category, input.Subject, input.CorrelationID, now)
	if err != nil {
		return Ticket{}, false, err
	}
	message := TicketMessage{ID: messageID, TicketID: ticket.ID, ActorType: ActorRequester, ActorID: input.RequesterUserID, Kind: MessageRequesterReply, Body: strings.TrimSpace(input.Body), IdempotencyKeyHash: input.IdempotencyKeyHash, CreatedAt: now, CorrelationID: input.CorrelationID}
	ticket, err = ApplyTicketMessage(ticket, message, now)
	if err != nil {
		return Ticket{}, false, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
INSERT INTO support_tickets
(id,workspace_id,requester_user_id,category,subject,status,idempotency_key_hash,created_at,updated_at,closed_at,version,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
ON DUPLICATE KEY UPDATE id=id`,
		ticket.ID, ticket.WorkspaceID, ticket.RequesterUserID, ticket.Category, ticket.Subject, string(ticket.Status), input.IdempotencyKeyHash[:], ticket.CreatedAt, ticket.UpdatedAt, nil, ticket.Version, ticket.CorrelationID)
	if err != nil {
		return Ticket{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Ticket{}, false, err
	}
	if rows == 0 {
		existing, err := ticketByWorkspaceIdempotencyTx(ctx, tx, input.WorkspaceID, input.IdempotencyKeyHash)
		if err != nil {
			return Ticket{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, false, err
		}
		return existing, false, nil
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO support_ticket_messages
(id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?)`, message.ID, message.TicketID, string(message.ActorType), message.ActorID, string(message.Kind), message.Body, message.IdempotencyKeyHash[:], message.CreatedAt, message.CorrelationID); err != nil {
		return Ticket{}, false, err
	}
	var recipient string
	if err := tx.QueryRowContext(ctx, `
SELECT email FROM workspace_memberships WHERE workspace_id=? AND user_id=?`, ticket.WorkspaceID, ticket.RequesterUserID).Scan(&recipient); err != nil {
		return Ticket{}, false, err
	}
	if err := enqueueMailTx(ctx, tx, "support-ticket-created", "en", "requester", recipient, "ticket", ticket.ID, now); err != nil {
		return Ticket{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, false, err
	}
	return ticket, true, nil
}

func (s *Store) CreatePublicContact(ctx context.Context, input CreatePublicContactInput) (Ticket, bool, error) {
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)
	input.Subject = strings.TrimSpace(input.Subject)
	input.Message = strings.TrimSpace(input.Message)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	parsedAddress, addressErr := mail.ParseAddress(input.Email)
	if s == nil || s.db == nil || addressErr != nil || parsedAddress.Address != input.Email || input.Name == "" || input.Subject == "" || input.Message == "" || input.CorrelationID == "" || input.IdempotencyKeyHash == ([32]byte{}) || len(input.Email) > 320 || len(input.Name) > 160 || len(input.Subject) > 300 {
		return Ticket{}, false, ErrInvalidInput
	}
	now := time.Now().UTC()
	contactID, err := newOpaqueID("contact")
	if err != nil {
		return Ticket{}, false, err
	}
	ticketID, err := newOpaqueID("tkt")
	if err != nil {
		return Ticket{}, false, err
	}
	messageID, err := newOpaqueID("msg")
	if err != nil {
		return Ticket{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
INSERT INTO support_public_contacts
(id,email,name,subject,message,status,idempotency_key_hash,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,?,'new',?,?,?,?)
ON DUPLICATE KEY UPDATE id=id`, contactID, input.Email, input.Name, input.Subject, input.Message, input.IdempotencyKeyHash[:], input.CorrelationID, now, now)
	if err != nil {
		return Ticket{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Ticket{}, false, err
	}
	if rows == 0 {
		var existing Ticket
		err := scanTicketRow(tx.QueryRowContext(ctx, `
SELECT t.id,COALESCE(t.workspace_id,''),COALESCE(t.requester_user_id,''),COALESCE(t.public_contact_id,''),t.category,t.subject,t.status,t.created_at,t.updated_at,t.closed_at,t.version,t.correlation_id
FROM support_tickets t JOIN support_public_contacts pc ON pc.id=t.public_contact_id
WHERE pc.idempotency_key_hash=?`, input.IdempotencyKeyHash[:]), &existing)
		if err != nil {
			return Ticket{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, false, err
		}
		return existing, false, nil
	}
	ticket := Ticket{ID: ticketID, PublicContactID: contactID, Category: "public-contact", Subject: input.Subject, Status: TicketAwaitingSupport, CreatedAt: now, UpdatedAt: now, Version: 2, CorrelationID: input.CorrelationID}
	if err := ticket.Validate(); err != nil {
		return Ticket{}, false, err
	}
	message := TicketMessage{ID: messageID, TicketID: ticket.ID, ActorType: ActorRequester, ActorID: contactID, Kind: MessageRequesterReply, Body: input.Message, IdempotencyKeyHash: input.IdempotencyKeyHash, CreatedAt: now, CorrelationID: input.CorrelationID}
	if err := message.Validate(); err != nil {
		return Ticket{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO support_tickets
(id,public_contact_id,category,subject,status,idempotency_key_hash,created_at,updated_at,version,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?,?)`, ticket.ID, ticket.PublicContactID, ticket.Category, ticket.Subject, string(ticket.Status), input.IdempotencyKeyHash[:], ticket.CreatedAt, ticket.UpdatedAt, ticket.Version, ticket.CorrelationID); err != nil {
		return Ticket{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO support_ticket_messages
(id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?)`, message.ID, message.TicketID, string(message.ActorType), message.ActorID, string(message.Kind), message.Body, message.IdempotencyKeyHash[:], message.CreatedAt, message.CorrelationID); err != nil {
		return Ticket{}, false, err
	}
	if err := enqueueMailTx(ctx, tx, "public-contact-received", "en", "public_contact", input.Email, "public_contact", contactID, now); err != nil {
		return Ticket{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, false, err
	}
	return ticket, true, nil
}

func (s *Store) ListRequesterTickets(ctx context.Context, workspaceID, requesterUserID string, limit int) ([]Ticket, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	requesterUserID = strings.TrimSpace(requesterUserID)
	if s == nil || s.db == nil || workspaceID == "" || requesterUserID == "" {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets WHERE workspace_id=? AND requester_user_id=?
ORDER BY updated_at DESC,id DESC LIMIT ?`, workspaceID, requesterUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tickets []Ticket
	for rows.Next() {
		var ticket Ticket
		if err := scanTicketRow(rows, &ticket); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func (s *Store) GetTicket(ctx context.Context, ticketID string) (Ticket, error) {
	if s == nil || s.db == nil || strings.TrimSpace(ticketID) == "" {
		return Ticket{}, ErrInvalidInput
	}
	var ticket Ticket
	err := scanTicketRow(s.db.QueryRowContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets WHERE id=?`, strings.TrimSpace(ticketID)), &ticket)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, ErrSupportNotFound
	}
	return ticket, err
}

func (s *Store) ReplyRequester(ctx context.Context, input ReplyTicketInput) (Ticket, TicketMessage, bool, error) {
	input.TicketID = strings.TrimSpace(input.TicketID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Body = strings.TrimSpace(input.Body)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || input.TicketID == "" || input.ActorID == "" || input.Body == "" || input.CorrelationID == "" || input.IdempotencyKeyHash == ([32]byte{}) {
		return Ticket{}, TicketMessage{}, false, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	defer tx.Rollback()
	var ticket Ticket
	err = scanTicketRow(tx.QueryRowContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets WHERE id=? FOR UPDATE`, input.TicketID), &ticket)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, TicketMessage{}, false, ErrSupportNotFound
	}
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	if ticket.RequesterUserID != input.ActorID || ticket.WorkspaceID == "" {
		return Ticket{}, TicketMessage{}, false, ErrTicketUnavailable
	}
	var existing TicketMessage
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `
SELECT id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id
FROM support_ticket_messages WHERE ticket_id=? AND idempotency_key_hash=?`, ticket.ID, input.IdempotencyKeyHash[:]).
		Scan(&existing.ID, &existing.TicketID, &existing.ActorType, &existing.ActorID, &existing.Kind, &existing.Body, &existingHash, &existing.CreatedAt, &existing.CorrelationID)
	if err == nil {
		if len(existingHash) != 32 {
			return Ticket{}, TicketMessage{}, false, ErrInvalidInput
		}
		copy(existing.IdempotencyKeyHash[:], existingHash)
		if err := existing.Validate(); err != nil {
			return Ticket{}, TicketMessage{}, false, err
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, TicketMessage{}, false, err
		}
		return ticket, existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, TicketMessage{}, false, err
	}
	messageID, err := newOpaqueID("msg")
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	now := time.Now().UTC()
	message := TicketMessage{ID: messageID, TicketID: ticket.ID, ActorType: ActorRequester, ActorID: input.ActorID, Kind: MessageRequesterReply, Body: input.Body, IdempotencyKeyHash: input.IdempotencyKeyHash, CreatedAt: now, CorrelationID: input.CorrelationID}
	next, err := ApplyTicketMessage(ticket, message, now)
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE support_tickets SET status=?,updated_at=?,version=? WHERE id=? AND version=?`, string(next.Status), next.UpdatedAt, next.Version, ticket.ID, ticket.Version)
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Ticket{}, TicketMessage{}, false, ErrSupportConflict
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO support_ticket_messages
(id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?)`, message.ID, message.TicketID, string(message.ActorType), message.ActorID, string(message.Kind), message.Body, message.IdempotencyKeyHash[:], message.CreatedAt, message.CorrelationID); err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	return next, message, true, nil
}

func (s *Store) CloseRequesterTicket(ctx context.Context, ticketID, requesterUserID string) (Ticket, bool, error) {
	ticketID = strings.TrimSpace(ticketID)
	requesterUserID = strings.TrimSpace(requesterUserID)
	if s == nil || s.db == nil || ticketID == "" || requesterUserID == "" {
		return Ticket{}, false, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, false, err
	}
	defer tx.Rollback()
	var ticket Ticket
	err = scanTicketRow(tx.QueryRowContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets WHERE id=? FOR UPDATE`, ticketID), &ticket)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, false, ErrSupportNotFound
	}
	if err != nil {
		return Ticket{}, false, err
	}
	if ticket.RequesterUserID != requesterUserID || ticket.WorkspaceID == "" {
		return Ticket{}, false, ErrTicketUnavailable
	}
	if ticket.Status == TicketClosedStatus {
		if err := tx.Commit(); err != nil {
			return Ticket{}, false, err
		}
		return ticket, false, nil
	}
	next, err := CloseTicket(ticket, time.Now().UTC())
	if err != nil {
		return Ticket{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE support_tickets SET status=?,closed_at=?,updated_at=?,version=? WHERE id=? AND version=?`, string(next.Status), next.ClosedAt, next.UpdatedAt, next.Version, ticket.ID, ticket.Version)
	if err != nil {
		return Ticket{}, false, err
	}
	rows, _ := result.RowsAffected()
	if rows != 1 {
		return Ticket{}, false, ErrSupportConflict
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, false, err
	}
	return next, true, nil
}

func validCreateTicketInput(input CreateWorkspaceTicketInput) bool {
	return strings.TrimSpace(input.WorkspaceID) != "" && strings.TrimSpace(input.RequesterUserID) != "" && strings.TrimSpace(input.Category) != "" && strings.TrimSpace(input.Subject) != "" && strings.TrimSpace(input.Body) != "" && strings.TrimSpace(input.CorrelationID) != "" && input.IdempotencyKeyHash != ([32]byte{}) && len(strings.TrimSpace(input.Subject)) <= 300
}

func newOpaqueID(prefix string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(raw[:]), nil
}

func enqueueMailTx(ctx context.Context, tx *sql.Tx, templateKey, locale, recipientKind, recipientValue, resourceType, resourceID string, now time.Time) error {
	hash, err := MailLogicalIdempotencyHash(templateKey, 1, recipientKind, resourceType, resourceID)
	if err != nil {
		return err
	}
	jobID, err := newOpaqueID("mail")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO mail_jobs
(id,template_key,template_locale,template_version,recipient_kind,recipient_value,resource_type,resource_id,status,attempt_count,next_attempt_at,idempotency_key_hash,claim_token_hash,claim_expires_at,last_error_code,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,'queued',0,NULL,?,NULL,NULL,NULL,?,?)
ON DUPLICATE KEY UPDATE id=id`, jobID, templateKey, locale, 1, recipientKind, strings.TrimSpace(recipientValue), resourceType, resourceID, hash[:], now.UTC(), now.UTC())
	return err
}

func ticketByWorkspaceIdempotencyTx(ctx context.Context, tx *sql.Tx, workspaceID string, hash [32]byte) (Ticket, error) {
	var ticket Ticket
	err := scanTicketRow(tx.QueryRowContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets WHERE workspace_id=? AND idempotency_key_hash=?`, workspaceID, hash[:]), &ticket)
	return ticket, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTicketRow(row rowScanner, ticket *Ticket) error {
	var status string
	var closed sql.NullTime
	if err := row.Scan(&ticket.ID, &ticket.WorkspaceID, &ticket.RequesterUserID, &ticket.PublicContactID, &ticket.Category, &ticket.Subject, &status, &ticket.CreatedAt, &ticket.UpdatedAt, &closed, &ticket.Version, &ticket.CorrelationID); err != nil {
		return err
	}
	ticket.Status = TicketStatus(status)
	if closed.Valid {
		v := closed.Time.UTC()
		ticket.ClosedAt = &v
	}
	return ticket.Validate()
}
