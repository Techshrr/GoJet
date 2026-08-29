package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Service) BootstrapAdministrator(ctx context.Context, email, displayName, password string, permissions []string, correlationID string, now time.Time) (Administrator, error) {
	if s == nil || s.db == nil || !validCorrelation(correlationID) || !validPassword(password) {
		return Administrator{}, ErrInvalid
	}
	normalized, err := normalizeEmail(email)
	if err != nil {
		return Administrator{}, err
	}
	permissions, err = sortedPermissions(permissions)
	if err != nil || len(permissions) == 0 {
		return Administrator{}, ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	passwordHash, err := hashPassword(password)
	if err != nil {
		return Administrator{}, err
	}
	adminID, err := newOpaque("adm_", 18)
	if err != nil {
		return Administrator{}, err
	}
	roleID, err := newOpaque("adr_", 18)
	if err != nil {
		return Administrator{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Administrator{}, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_administrators`).Scan(&count); err != nil {
		return Administrator{}, err
	}
	if count != 0 {
		return Administrator{}, ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_administrators(id,email,email_normalized,display_name,status,version,created_at,updated_at) VALUES (?,?,?,?,'active',1,?,?)`, adminID, strings.TrimSpace(email), normalized, strings.TrimSpace(displayName), now, now)
	if err != nil {
		return Administrator{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_credentials(administrator_id,password_hash,password_algorithm,password_version,failed_attempts,updated_at) VALUES (?,?,'pbkdf2-sha256',1,0,?)`, adminID, passwordHash, now)
	if err != nil {
		return Administrator{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_roles(id,name,normalized_name,description,version,created_by,created_at,updated_at) VALUES (?,'Initial administrator','initial administrator','Installer-created explicit permission role',1,'system',?,?)`, roleID, now, now)
	if err != nil {
		return Administrator{}, err
	}
	for _, permission := range permissions {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_role_permissions(role_id,permission,created_at) VALUES (?,?,?)`, roleID, permission, now); err != nil {
			return Administrator{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO admin_role_assignments(administrator_id,role_id,assigned_by,created_at) VALUES (?,?,?,?)`, adminID, roleID, "system", now); err != nil {
		return Administrator{}, err
	}
	_, err = recordAuditTx(ctx, tx, auditInput{ActorKind: "system", ActorID: "installer", Action: "admin.bootstrap", ResourceType: "administrator", ResourceID: adminID, Result: "success", CorrelationID: correlationID, After: map[string]any{"status": "active", "role_ids": []string{roleID}, "permissions": permissions, "mfa_enabled": false, "version": uint64(1)}, CreatedAt: now})
	if err != nil {
		return Administrator{}, err
	}
	if err = tx.Commit(); err != nil {
		return Administrator{}, err
	}
	return Administrator{ID: adminID, Email: strings.TrimSpace(email), DisplayName: strings.TrimSpace(displayName), Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now}, nil
}
