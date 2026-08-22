package files

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("file store unavailable")
	}
	return s.db.PingContext(ctx)
}

func newClaimToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func (s *Store) ClaimNextScan(ctx context.Context, workerID string) (ScanJob, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workerID) == "" {
		return ScanJob{}, ErrInvalidInput
	}
	workerID = strings.TrimSpace(workerID)
	if len(workerID) > 128 {
		return ScanJob{}, ErrInvalidInput
	}
	token, err := newClaimToken()
	if err != nil {
		return ScanJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ScanJob{}, err
	}
	defer tx.Rollback()

	var job ScanJob
	err = tx.QueryRowContext(ctx, `
SELECT a.id, a.file_id, a.workspace_id, a.generation, f.storage_key, f.size_bytes
FROM file_scan_attempts a
JOIN files f ON f.id = a.file_id
WHERE a.status = 'queued'
  AND f.deleted_at IS NULL
  AND f.scan_generation = a.generation
  AND f.scan_state = 'quarantined'
ORDER BY a.queued_at, a.id
LIMIT 1
FOR UPDATE SKIP LOCKED`).Scan(&job.AttemptID, &job.FileID, &job.WorkspaceID, &job.Generation, &job.StorageKey, &job.SizeBytes)
	if errors.Is(err, sql.ErrNoRows) {
		return ScanJob{}, ErrNoScanJobs
	}
	if err != nil {
		return ScanJob{}, err
	}
	job.ClaimToken = token

	result, err := tx.ExecContext(ctx, `
UPDATE file_scan_attempts
SET status='processing', claim_token=?, worker_id=?, claimed_at=CURRENT_TIMESTAMP(6), completed_at=NULL
WHERE id=? AND file_id=? AND generation=? AND status='queued'`, token, workerID, job.AttemptID, job.FileID, job.Generation)
	if err != nil {
		return ScanJob{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ScanJob{}, err
	}
	if affected != 1 {
		return ScanJob{}, ErrScanClaimConflict
	}
	result, err = tx.ExecContext(ctx, `
UPDATE files
SET scan_state='scanning', published=0, published_at=NULL, updated_at=CURRENT_TIMESTAMP(6)
WHERE id=? AND workspace_id=? AND scan_generation=? AND scan_state='quarantined' AND deleted_at IS NULL`, job.FileID, job.WorkspaceID, job.Generation)
	if err != nil {
		return ScanJob{}, err
	}
	affected, err = result.RowsAffected()
	if err != nil {
		return ScanJob{}, err
	}
	if affected != 1 {
		return ScanJob{}, ErrScanClaimConflict
	}
	if err := tx.Commit(); err != nil {
		return ScanJob{}, err
	}
	return job, nil
}

func (s *Store) CompleteScan(ctx context.Context, job ScanJob, result ScanResult) error {
	if s == nil || s.db == nil || job.AttemptID == 0 || job.FileID == 0 || job.Generation == 0 || !validStorageKey(job.ClaimToken) {
		return ErrInvalidInput
	}
	attemptStatus := "error"
	fileState := ScanError
	switch result.Verdict {
	case VerdictClean:
		attemptStatus = "clean"
		fileState = ScanSafe
	case VerdictInfected:
		attemptStatus = "infected"
		fileState = ScanBlocked
	case VerdictError:
		attemptStatus = "error"
		fileState = ScanError
	default:
		return ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var attemptFileID, attemptGeneration uint64
	var attemptWorkspace, status, claimToken string
	err = tx.QueryRowContext(ctx, `SELECT file_id, workspace_id, generation, status, COALESCE(claim_token,'') FROM file_scan_attempts WHERE id=? FOR UPDATE`, job.AttemptID).
		Scan(&attemptFileID, &attemptWorkspace, &attemptGeneration, &status, &claimToken)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if attemptFileID != job.FileID || attemptWorkspace != job.WorkspaceID || attemptGeneration != job.Generation || status != "processing" || claimToken != job.ClaimToken {
		return ErrScanClaimConflict
	}
	var currentGeneration uint64
	var currentState string
	var deletedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT scan_generation, scan_state, deleted_at FROM files WHERE id=? AND workspace_id=? FOR UPDATE`, job.FileID, job.WorkspaceID).
		Scan(&currentGeneration, &currentState, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if deletedAt.Valid || currentGeneration != job.Generation || currentState != string(ScanScanning) {
		return ErrScanClaimConflict
	}

	var signatureDate any
	if result.SignatureDate != nil {
		signatureDate = result.SignatureDate.UTC()
	}
	_, err = tx.ExecContext(ctx, `
UPDATE file_scan_attempts
SET status=?, engine_version=NULLIF(?,''), signature_version=NULLIF(?,''), signature_date=?, verdict_code=NULLIF(?,''), reason=NULLIF(?,''), error_code=NULLIF(?,''), completed_at=CURRENT_TIMESTAMP(6)
WHERE id=?`, attemptStatus, result.EngineVersion, result.SignatureVersion, signatureDate, result.VerdictCode, result.Reason, result.ErrorCode, job.AttemptID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
UPDATE files
SET scan_state=?, published=0, published_at=NULL, updated_at=CURRENT_TIMESTAMP(6)
WHERE id=? AND workspace_id=? AND scan_generation=?`, string(fileState), job.FileID, job.WorkspaceID, job.Generation)
	if err != nil {
		return err
	}
	metadata := map[string]any{
		"attempt_id":   job.AttemptID,
		"generation":   job.Generation,
		"verdict":      string(result.Verdict),
		"error_code":   result.ErrorCode,
		"verdict_code": result.VerdictCode,
	}
	if err := appendAuditTx(ctx, tx, job.WorkspaceID, &job.FileID, "system:fileworker", fmt.Sprintf("p09-scan-%d", job.AttemptID), "file.scan.complete", "ClamAV scan completion", "success", metadata); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecoverStaleClaims(ctx context.Context, staleBefore time.Time) (int64, error) {
	if s == nil || s.db == nil || staleBefore.IsZero() {
		return 0, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
SELECT a.id, a.file_id, a.workspace_id, a.generation
FROM file_scan_attempts a
JOIN files f ON f.id=a.file_id
WHERE a.status='processing' AND a.claimed_at < ?
  AND f.deleted_at IS NULL AND f.scan_generation=a.generation AND f.scan_state='scanning'
ORDER BY a.claimed_at, a.id
FOR UPDATE`, staleBefore.UTC())
	if err != nil {
		return 0, err
	}
	type stale struct {
		attemptID, fileID, generation uint64
		workspace                     string
	}
	var items []stale
	for rows.Next() {
		var item stale
		if err := rows.Scan(&item.attemptID, &item.fileID, &item.workspace, &item.generation); err != nil {
			_ = rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE file_scan_attempts SET status='queued', claim_token=NULL, worker_id=NULL, claimed_at=NULL WHERE id=? AND status='processing'`, item.attemptID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE files SET scan_state='quarantined', published=0, published_at=NULL, updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND workspace_id=? AND scan_generation=? AND scan_state='scanning'`, item.fileID, item.workspace, item.generation); err != nil {
			return 0, err
		}
		fileID := item.fileID
		if err := appendAuditTx(ctx, tx, item.workspace, &fileID, "system:fileworker", fmt.Sprintf("p09-recover-%d", item.attemptID), "file.scan.recover", "Recover stale interrupted scan claim", "success", map[string]any{"attempt_id": item.attemptID, "generation": item.generation}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int64(len(items)), nil
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, fileID *uint64, actorID, correlationID, action, reason, result string, metadata map[string]any) error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(action) == "" {
		return ErrInvalidInput
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO file_audit_events (workspace_id, file_id, actor_id, action, request_correlation_id, reason, result, metadata_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, workspaceID, fileID, actorID, correlationID, action, reason, result, string(raw))
	return err
}
