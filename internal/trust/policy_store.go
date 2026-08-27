package trust

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

type RecordProviderObservationInput struct {
	WorkspaceID   string
	ScanID        uint64
	Observation   ProviderObservation
	ActorID       string
	CorrelationID string
}

type FinalizeDestinationDecisionInput struct {
	WorkspaceID       string
	ScanID            uint64
	Policy            DestinationPolicy
	LocalSafetyPassed bool
	ActorID           string
	CorrelationID     string
}

func (s *Store) RecordProviderObservation(ctx context.Context, in RecordProviderObservationInput) (ProviderObservation, error) {
	if s == nil || s.db == nil || strings.TrimSpace(in.WorkspaceID) == "" || in.ScanID == 0 || strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.CorrelationID) == "" {
		return ProviderObservation{}, ErrInvalid
	}
	observation := in.Observation
	if observation.ScanID != 0 && observation.ScanID != in.ScanID {
		return ProviderObservation{}, ErrInvalid
	}
	observation.ScanID = in.ScanID
	observation.Provider = strings.TrimSpace(observation.Provider)
	observation.SignalCode = normalizeSignalCode(observation.SignalCode)
	if !validProviderName(observation.Provider) || observation.SignalCode == "" || !validProviderOutcome(observation.Outcome) {
		return ProviderObservation{}, ErrInvalid
	}
	if observation.ObservedAt.IsZero() {
		observation.ObservedAt = time.Now().UTC().Truncate(time.Microsecond)
	} else {
		observation.ObservedAt = observation.ObservedAt.UTC().Truncate(time.Microsecond)
	}
	observation.Evidence = SanitizeProviderEvidence(observation.Evidence)
	evidenceRaw, err := json.Marshal(observation.Evidence)
	if err != nil {
		return ProviderObservation{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ProviderObservation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	scan, err := getDestinationScanForUpdateTx(ctx, tx, strings.TrimSpace(in.WorkspaceID), in.ScanID)
	if err != nil {
		return ProviderObservation{}, err
	}
	if scan.CorrelationID != strings.TrimSpace(in.CorrelationID) || scan.Status == ScanStatusCompleted || scan.Status == ScanStatusFailed {
		return ProviderObservation{}, ErrConflict
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	res, err := tx.ExecContext(ctx, `
INSERT INTO destination_risk_provider_observations
(scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at)
VALUES (?,?,?,?,?,?,?)`,
		in.ScanID,
		observation.Provider,
		string(observation.Outcome),
		observation.SignalCode,
		string(evidenceRaw),
		observation.ObservedAt,
		now,
	)
	if err != nil {
		if !mysqlDuplicate(err) {
			return ProviderObservation{}, err
		}
		existing, existingErr := getProviderObservationTx(ctx, tx, in.ScanID, observation.Provider)
		if existingErr != nil {
			return ProviderObservation{}, existingErr
		}
		if existing.Outcome != observation.Outcome || existing.SignalCode != observation.SignalCode || !reflect.DeepEqual(existing.Evidence, observation.Evidence) {
			return ProviderObservation{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return ProviderObservation{}, err
		}
		return existing, nil
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		return ProviderObservation{}, ErrConflict
	}
	observation.ID = uint64(id)
	observation.CreatedAt = now

	metadata := map[string]any{
		"provider":       observation.Provider,
		"outcome":        string(observation.Outcome),
		"signal_code":    observation.SignalCode,
		"policy_version": scan.PolicyVersion,
	}
	if err := appendRiskAuditTx(
		ctx,
		tx,
		scan.WorkspaceID,
		&scan.LinkID,
		&scan.ID,
		strings.TrimSpace(in.ActorID),
		"destination-risk.provider-observation",
		"success",
		"",
		scan.CorrelationID,
		metadata,
	); err != nil {
		return ProviderObservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderObservation{}, err
	}
	return observation, nil
}

func (s *Store) FinalizeDestinationDecision(ctx context.Context, in FinalizeDestinationDecisionInput) (DestinationDecision, error) {
	if s == nil || s.db == nil || strings.TrimSpace(in.WorkspaceID) == "" || in.ScanID == 0 || strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.CorrelationID) == "" || !in.Policy.Validate() {
		return DestinationDecision{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DestinationDecision{}, err
	}
	defer func() { _ = tx.Rollback() }()

	scan, err := getDestinationScanForUpdateTx(ctx, tx, strings.TrimSpace(in.WorkspaceID), in.ScanID)
	if err != nil {
		return DestinationDecision{}, err
	}
	if scan.CorrelationID != strings.TrimSpace(in.CorrelationID) {
		return DestinationDecision{}, ErrConflict
	}
	if scan.PolicyVersion != strings.TrimSpace(in.Policy.Version) {
		return DestinationDecision{}, ErrPolicyMismatch
	}
	observations, err := getProviderObservationsTx(ctx, tx, scan.ID)
	if err != nil {
		return DestinationDecision{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	evaluation, err := EvaluateDestinationPolicy(in.Policy, observations, in.LocalSafetyPassed, now)
	if err != nil {
		return DestinationDecision{}, err
	}
	if evaluation.State == DecisionPending {
		return DestinationDecision{}, ErrConflict
	}
	metadataRaw, err := json.Marshal(evaluation.Metadata)
	if err != nil {
		return DestinationDecision{}, err
	}

	res, err := tx.ExecContext(ctx, `
INSERT INTO destination_risk_decisions
(workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,decision_metadata_json,valid_until,decided_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		scan.WorkspaceID,
		scan.LinkID,
		scan.ID,
		scan.RiskFingerprint,
		scan.PolicyVersion,
		string(evaluation.State),
		evaluation.ReasonCategory,
		string(metadataRaw),
		evaluation.ValidUntil,
		now,
		now,
	)
	if err != nil {
		if !mysqlDuplicate(err) {
			return DestinationDecision{}, err
		}
		existing, existingErr := getDestinationDecisionTx(ctx, tx, scan.ID)
		if existingErr != nil {
			return DestinationDecision{}, existingErr
		}
		if existing.State != evaluation.State || existing.ReasonCategory != evaluation.ReasonCategory || existing.RiskFingerprint != scan.RiskFingerprint || existing.PolicyVersion != scan.PolicyVersion {
			return DestinationDecision{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return DestinationDecision{}, err
		}
		return existing, nil
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		return DestinationDecision{}, ErrConflict
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE destination_risk_scans
SET status='completed', completed_at=?, lease_owner=NULL, lease_expires_at=NULL, last_error_code=NULL, updated_at=?
WHERE id=? AND workspace_id=? AND status NOT IN ('completed','failed')`, now, now, scan.ID, scan.WorkspaceID); err != nil {
		return DestinationDecision{}, err
	}
	metadata := map[string]any{
		"decision_state":  string(evaluation.State),
		"reason_category": evaluation.ReasonCategory,
		"policy_version":  scan.PolicyVersion,
		"provider_count":  len(observations),
		"local_safety":    in.LocalSafetyPassed,
	}
	if err := appendRiskAuditTx(
		ctx,
		tx,
		scan.WorkspaceID,
		&scan.LinkID,
		&scan.ID,
		strings.TrimSpace(in.ActorID),
		"destination-risk.policy-decision",
		"success",
		evaluation.ReasonCategory,
		scan.CorrelationID,
		metadata,
	); err != nil {
		return DestinationDecision{}, err
	}

	decision := DestinationDecision{
		ID:               uint64(id),
		WorkspaceID:      scan.WorkspaceID,
		LinkID:           scan.LinkID,
		ScanID:           scan.ID,
		RiskFingerprint:  scan.RiskFingerprint,
		PolicyVersion:    scan.PolicyVersion,
		State:            evaluation.State,
		ReasonCategory:   evaluation.ReasonCategory,
		DecisionMetadata: evaluation.Metadata,
		ValidUntil:       evaluation.ValidUntil,
		DecidedAt:        now,
		CreatedAt:        now,
	}
	if err := tx.Commit(); err != nil {
		return DestinationDecision{}, err
	}
	return decision, nil
}

func (s *Store) GetProviderObservations(ctx context.Context, workspaceID string, scanID uint64) ([]ProviderObservation, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || scanID == 0 {
		return nil, ErrInvalid
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM destination_risk_scans WHERE id=? AND workspace_id=?`, scanID, strings.TrimSpace(workspaceID)).Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, ErrNotFound
	}
	return getProviderObservationsQuery(ctx, s.db, scanID)
}

func (s *Store) GetDestinationDecision(ctx context.Context, workspaceID string, scanID uint64) (DestinationDecision, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || scanID == 0 {
		return DestinationDecision{}, ErrInvalid
	}
	return scanDestinationDecision(s.db.QueryRowContext(ctx, `
SELECT d.id,d.workspace_id,d.link_id,d.scan_id,d.risk_fingerprint,d.policy_version,d.state,d.reason_category,
       d.decision_metadata_json,d.valid_until,d.decided_at,d.created_at
FROM destination_risk_decisions d
JOIN destination_risk_scans s ON s.id=d.scan_id
WHERE d.scan_id=? AND s.workspace_id=?`, scanID, strings.TrimSpace(workspaceID)))
}

func getDestinationScanForUpdateTx(ctx context.Context, tx *sql.Tx, workspaceID string, scanID uint64) (DestinationScan, error) {
	return scanDestinationScan(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,
       attempts,max_attempts,available_at,lease_owner,lease_expires_at,correlation_id,last_error_code,
       completed_at,created_at,updated_at
FROM destination_risk_scans
WHERE id=? AND workspace_id=?
FOR UPDATE`, scanID, workspaceID))
}

func getProviderObservationTx(ctx context.Context, tx *sql.Tx, scanID uint64, provider string) (ProviderObservation, error) {
	return scanProviderObservation(tx.QueryRowContext(ctx, `
SELECT id,scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at
FROM destination_risk_provider_observations
WHERE scan_id=? AND provider=?`, scanID, provider))
}

func getProviderObservationsTx(ctx context.Context, tx *sql.Tx, scanID uint64) ([]ProviderObservation, error) {
	return getProviderObservationsQuery(ctx, tx, scanID)
}

func getProviderObservationsQuery(ctx context.Context, q queryer, scanID uint64) ([]ProviderObservation, error) {
	rows, err := q.QueryContext(ctx, `
SELECT id,scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at
FROM destination_risk_provider_observations
WHERE scan_id=?
ORDER BY provider,id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProviderObservation, 0)
	for rows.Next() {
		observation, err := scanProviderObservation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, observation)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanProviderObservation(row rowScanner) (ProviderObservation, error) {
	var observation ProviderObservation
	var outcome string
	var evidenceRaw []byte
	err := row.Scan(
		&observation.ID,
		&observation.ScanID,
		&observation.Provider,
		&outcome,
		&observation.SignalCode,
		&evidenceRaw,
		&observation.ObservedAt,
		&observation.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderObservation{}, ErrNotFound
	}
	if err != nil {
		return ProviderObservation{}, err
	}
	observation.Outcome = ProviderOutcome(outcome)
	if err := json.Unmarshal(evidenceRaw, &observation.Evidence); err != nil {
		return ProviderObservation{}, err
	}
	return observation, nil
}

func getDestinationDecisionTx(ctx context.Context, tx *sql.Tx, scanID uint64) (DestinationDecision, error) {
	return scanDestinationDecision(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,
       decision_metadata_json,valid_until,decided_at,created_at
FROM destination_risk_decisions
WHERE scan_id=?`, scanID))
}

func scanDestinationDecision(row rowScanner) (DestinationDecision, error) {
	var decision DestinationDecision
	var state string
	var metadataRaw []byte
	var validUntil sql.NullTime
	err := row.Scan(
		&decision.ID,
		&decision.WorkspaceID,
		&decision.LinkID,
		&decision.ScanID,
		&decision.RiskFingerprint,
		&decision.PolicyVersion,
		&state,
		&decision.ReasonCategory,
		&metadataRaw,
		&validUntil,
		&decision.DecidedAt,
		&decision.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DestinationDecision{}, ErrNotFound
	}
	if err != nil {
		return DestinationDecision{}, err
	}
	decision.State = DecisionState(state)
	if validUntil.Valid {
		value := validUntil.Time
		decision.ValidUntil = &value
	}
	if err := json.Unmarshal(metadataRaw, &decision.DecisionMetadata); err != nil {
		return DestinationDecision{}, err
	}
	return decision, nil
}

func validProviderOutcome(outcome ProviderOutcome) bool {
	switch outcome {
	case ProviderAllow, ProviderReview, ProviderBlock, ProviderUnknown, ProviderUnavailable:
		return true
	default:
		return false
	}
}
