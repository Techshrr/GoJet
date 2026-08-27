package trust

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"github.com/Techshrr/GoJet/internal/links"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrInvalid
	}
	return s.db.PingContext(ctx)
}

func (s *Store) EnqueueDestinationScan(ctx context.Context, in EnqueueDestinationScanInput) (EnqueueDestinationScanResult, error) {
	if s == nil || s.db == nil || !validEnqueueInput(in) {
		return EnqueueDestinationScanResult{}, ErrInvalid
	}
	if in.MaxAttempts == 0 {
		in.MaxAttempts = 5
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnqueueDestinationScanResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	workspaceID, primary, routing, variants, storedFingerprint, err := loadLinkRiskSourceTx(ctx, tx, in.LinkID)
	if err != nil {
		return EnqueueDestinationScanResult{}, err
	}
	if workspaceID != strings.TrimSpace(in.WorkspaceID) {
		return EnqueueDestinationScanResult{}, ErrNotFound
	}

	calculatedFingerprint, targetURLs, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return EnqueueDestinationScanResult{}, fmt.Errorf("rebuild destination risk fingerprint: %w", err)
	}
	requestFingerprint := strings.ToLower(strings.TrimSpace(in.RiskFingerprint))
	if storedFingerprint != calculatedFingerprint || requestFingerprint != calculatedFingerprint {
		return EnqueueDestinationScanResult{}, ErrStaleFingerprint
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	res, err := tx.ExecContext(ctx, `
INSERT INTO destination_risk_scans
(workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,attempts,max_attempts,available_at,correlation_id,created_at,updated_at)
VALUES (?,?,?,?,?,?,'queued',0,?,?,?, ?, ?)`,
		workspaceID,
		in.LinkID,
		calculatedFingerprint,
		strings.TrimSpace(in.PolicyVersion),
		string(in.RequestKind),
		strings.TrimSpace(in.IdempotencyKey),
		in.MaxAttempts,
		now,
		strings.TrimSpace(in.CorrelationID),
		now,
		now,
	)
	if err != nil {
		if !mysqlDuplicate(err) {
			return EnqueueDestinationScanResult{}, err
		}
		existing, scanErr := getScanByIdempotencyTx(ctx, tx, workspaceID, strings.TrimSpace(in.IdempotencyKey))
		if scanErr != nil {
			return EnqueueDestinationScanResult{}, scanErr
		}
		if existing.LinkID != in.LinkID ||
			existing.RiskFingerprint != calculatedFingerprint ||
			existing.PolicyVersion != strings.TrimSpace(in.PolicyVersion) ||
			existing.RequestKind != in.RequestKind {
			return EnqueueDestinationScanResult{}, ErrConflict
		}
		targets, targetErr := getScanTargetsTx(ctx, tx, existing.ID)
		if targetErr != nil {
			return EnqueueDestinationScanResult{}, targetErr
		}
		if !sameTargetURLs(targets, targetURLs) {
			return EnqueueDestinationScanResult{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return EnqueueDestinationScanResult{}, err
		}
		return EnqueueDestinationScanResult{Scan: existing, Targets: targets, Created: false}, nil
	}

	scanIDRaw, err := res.LastInsertId()
	if err != nil || scanIDRaw <= 0 {
		if err == nil {
			err = errors.New("destination risk scan insert returned invalid id")
		}
		return EnqueueDestinationScanResult{}, err
	}
	scanID := uint64(scanIDRaw)

	targets := make([]ScanTarget, 0, len(targetURLs))
	for i, normalized := range targetURLs {
		hash := sha256.Sum256([]byte(normalized))
		hashHex := hex.EncodeToString(hash[:])
		order := uint32(i + 1)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO destination_risk_scan_targets
(scan_id,target_order,normalized_url,target_hash,created_at)
VALUES (?,?,?,?,?)`, scanID, order, normalized, hashHex, now); err != nil {
			return EnqueueDestinationScanResult{}, err
		}
		targets = append(targets, ScanTarget{
			ScanID:        scanID,
			Order:         order,
			NormalizedURL: normalized,
			TargetHash:    hashHex,
			CreatedAt:     now,
		})
	}

	metadata := map[string]any{
		"request_kind":       string(in.RequestKind),
		"policy_version":     strings.TrimSpace(in.PolicyVersion),
		"risk_fingerprint":   calculatedFingerprint,
		"target_count":       len(targets),
		"idempotency_replay": false,
	}
	if err := appendRiskAuditTx(
		ctx,
		tx,
		workspaceID,
		&in.LinkID,
		&scanID,
		strings.TrimSpace(in.ActorID),
		"destination-risk.scan-enqueue",
		"success",
		"",
		strings.TrimSpace(in.CorrelationID),
		metadata,
	); err != nil {
		return EnqueueDestinationScanResult{}, err
	}

	scan := DestinationScan{
		ID:              scanID,
		WorkspaceID:     workspaceID,
		LinkID:          in.LinkID,
		RiskFingerprint: calculatedFingerprint,
		PolicyVersion:   strings.TrimSpace(in.PolicyVersion),
		RequestKind:     in.RequestKind,
		IdempotencyKey:  strings.TrimSpace(in.IdempotencyKey),
		Status:          ScanStatusQueued,
		Attempts:        0,
		MaxAttempts:     in.MaxAttempts,
		AvailableAt:     now,
		CorrelationID:   strings.TrimSpace(in.CorrelationID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := tx.Commit(); err != nil {
		return EnqueueDestinationScanResult{}, err
	}
	return EnqueueDestinationScanResult{Scan: scan, Targets: targets, Created: true}, nil
}

func (s *Store) GetDestinationScan(ctx context.Context, workspaceID string, scanID uint64) (DestinationScan, []ScanTarget, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || scanID == 0 {
		return DestinationScan{}, nil, ErrInvalid
	}
	scan, err := scanDestinationScan(s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,
       attempts,max_attempts,available_at,lease_owner,lease_expires_at,correlation_id,last_error_code,
       completed_at,created_at,updated_at
FROM destination_risk_scans
WHERE id=? AND workspace_id=?`, scanID, strings.TrimSpace(workspaceID)))
	if err != nil {
		return DestinationScan{}, nil, err
	}
	targets, err := getScanTargetsQuery(ctx, s.db, scan.ID)
	if err != nil {
		return DestinationScan{}, nil, err
	}
	return scan, targets, nil
}

func loadLinkRiskSourceTx(ctx context.Context, tx *sql.Tx, linkID uint64) (string, string, []links.RoutingRule, []links.ABVariant, string, error) {
	var workspaceID, primary, storedFingerprint string
	var routingRaw, abRaw []byte
	err := tx.QueryRowContext(ctx, `
SELECT workspace_id,primary_destination,routing_json,ab_json,risk_fingerprint
FROM links
WHERE id=? AND deleted_at IS NULL
FOR UPDATE`, linkID).Scan(&workspaceID, &primary, &routingRaw, &abRaw, &storedFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil, nil, "", ErrNotFound
	}
	if err != nil {
		return "", "", nil, nil, "", err
	}
	var routing []links.RoutingRule
	var variants []links.ABVariant
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return "", "", nil, nil, "", fmt.Errorf("decode link routing: %w", err)
	}
	if err := json.Unmarshal(abRaw, &variants); err != nil {
		return "", "", nil, nil, "", fmt.Errorf("decode link A/B: %w", err)
	}
	return workspaceID, primary, routing, variants, strings.ToLower(storedFingerprint), nil
}

func getScanByIdempotencyTx(ctx context.Context, tx *sql.Tx, workspaceID, idempotencyKey string) (DestinationScan, error) {
	return scanDestinationScan(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,
       attempts,max_attempts,available_at,lease_owner,lease_expires_at,correlation_id,last_error_code,
       completed_at,created_at,updated_at
FROM destination_risk_scans
WHERE workspace_id=? AND idempotency_key=?`, workspaceID, idempotencyKey))
}

func getScanTargetsTx(ctx context.Context, tx *sql.Tx, scanID uint64) ([]ScanTarget, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT scan_id,target_order,normalized_url,target_hash,created_at
FROM destination_risk_scan_targets
WHERE scan_id=?
ORDER BY target_order`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTargets(rows)
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func getScanTargetsQuery(ctx context.Context, q queryer, scanID uint64) ([]ScanTarget, error) {
	rows, err := q.QueryContext(ctx, `
SELECT scan_id,target_order,normalized_url,target_hash,created_at
FROM destination_risk_scan_targets
WHERE scan_id=?
ORDER BY target_order`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTargets(rows)
}

func scanTargets(rows *sql.Rows) ([]ScanTarget, error) {
	out := make([]ScanTarget, 0)
	for rows.Next() {
		var target ScanTarget
		if err := rows.Scan(&target.ScanID, &target.Order, &target.NormalizedURL, &target.TargetHash, &target.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDestinationScan(row rowScanner) (DestinationScan, error) {
	var scan DestinationScan
	var requestKind, status string
	var leaseOwner, lastError sql.NullString
	var leaseExpires, completed sql.NullTime
	err := row.Scan(
		&scan.ID,
		&scan.WorkspaceID,
		&scan.LinkID,
		&scan.RiskFingerprint,
		&scan.PolicyVersion,
		&requestKind,
		&scan.IdempotencyKey,
		&status,
		&scan.Attempts,
		&scan.MaxAttempts,
		&scan.AvailableAt,
		&leaseOwner,
		&leaseExpires,
		&scan.CorrelationID,
		&lastError,
		&completed,
		&scan.CreatedAt,
		&scan.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DestinationScan{}, ErrNotFound
	}
	if err != nil {
		return DestinationScan{}, err
	}
	scan.RequestKind = ScanRequestKind(requestKind)
	scan.Status = ScanStatus(status)
	if leaseOwner.Valid {
		scan.LeaseOwner = leaseOwner.String
	}
	if leaseExpires.Valid {
		v := leaseExpires.Time
		scan.LeaseExpiresAt = &v
	}
	if lastError.Valid {
		scan.LastErrorCode = lastError.String
	}
	if completed.Valid {
		v := completed.Time
		scan.CompletedAt = &v
	}
	return scan, nil
}

func appendRiskAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	linkID *uint64,
	scanID *uint64,
	actorID string,
	action string,
	result string,
	reason string,
	correlationID string,
	metadata map[string]any,
) error {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	action = strings.TrimSpace(action)
	correlationID = strings.TrimSpace(correlationID)
	if workspaceID == "" || actorID == "" || action == "" || correlationID == "" {
		return ErrInvalid
	}
	switch result {
	case "success", "denied", "conflict", "failed":
	default:
		return ErrInvalid
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO destination_risk_audit_events
(workspace_id,link_id,scan_id,actor_id,action,result,reason,correlation_id,metadata_json)
VALUES (?,?,?,?,?,?,NULLIF(?,''),?,?)`,
		workspaceID,
		linkID,
		scanID,
		actorID,
		action,
		result,
		strings.TrimSpace(reason),
		correlationID,
		string(raw),
	)
	return err
}

func validEnqueueInput(in EnqueueDestinationScanInput) bool {
	workspace := strings.TrimSpace(in.WorkspaceID)
	policy := strings.TrimSpace(in.PolicyVersion)
	idempotency := strings.TrimSpace(in.IdempotencyKey)
	correlation := strings.TrimSpace(in.CorrelationID)
	actor := strings.TrimSpace(in.ActorID)
	if workspace == "" || len(workspace) > 64 || in.LinkID == 0 || !validFingerprint(in.RiskFingerprint) {
		return false
	}
	if policy == "" || len(policy) > 64 || idempotency == "" || len(idempotency) > 128 || correlation == "" || len(correlation) > 128 || actor == "" || len(actor) > 128 {
		return false
	}
	if in.RequestKind != ScanRequestInitial && in.RequestKind != ScanRequestRescan {
		return false
	}
	return in.MaxAttempts <= 20
}

func validFingerprint(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == sha256.Size
}

func sameTargetURLs(existing []ScanTarget, expected []string) bool {
	if len(existing) != len(expected) {
		return false
	}
	for i := range expected {
		if existing[i].Order != uint32(i+1) || existing[i].NormalizedURL != expected[i] {
			return false
		}
	}
	return true
}

func mysqlDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
