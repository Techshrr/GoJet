package files

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

func (s *ResourceStore) MarkPublished(ctx context.Context, workspaceID string, fileID uint64, actorID, correlationID, reason string) (Resource, error) {
	if s == nil || s.db == nil || fileID == 0 || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	resource, err := getResourceTx(ctx, tx, strings.TrimSpace(workspaceID), fileID, true)
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	if resource.ScanState != ScanSafe {
		return Resource{}, ErrNotSafe
	}
	if !resource.Published {
		if _, err := tx.ExecContext(ctx, `UPDATE files SET published=1,published_at=CURRENT_TIMESTAMP(6),updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND workspace_id=? AND scan_state='safe' AND published=0`, fileID, workspaceID); err != nil {
			return Resource{}, err
		}
	}
	resource, err = getResourceTx(ctx, tx, strings.TrimSpace(workspaceID), fileID, false)
	if err != nil {
		return Resource{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &fileID, actorID, correlationID, "file.publish", reason, "success", map[string]any{"scan_generation": resource.ScanGeneration}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

func (s *ResourceStore) BeginRescan(ctx context.Context, workspaceID string, fileID uint64, actorID, correlationID, reason string) (Resource, error) {
	if s == nil || s.db == nil || fileID == 0 || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	resource, err := getResourceTx(ctx, tx, strings.TrimSpace(workspaceID), fileID, true)
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	if resource.ScanState == ScanScanning || resource.ScanState == ScanQuarantined {
		return Resource{}, ErrConflict
	}
	generation := resource.ScanGeneration + 1
	if _, err := tx.ExecContext(ctx, `UPDATE files SET scan_state='quarantined',scan_generation=?,published=0,published_at=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND workspace_id=?`, generation, fileID, workspaceID); err != nil {
		return Resource{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO file_scan_attempts (file_id,workspace_id,generation,status) VALUES (?,?,?,'queued')`, fileID, workspaceID, generation); err != nil {
		return Resource{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &fileID, actorID, correlationID, "file.rescan", reason, "success", map[string]any{"previous_generation": resource.ScanGeneration, "generation": generation}); err != nil {
		return Resource{}, err
	}
	resource, err = getResourceTx(ctx, tx, strings.TrimSpace(workspaceID), fileID, false)
	if err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}

func (s *ResourceStore) Delete(ctx context.Context, workspaceID string, fileID uint64, actorID, correlationID, reason string) (Resource, error) {
	if s == nil || s.db == nil || fileID == 0 || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	resource, err := getResourceTx(ctx, tx, strings.TrimSpace(workspaceID), fileID, true)
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	result, err := tx.ExecContext(ctx, `UPDATE files SET deleted_at=CURRENT_TIMESTAMP(6),published=0,published_at=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND workspace_id=? AND deleted_at IS NULL`, fileID, workspaceID)
	if err != nil {
		return Resource{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Resource{}, err
	}
	if affected != 1 {
		return Resource{}, ErrConflict
	}
	result, err = tx.ExecContext(ctx, `UPDATE file_workspace_counters SET active_count=active_count-1,active_bytes=active_bytes-? WHERE workspace_id=? AND active_count>0 AND active_bytes>=?`, resource.SizeBytes, workspaceID, resource.SizeBytes)
	if err != nil {
		return Resource{}, err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return Resource{}, err
	}
	if affected != 1 {
		return Resource{}, errors.New("file workspace counter invariant violated")
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &fileID, actorID, correlationID, "file.delete", reason, "success", map[string]any{"size_bytes": resource.SizeBytes}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}
