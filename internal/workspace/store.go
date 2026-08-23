package workspace

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) CreateWorkspace(ctx context.Context, principal Principal, name string) (Workspace, Membership, error) {
	name = strings.TrimSpace(name)
	if principal.UserID == "" || normalizeEmail(principal.Email) == "" || name == "" || len(name) > 160 {
		return Workspace{}, Membership{}, ErrInvalid
	}
	id, err := newOpaqueID("ws_", 18)
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspaces (id,name,status,version,created_by) VALUES (?,?, 'active',1,?)`,
		id, name, principal.UserID); err != nil {
		return Workspace{}, Membership{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (?,?,?,?, 'owner')`,
		id, principal.UserID, normalizeEmail(principal.Email), strings.TrimSpace(principal.DisplayName))
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	memberID, err := res.LastInsertId()
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_organizations (workspace_id,name,description,version) VALUES (?,?,'',1)`,
		id, name); err != nil {
		return Workspace{}, Membership{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_notification_state (workspace_id,status,data_through_at,state_reason) VALUES (?,'complete',CURRENT_TIMESTAMP(6),'current')`,
		id); err != nil {
		return Workspace{}, Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, Membership{}, err
	}
	ws, err := s.GetWorkspace(ctx, id)
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	m, err := s.GetMembership(ctx, id, principal.UserID)
	if err != nil {
		return Workspace{}, Membership{}, err
	}
	m.ID = uint64(memberID)
	return ws, m, nil
}

func (s *Store) ListWorkspaces(ctx context.Context, userID string) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT w.id,w.name,w.status,w.version,w.created_by,w.created_at,w.updated_at
FROM workspaces w
JOIN workspace_memberships m ON m.workspace_id=w.id
WHERE m.user_id=?
ORDER BY w.updated_at DESC,w.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Workspace
	for rows.Next() {
		var w Workspace
		if err := rows.Scan(&w.ID, &w.Name, &w.Status, &w.Version, &w.CreatedBy, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) GetWorkspace(ctx context.Context, workspaceID string) (Workspace, error) {
	var w Workspace
	err := s.db.QueryRowContext(ctx, `
SELECT id,name,status,version,created_by,created_at,updated_at
FROM workspaces WHERE id=?`, workspaceID).
		Scan(&w.ID, &w.Name, &w.Status, &w.Version, &w.CreatedBy, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	return w, err
}

func (s *Store) GetMembership(ctx context.Context, workspaceID, userID string) (Membership, error) {
	var m Membership
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,user_id,email,display_name,role,joined_at,updated_at
FROM workspace_memberships WHERE workspace_id=? AND user_id=?`, workspaceID, userID).
		Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.JoinedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrForbidden
	}
	return m, err
}

func (s *Store) ListMembers(ctx context.Context, workspaceID string) ([]Membership, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,user_id,email,display_name,role,joined_at,updated_at
FROM workspace_memberships WHERE workspace_id=?
ORDER BY FIELD(role,'owner','admin','member','viewer'),joined_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.JoinedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) UpdateWorkspace(ctx context.Context, workspaceID, name string, expectedVersion uint64) (Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 || expectedVersion == 0 {
		return Workspace{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE workspaces SET name=?,version=version+1
WHERE id=? AND version=?`, name, workspaceID, expectedVersion)
	if err != nil {
		return Workspace{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Workspace{}, err
	}
	if n == 0 {
		if _, err := s.GetWorkspace(ctx, workspaceID); err != nil {
			return Workspace{}, err
		}
		return Workspace{}, ErrConflict
	}
	return s.GetWorkspace(ctx, workspaceID)
}

func (s *Store) UpdateMemberRole(ctx context.Context, workspaceID string, memberID uint64, actorRole, role string) (Membership, error) {
	if !validRole(role) || memberID == 0 {
		return Membership{}, ErrInvalid
	}
	if actorRole != RoleOwner && role == RoleOwner {
		return Membership{}, ErrForbidden
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Membership{}, err
	}
	defer tx.Rollback()
	if _, err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return Membership{}, err
	}
	current, err := membershipByID(ctx, tx, workspaceID, memberID)
	if err != nil {
		return Membership{}, err
	}
	if actorRole == RoleAdmin && current.Role == RoleOwner {
		return Membership{}, ErrForbidden
	}
	if current.Role == RoleOwner && role != RoleOwner {
		count, err := ownerCount(ctx, tx, workspaceID)
		if err != nil {
			return Membership{}, err
		}
		if count <= 1 {
			return Membership{}, ErrLastOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_memberships SET role=? WHERE id=? AND workspace_id=?`, role, memberID, workspaceID); err != nil {
		return Membership{}, err
	}
	if err := tx.Commit(); err != nil {
		return Membership{}, err
	}
	return s.membershipByID(ctx, workspaceID, memberID)
}

func (s *Store) RemoveMember(ctx context.Context, workspaceID string, memberID uint64, actorRole string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := lockWorkspace(ctx, tx, workspaceID); err != nil {
		return err
	}
	current, err := membershipByID(ctx, tx, workspaceID, memberID)
	if err != nil {
		return err
	}
	if actorRole == RoleAdmin && current.Role == RoleOwner {
		return ErrForbidden
	}
	if current.Role == RoleOwner {
		count, err := ownerCount(ctx, tx, workspaceID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return ErrLastOwner
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_memberships WHERE id=? AND workspace_id=?`, memberID, workspaceID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) membershipByID(ctx context.Context, workspaceID string, memberID uint64) (Membership, error) {
	var m Membership
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,user_id,email,display_name,role,joined_at,updated_at
FROM workspace_memberships WHERE workspace_id=? AND id=?`, workspaceID, memberID).
		Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.JoinedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	return m, err
}

func membershipByID(ctx context.Context, tx *sql.Tx, workspaceID string, memberID uint64) (Membership, error) {
	var m Membership
	err := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,user_id,email,display_name,role,joined_at,updated_at
FROM workspace_memberships WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, memberID).
		Scan(&m.ID, &m.WorkspaceID, &m.UserID, &m.Email, &m.DisplayName, &m.Role, &m.JoinedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Membership{}, ErrNotFound
	}
	return m, err
}

func lockWorkspace(ctx context.Context, tx *sql.Tx, workspaceID string) (uint64, error) {
	var version uint64
	err := tx.QueryRowContext(ctx, `SELECT version FROM workspaces WHERE id=? FOR UPDATE`, workspaceID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return version, err
}

func ownerCount(ctx context.Context, tx *sql.Tx, workspaceID string) (uint64, error) {
	var count uint64
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id=? AND role='owner'`, workspaceID).Scan(&count)
	return count, err
}

func (s *Store) WriteAudit(ctx context.Context, e AuditEvent) error {
	if e.WorkspaceID == "" || e.ActorID == "" || e.Action == "" || e.ResourceType == "" || e.RequestCorrelationID == "" {
		return ErrInvalid
	}
	if e.Result != "success" && e.Result != "denied" && e.Result != "conflict" && e.Result != "failed" {
		return ErrInvalid
	}
	metadata := e.MetadataJSON
	if strings.TrimSpace(metadata) == "" {
		metadata = "{}"
	}
	var tmp any
	if json.Unmarshal([]byte(metadata), &tmp) != nil {
		metadata = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO workspace_audit_events
(workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json)
VALUES (?,?,?,?,?,?,?,?,CAST(? AS JSON))`,
		e.WorkspaceID, e.ActorID, e.Action, e.ResourceType, e.ResourceID, e.Reason,
		e.RequestCorrelationID, e.Result, metadata)
	return err
}

func (s *Store) GetOrganization(ctx context.Context, workspaceID string) (Organization, error) {
	var o Organization
	err := s.db.QueryRowContext(ctx, `
SELECT workspace_id,name,description,version,updated_at
FROM workspace_organizations WHERE workspace_id=?`, workspaceID).
		Scan(&o.WorkspaceID, &o.Name, &o.Description, &o.Version, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Organization{}, ErrNotFound
	}
	return o, err
}

func (s *Store) UpdateOrganization(ctx context.Context, workspaceID, name, description string, expectedVersion uint64) (Organization, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len(name) > 160 || len(description) > 1000 || expectedVersion == 0 {
		return Organization{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_organizations SET name=?,description=?,version=version+1
WHERE workspace_id=? AND version=?`, name, description, workspaceID, expectedVersion)
	if err != nil {
		return Organization{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Organization{}, err
	}
	if n == 0 {
		if _, err := s.GetOrganization(ctx, workspaceID); err != nil {
			return Organization{}, err
		}
		return Organization{}, ErrConflict
	}
	return s.GetOrganization(ctx, workspaceID)
}

func correlationMetadata(values map[string]string) string {
	raw, err := json.Marshal(values)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mysqlDuplicate(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate entry") || strings.Contains(text, "error 1062")
}

func wrapConflict(err error) error {
	if mysqlDuplicate(err) {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}

func expired(now, expiry time.Time) bool {
	return !expiry.After(now)
}
