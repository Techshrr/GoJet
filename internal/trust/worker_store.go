package trust

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type LeaseDestinationScanResult struct {
	Scan      DestinationScan
	Targets   []ScanTarget
	Recovered bool
}

func (s *Store) LeaseDestinationScan(ctx context.Context, workerID string, now time.Time, leaseTTL time.Duration) (LeaseDestinationScanResult, error) {
	workerID = strings.TrimSpace(workerID)
	if s == nil || s.db == nil || workerID == "" || len(workerID) > 128 || leaseTTL < time.Second || leaseTTL > 15*time.Minute {
		return LeaseDestinationScanResult{}, ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LeaseDestinationScanResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	scan, err := scanDestinationScan(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,
       attempts,max_attempts,available_at,lease_owner,lease_expires_at,correlation_id,last_error_code,
       completed_at,created_at,updated_at
FROM destination_risk_scans
WHERE (
    (status IN ('queued','retry') AND available_at <= ? AND attempts < max_attempts)
 OR (status='leased' AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?)
)
ORDER BY available_at,id
LIMIT 1
FOR UPDATE SKIP LOCKED`, now, now))
	if err != nil {
		return LeaseDestinationScanResult{}, err
	}

	recovered := scan.Status == ScanStatusLeased
	if recovered && scan.Attempts >= scan.MaxAttempts {
		if _, err := tx.ExecContext(ctx, `
UPDATE destination_risk_scans
SET status='failed',lease_owner=NULL,lease_expires_at=NULL,last_error_code='lease-exhausted',completed_at=?,updated_at=?
WHERE id=? AND status='leased'`, now, now, scan.ID); err != nil {
			return LeaseDestinationScanResult{}, err
		}
		if err := appendRiskAuditTx(ctx, tx, scan.WorkspaceID, &scan.LinkID, &scan.ID, workerID,
			"destination-risk.scan-lease-exhausted", "failed", "lease-exhausted", scan.CorrelationID,
			map[string]any{"attempts": scan.Attempts, "max_attempts": scan.MaxAttempts}); err != nil {
			return LeaseDestinationScanResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return LeaseDestinationScanResult{}, err
		}
		return LeaseDestinationScanResult{}, ErrNotFound
	}

	leaseExpires := now.Add(leaseTTL).UTC().Truncate(time.Microsecond)
	newAttempts := scan.Attempts + 1
	res, err := tx.ExecContext(ctx, `
UPDATE destination_risk_scans
SET status='leased',attempts=?,lease_owner=?,lease_expires_at=?,last_error_code=NULL,updated_at=?
WHERE id=? AND status=? AND attempts=?`, newAttempts, workerID, leaseExpires, now, scan.ID, string(scan.Status), scan.Attempts)
	if err != nil {
		return LeaseDestinationScanResult{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return LeaseDestinationScanResult{}, ErrConflict
	}
	targets, err := getScanTargetsTx(ctx, tx, scan.ID)
	if err != nil {
		return LeaseDestinationScanResult{}, err
	}
	action := "destination-risk.scan-lease"
	if recovered {
		action = "destination-risk.scan-recover"
	}
	if err := appendRiskAuditTx(ctx, tx, scan.WorkspaceID, &scan.LinkID, &scan.ID, workerID, action, "success", "", scan.CorrelationID,
		map[string]any{"attempt": newAttempts, "lease_seconds": int64(leaseTTL / time.Second), "recovered": recovered}); err != nil {
		return LeaseDestinationScanResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LeaseDestinationScanResult{}, err
	}
	scan.Status = ScanStatusLeased
	scan.Attempts = newAttempts
	scan.LeaseOwner = workerID
	scan.LeaseExpiresAt = &leaseExpires
	scan.LastErrorCode = ""
	scan.UpdatedAt = now
	return LeaseDestinationScanResult{Scan: scan, Targets: targets, Recovered: recovered}, nil
}

func (s *Store) ReleaseDestinationScanForRetry(ctx context.Context, workspaceID string, scanID uint64, workerID, errorCode string, now time.Time, delay time.Duration) (DestinationScan, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	workerID = strings.TrimSpace(workerID)
	errorCode = normalizeSignalCode(errorCode)
	if s == nil || s.db == nil || workspaceID == "" || scanID == 0 || workerID == "" || errorCode == "" || delay < 0 || delay > time.Hour {
		return DestinationScan{}, ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DestinationScan{}, err
	}
	defer func() { _ = tx.Rollback() }()
	scan, err := getDestinationScanForUpdateTx(ctx, tx, workspaceID, scanID)
	if err != nil {
		return DestinationScan{}, err
	}
	if scan.Status == ScanStatusCompleted {
		if err := tx.Commit(); err != nil {
			return DestinationScan{}, err
		}
		return scan, nil
	}
	if scan.Status != ScanStatusLeased || scan.LeaseOwner != workerID {
		return DestinationScan{}, ErrConflict
	}

	status := ScanStatusRetry
	availableAt := now.Add(delay).UTC().Truncate(time.Microsecond)
	var completedAt any
	action := "destination-risk.scan-retry"
	result := "success"
	if scan.Attempts >= scan.MaxAttempts {
		status = ScanStatusFailed
		availableAt = now
		completedAt = now
		action = "destination-risk.scan-failed"
		result = "failed"
	}
	_, err = tx.ExecContext(ctx, `
UPDATE destination_risk_scans
SET status=?,available_at=?,lease_owner=NULL,lease_expires_at=NULL,last_error_code=?,completed_at=?,updated_at=?
WHERE id=? AND workspace_id=? AND status='leased' AND lease_owner=?`,
		string(status), availableAt, errorCode, completedAt, now, scan.ID, scan.WorkspaceID, workerID)
	if err != nil {
		return DestinationScan{}, err
	}
	if err := appendRiskAuditTx(ctx, tx, scan.WorkspaceID, &scan.LinkID, &scan.ID, workerID, action, result, errorCode, scan.CorrelationID,
		map[string]any{"attempts": scan.Attempts, "max_attempts": scan.MaxAttempts, "retry_delay_ms": delay.Milliseconds()}); err != nil {
		return DestinationScan{}, err
	}
	if err := tx.Commit(); err != nil {
		return DestinationScan{}, err
	}
	scan.Status = status
	scan.AvailableAt = availableAt
	scan.LeaseOwner = ""
	scan.LeaseExpiresAt = nil
	scan.LastErrorCode = errorCode
	scan.UpdatedAt = now
	if status == ScanStatusFailed {
		v := now
		scan.CompletedAt = &v
	}
	return scan, nil
}

func (s *Store) GetDestinationScanState(ctx context.Context, workspaceID string, scanID uint64) (DestinationScan, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || scanID == 0 {
		return DestinationScan{}, ErrInvalid
	}
	scan, err := scanDestinationScan(s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,
       attempts,max_attempts,available_at,lease_owner,lease_expires_at,correlation_id,last_error_code,
       completed_at,created_at,updated_at
FROM destination_risk_scans WHERE id=? AND workspace_id=?`, scanID, strings.TrimSpace(workspaceID)))
	if errors.Is(err, sql.ErrNoRows) {
		return DestinationScan{}, ErrNotFound
	}
	return scan, err
}
