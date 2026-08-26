package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type SessionSummary struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
	Current    bool      `json:"current"`
}

type AccountService struct {
	db *sql.DB
}

func NewAccountService(db *sql.DB) (*AccountService, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &AccountService{db: db}, nil
}

func (s *AccountService) ListSessions(ctx context.Context, current Session, now time.Time) ([]SessionSummary, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalid
	}
	if err := requireCurrentSessionDB(ctx, s.db, current, now); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,status,expires_at,last_seen_at,created_at
FROM auth_sessions
WHERE user_id=?
ORDER BY created_at DESC,id DESC`, current.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SessionSummary, 0, 4)
	for rows.Next() {
		var item SessionSummary
		if err := rows.Scan(&item.ID, &item.Status, &item.ExpiresAt, &item.LastSeenAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.Current = item.ID == current.ID
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *AccountService) RevokeSession(ctx context.Context, current Session, authority *UnsafeMutationAuthority, targetSessionID, correlationID string, now time.Time) error {
	targetSessionID = strings.TrimSpace(targetSessionID)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !authority.consumeFor(current) || targetSessionID == "" || !validCorrelationID(correlationID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCurrentSessionTx(ctx, tx, current, now); err != nil {
		return err
	}
	when := now.UTC().Truncate(time.Microsecond)
	res, err := tx.ExecContext(ctx, `
UPDATE auth_sessions
SET status='revoked',revoked_at=?,updated_at=?
WHERE id=? AND user_id=? AND status='active'`, when, when, targetSessionID, current.UserID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	if err := recordAccountAuditTx(ctx, tx, current.UserID, "auth.session.revoked", "auth_session", targetSessionID, correlationID, "success", when); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AccountService) UpdateProfile(ctx context.Context, current Session, authority *UnsafeMutationAuthority, displayName, correlationID string, now time.Time) (User, error) {
	displayName = strings.TrimSpace(displayName)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !authority.consumeFor(current) || len(displayName) > 255 || !validCorrelationID(correlationID) {
		return User{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	if err := requireCurrentSessionTx(ctx, tx, current, now); err != nil {
		return User{}, err
	}
	user, err := scanUser(tx.QueryRowContext(ctx, `
SELECT id,email,email_normalized,display_name,status,email_verified_at,password_changed_at,version,created_at,updated_at
FROM auth_users WHERE id=? FOR UPDATE`, current.UserID))
	if err != nil {
		return User{}, err
	}
	when := now.UTC().Truncate(time.Microsecond)
	res, err := tx.ExecContext(ctx, `
UPDATE auth_users SET display_name=?,version=version+1,updated_at=? WHERE id=? AND version=?`,
		displayName, when, user.ID, user.Version)
	if err != nil {
		return User{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return User{}, err
	}
	if n != 1 {
		return User{}, ErrConflict
	}
	if err := recordAccountAuditTx(ctx, tx, current.UserID, "auth.profile.updated", "auth_user", current.UserID, correlationID, "success", when); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	user.DisplayName = displayName
	user.Version++
	user.UpdatedAt = when
	return user, nil
}

func (s *AccountService) ChangePassword(ctx context.Context, current Session, authority *UnsafeMutationAuthority, currentPassword, newPassword, correlationID string, now time.Time) error {
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !authority.consumeFor(current) || !validPassword(currentPassword) || !validPassword(newPassword) || currentPassword == newPassword || !validCorrelationID(correlationID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCurrentSessionTx(ctx, tx, current, now); err != nil {
		return err
	}
	var encoded string
	if err := tx.QueryRowContext(ctx, `SELECT password_hash FROM auth_credentials WHERE user_id=? FOR UPDATE`, current.UserID).Scan(&encoded); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if !verifyPassword(encoded, currentPassword) {
		return ErrUnauthorized
	}
	next, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	when := now.UTC().Truncate(time.Microsecond)
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_credentials
SET password_hash=?,password_algorithm=?,password_version=?,failed_attempts=0,locked_until=NULL,updated_at=?
WHERE user_id=?`, next, passwordAlgorithm, passwordAlgorithmVersion, when, current.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_users SET password_changed_at=?,version=version+1,updated_at=? WHERE id=?`, when, when, current.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE auth_sessions
SET status='revoked',revoked_at=?,updated_at=?
WHERE user_id=? AND id<>? AND status='active'`, when, when, current.UserID, current.ID); err != nil {
		return err
	}
	if err := recordAccountAuditTx(ctx, tx, current.UserID, "auth.password.changed", "auth_user", current.UserID, correlationID, "success", when); err != nil {
		return err
	}
	return tx.Commit()
}

func requireCurrentSessionDB(ctx context.Context, db *sql.DB, current Session, now time.Time) error {
	if db == nil || strings.TrimSpace(current.ID) == "" || strings.TrimSpace(current.UserID) == "" {
		return ErrUnauthorized
	}
	var status string
	var expiresAt time.Time
	err := db.QueryRowContext(ctx, `SELECT status,expires_at FROM auth_sessions WHERE id=? AND user_id=?`, current.ID, current.UserID).Scan(&status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if status == SessionStatusRevoked {
		return ErrRevoked
	}
	if status == SessionStatusExpired || !expiresAt.After(now.UTC()) {
		return ErrExpired
	}
	if status != SessionStatusActive {
		return ErrUnauthorized
	}
	return nil
}

func requireCurrentSessionTx(ctx context.Context, tx *sql.Tx, current Session, now time.Time) error {
	if tx == nil || strings.TrimSpace(current.ID) == "" || strings.TrimSpace(current.UserID) == "" {
		return ErrUnauthorized
	}
	var status string
	var expiresAt time.Time
	err := tx.QueryRowContext(ctx, `SELECT status,expires_at FROM auth_sessions WHERE id=? AND user_id=? FOR UPDATE`, current.ID, current.UserID).Scan(&status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if status == SessionStatusRevoked {
		return ErrRevoked
	}
	if status == SessionStatusExpired || !expiresAt.After(now.UTC()) {
		return ErrExpired
	}
	if status != SessionStatusActive {
		return ErrUnauthorized
	}
	return nil
}

func recordAccountAuditTx(ctx context.Context, tx *sql.Tx, userID, action, resourceType, resourceID, correlationID, result string, now time.Time) error {
	if tx == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(resourceType) == "" || !validCorrelationID(correlationID) || (result != "success" && result != "denied" && result != "conflict" && result != "failed") {
		return ErrInvalid
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('user',?,?,?, ?,?,?,?,JSON_OBJECT(),?)`,
		userID, userID, action, resourceType, resourceID, result, correlationID, now.UTC().Truncate(time.Microsecond))
	return err
}
