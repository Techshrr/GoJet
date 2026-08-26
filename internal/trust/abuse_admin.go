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
)

const abuseAdminTransitionAction = "abuse.admin-transition"

type AbuseAdminTransitionInput struct {
	ReportID        uint64
	ExpectedVersion uint64
	ToStatus        AbuseStatus
	Reason          string
	ActorID         string
	CorrelationID   string
	IdempotencyKey  string
}

type AbuseAdminTransitionResult struct {
	Report  AbuseReport
	Changed bool
}

type abuseEventIdempotency struct {
	RequestFingerprint string
	ToStatus           AbuseStatus
	Result             string
}

func (s *Store) TransitionAbuseReport(ctx context.Context, input AbuseAdminTransitionInput, authorizer PermissionAuthorizer) (AbuseAdminTransitionResult, error) {
	input = normalizeAbuseAdminTransitionInput(input)
	if s == nil || s.db == nil || authorizer == nil || !validAbuseAdminTransitionInput(input) {
		return AbuseAdminTransitionResult{}, ErrInvalid
	}
	idempotencyHash := abuseAdminIdempotencyHash(input.IdempotencyKey)
	requestFingerprint := abuseAdminTransitionFingerprint(input)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return AbuseAdminTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	report, err := loadAbuseByIDForUpdate(ctx, tx, input.ReportID)
	if err != nil {
		return AbuseAdminTransitionResult{}, err
	}

	if existing, eventErr := loadAbuseEventIdempotency(ctx, tx, report.ID, abuseAdminTransitionAction, idempotencyHash); eventErr == nil {
		if existing.RequestFingerprint != requestFingerprint || existing.ToStatus != input.ToStatus || existing.Result != "success" {
			if auditErr := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, abuseAdminTransitionAction, &report.Status, &input.ToStatus, "conflict", "idempotency-conflict", input.CorrelationID, "", requestFingerprint, map[string]any{
				"expected_version": input.ExpectedVersion,
				"requested_status": string(input.ToStatus),
			}); auditErr != nil {
				return AbuseAdminTransitionResult{}, auditErr
			}
			if err := tx.Commit(); err != nil {
				return AbuseAdminTransitionResult{}, err
			}
			return AbuseAdminTransitionResult{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return AbuseAdminTransitionResult{}, err
		}
		return AbuseAdminTransitionResult{Report: report, Changed: false}, nil
	} else if !errors.Is(eventErr, ErrNotFound) {
		return AbuseAdminTransitionResult{}, eventErr
	}

	if err := authorizer.Authorize(ctx, input.ActorID, SecurityManagePermission); err != nil {
		if auditErr := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, abuseAdminTransitionAction, &report.Status, nil, "denied", "permission-denied", input.CorrelationID, "", requestFingerprint, map[string]any{
			"requested_status": string(input.ToStatus),
			"expected_version": input.ExpectedVersion,
		}); auditErr != nil {
			return AbuseAdminTransitionResult{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return AbuseAdminTransitionResult{}, err
		}
		return AbuseAdminTransitionResult{}, ErrUnauthorized
	}

	if report.Version != input.ExpectedVersion || !validAbuseStatusTransition(report.Status, input.ToStatus) {
		if auditErr := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, abuseAdminTransitionAction, &report.Status, &input.ToStatus, "conflict", "state-conflict", input.CorrelationID, "", requestFingerprint, map[string]any{
			"actual_version":   report.Version,
			"expected_version": input.ExpectedVersion,
		}); auditErr != nil {
			return AbuseAdminTransitionResult{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return AbuseAdminTransitionResult{}, err
		}
		return AbuseAdminTransitionResult{}, ErrConflict
	}

	previous := report.Status
	res, err := tx.ExecContext(ctx, `
UPDATE abuse_reports
SET status=?,version=version+1,updated_at=CURRENT_TIMESTAMP(6)
WHERE id=? AND version=? AND status=?`, string(input.ToStatus), report.ID, input.ExpectedVersion, string(previous))
	if err != nil {
		return AbuseAdminTransitionResult{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return AbuseAdminTransitionResult{}, ErrConflict
	}

	if err := appendAbuseReportEventTx(ctx, tx, report, input.ActorID, abuseAdminTransitionAction, &previous, &input.ToStatus, "success", abuseTransitionReasonCategory(input.ToStatus), input.CorrelationID, idempotencyHash, requestFingerprint, map[string]any{
		"expected_version":  input.ExpectedVersion,
		"resulting_version": input.ExpectedVersion + 1,
		"reason_redacted":   SanitizeAbuseDetails(input.Reason),
	}); err != nil {
		if mysqlDuplicate(err) {
			return AbuseAdminTransitionResult{}, ErrConflict
		}
		return AbuseAdminTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AbuseAdminTransitionResult{}, err
	}
	updated, err := loadAbuseByID(ctx, s.db, report.ID)
	if err != nil {
		return AbuseAdminTransitionResult{}, err
	}
	return AbuseAdminTransitionResult{Report: updated, Changed: true}, nil
}

func (s *Store) GetAbuseReport(ctx context.Context, reportID uint64) (AbuseReport, error) {
	if s == nil || s.db == nil || reportID == 0 {
		return AbuseReport{}, ErrInvalid
	}
	return loadAbuseByID(ctx, s.db, reportID)
}

func loadAbuseByIDForUpdate(ctx context.Context, tx *sql.Tx, id uint64) (AbuseReport, error) {
	return scanAbuse(tx.QueryRowContext(ctx, abuseSelect+` WHERE id=? FOR UPDATE`, id))
}

func loadAbuseEventIdempotency(ctx context.Context, tx *sql.Tx, reportID uint64, action, idempotencyHash string) (abuseEventIdempotency, error) {
	var event abuseEventIdempotency
	var toStatus sql.NullString
	err := tx.QueryRowContext(ctx, `
SELECT COALESCE(request_fingerprint,''),to_status,result
FROM abuse_report_events
WHERE report_id=? AND action=? AND idempotency_key_hash=?
ORDER BY id DESC
LIMIT 1`, reportID, action, idempotencyHash).Scan(&event.RequestFingerprint, &toStatus, &event.Result)
	if errors.Is(err, sql.ErrNoRows) {
		return abuseEventIdempotency{}, ErrNotFound
	}
	if err != nil {
		return abuseEventIdempotency{}, err
	}
	if toStatus.Valid {
		event.ToStatus = AbuseStatus(toStatus.String)
	}
	return event, nil
}

func appendAbuseReportEventTx(
	ctx context.Context,
	tx *sql.Tx,
	report AbuseReport,
	actorID, action string,
	fromStatus, toStatus *AbuseStatus,
	result, reasonCategory, correlationID, idempotencyHash, requestFingerprint string,
	metadata map[string]any,
) error {
	actorID = strings.TrimSpace(actorID)
	action = strings.TrimSpace(action)
	reasonCategory = strings.TrimSpace(reasonCategory)
	correlationID = strings.TrimSpace(correlationID)
	if report.ID == 0 || report.WorkspaceID == "" || actorID == "" || action == "" || reasonCategory == "" || correlationID == "" {
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
	var fromValue, toValue any
	if fromStatus != nil {
		fromValue = string(*fromStatus)
	}
	if toStatus != nil {
		toValue = string(*toStatus)
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO abuse_report_events
(report_id,workspace_id,actor_id,action,from_status,to_status,result,reason_category,correlation_id,idempotency_key_hash,request_fingerprint,metadata_json)
VALUES (?,?,?,?,?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?)`, report.ID, report.WorkspaceID, actorID, action, fromValue, toValue, result, reasonCategory, correlationID, idempotencyHash, requestFingerprint, string(raw))
	return err
}

func normalizeAbuseAdminTransitionInput(input AbuseAdminTransitionInput) AbuseAdminTransitionInput {
	input.Reason = strings.TrimSpace(input.Reason)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	return input
}

func validAbuseAdminTransitionInput(input AbuseAdminTransitionInput) bool {
	if input.ReportID == 0 || input.ExpectedVersion == 0 || !validAbuseStatus(input.ToStatus) || input.Reason == "" || len(input.Reason) > 500 || input.ActorID == "" || len(input.ActorID) > 128 || input.CorrelationID == "" || len(input.CorrelationID) > 128 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 {
		return false
	}
	return input.ToStatus != AbuseOpen
}

func validAbuseStatus(status AbuseStatus) bool {
	switch status {
	case AbuseOpen, AbuseInvestigating, AbuseResolved, AbuseDismissed:
		return true
	default:
		return false
	}
}

func validAbuseStatusTransition(from, to AbuseStatus) bool {
	switch from {
	case AbuseOpen:
		return to == AbuseInvestigating || to == AbuseDismissed
	case AbuseInvestigating:
		return to == AbuseResolved || to == AbuseDismissed
	default:
		return false
	}
}

func abuseTransitionReasonCategory(status AbuseStatus) string {
	switch status {
	case AbuseInvestigating:
		return "review-started"
	case AbuseResolved:
		return "report-resolved"
	case AbuseDismissed:
		return "report-dismissed"
	default:
		return "state-transition"
	}
}

func abuseAdminIdempotencyHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func abuseAdminTransitionFingerprint(input AbuseAdminTransitionInput) string {
	canonical := fmt.Sprintf("%d\n%d\n%s\n%s", input.ReportID, input.ExpectedVersion, input.ToStatus, SanitizeAbuseDetails(input.Reason))
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
