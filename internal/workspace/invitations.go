package workspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type CreatedInvitation struct {
	Invitation Invitation `json:"invitation"`
	Token      string     `json:"token"`
}

func (s *Store) ListInvitations(ctx context.Context, workspaceID string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,email,role,
       CASE WHEN status='pending' AND expires_at<=CURRENT_TIMESTAMP(6) THEN 'expired' ELSE status END,
       expires_at,invited_by,accepted_by,created_at,updated_at
FROM workspace_invitations
WHERE workspace_id=?
ORDER BY created_at DESC,id DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var item Invitation
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Email, &item.Role, &item.Status,
			&item.ExpiresAt, &item.InvitedBy, &item.AcceptedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateInvitation(ctx context.Context, workspaceID string, principal Principal, role string, expiry time.Time) (CreatedInvitation, error) {
	email := normalizeEmail(principal.Email)
	if email == "" || !validInvitationRole(role) || !expiry.After(time.Now().UTC()) {
		return CreatedInvitation{}, ErrInvalid
	}
	// This method intentionally interprets principal.Email as the invited address only
	// for direct store callers. HTTP uses CreateInvitationForEmail to keep actor/invitee separate.
	return s.CreateInvitationForEmail(ctx, workspaceID, principal.UserID, email, role, expiry)
}

func (s *Store) CreateInvitationForEmail(ctx context.Context, workspaceID, actorID, invitedEmail, role string, expiry time.Time) (CreatedInvitation, error) {
	invitedEmail = normalizeEmail(invitedEmail)
	if workspaceID == "" || actorID == "" || invitedEmail == "" || len(invitedEmail) > 320 ||
		!validInvitationRole(role) || !expiry.After(time.Now().UTC()) {
		return CreatedInvitation{}, ErrInvalid
	}
	token, tokenHash, err := newInvitationToken()
	if err != nil {
		return CreatedInvitation{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CreatedInvitation{}, err
	}
	defer tx.Rollback()
	if _, err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return CreatedInvitation{}, err
	}
	var existing uint64
	err = tx.QueryRowContext(ctx, `
SELECT id FROM workspace_invitations
WHERE workspace_id=? AND email_normalized=? AND status='pending' AND expires_at>CURRENT_TIMESTAMP(6)
LIMIT 1 FOR UPDATE`, workspaceID, invitedEmail).Scan(&existing)
	if err == nil {
		return CreatedInvitation{}, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreatedInvitation{}, err
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO workspace_invitations
(workspace_id,email,email_normalized,role,status,token_hash,expires_at,invited_by)
VALUES (?,?,?,?, 'pending',?,?,?)`,
		workspaceID, invitedEmail, invitedEmail, role, tokenHash, expiry.UTC(), actorID)
	if err != nil {
		return CreatedInvitation{}, wrapConflict(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return CreatedInvitation{}, err
	}
	if err := tx.Commit(); err != nil {
		return CreatedInvitation{}, err
	}
	item, err := s.invitationByID(ctx, workspaceID, uint64(id))
	if err != nil {
		return CreatedInvitation{}, err
	}
	return CreatedInvitation{Invitation: item, Token: token}, nil
}

func (s *Store) RevokeInvitation(ctx context.Context, workspaceID string, invitationID uint64) error {
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_invitations
SET status='revoked'
WHERE id=? AND workspace_id=? AND status='pending' AND expires_at>CURRENT_TIMESTAMP(6)`,
		invitationID, workspaceID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		item, err := s.invitationByID(ctx, workspaceID, invitationID)
		if err != nil {
			return err
		}
		if item.Status == "pending" && expired(time.Now().UTC(), item.ExpiresAt) {
			_, _ = s.db.ExecContext(ctx, `UPDATE workspace_invitations SET status='expired' WHERE id=? AND status='pending'`, invitationID)
			return ErrInviteExpired
		}
		return ErrInviteState
	}
	return nil
}

func (s *Store) InspectInvitation(ctx context.Context, token, accountEmail string) (InvitationInspection, error) {
	token = strings.TrimSpace(token)
	accountEmail = normalizeEmail(accountEmail)
	if token == "" || accountEmail == "" {
		return InvitationInspection{}, ErrInvalid
	}
	var out InvitationInspection
	var emailNormalized string
	err := s.db.QueryRowContext(ctx, `
SELECT i.workspace_id,w.name,i.role,
       CASE WHEN i.status='pending' AND i.expires_at<=CURRENT_TIMESTAMP(6) THEN 'expired' ELSE i.status END,
       i.expires_at,i.email_normalized
FROM workspace_invitations i
JOIN workspaces w ON w.id=i.workspace_id
WHERE i.token_hash=?`, hashInvitationToken(token)).
		Scan(&out.WorkspaceID, &out.WorkspaceName, &out.Role, &out.Status, &out.ExpiresAt, &emailNormalized)
	if errors.Is(err, sql.ErrNoRows) {
		return InvitationInspection{}, ErrNotFound
	}
	if err != nil {
		return InvitationInspection{}, err
	}
	out.AccountMatch = emailNormalized == accountEmail
	return out, nil
}

func (s *Store) AcceptInvitation(ctx context.Context, token string, principal Principal) (Membership, error) {
	return s.consumeInvitation(ctx, token, principal, "accepted")
}

func (s *Store) RejectInvitation(ctx context.Context, token string, principal Principal) error {
	_, err := s.consumeInvitation(ctx, token, principal, "rejected")
	return err
}

func (s *Store) consumeInvitation(ctx context.Context, token string, principal Principal, target string) (Membership, error) {
	token = strings.TrimSpace(token)
	email := normalizeEmail(principal.Email)
	if token == "" || principal.UserID == "" || email == "" || (target != "accepted" && target != "rejected") {
		return Membership{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Membership{}, err
	}
	defer tx.Rollback()

	var item Invitation
	var emailNormalized string
	err = tx.QueryRowContext(ctx, `
SELECT id,workspace_id,email,role,status,expires_at,invited_by,accepted_by,created_at,updated_at,email_normalized
FROM workspace_invitations
WHERE token_hash=? FOR UPDATE`, hashInvitationToken(token)).
		Scan(&item.ID, &item.WorkspaceID, &item.Email, &item.Role, &item.Status, &item.ExpiresAt,
			&item.InvitedBy, &item.AcceptedBy, &item.CreatedAt, &item.UpdatedAt, &emailNormalized)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	if err != nil {
		return Membership{}, err
	}
	if item.Status != "pending" {
		return Membership{}, ErrInviteState
	}
	if expired(time.Now().UTC(), item.ExpiresAt) {
		if _, err := tx.ExecContext(ctx, `UPDATE workspace_invitations SET status='expired' WHERE id=?`, item.ID); err != nil {
			return Membership{}, err
		}
		if err := tx.Commit(); err != nil {
			return Membership{}, err
		}
		return Membership{}, ErrInviteExpired
	}
	if emailNormalized != email {
		return Membership{}, ErrAccountMatch
	}
	if target == "rejected" {
		if _, err := tx.ExecContext(ctx, `
UPDATE workspace_invitations SET status='rejected',accepted_by=NULL WHERE id=? AND status='pending'`, item.ID); err != nil {
			return Membership{}, err
		}
		if err := tx.Commit(); err != nil {
			return Membership{}, err
		}
		return Membership{}, nil
	}
	if _, err := lockWorkspace(ctx, tx, item.WorkspaceID); err != nil {
		return Membership{}, err
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role)
VALUES (?,?,?,?,?)`,
		item.WorkspaceID, principal.UserID, email, strings.TrimSpace(principal.DisplayName), item.Role)
	if err != nil {
		return Membership{}, wrapConflict(err)
	}
	memberID, err := res.LastInsertId()
	if err != nil {
		return Membership{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE workspace_invitations SET status='accepted',accepted_by=? WHERE id=? AND status='pending'`,
		principal.UserID, item.ID); err != nil {
		return Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Membership{}, err
	}
	m, err := s.membershipByID(ctx, item.WorkspaceID, uint64(memberID))
	if err != nil {
		return Membership{}, err
	}
	return m, nil
}

func (s *Store) invitationByID(ctx context.Context, workspaceID string, invitationID uint64) (Invitation, error) {
	var item Invitation
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,email,role,
       CASE WHEN status='pending' AND expires_at<=CURRENT_TIMESTAMP(6) THEN 'expired' ELSE status END,
       expires_at,invited_by,accepted_by,created_at,updated_at
FROM workspace_invitations WHERE workspace_id=? AND id=?`, workspaceID, invitationID).
		Scan(&item.ID, &item.WorkspaceID, &item.Email, &item.Role, &item.Status, &item.ExpiresAt,
			&item.InvitedBy, &item.AcceptedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Invitation{}, ErrNotFound
	}
	return item, err
}
