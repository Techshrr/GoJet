package files

import (
	"context"
	"database/sql"
	"strings"
)

type ResourceStore struct {
	db       *sql.DB
	maxFiles uint64
	maxBytes uint64
}

func NewResourceStoreWithQuota(db *sql.DB, maxFiles, maxBytes uint64) (*ResourceStore, error) {
	if db == nil || maxFiles == 0 || maxBytes == 0 {
		return nil, ErrInvalidInput
	}
	return &ResourceStore{db: db, maxFiles: maxFiles, maxBytes: maxBytes}, nil
}

type CreateInput struct {
	WorkspaceID   string
	PublicSlug    string
	OriginalName  string
	StorageKey    string
	SizeBytes     uint64
	ContentSHA256 string
	DeclaredMIME  string
	DetectedMIME  string
	CreatedBy     string
	CorrelationID string
	Reason        string
}

func (s *ResourceStore) CreateQuarantined(ctx context.Context, input CreateInput) (Resource, error) {
	if s == nil || s.db == nil || s.maxFiles == 0 || s.maxBytes == 0 || strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.PublicSlug) == "" || strings.TrimSpace(input.OriginalName) == "" || !validStorageKey(input.StorageKey) || !validStorageKey(input.ContentSHA256) || input.SizeBytes == 0 || strings.TrimSpace(input.DeclaredMIME) == "" || strings.TrimSpace(input.DetectedMIME) == "" || strings.TrimSpace(input.CreatedBy) == "" || strings.TrimSpace(input.CorrelationID) == "" || strings.TrimSpace(input.Reason) == "" {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_workspace_counters (workspace_id, active_count, active_bytes) VALUES (?,0,0) ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id)`, workspaceID); err != nil {
		return Resource{}, err
	}
	var count, bytes uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count, active_bytes FROM file_workspace_counters WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&count, &bytes); err != nil {
		return Resource{}, err
	}
	if count >= s.maxFiles || input.SizeBytes > s.maxBytes || bytes > s.maxBytes-input.SizeBytes {
		if err := appendAuditTx(ctx, tx, workspaceID, nil, input.CreatedBy, input.CorrelationID, "file.create", input.Reason, "denied", map[string]any{"reason": "quota", "active_count": count, "active_bytes": bytes}); err != nil {
			return Resource{}, err
		}
		if err := tx.Commit(); err != nil {
			return Resource{}, err
		}
		return Resource{}, ErrQuota
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO files (workspace_id, public_slug, original_name, storage_key, size_bytes, content_sha256, declared_mime, detected_mime, scan_state, scan_generation, created_by)
VALUES (?,?,?,?,?,?,?,?,'quarantined',1,?)`, workspaceID, strings.TrimSpace(input.PublicSlug), strings.TrimSpace(input.OriginalName), input.StorageKey, input.SizeBytes, input.ContentSHA256, strings.TrimSpace(input.DeclaredMIME), strings.TrimSpace(input.DetectedMIME), strings.TrimSpace(input.CreatedBy))
	if err != nil {
		return Resource{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return Resource{}, err
	}
	fileID := uint64(insertID)
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_scan_attempts (file_id, workspace_id, generation, status) VALUES (?,?,1,'queued')`, fileID, workspaceID); err != nil {
		return Resource{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_workspace_counters SET active_count=active_count+1, active_bytes=active_bytes+? WHERE workspace_id=?`, input.SizeBytes, workspaceID); err != nil {
		return Resource{}, err
	}
	resource, err := getResourceTx(ctx, tx, workspaceID, fileID, false)
	if err != nil {
		return Resource{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &fileID, input.CreatedBy, input.CorrelationID, "file.create", input.Reason, "success", map[string]any{"size_bytes": input.SizeBytes, "scan_generation": 1}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

func (s *ResourceStore) List(ctx context.Context, workspaceID string, limit, offset int) ([]Resource, int64, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || offset < 0 {
		return nil, 0, ErrInvalidInput
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE workspace_id=? AND deleted_at IS NULL`, workspaceID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, resourceSelect+` WHERE workspace_id=? AND deleted_at IS NULL ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, workspaceID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Resource, 0, min(limit, int(total)))
	for rows.Next() {
		item, err := scanResource(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *ResourceStore) Get(ctx context.Context, workspaceID string, fileID uint64) (Resource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || fileID == 0 {
		return Resource{}, ErrInvalidInput
	}
	resource, err := scanResource(s.db.QueryRowContext(ctx, resourceSelect+` WHERE workspace_id=? AND id=?`, strings.TrimSpace(workspaceID), fileID))
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	return resource, nil
}
