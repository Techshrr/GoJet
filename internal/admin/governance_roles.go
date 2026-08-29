package admin

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

func (s *Service) ListRoles(ctx context.Context, p Principal) ([]Role, error) {
	if err := s.Require(p, PermissionAdminsManage); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,version,created_at,updated_at FROM admin_roles ORDER BY normalized_name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Version, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, err
		}
		perms, err := s.rolePermissions(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		role.Permissions = perms
		out = append(out, role)
	}
	return out, rows.Err()
}
func (s *Service) rolePermissions(ctx context.Context, roleID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT permission FROM admin_role_permissions WHERE role_id=? ORDER BY permission`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		if !ValidPermission(p) {
			return nil, ErrForbidden
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Service) CreateRole(ctx context.Context, p Principal, input CreateRoleInput, authority MutationAuthority, now time.Time) (Role, bool, error) {
	if err := s.RequireHighRisk(p, PermissionAdminsManage, authority, now); err != nil {
		return Role{}, false, err
	}
	normalized, err := normalizeName(input.Name)
	if err != nil {
		return Role{}, false, err
	}
	perms, err := sortedPermissions(input.Permissions)
	if err != nil || len(perms) == 0 {
		return Role{}, false, ErrInvalid
	}
	if len(input.Description) > 500 {
		return Role{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		Name        string
		Description string
		Permissions []string
	}{normalized, strings.TrimSpace(input.Description), perms})
	if err != nil {
		return Role{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Role{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, err := loadIdempotency[Role](ctx, tx, p.Administrator.ID, "admin.role.create", authority.IdempotencyKey, fingerprint); err != nil {
		return Role{}, false, err
	} else if ok {
		return replay, true, nil
	}
	roleID, err := newOpaque("adr_", 18)
	if err != nil {
		return Role{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_roles(id,name,normalized_name,description,version,created_by,created_at,updated_at) VALUES (?,?,?,?,1,?,?,?)`, roleID, strings.TrimSpace(input.Name), normalized, strings.TrimSpace(input.Description), p.Administrator.ID, now, now)
	if err != nil {
		return Role{}, false, mapDuplicate(err)
	}
	for _, permission := range perms {
		if _, err = tx.ExecContext(ctx, `INSERT INTO admin_role_permissions(role_id,permission,created_at) VALUES (?,?,?)`, roleID, permission, now); err != nil {
			return Role{}, false, err
		}
	}
	role := Role{ID: roleID, Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Permissions: perms, Version: 1, CreatedAt: now, UpdatedAt: now}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: "admin.role.create", ResourceType: "admin_role", ResourceID: roleID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, After: map[string]any{"role_id": roleID, "permissions": perms, "version": uint64(1)}, CreatedAt: now})
	if err != nil {
		return Role{}, false, err
	}
	if err = storeIdempotency(ctx, tx, p.Administrator.ID, "admin.role.create", authority.IdempotencyKey, fingerprint, role, auditID, now); err != nil {
		return Role{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return Role{}, false, err
	}
	return role, false, nil
}
