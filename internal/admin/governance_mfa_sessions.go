package admin

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *Service) EnrollTOTP(ctx context.Context, p Principal, correlationID string, now time.Time) (string, error) {
	if !validCorrelation(correlationID) {
		return "", ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	secret, err := NewTOTPSecret()
	if err != nil {
		return "", err
	}
	ciphertext, err := s.cipher.Encrypt(secret, "admin-totp:"+p.Administrator.ID)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM admin_totp_credentials WHERE administrator_id=? FOR UPDATE`, p.Administrator.ID).Scan(&state)
	if err == nil && state == "active" {
		return "", ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO admin_totp_credentials(administrator_id,secret_ciphertext,secret_key_id,state,enrolled_at,created_at,updated_at) VALUES (?,?,?,'pending',NULL,?,?)`, p.Administrator.ID, ciphertext, s.cipher.KeyID(), now, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE admin_totp_credentials SET secret_ciphertext=?,secret_key_id=?,state='pending',enrolled_at=NULL,updated_at=? WHERE administrator_id=?`, ciphertext, s.cipher.KeyID(), now, p.Administrator.ID)
	}
	if err != nil {
		return "", err
	}
	_, err = recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.mfa.enroll.start", ResourceType: "administrator", ResourceID: p.Administrator.ID, Result: "success", CorrelationID: correlationID, Before: map[string]any{"mfa_enabled": false}, After: map[string]any{"mfa_enabled": false}, CreatedAt: now})
	if err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *Service) ConfirmTOTP(ctx context.Context, p Principal, code, correlationID string, now time.Time) error {
	if !validCorrelation(correlationID) {
		return ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var ciphertext []byte
	var keyID, state string
	if err = tx.QueryRowContext(ctx, `SELECT secret_ciphertext,secret_key_id,state FROM admin_totp_credentials WHERE administrator_id=? FOR UPDATE`, p.Administrator.ID).Scan(&ciphertext, &keyID, &state); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if state != "pending" {
		return ErrConflict
	}
	secret, err := s.cipher.Decrypt(ciphertext, keyID, "admin-totp:"+p.Administrator.ID)
	if err != nil {
		return err
	}
	if !VerifyTOTP(secret, code, now) {
		return ErrMFAInvalid
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_totp_credentials SET state='active',enrolled_at=?,updated_at=? WHERE administrator_id=? AND state='pending'`, now, now, p.Administrator.ID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_sessions SET mfa_verified_at=?,updated_at=? WHERE id=? AND administrator_id=? AND status='active'`, now, now, p.Session.ID, p.Administrator.ID); err != nil {
		return err
	}
	_, err = recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.mfa.enroll.confirm", ResourceType: "administrator", ResourceID: p.Administrator.ID, Result: "success", CorrelationID: correlationID, Before: map[string]any{"mfa_enabled": false}, After: map[string]any{"mfa_enabled": true}, CreatedAt: now})
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListSessions(ctx context.Context, p Principal, targetAdminID string) ([]Session, error) {
	targetAdminID = strings.TrimSpace(targetAdminID)
	if targetAdminID == "" {
		targetAdminID = p.Administrator.ID
	}
	if targetAdminID != p.Administrator.ID {
		if err := s.Require(p, PermissionAdminsManage); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,administrator_id,status,expires_at,mfa_verified_at,last_seen_at,created_at,revoked_at FROM admin_sessions WHERE administrator_id=? ORDER BY created_at DESC,id DESC`, targetAdminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var item Session
		var mfa, revoked sql.NullTime
		if err := rows.Scan(&item.ID, &item.AdministratorID, &item.Status, &item.ExpiresAt, &mfa, &item.LastSeenAt, &item.CreatedAt, &revoked); err != nil {
			return nil, err
		}
		if mfa.Valid {
			t := mfa.Time.UTC()
			item.MFAVerifiedAt = &t
		}
		if revoked.Valid {
			t := revoked.Time.UTC()
			item.RevokedAt = &t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) RevokeSession(ctx context.Context, p Principal, targetSessionID string, authority MutationAuthority, now time.Time) (Session, bool, error) {
	if err := s.RequireHighRisk(p, PermissionAdminsManage, authority, now); err != nil {
		return Session{}, false, err
	}
	if !validID(targetSessionID, 64) {
		return Session{}, false, ErrInvalid
	}
	fingerprint := sha256.Sum256([]byte(targetSessionID))
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Session{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, err := loadIdempotency[Session](ctx, tx, p.Administrator.ID, "admin.session.revoke", authority.IdempotencyKey, fingerprint); err != nil {
		return Session{}, false, err
	} else if ok {
		return replay, true, nil
	}
	var item Session
	var mfa, revoked sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,administrator_id,status,expires_at,mfa_verified_at,last_seen_at,created_at,revoked_at FROM admin_sessions WHERE id=? FOR UPDATE`, targetSessionID).Scan(&item.ID, &item.AdministratorID, &item.Status, &item.ExpiresAt, &mfa, &item.LastSeenAt, &item.CreatedAt, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, ErrNotFound
	}
	if err != nil {
		return Session{}, false, err
	}
	beforeStatus := item.Status
	if item.Status == "active" {
		if _, err = tx.ExecContext(ctx, `UPDATE admin_sessions SET status='revoked',revoked_at=?,updated_at=? WHERE id=? AND status='active'`, now, now, targetSessionID); err != nil {
			return Session{}, false, err
		}
		item.Status = "revoked"
		item.RevokedAt = &now
	} else if item.Status != "revoked" {
		return Session{}, false, ErrConflict
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.session.revoke", ResourceType: "admin_session", ResourceID: targetSessionID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"target_session_id": targetSessionID, "status": beforeStatus}, After: map[string]any{"target_session_id": targetSessionID, "status": "revoked"}, CreatedAt: now})
	if err != nil {
		return Session{}, false, err
	}
	if err = storeIdempotency(ctx, tx, p.Administrator.ID, "admin.session.revoke", authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return Session{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, false, err
	}
	return item, false, nil
}

func (s *Service) ListAudit(ctx context.Context, p Principal, limit int) ([]AuditEvent, error) {
	if err := s.Require(p, PermissionPlatformRead); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_kind,actor_id,action,resource_type,resource_id,result,request_correlation_id,COALESCE(reason,''),before_json,after_json,metadata_json,created_at FROM admin_audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var before, after, metadata []byte
		if err := rows.Scan(&event.ID, &event.ActorKind, &event.ActorID, &event.Action, &event.ResourceType, &event.ResourceID, &event.Result, &event.RequestID, &event.Reason, &before, &after, &metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(before, &event.Before)
		_ = json.Unmarshal(after, &event.After)
		_ = json.Unmarshal(metadata, &event.Metadata)
		out = append(out, event)
	}
	return out, rows.Err()
}
