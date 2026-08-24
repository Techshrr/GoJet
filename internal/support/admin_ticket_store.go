package support

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AdminMessageInput struct {
	TicketID           string
	ActorID            string
	Kind               MessageKind
	Body               string
	CorrelationID      string
	IdempotencyKeyHash [32]byte
}

func (s *Store) ListAdminTickets(ctx context.Context, limit int) ([]Ticket, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,COALESCE(workspace_id,''),COALESCE(requester_user_id,''),COALESCE(public_contact_id,''),category,subject,status,created_at,updated_at,closed_at,version,correlation_id
FROM support_tickets ORDER BY updated_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Ticket
	for rows.Next() {
		var ticket Ticket
		if err := scanTicketRow(rows, &ticket); err != nil {
			return nil, err
		}
		items = append(items, ticket)
	}
	return items, rows.Err()
}

func (s *Store) ListTicketMessages(ctx context.Context, ticketID string, includeInternal bool) ([]TicketMessage, error) {
	if s == nil || s.db == nil || strings.TrimSpace(ticketID) == "" {
		return nil, ErrInvalidInput
	}
	query := `
SELECT id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id
FROM support_ticket_messages WHERE ticket_id=?`
	if !includeInternal {
		query += ` AND kind<>'internal_note'`
	}
	query += ` ORDER BY created_at,id`
	rows, err := s.db.QueryContext(ctx, query, strings.TrimSpace(ticketID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []TicketMessage
	for rows.Next() {
		message, err := scanTicketMessageRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, message)
	}
	return items, rows.Err()
}

func (s *Store) AddAdminMessage(ctx context.Context, input AdminMessageInput) (Ticket, TicketMessage, bool, error) {
	input.TicketID = strings.TrimSpace(input.TicketID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Body = strings.TrimSpace(input.Body)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || input.TicketID == "" || input.ActorID == "" || input.Body == "" || input.CorrelationID == "" || input.IdempotencyKeyHash == ([32]byte{}) {
		return Ticket{}, TicketMessage{}, false, ErrInvalidInput
	}
	if input.Kind != MessageSupportReply && input.Kind != MessageInternalNote {
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
	var existingHash []byte
	existing, err := scanTicketMessageQuery(tx.QueryRowContext(ctx, `
SELECT id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id
FROM support_ticket_messages WHERE ticket_id=? AND idempotency_key_hash=?`, ticket.ID, input.IdempotencyKeyHash[:]), &existingHash)
	if err == nil {
		if existing.ActorType != ActorSupport || existing.Kind != input.Kind || existing.ActorID != input.ActorID {
			return Ticket{}, TicketMessage{}, false, ErrSupportConflict
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
	message := TicketMessage{
		ID: messageID, TicketID: ticket.ID, ActorType: ActorSupport, ActorID: input.ActorID,
		Kind: input.Kind, Body: input.Body, IdempotencyKeyHash: input.IdempotencyKeyHash,
		CreatedAt: now, CorrelationID: input.CorrelationID,
	}
	next, err := ApplyTicketMessage(ticket, message, now)
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE support_tickets SET status=?,updated_at=?,version=? WHERE id=? AND version=?`,
		string(next.Status), next.UpdatedAt, next.Version, ticket.ID, ticket.Version)
	if err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err != nil {
			return Ticket{}, TicketMessage{}, false, err
		}
		return Ticket{}, TicketMessage{}, false, ErrSupportConflict
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO support_ticket_messages
(id,ticket_id,actor_type,actor_id,kind,body,idempotency_key_hash,created_at,correlation_id)
VALUES (?,?,?,?,?,?,?,?,?)`, message.ID, message.TicketID, string(message.ActorType), message.ActorID,
		string(message.Kind), message.Body, message.IdempotencyKeyHash[:], message.CreatedAt, message.CorrelationID); err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	if input.Kind == MessageSupportReply {
		var recipient string
		if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(NULLIF(wm.email,''),NULLIF(pc.email,''),'')
FROM support_tickets t
LEFT JOIN workspace_memberships wm ON wm.workspace_id=t.workspace_id AND wm.user_id=t.requester_user_id
LEFT JOIN support_public_contacts pc ON pc.id=t.public_contact_id
WHERE t.id=?`, ticket.ID).Scan(&recipient); err != nil {
			return Ticket{}, TicketMessage{}, false, err
		}
		if strings.TrimSpace(recipient) == "" {
			return Ticket{}, TicketMessage{}, false, ErrInvalidInput
		}
		if err := enqueueMailTx(ctx, tx, "support-ticket-reply", "en", "requester", recipient, "ticket_message", message.ID, now); err != nil {
			return Ticket{}, TicketMessage{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, TicketMessage{}, false, err
	}
	return next, message, true, nil
}

func (s *Store) CloseAdminTicket(ctx context.Context, ticketID string, expectedVersion uint64) (Ticket, bool, error) {
	if s == nil || s.db == nil || strings.TrimSpace(ticketID) == "" || expectedVersion == 0 {
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
FROM support_tickets WHERE id=? FOR UPDATE`, strings.TrimSpace(ticketID)), &ticket)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, false, ErrSupportNotFound
	}
	if err != nil {
		return Ticket{}, false, err
	}
	if ticket.Status == TicketClosedStatus {
		// Accept the current version and the exact one-version-behind retry that
		// represents replay of the close which advanced the ticket by one. Any
		// older or unrelated stale version remains a conflict.
		if expectedVersion != ticket.Version && (expectedVersion == ^uint64(0) || expectedVersion+1 != ticket.Version) {
			return Ticket{}, false, ErrSupportConflict
		}
		if err := tx.Commit(); err != nil {
			return Ticket{}, false, err
		}
		return ticket, false, nil
	}
	if ticket.Version != expectedVersion {
		return Ticket{}, false, ErrSupportConflict
	}
	next, err := CloseTicket(ticket, time.Now().UTC())
	if err != nil {
		return Ticket{}, false, err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE support_tickets SET status=?,closed_at=?,updated_at=?,version=? WHERE id=? AND version=?`,
		string(next.Status), next.ClosedAt, next.UpdatedAt, next.Version, ticket.ID, ticket.Version)
	if err != nil {
		return Ticket{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		if err != nil {
			return Ticket{}, false, err
		}
		return Ticket{}, false, ErrSupportConflict
	}
	if err := tx.Commit(); err != nil {
		return Ticket{}, false, err
	}
	return next, true, nil
}

func scanTicketMessageRow(row rowScanner) (TicketMessage, error) {
	var rawHash []byte
	return scanTicketMessageQuery(row, &rawHash)
}

func scanTicketMessageQuery(row rowScanner, rawHash *[]byte) (TicketMessage, error) {
	var message TicketMessage
	var actorType, kind string
	if err := row.Scan(&message.ID, &message.TicketID, &actorType, &message.ActorID, &kind, &message.Body,
		rawHash, &message.CreatedAt, &message.CorrelationID); err != nil {
		return TicketMessage{}, err
	}
	if len(*rawHash) != 32 {
		return TicketMessage{}, ErrInvalidInput
	}
	copy(message.IdempotencyKeyHash[:], *rawHash)
	message.ActorType = ActorType(actorType)
	message.Kind = MessageKind(kind)
	if err := message.Validate(); err != nil {
		return TicketMessage{}, err
	}
	return message, nil
}
