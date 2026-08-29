package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func loadIdempotency[T any](ctx context.Context, tx *sql.Tx, actorID, action, key string, fingerprint [32]byte) (T, bool, error) {
	var zero T
	keyHash := hashOpaque(strings.TrimSpace(key))
	var storedFingerprint []byte
	var response []byte
	err := tx.QueryRowContext(ctx, `SELECT request_fingerprint,response_json FROM admin_idempotency_records WHERE actor_id=? AND action=? AND idempotency_key_hash=?`, actorID, action, keyHash[:]).Scan(&storedFingerprint, &response)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, err
	}
	if len(storedFingerprint) != 32 || subtleCompare(storedFingerprint, fingerprint[:]) == false {
		return zero, false, ErrReplayMismatch
	}
	if err = json.Unmarshal(response, &zero); err != nil {
		return zero, false, err
	}
	return zero, true, nil
}
func storeIdempotency(ctx context.Context, tx *sql.Tx, actorID, action, key string, fingerprint [32]byte, response any, auditID uint64, now time.Time) error {
	keyHash := hashOpaque(strings.TrimSpace(key))
	raw, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_idempotency_records(actor_id,action,idempotency_key_hash,request_fingerprint,response_json,audit_event_id,created_at) VALUES (?,?,?,?,CAST(? AS JSON),?,?)`, actorID, action, keyHash[:], fingerprint[:], string(raw), auditID, now)
	return err
}
func subtleCompare(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
func mapDuplicate(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "duplicate") || strings.Contains(text, "1062") {
		return ErrConflict
	}
	return err
}

func (s *Service) RotateCSRF(ctx context.Context, p Principal, now time.Time) (string, error) {
	token, err := newOpaque("gac_", 32)
	if err != nil {
		return "", err
	}
	h := hashOpaque(token)
	now = now.UTC().Truncate(time.Microsecond)
	result, err := s.db.ExecContext(ctx, `UPDATE admin_sessions SET csrf_hash=?,updated_at=? WHERE id=? AND administrator_id=? AND status='active'`, h[:], now, p.Session.ID, p.Administrator.ID)
	if err != nil {
		return "", err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if rows != 1 {
		return "", ErrUnauthorized
	}
	return token, nil
}

func (s *Service) Logout(ctx context.Context, p Principal, correlationID string, now time.Time) error {
	if !validCorrelation(correlationID) {
		return ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE admin_sessions SET status='revoked',revoked_at=?,updated_at=? WHERE id=? AND administrator_id=? AND status='active'`, now, now, p.Session.ID, p.Administrator.ID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrUnauthorized
	}
	_, err = recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.auth.logout", ResourceType: "admin_session", ResourceID: p.Session.ID, Result: "success", CorrelationID: correlationID, Before: map[string]any{"session_id": p.Session.ID, "status": "active"}, After: map[string]any{"session_id": p.Session.ID, "status": "revoked"}, CreatedAt: now})
	if err != nil {
		return err
	}
	return tx.Commit()
}
