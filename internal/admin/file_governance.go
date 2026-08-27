package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ManagedFile struct {
	ID             uint64     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	OriginalName   string     `json:"original_name"`
	SizeBytes      uint64     `json:"size_bytes"`
	ContentSHA256  string     `json:"content_sha256"`
	DeclaredMIME   string     `json:"declared_mime"`
	DetectedMIME   string     `json:"detected_mime"`
	ScanState      string     `json:"scan_state"`
	ScanGeneration uint64     `json:"scan_generation"`
	Published      bool       `json:"published"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	DownloadLimit  *uint64    `json:"download_limit,omitempty"`
	DownloadCount  uint64     `json:"download_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	DeletedAt      *time.Time `json:"deleted_at,omitempty"`
}

func (s *Service) ListManagedFiles(ctx context.Context, p Principal, limit int) ([]ManagedFile, error) {
	if err := s.Require(p, PermissionFilesManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,original_name,size_bytes,content_sha256,declared_mime,detected_mime,scan_state,scan_generation,published,expires_at,download_limit,download_count,created_at,updated_at,deleted_at
FROM files ORDER BY updated_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedFile, 0)
	for rows.Next() {
		item, err := scanManagedFile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetManagedFile(ctx context.Context, p Principal, fileID uint64) (ManagedFile, error) {
	if err := s.Require(p, PermissionFilesManage); err != nil {
		return ManagedFile{}, err
	}
	if fileID == 0 {
		return ManagedFile{}, ErrInvalid
	}
	item, err := managedFileByID(ctx, s.db, fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedFile{}, ErrNotFound
	}
	return item, err
}

func (s *Service) QuarantineManagedFile(ctx context.Context, p Principal, fileID uint64, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	return s.mutateManagedFile(ctx, p, fileID, "quarantine", nil, authority, now)
}

func (s *Service) RescanManagedFile(ctx context.Context, p Principal, fileID uint64, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	return s.mutateManagedFile(ctx, p, fileID, "rescan", nil, authority, now)
}

func (s *Service) RestoreManagedFile(ctx context.Context, p Principal, fileID uint64, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	return s.mutateManagedFile(ctx, p, fileID, "restore", nil, authority, now)
}

func (s *Service) DeleteManagedFile(ctx context.Context, p Principal, fileID uint64, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	return s.mutateManagedFile(ctx, p, fileID, "delete", nil, authority, now)
}

func (s *Service) SetManagedFileExpiry(ctx context.Context, p Principal, fileID uint64, expiresAt *time.Time, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	return s.mutateManagedFile(ctx, p, fileID, "expiry", expiresAt, authority, now)
}

func (s *Service) mutateManagedFile(ctx context.Context, p Principal, fileID uint64, operation string, expiresAt *time.Time, authority MutationAuthority, now time.Time) (ManagedFile, bool, error) {
	if fileID == 0 {
		return ManagedFile{}, false, ErrInvalid
	}
	if operation != "quarantine" && operation != "rescan" && operation != "restore" && operation != "delete" && operation != "expiry" {
		return ManagedFile{}, false, ErrInvalid
	}
	if err := s.RequireHighRisk(p, PermissionFilesManage, authority, now); err != nil {
		return ManagedFile{}, false, err
	}
	var expiryValue any
	if expiresAt != nil {
		v := expiresAt.UTC().Truncate(time.Microsecond)
		if !v.After(now.UTC()) {
			return ManagedFile{}, false, ErrInvalid
		}
		expiryValue = v
	}
	action := "admin.file." + operation
	fingerprint, err := requestFingerprint(map[string]any{"file_id": fileID, "operation": operation, "expires_at": expiryValue, "reason": strings.TrimSpace(authority.Reason)})
	if err != nil {
		return ManagedFile{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ManagedFile{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, replayErr := loadIdempotency[ManagedFile](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); replayErr != nil {
		return ManagedFile{}, false, replayErr
	} else if ok {
		return replay, true, nil
	}
	before, err := managedFileByIDTx(ctx, tx, fileID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedFile{}, false, ErrNotFound
	}
	if err != nil {
		return ManagedFile{}, false, err
	}
	if before.DeletedAt != nil {
		return ManagedFile{}, false, ErrConflict
	}
	switch operation {
	case "quarantine", "rescan":
		if before.ScanState == "quarantined" || before.ScanState == "scanning" {
			return ManagedFile{}, false, ErrConflict
		}
		generation := before.ScanGeneration + 1
		if _, err := tx.ExecContext(ctx, `UPDATE files SET scan_state='quarantined',scan_generation=?,published=0,published_at=NULL,updated_at=? WHERE id=?`, generation, now, fileID); err != nil {
			return ManagedFile{}, false, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO file_scan_attempts (file_id,workspace_id,generation,status) VALUES (?,?,?,'queued')`, fileID, before.WorkspaceID, generation); err != nil {
			return ManagedFile{}, false, err
		}
	case "restore":
		// P17 never marks a file safe. Only the inherited P09 ClamAV worker can
		// produce scan_state='safe'; administrative restore merely republishes it.
		if before.ScanState != "safe" {
			return ManagedFile{}, false, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE files SET published=1,published_at=COALESCE(published_at,?),updated_at=? WHERE id=? AND scan_state='safe'`, now, now, fileID); err != nil {
			return ManagedFile{}, false, err
		}
	case "delete":
		res, err := tx.ExecContext(ctx, `UPDATE files SET deleted_at=?,published=0,published_at=NULL,updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, fileID)
		if err != nil {
			return ManagedFile{}, false, err
		}
		rows, err := res.RowsAffected()
		if err != nil || rows != 1 {
			return ManagedFile{}, false, ErrConflict
		}
		res, err = tx.ExecContext(ctx, `UPDATE file_workspace_counters SET active_count=active_count-1,active_bytes=active_bytes-? WHERE workspace_id=? AND active_count>0 AND active_bytes>=?`, before.SizeBytes, before.WorkspaceID, before.SizeBytes)
		if err != nil {
			return ManagedFile{}, false, err
		}
		rows, err = res.RowsAffected()
		if err != nil || rows != 1 {
			return ManagedFile{}, false, ErrConflict
		}
	case "expiry":
		if _, err := tx.ExecContext(ctx, `UPDATE files SET expires_at=?,updated_at=? WHERE id=?`, expiryValue, now, fileID); err != nil {
			return ManagedFile{}, false, err
		}
	}
	after, err := managedFileByIDTx(ctx, tx, fileID, false)
	if err != nil {
		return ManagedFile{}, false, err
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action,
		ResourceType: "file", ResourceID: fmt.Sprintf("%d", fileID), Result: "success",
		CorrelationID: authority.CorrelationID, Reason: authority.Reason,
		Before:   map[string]any{"scan_state": before.ScanState, "scan_generation": before.ScanGeneration, "published": before.Published, "expires_at": auditOptionalTime(before.ExpiresAt), "deleted": before.DeletedAt != nil},
		After:    map[string]any{"scan_state": after.ScanState, "scan_generation": after.ScanGeneration, "published": after.Published, "expires_at": auditOptionalTime(after.ExpiresAt), "deleted": after.DeletedAt != nil},
		Metadata: map[string]any{"workspace_id": before.WorkspaceID, "clamav_bypass": false}, CreatedAt: now,
	})
	if err != nil {
		return ManagedFile{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, after, auditID, now); err != nil {
		return ManagedFile{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedFile{}, false, err
	}
	return after, false, nil
}

func auditOptionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)
}

func managedFileByID(ctx context.Context, db *sql.DB, fileID uint64) (ManagedFile, error) {
	return scanManagedFile(db.QueryRowContext(ctx, managedFileSelect+` WHERE id=?`, fileID))
}

func managedFileByIDTx(ctx context.Context, tx *sql.Tx, fileID uint64, forUpdate bool) (ManagedFile, error) {
	query := managedFileSelect + ` WHERE id=?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanManagedFile(tx.QueryRowContext(ctx, query, fileID))
}

const managedFileSelect = `SELECT id,workspace_id,original_name,size_bytes,content_sha256,declared_mime,detected_mime,scan_state,scan_generation,published,expires_at,download_limit,download_count,created_at,updated_at,deleted_at FROM files`

func scanManagedFile(row scanner) (ManagedFile, error) {
	var item ManagedFile
	var expiresAt, deletedAt sql.NullTime
	var downloadLimit sql.NullInt64
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.OriginalName, &item.SizeBytes, &item.ContentSHA256, &item.DeclaredMIME, &item.DetectedMIME, &item.ScanState, &item.ScanGeneration, &item.Published, &expiresAt, &downloadLimit, &item.DownloadCount, &item.CreatedAt, &item.UpdatedAt, &deletedAt); err != nil {
		return ManagedFile{}, err
	}
	if expiresAt.Valid {
		v := expiresAt.Time
		item.ExpiresAt = &v
	}
	if downloadLimit.Valid {
		v := uint64(downloadLimit.Int64)
		item.DownloadLimit = &v
	}
	if deletedAt.Valid {
		v := deletedAt.Time
		item.DeletedAt = &v
	}
	return item, nil
}
