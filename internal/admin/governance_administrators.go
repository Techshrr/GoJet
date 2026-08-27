package admin

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"
)

func (s *Service) ListAdministrators(ctx context.Context, p Principal) ([]Administrator, error) {
	if err := s.Require(p, PermissionAdminsManage); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.email,a.display_name,a.status,a.version,a.created_at,a.updated_at,EXISTS(SELECT 1 FROM admin_totp_credentials t WHERE t.administrator_id=a.id AND t.state='active') FROM admin_administrators a ORDER BY a.created_at,a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Administrator
	for rows.Next() {
		var a Administrator
		if err := rows.Scan(&a.ID, &a.Email, &a.DisplayName, &a.Status, &a.Version, &a.CreatedAt, &a.UpdatedAt, &a.MFAEnabled); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) CreateAdministrator(ctx context.Context, p Principal, input CreateAdministratorInput, authority MutationAuthority, now time.Time) (Administrator, bool, error) {
	if err := s.RequireHighRisk(p, PermissionAdminsManage, authority, now); err != nil {
		return Administrator{}, false, err
	}
	normalized, err := normalizeEmail(input.Email)
	if err != nil || !validPassword(input.Password) {
		return Administrator{}, false, ErrInvalid
	}
	roles := append([]string(nil), input.RoleIDs...)
	sort.Strings(roles)
	for _, id := range roles {
		if !validID(id, 64) {
			return Administrator{}, false, ErrInvalid
		}
	}
	fingerprint, err := requestFingerprint(struct {
		Email       string
		DisplayName string
		Roles       []string
	}{normalized, strings.TrimSpace(input.DisplayName), roles})
	if err != nil {
		return Administrator{}, false, err
	}
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return Administrator{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Administrator{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, err := loadIdempotency[Administrator](ctx, tx, p.Administrator.ID, "admin.administrator.create", authority.IdempotencyKey, fingerprint); err != nil {
		return Administrator{}, false, err
	} else if ok {
		return replay, true, nil
	}
	for _, roleID := range roles {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT 1 FROM admin_roles WHERE id=?`, roleID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return Administrator{}, false, ErrInvalid
		} else if err != nil {
			return Administrator{}, false, err
		}
	}
	adminID, err := newOpaque("adm_", 18)
	if err != nil {
		return Administrator{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_administrators(id,email,email_normalized,display_name,status,version,created_at,updated_at) VALUES (?,?,?,?,'active',1,?,?)`, adminID, strings.TrimSpace(input.Email), normalized, strings.TrimSpace(input.DisplayName), now, now)
	if err != nil {
		return Administrator{}, false, mapDuplicate(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_credentials(administrator_id,password_hash,password_algorithm,password_version,failed_attempts,updated_at) VALUES (?,?,'pbkdf2-sha256',1,0,?)`, adminID, passwordHash, now); err != nil {
		return Administrator{}, false, err
	}
	for _, roleID := range roles {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_role_assignments(administrator_id,role_id,assigned_by,created_at) VALUES (?,?,?,?)`, adminID, roleID, p.Administrator.ID, now); err != nil {
			return Administrator{}, false, err
		}
	}
	a := Administrator{ID: adminID, Email: strings.TrimSpace(input.Email), DisplayName: strings.TrimSpace(input.DisplayName), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.administrator.create", ResourceType: "administrator", ResourceID: adminID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, After: map[string]any{"administrator_id": adminID, "status": "active", "role_ids": roles, "mfa_enabled": false, "version": uint64(1)}, CreatedAt: now})
	if err != nil {
		return Administrator{}, false, err
	}
	if err = storeIdempotency(ctx, tx, p.Administrator.ID, "admin.administrator.create", authority.IdempotencyKey, fingerprint, a, auditID, now); err != nil {
		return Administrator{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Administrator{}, false, err
	}
	return a, false, nil
}
