package admin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var adminRuntimeServiceIDs = [...]string{
	"redirectengine",
	"analyticsworker",
	"analyticsreconciler",
	"platformapi",
	"mailworker",
	"fileworker",
	"operationsmonitor",
	"logreceiver",
}

type DependencyProbe interface {
	Probe(context.Context, string) map[string]bool
}

type ServiceRestarter interface {
	Restart(context.Context, string) error
}

type OperationsGovernance struct {
	service   *Service
	probe     DependencyProbe
	restarter ServiceRestarter
}

func NewOperationsGovernance(service *Service, probe DependencyProbe, restarter ServiceRestarter) (*OperationsGovernance, error) {
	if service == nil || probe == nil {
		return nil, ErrInvalid
	}
	return &OperationsGovernance{service: service, probe: probe, restarter: restarter}, nil
}

type ManagedOperationJob struct {
	ID            uint64    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	LinkID        uint64    `json:"link_id"`
	RequestKind   string    `json:"request_kind"`
	Status        string    `json:"status"`
	Attempts      uint64    `json:"attempts"`
	MaxAttempts   uint64    `json:"max_attempts"`
	AvailableAt   time.Time `json:"available_at"`
	CorrelationID string    `json:"correlation_id"`
	LastErrorCode *string   `json:"last_error_code,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ManagedRuntimeService struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Dependencies map[string]bool `json:"dependencies"`
}

func AdminRuntimeServiceIDs() []string {
	out := make([]string, len(adminRuntimeServiceIDs))
	copy(out, adminRuntimeServiceIDs[:])
	return out
}

func validRuntimeServiceID(value string) bool {
	for _, id := range adminRuntimeServiceIDs {
		if value == id {
			return true
		}
	}
	return false
}

func expectedRequeueImpact(jobID uint64) string {
	return fmt.Sprintf("requeue destination-risk job %d", jobID)
}

func expectedRestartImpact(serviceID string) string {
	return "restart service " + serviceID
}

func validImpactConfirmation(actual, expected string) bool {
	return strings.TrimSpace(actual) == expected
}

func (o *OperationsGovernance) ListJobs(ctx context.Context, p Principal, limit int) ([]ManagedOperationJob, error) {
	if err := o.service.Require(p, PermissionOperationsManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := o.service.db.QueryContext(ctx, `
SELECT id,workspace_id,link_id,request_kind,status,attempts,max_attempts,available_at,correlation_id,last_error_code,updated_at
FROM destination_risk_scans
WHERE status IN ('retry','failed')
ORDER BY updated_at DESC,id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ManagedOperationJob, 0)
	for rows.Next() {
		var item ManagedOperationJob
		var lastErr sql.NullString
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.LinkID, &item.RequestKind, &item.Status, &item.Attempts, &item.MaxAttempts, &item.AvailableAt, &item.CorrelationID, &lastErr, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if lastErr.Valid {
			v := strings.TrimSpace(lastErr.String)
			item.LastErrorCode = &v
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (o *OperationsGovernance) RequeueJob(ctx context.Context, p Principal, jobID uint64, impactConfirmation string, authority MutationAuthority, now time.Time) (ManagedOperationJob, bool, error) {
	if jobID == 0 || !validImpactConfirmation(impactConfirmation, expectedRequeueImpact(jobID)) {
		return ManagedOperationJob{}, false, ErrInvalid
	}
	if err := o.service.RequireHighRisk(p, PermissionOperationsManage, authority, now); err != nil {
		return ManagedOperationJob{}, false, err
	}
	action := "admin.operations.job.requeue"
	fingerprint, err := requestFingerprint(map[string]any{
		"job_id": jobID, "impact_confirmation": expectedRequeueImpact(jobID), "reason": strings.TrimSpace(authority.Reason),
	})
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := o.service.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, replayErr := loadIdempotency[ManagedOperationJob](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); replayErr != nil {
		return ManagedOperationJob{}, false, replayErr
	} else if ok {
		return replay, true, nil
	}
	before, err := operationJobTx(ctx, tx, jobID, true)
	if errors.Is(err, sql.ErrNoRows) {
		return ManagedOperationJob{}, false, ErrNotFound
	}
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	if before.Status != "failed" && before.Status != "retry" {
		return ManagedOperationJob{}, false, ErrConflict
	}
	if before.Attempts >= before.MaxAttempts {
		return ManagedOperationJob{}, false, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `
UPDATE destination_risk_scans
SET status='queued',available_at=?,lease_owner=NULL,lease_expires_at=NULL,updated_at=?
WHERE id=? AND status IN ('retry','failed') AND attempts<max_attempts`, now, now, jobID)
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return ManagedOperationJob{}, false, ErrConflict
	}
	after, err := operationJobTx(ctx, tx, jobID, false)
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action,
		ResourceType: "destination_risk_scan", ResourceID: fmt.Sprintf("%d", jobID), Result: "success",
		CorrelationID: authority.CorrelationID, Reason: authority.Reason,
		Before: map[string]any{"status": before.Status, "attempts": before.Attempts, "max_attempts": before.MaxAttempts, "last_error_code": auditOptionalString(before.LastErrorCode)},
		After:  map[string]any{"status": after.Status, "attempts": after.Attempts, "max_attempts": after.MaxAttempts, "last_error_code": auditOptionalString(after.LastErrorCode)},
		Metadata: map[string]any{
			"workspace_id": before.WorkspaceID, "impact_confirmation": true,
		},
		CreatedAt: now,
	})
	if err != nil {
		return ManagedOperationJob{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, after, auditID, now); err != nil {
		return ManagedOperationJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedOperationJob{}, false, err
	}
	return after, false, nil
}

func auditOptionalString(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func operationJobTx(ctx context.Context, tx *sql.Tx, id uint64, forUpdate bool) (ManagedOperationJob, error) {
	query := `SELECT id,workspace_id,link_id,request_kind,status,attempts,max_attempts,available_at,correlation_id,last_error_code,updated_at FROM destination_risk_scans WHERE id=?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var item ManagedOperationJob
	var lastErr sql.NullString
	if err := tx.QueryRowContext(ctx, query, id).Scan(&item.ID, &item.WorkspaceID, &item.LinkID, &item.RequestKind, &item.Status, &item.Attempts, &item.MaxAttempts, &item.AvailableAt, &item.CorrelationID, &lastErr, &item.UpdatedAt); err != nil {
		return ManagedOperationJob{}, err
	}
	if lastErr.Valid {
		v := strings.TrimSpace(lastErr.String)
		item.LastErrorCode = &v
	}
	return item, nil
}

func (o *OperationsGovernance) ListServices(ctx context.Context, p Principal) ([]ManagedRuntimeService, error) {
	if err := o.service.Require(p, PermissionOperationsManage); err != nil {
		return nil, err
	}
	items := make([]ManagedRuntimeService, 0, len(adminRuntimeServiceIDs))
	for _, id := range adminRuntimeServiceIDs {
		dependencies := o.probe.Probe(ctx, id)
		items = append(items, ManagedRuntimeService{ID: id, Status: runtimeServiceStatus(dependencies), Dependencies: dependencies})
	}
	return items, nil
}

func runtimeServiceStatus(dependencies map[string]bool) string {
	if len(dependencies) == 0 {
		return "unknown"
	}
	for _, ok := range dependencies {
		if !ok {
			return "degraded"
		}
	}
	return "healthy"
}

func dependencyAuditFields(status string, dependencies map[string]bool) map[string]any {
	out := map[string]any{"service_status": status}
	for _, key := range []string{"unit", "mysql", "redis", "clamav"} {
		if value, ok := dependencies[key]; ok {
			out[key] = value
		}
	}
	return out
}

func (o *OperationsGovernance) RestartService(ctx context.Context, p Principal, serviceID, impactConfirmation string, authority MutationAuthority, now time.Time) (ManagedRuntimeService, bool, error) {
	serviceID = strings.TrimSpace(serviceID)
	if !validRuntimeServiceID(serviceID) || !validImpactConfirmation(impactConfirmation, expectedRestartImpact(serviceID)) {
		return ManagedRuntimeService{}, false, ErrInvalid
	}
	if err := o.service.RequireHighRisk(p, PermissionOperationsManage, authority, now); err != nil {
		return ManagedRuntimeService{}, false, err
	}
	if o.restarter == nil {
		return ManagedRuntimeService{}, false, ErrForbidden
	}
	action := "admin.operations.service.restart"
	fingerprint, err := requestFingerprint(map[string]any{
		"service_id": serviceID, "impact_confirmation": expectedRestartImpact(serviceID), "reason": strings.TrimSpace(authority.Reason),
	})
	if err != nil {
		return ManagedRuntimeService{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := o.service.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ManagedRuntimeService{}, false, err
	}
	defer tx.Rollback()
	if replay, ok, replayErr := loadIdempotency[ManagedRuntimeService](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); replayErr != nil {
		return ManagedRuntimeService{}, false, replayErr
	} else if ok {
		return replay, true, nil
	}
	beforeDeps := o.probe.Probe(ctx, serviceID)
	beforeStatus := runtimeServiceStatus(beforeDeps)
	if err := o.restarter.Restart(ctx, serviceID); err != nil {
		_, _ = recordAuditTx(ctx, tx, auditInput{
			ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "service", ResourceID: serviceID,
			Result: "failed", CorrelationID: authority.CorrelationID, Reason: authority.Reason,
			Before: dependencyAuditFields(beforeStatus, beforeDeps), After: map[string]any{},
			Metadata: map[string]any{"allowlisted": true, "shell_input": false, "impact_confirmation": true}, CreatedAt: now,
		})
		_ = tx.Commit()
		return ManagedRuntimeService{}, false, err
	}
	afterDeps := o.probe.Probe(ctx, serviceID)
	afterStatus := runtimeServiceStatus(afterDeps)
	result := ManagedRuntimeService{ID: serviceID, Status: afterStatus, Dependencies: afterDeps}
	auditID, err := recordAuditTx(ctx, tx, auditInput{
		ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "service", ResourceID: serviceID,
		Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason,
		Before: dependencyAuditFields(beforeStatus, beforeDeps), After: dependencyAuditFields(afterStatus, afterDeps),
		Metadata: map[string]any{"allowlisted": true, "shell_input": false, "impact_confirmation": true}, CreatedAt: now,
	})
	if err != nil {
		return ManagedRuntimeService{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, result, auditID, now); err != nil {
		return ManagedRuntimeService{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ManagedRuntimeService{}, false, err
	}
	return result, false, nil
}
