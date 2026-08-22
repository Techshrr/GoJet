package qrcodes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid qr input")
	ErrNotFound     = errors.New("qr not found")
	ErrDeleted      = errors.New("qr deleted")
	ErrQuota        = errors.New("qr quota reached")
)

type Resource struct {
	ID           uint64     `json:"id"`
	WorkspaceID  string     `json:"workspace_id"`
	SourceLinkID uint64     `json:"source_link_id"`
	Label        string     `json:"label"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	DeletedAt    *time.Time `json:"deleted_at,omitempty"`
}

type ListResult struct {
	Items      []Resource `json:"items"`
	Total      int64      `json:"total"`
	QuotaLimit uint64     `json:"quota_limit"`
	QuotaUsed  uint64     `json:"quota_used"`
}

type Store struct {
	db             *sql.DB
	workspaceQuota uint64
}

func NewStore(db *sql.DB, workspaceQuota uint64) *Store {
	if workspaceQuota == 0 {
		workspaceQuota = 100
	}
	return &Store{db: db, workspaceQuota: workspaceQuota}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("qr store unavailable")
	}
	return s.db.PingContext(ctx)
}

func normalizeLabel(raw string) (string, error) {
	label := strings.TrimSpace(raw)
	if len([]rune(label)) > 120 {
		return "", ErrInvalidInput
	}
	return label, nil
}

func validateWriteIdentity(workspaceID, actorID, correlationID, reason string) error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalidInput
	}
	return nil
}

func (s *Store) Create(ctx context.Context, workspaceID string, sourceLinkID uint64, label, actorID, correlationID, reason string) (Resource, error) {
	if s == nil || s.db == nil || sourceLinkID == 0 || validateWriteIdentity(workspaceID, actorID, correlationID, reason) != nil {
		return Resource{}, ErrInvalidInput
	}
	label, err := normalizeLabel(label)
	if err != nil {
		return Resource{}, err
	}
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	reason = strings.TrimSpace(reason)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO qr_workspace_counters (workspace_id, active_count) VALUES (?, 0) ON DUPLICATE KEY UPDATE workspace_id = VALUES(workspace_id)`, workspaceID); err != nil {
		return Resource{}, err
	}
	var used uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count FROM qr_workspace_counters WHERE workspace_id = ? FOR UPDATE`, workspaceID).Scan(&used); err != nil {
		return Resource{}, err
	}
	if used >= s.workspaceQuota {
		if err := appendAuditTx(ctx, tx, workspaceID, nil, &sourceLinkID, actorID, correlationID, "qr.create", reason, "denied", map[string]any{"reason": "quota", "quota_limit": s.workspaceQuota}); err != nil {
			return Resource{}, err
		}
		if err := tx.Commit(); err != nil {
			return Resource{}, err
		}
		return Resource{}, ErrQuota
	}

	result, err := tx.ExecContext(ctx, `INSERT INTO qr_codes (workspace_id, source_link_id, label, created_by) VALUES (?, ?, ?, ?)`, workspaceID, sourceLinkID, label, actorID)
	if err != nil {
		return Resource{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return Resource{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE qr_workspace_counters SET active_count = active_count + 1 WHERE workspace_id = ?`, workspaceID); err != nil {
		return Resource{}, err
	}
	created, err := getTx(ctx, tx, workspaceID, uint64(insertID), false)
	if err != nil {
		return Resource{}, err
	}
	qrID := created.ID
	if err := appendAuditTx(ctx, tx, workspaceID, &qrID, &sourceLinkID, actorID, correlationID, "qr.create", reason, "success", map[string]any{"quota_used": used + 1, "quota_limit": s.workspaceQuota}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return created, nil
}

func (s *Store) List(ctx context.Context, workspaceID string, limit, offset int) (ListResult, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || offset < 0 {
		return ListResult{}, ErrInvalidInput
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM qr_codes WHERE workspace_id = ? AND deleted_at IS NULL`, workspaceID).Scan(&total); err != nil {
		return ListResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, workspace_id, source_link_id, label, created_by, created_at, updated_at, deleted_at FROM qr_codes WHERE workspace_id = ? AND deleted_at IS NULL ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`, workspaceID, limit, offset)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items := make([]Resource, 0, min(limit, int(total)))
	for rows.Next() {
		item, err := scanResource(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	var used uint64
	err = s.db.QueryRowContext(ctx, `SELECT active_count FROM qr_workspace_counters WHERE workspace_id = ?`, workspaceID).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		used = 0
	} else if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total, QuotaLimit: s.workspaceQuota, QuotaUsed: used}, nil
}

func (s *Store) Get(ctx context.Context, workspaceID string, id uint64) (Resource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || id == 0 {
		return Resource{}, ErrInvalidInput
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, workspace_id, source_link_id, label, created_by, created_at, updated_at, deleted_at FROM qr_codes WHERE workspace_id = ? AND id = ?`, strings.TrimSpace(workspaceID), id)
	resource, err := scanResource(row)
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	return resource, nil
}

func (s *Store) Delete(ctx context.Context, workspaceID string, id uint64, actorID, correlationID, reason string) error {
	if s == nil || s.db == nil || id == 0 || validateWriteIdentity(workspaceID, actorID, correlationID, reason) != nil {
		return ErrInvalidInput
	}
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	reason = strings.TrimSpace(reason)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	resource, err := getTx(ctx, tx, workspaceID, id, true)
	if err != nil {
		return err
	}
	if resource.DeletedAt != nil {
		return ErrDeleted
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO qr_workspace_counters (workspace_id, active_count) VALUES (?, 0) ON DUPLICATE KEY UPDATE workspace_id = VALUES(workspace_id)`, workspaceID); err != nil {
		return err
	}
	var used uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count FROM qr_workspace_counters WHERE workspace_id = ? FOR UPDATE`, workspaceID).Scan(&used); err != nil {
		return err
	}
	if used == 0 {
		return errors.New("qr counter invariant violated")
	}
	result, err := tx.ExecContext(ctx, `UPDATE qr_codes SET deleted_at = CURRENT_TIMESTAMP(6), updated_at = CURRENT_TIMESTAMP(6) WHERE workspace_id = ? AND id = ? AND deleted_at IS NULL`, workspaceID, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrDeleted
	}
	if _, err := tx.ExecContext(ctx, `UPDATE qr_workspace_counters SET active_count = active_count - 1 WHERE workspace_id = ? AND active_count > 0`, workspaceID); err != nil {
		return err
	}
	qrID := resource.ID
	sourceID := resource.SourceLinkID
	if err := appendAuditTx(ctx, tx, workspaceID, &qrID, &sourceID, actorID, correlationID, "qr.delete", reason, "success", map[string]any{"quota_used": used - 1, "quota_limit": s.workspaceQuota}); err != nil {
		return err
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanResource(row rowScanner) (Resource, error) {
	var resource Resource
	if err := row.Scan(&resource.ID, &resource.WorkspaceID, &resource.SourceLinkID, &resource.Label, &resource.CreatedBy, &resource.CreatedAt, &resource.UpdatedAt, &resource.DeletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Resource{}, ErrNotFound
		}
		return Resource{}, err
	}
	return resource, nil
}

func getTx(ctx context.Context, tx *sql.Tx, workspaceID string, id uint64, forUpdate bool) (Resource, error) {
	query := `SELECT id, workspace_id, source_link_id, label, created_by, created_at, updated_at, deleted_at FROM qr_codes WHERE workspace_id = ? AND id = ?`
	if forUpdate {
		query += " FOR UPDATE"
	}
	return scanResource(tx.QueryRowContext(ctx, query, workspaceID, id))
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, qrID, sourceLinkID *uint64, actorID, correlationID, action, reason, result string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO qr_audit_events (workspace_id, qr_id, source_link_id, actor_id, action, request_correlation_id, reason, result, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, qrID, sourceLinkID, actorID, action, correlationID, reason, result, string(raw))
	return err
}
