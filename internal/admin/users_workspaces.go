package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const maxAdminEnumerationLimit = 200

type ManagedUser struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	Status         string    `json:"status"`
	EmailVerified  bool      `json:"email_verified"`
	WorkspaceCount int       `json:"workspace_count"`
	Version        uint64    `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ManagedWorkspace struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	CreatedBy   string    `json:"created_by"`
	MemberCount int       `json:"member_count"`
	OwnerCount  int       `json:"owner_count"`
	Version     uint64    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func normalizeAdminEnumerationLimit(limit int) (int, error) {
	if limit == 0 {
		return 100, nil
	}
	if limit < 1 || limit > maxAdminEnumerationLimit {
		return 0, ErrInvalid
	}
	return limit, nil
}

func (s *Service) ListManagedUsers(ctx context.Context, p Principal, limit int) ([]ManagedUser, error) {
	if err := s.Require(p, PermissionUsersManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT u.id,u.email,u.display_name,u.status,u.email_verified_at,u.version,u.created_at,u.updated_at,
       COUNT(DISTINCT m.workspace_id) AS workspace_count
FROM auth_users u
LEFT JOIN workspace_memberships m ON m.user_id=u.id
GROUP BY u.id,u.email,u.display_name,u.status,u.email_verified_at,u.version,u.created_at,u.updated_at
ORDER BY u.updated_at DESC,u.id
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedUser, 0)
	for rows.Next() {
		item, scanErr := scanManagedUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedUser(ctx context.Context, p Principal, userID string) (ManagedUser, error) {
	if err := s.Require(p, PermissionUsersManage); err != nil {
		return ManagedUser{}, err
	}
	if !validID(userID, 128) {
		return ManagedUser{}, ErrInvalid
	}
	return managedUserByID(ctx, s.db, strings.TrimSpace(userID), false)
}

func (s *Service) SuspendManagedUser(ctx context.Context, p Principal, userID string, authority MutationAuthority, now time.Time) (ManagedUser, bool, error) {
	return s.mutateManagedUser(ctx, p, userID, "suspend", authority, now)
}

func (s *Service) RestoreManagedUser(ctx context.Context, p Principal, userID string, authority MutationAuthority, now time.Time) (ManagedUser, bool, error) {
	return s.mutateManagedUser(ctx, p, userID, "restore", authority, now)
}

func (s *Service) mutateManagedUser(ctx context.Context, p Principal, userID, operation string, authority MutationAuthority, now time.Time) (ManagedUser, bool, error) {
	if !validID(userID, 128) || (operation != "suspend" && operation != "restore") {
		return ManagedUser{}, false, ErrInvalid
	}
	if err := s.RequireHighRisk(p, PermissionUsersManage, authority, now); err != nil {
		return ManagedUser{}, false, err
	}
	userID = strings.TrimSpace(userID)
	action := "admin.user." + operation
	fingerprint, err := requestFingerprint(struct {
		UserID    string `json:"user_id"`
		Operation string `json:"operation"`
		Reason    string `json:"reason"`
	}{UserID: userID, Operation: operation, Reason: strings.TrimSpace(authority.Reason)})
	if err != nil {
		return ManagedUser{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ManagedUser{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, replayErr := loadIdempotency[ManagedUser](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); replayErr != nil {
		return ManagedUser{}, false, replayErr
	} else if ok {
		return replay, true, nil
	}

	before, verifiedAt, err := lockManagedUser(ctx, tx, userID)
	if err != nil {
		return ManagedUser{}, false, err
	}
	if operation == "suspend" && before.Status == "locked" {
		// P15 owns account lock authority. P17 user lifecycle governance must
		// not collapse a locked account into an administrator-restorable state.
		return ManagedUser{}, false, ErrConflict
	}
	targetStatus := "disabled"
	if operation == "restore" {
		if before.Status != "disabled" {
			return ManagedUser{}, false, ErrConflict
		}
		targetStatus = "pending_verification"
		if verifiedAt.Valid {
			targetStatus = "active"
		}
	}

	if before.Status != targetStatus {
		result, execErr := tx.ExecContext(ctx, `UPDATE auth_users SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, targetStatus, now, userID, before.Version)
		if execErr != nil {
			return ManagedUser{}, false, execErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return ManagedUser{}, false, rowsErr
		}
		if rows != 1 {
			return ManagedUser{}, false, ErrConflict
		}
	}

	revokedSessions := int64(0)
	if operation == "suspend" {
		result, execErr := tx.ExecContext(ctx, `UPDATE auth_sessions SET status='revoked',revoked_at=?,updated_at=? WHERE user_id=? AND status='active'`, now, now, userID)
		if execErr != nil {
			return ManagedUser{}, false, execErr
		}
		revokedSessions, err = result.RowsAffected()
		if err != nil {
			return ManagedUser{}, false, err
		}
	}
	after, err := managedUserByIDTx(ctx, tx, userID)
	if err != nil {
		return ManagedUser{}, false, err
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind:     "administrator",
		ActorID:       p.Administrator.ID,
		Action:        action,
		ResourceType:  "auth_user",
		ResourceID:    userID,
		Result:        "success",
		CorrelationID: authority.CorrelationID,
		Reason:        authority.Reason,
		Before: map[string]any{
			"status":         before.Status,
			"version":        before.Version,
			"email_verified": before.EmailVerified,
		},
		After: map[string]any{
			"status":         after.Status,
			"version":        after.Version,
			"email_verified": after.EmailVerified,
		},
		Metadata: map[string]any{
			"user_id":          userID,
			"revoked_sessions": revokedSessions,
		},
		CreatedAt: now,
	})
	if err != nil {
		return ManagedUser{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, after, auditID, now); err != nil {
		return ManagedUser{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedUser{}, false, err
	}
	return after, false, nil
}

func (s *Service) ListManagedWorkspaces(ctx context.Context, p Principal, limit int) ([]ManagedWorkspace, error) {
	if err := s.Require(p, PermissionWorkspacesManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT w.id,w.name,w.status,w.version,w.created_by,w.created_at,w.updated_at,
       COUNT(m.id) AS member_count,
       COALESCE(SUM(CASE WHEN m.role='owner' THEN 1 ELSE 0 END),0) AS owner_count
FROM workspaces w
LEFT JOIN workspace_memberships m ON m.workspace_id=w.id
GROUP BY w.id,w.name,w.status,w.version,w.created_by,w.created_at,w.updated_at
ORDER BY w.updated_at DESC,w.id
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedWorkspace, 0)
	for rows.Next() {
		var item ManagedWorkspace
		if err := rows.Scan(&item.ID, &item.Name, &item.Status, &item.Version, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.MemberCount, &item.OwnerCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedWorkspace(ctx context.Context, p Principal, workspaceID string) (ManagedWorkspace, error) {
	if err := s.Require(p, PermissionWorkspacesManage); err != nil {
		return ManagedWorkspace{}, err
	}
	if !validID(workspaceID, 64) {
		return ManagedWorkspace{}, ErrInvalid
	}
	return managedWorkspaceByID(ctx, s.db, strings.TrimSpace(workspaceID))
}

func (s *Service) SuspendManagedWorkspace(ctx context.Context, p Principal, workspaceID string, authority MutationAuthority, now time.Time) (ManagedWorkspace, bool, error) {
	return s.mutateManagedWorkspace(ctx, p, workspaceID, "suspend", authority, now)
}

func (s *Service) RestoreManagedWorkspace(ctx context.Context, p Principal, workspaceID string, authority MutationAuthority, now time.Time) (ManagedWorkspace, bool, error) {
	return s.mutateManagedWorkspace(ctx, p, workspaceID, "restore", authority, now)
}

func (s *Service) mutateManagedWorkspace(ctx context.Context, p Principal, workspaceID, operation string, authority MutationAuthority, now time.Time) (ManagedWorkspace, bool, error) {
	if !validID(workspaceID, 64) || (operation != "suspend" && operation != "restore") {
		return ManagedWorkspace{}, false, ErrInvalid
	}
	if err := s.RequireHighRisk(p, PermissionWorkspacesManage, authority, now); err != nil {
		return ManagedWorkspace{}, false, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	action := "admin.workspace." + operation
	fingerprint, err := requestFingerprint(struct {
		WorkspaceID string `json:"workspace_id"`
		Operation   string `json:"operation"`
		Reason      string `json:"reason"`
	}{WorkspaceID: workspaceID, Operation: operation, Reason: strings.TrimSpace(authority.Reason)})
	if err != nil {
		return ManagedWorkspace{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ManagedWorkspace{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, replayErr := loadIdempotency[ManagedWorkspace](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); replayErr != nil {
		return ManagedWorkspace{}, false, replayErr
	} else if ok {
		return replay, true, nil
	}
	before, err := lockManagedWorkspace(ctx, tx, workspaceID)
	if err != nil {
		return ManagedWorkspace{}, false, err
	}
	targetStatus := "suspended"
	if operation == "restore" {
		if before.Status != "suspended" {
			return ManagedWorkspace{}, false, ErrConflict
		}
		targetStatus = "active"
	}
	if before.Status != targetStatus {
		result, execErr := tx.ExecContext(ctx, `UPDATE workspaces SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, targetStatus, now, workspaceID, before.Version)
		if execErr != nil {
			return ManagedWorkspace{}, false, execErr
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			return ManagedWorkspace{}, false, rowsErr
		}
		if rows != 1 {
			return ManagedWorkspace{}, false, ErrConflict
		}
	}
	after, err := managedWorkspaceByIDTx(ctx, tx, workspaceID)
	if err != nil {
		return ManagedWorkspace{}, false, err
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind:     "administrator",
		ActorID:       p.Administrator.ID,
		Action:        action,
		ResourceType:  "workspace",
		ResourceID:    workspaceID,
		Result:        "success",
		CorrelationID: authority.CorrelationID,
		Reason:        authority.Reason,
		Before: map[string]any{
			"status":  before.Status,
			"version": before.Version,
		},
		After: map[string]any{
			"status":  after.Status,
			"version": after.Version,
		},
		Metadata: map[string]any{
			"workspace_id": workspaceID,
			"member_count": after.MemberCount,
			"owner_count":  after.OwnerCount,
		},
		CreatedAt: now,
	})
	if err != nil {
		return ManagedWorkspace{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, after, auditID, now); err != nil {
		return ManagedWorkspace{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedWorkspace{}, false, err
	}
	return after, false, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanManagedUser(row scanner) (ManagedUser, error) {
	var item ManagedUser
	var verifiedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Email, &item.DisplayName, &item.Status, &verifiedAt, &item.Version, &item.CreatedAt, &item.UpdatedAt, &item.WorkspaceCount); err != nil {
		return ManagedUser{}, err
	}
	item.EmailVerified = verifiedAt.Valid
	return item, nil
}

func managedUserByID(ctx context.Context, db *sql.DB, userID string, forUpdate bool) (ManagedUser, error) {
	query := `
SELECT u.id,u.email,u.display_name,u.status,u.email_verified_at,u.version,u.created_at,u.updated_at,
       COUNT(DISTINCT m.workspace_id) AS workspace_count
FROM auth_users u
LEFT JOIN workspace_memberships m ON m.user_id=u.id
WHERE u.id=?
GROUP BY u.id,u.email,u.display_name,u.status,u.email_verified_at,u.version,u.created_at,u.updated_at`
	if forUpdate {
		query += " FOR UPDATE"
	}
	item, err := scanManagedUser(db.QueryRowContext(ctx, query, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedUser{}, ErrNotFound
	}
	return item, err
}

func lockManagedUser(ctx context.Context, tx *sql.Tx, userID string) (ManagedUser, sql.NullTime, error) {
	var item ManagedUser
	var verifiedAt sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT id,email,display_name,status,email_verified_at,version,created_at,updated_at FROM auth_users WHERE id=? FOR UPDATE`, userID).Scan(&item.ID, &item.Email, &item.DisplayName, &item.Status, &verifiedAt, &item.Version, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedUser{}, sql.NullTime{}, ErrNotFound
	}
	if err != nil {
		return ManagedUser{}, sql.NullTime{}, err
	}
	item.EmailVerified = verifiedAt.Valid
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT workspace_id) FROM workspace_memberships WHERE user_id=?`, userID).Scan(&item.WorkspaceCount); err != nil {
		return ManagedUser{}, sql.NullTime{}, err
	}
	return item, verifiedAt, nil
}

func managedUserByIDTx(ctx context.Context, tx *sql.Tx, userID string) (ManagedUser, error) {
	item, verifiedAt, err := lockManagedUser(ctx, tx, userID)
	if err != nil {
		return ManagedUser{}, err
	}
	item.EmailVerified = verifiedAt.Valid
	return item, nil
}

func managedWorkspaceByID(ctx context.Context, db *sql.DB, workspaceID string) (ManagedWorkspace, error) {
	var item ManagedWorkspace
	err := db.QueryRowContext(ctx, `
SELECT w.id,w.name,w.status,w.version,w.created_by,w.created_at,w.updated_at,
       COUNT(m.id) AS member_count,
       COALESCE(SUM(CASE WHEN m.role='owner' THEN 1 ELSE 0 END),0) AS owner_count
FROM workspaces w
LEFT JOIN workspace_memberships m ON m.workspace_id=w.id
WHERE w.id=?
GROUP BY w.id,w.name,w.status,w.version,w.created_by,w.created_at,w.updated_at`, workspaceID).Scan(&item.ID, &item.Name, &item.Status, &item.Version, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.MemberCount, &item.OwnerCount)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedWorkspace{}, ErrNotFound
	}
	return item, err
}

func lockManagedWorkspace(ctx context.Context, tx *sql.Tx, workspaceID string) (ManagedWorkspace, error) {
	var item ManagedWorkspace
	err := tx.QueryRowContext(ctx, `SELECT id,name,status,version,created_by,created_at,updated_at FROM workspaces WHERE id=? FOR UPDATE`, workspaceID).Scan(&item.ID, &item.Name, &item.Status, &item.Version, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedWorkspace{}, ErrNotFound
	}
	if err != nil {
		return ManagedWorkspace{}, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN role='owner' THEN 1 ELSE 0 END),0) FROM workspace_memberships WHERE workspace_id=?`, workspaceID).Scan(&item.MemberCount, &item.OwnerCount); err != nil {
		return ManagedWorkspace{}, err
	}
	return item, nil
}

func managedWorkspaceByIDTx(ctx context.Context, tx *sql.Tx, workspaceID string) (ManagedWorkspace, error) {
	return lockManagedWorkspace(ctx, tx, workspaceID)
}
