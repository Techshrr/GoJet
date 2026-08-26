package trust

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

const domainRiskSelect = `
SELECT id,workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,
       valid_until,checked_at,next_due_at,entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at
FROM domain_risk_evaluations`

type domainRiskQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type domainRiskRowScanner interface {
	Scan(...any) error
}

type domainRiskSnapshot struct {
	hostname    string
	entitlement string
	ownership   string
	ingressDNS  string
	https       string
	routing     string
}

func (s *DomainRiskService) beginDomainRiskEvaluation(ctx context.Context, input EvaluateDomainRiskInput, now time.Time) (DomainRiskEvaluation, bool, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	defer tx.Rollback()

	existing, err := loadDomainRiskByIdempotency(ctx, tx, input.WorkspaceID, input.IdempotencyKey)
	if err == nil {
		if existing.DomainID != input.DomainID || existing.PolicyVersion != strings.TrimSpace(s.Policy.Version) || existing.RequestKind != input.RequestKind {
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskEvaluation{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return DomainRiskEvaluation{}, false, err
	}

	if input.RequestKind == DomainRiskRevalidation {
		latest, latestErr := loadLatestDomainRiskEvaluation(ctx, tx, input.WorkspaceID, input.DomainID, true)
		if latestErr == nil && latest.NextDueAt != nil && now.Before(latest.NextDueAt.UTC()) {
			if err := appendDomainRiskAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, nil, input.ActorID, "domain-risk.revalidate", "conflict", "revalidation-rate-limited", input.CorrelationID, map[string]any{"next_due_at": latest.NextDueAt.UTC().Format(time.RFC3339Nano)}); err != nil {
				return DomainRiskEvaluation{}, false, err
			}
			if err := tx.Commit(); err != nil {
				return DomainRiskEvaluation{}, false, err
			}
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		if latestErr != nil && !errors.Is(latestErr, ErrNotFound) {
			return DomainRiskEvaluation{}, false, latestErr
		}
	}

	snapshot, err := loadDomainRiskSnapshot(ctx, tx, input.WorkspaceID, input.DomainID, now)
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	state := DomainRiskPending
	if input.RequestKind == DomainRiskRevalidation {
		state = DomainRiskRevalidating
	}
	result, err := tx.ExecContext(ctx, `
INSERT INTO domain_risk_evaluations
(workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,
 entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.WorkspaceID, input.DomainID, snapshot.hostname, strings.TrimSpace(s.Policy.Version), string(input.RequestKind), input.IdempotencyKey,
		string(state), "evaluation-started", input.CorrelationID, input.ActorID,
		snapshot.entitlement, snapshot.ownership, snapshot.ingressDNS, snapshot.https, snapshot.routing, now, now)
	if err != nil {
		if isDomainRiskDuplicate(err) {
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		return DomainRiskEvaluation{}, false, err
	}
	idRaw, err := result.LastInsertId()
	if err != nil || idRaw <= 0 {
		if err == nil {
			err = ErrConflict
		}
		return DomainRiskEvaluation{}, false, fmt.Errorf("domain risk insert: %w", err)
	}
	id := uint64(idRaw)
	evidenceRef := fmt.Sprintf("domain-risk:%d:revalidating", id)
	updated, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status='stale',risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=? AND removed_at IS NULL`, now, strings.TrimSpace(s.Policy.Version), evidenceRef, input.WorkspaceID, input.DomainID)
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if rows, rowsErr := updated.RowsAffected(); rowsErr != nil || rows != 1 {
		if rowsErr != nil {
			return DomainRiskEvaluation{}, false, rowsErr
		}
		return DomainRiskEvaluation{}, false, ErrNotFound
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, input.WorkspaceID, input.DomainID, "pending", strings.TrimSpace(s.Policy.Version), now, nil, evidenceRef, input.CorrelationID+":begin", map[string]any{"p16_state": state}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := appendP06DomainAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, input.ActorID, "domain.risk.p16.begin", "success", input.Reason, input.CorrelationID, map[string]any{"evaluation_id": id, "risk_projection": "stale"}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, &id, input.ActorID, "domain-risk.evaluate.begin", "success", "evaluation-started", input.CorrelationID, map[string]any{"request_kind": input.RequestKind, "policy_version": strings.TrimSpace(s.Policy.Version)}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	created, err := s.GetDomainRiskEvaluation(ctx, input.WorkspaceID, id)
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	return created, false, nil
}

func (s *DomainRiskService) finishDomainRiskEvaluation(ctx context.Context, input EvaluateDomainRiskInput, started DomainRiskEvaluation, observations []ProviderObservation, state DomainRiskState, reason string, validUntil *time.Time, nextDueAt time.Time, now time.Time) (DomainRiskEvaluation, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	defer tx.Rollback()

	current, err := loadDomainRiskEvaluationByID(ctx, tx, input.WorkspaceID, started.ID, true)
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	if current.DomainID != input.DomainID || current.HostnameASCII != started.HostnameASCII || current.PolicyVersion != strings.TrimSpace(s.Policy.Version) || (current.State != DomainRiskPending && current.State != DomainRiskRevalidating) {
		return DomainRiskEvaluation{}, ErrConflict
	}
	var hostname string
	if err := tx.QueryRowContext(ctx, `SELECT hostname_ascii FROM custom_domains WHERE workspace_id=? AND id=? AND removed_at IS NULL FOR UPDATE`, input.WorkspaceID, input.DomainID).Scan(&hostname); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DomainRiskEvaluation{}, ErrNotFound
		}
		return DomainRiskEvaluation{}, err
	}
	if hostname != started.HostnameASCII {
		return DomainRiskEvaluation{}, ErrConflict
	}

	seen := map[string]struct{}{}
	for _, observation := range observations {
		provider := strings.TrimSpace(observation.Provider)
		if _, exists := seen[provider]; exists {
			return DomainRiskEvaluation{}, ErrConflict
		}
		seen[provider] = struct{}{}
		evidence, err := json.Marshal(SanitizeProviderEvidence(observation.Evidence))
		if err != nil {
			return DomainRiskEvaluation{}, err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO domain_risk_provider_observations
(evaluation_id,provider,outcome,signal_code,evidence_json,observed_at)
VALUES (?,?,?,?,?,?)`, started.ID, provider, string(observation.Outcome), observation.SignalCode, string(evidence), observation.ObservedAt.UTC()); err != nil {
			return DomainRiskEvaluation{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE domain_risk_evaluations
SET state=?,reason_category=?,valid_until=?,checked_at=?,next_due_at=?,updated_at=?
WHERE workspace_id=? AND id=?`, string(state), reason, validUntil, now, nextDueAt.UTC(), now, input.WorkspaceID, started.ID); err != nil {
		return DomainRiskEvaluation{}, err
	}

	projection := projectDomainRiskState(state)
	evidenceRef := fmt.Sprintf("domain-risk:%d", started.ID)
	updated, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status=?,risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=? AND removed_at IS NULL`, string(projection), now, strings.TrimSpace(s.Policy.Version), evidenceRef, input.WorkspaceID, input.DomainID)
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	if rows, rowsErr := updated.RowsAffected(); rowsErr != nil || rows != 1 {
		if rowsErr != nil {
			return DomainRiskEvaluation{}, rowsErr
		}
		return DomainRiskEvaluation{}, ErrNotFound
	}
	p06Result := "fail"
	if projection == domains.RiskAllow {
		p06Result = "pass"
	} else if projection == domains.RiskStale {
		p06Result = "stale"
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, input.WorkspaceID, input.DomainID, p06Result, strings.TrimSpace(s.Policy.Version), now, &nextDueAt, evidenceRef, input.CorrelationID+":complete", map[string]any{"p16_state": state, "reason_category": reason, "provider_count": len(observations)}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	auditResult := "denied"
	if state == DomainRiskAllow {
		auditResult = "success"
	}
	if err := appendP06DomainAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, input.ActorID, "domain.risk.p16.complete", auditResult, input.Reason, input.CorrelationID, map[string]any{"evaluation_id": started.ID, "p16_state": state, "risk_projection": projection}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, &started.ID, input.ActorID, "domain-risk.evaluate.complete", auditResult, reason, input.CorrelationID, map[string]any{"p16_state": state, "risk_projection": projection, "provider_count": len(observations)}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	if err := tx.Commit(); err != nil {
		return DomainRiskEvaluation{}, err
	}
	return s.GetDomainRiskEvaluation(ctx, input.WorkspaceID, started.ID)
}

func (s *DomainRiskService) expireDomainRiskAllow(ctx context.Context, workspaceID string, domainID uint64, actorID, correlationID string, now time.Time) (bool, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	latest, err := loadLatestDomainRiskEvaluation(ctx, tx, workspaceID, domainID, true)
	if errors.Is(err, ErrNotFound) {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if latest.State != DomainRiskAllow || latest.ValidUntil == nil || now.Before(latest.ValidUntil.UTC()) {
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return false, nil
	}
	evidenceRef := fmt.Sprintf("domain-risk:%d:stale", latest.ID)
	if _, err := tx.ExecContext(ctx, `UPDATE custom_domains SET risk_status='stale',risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=? WHERE workspace_id=? AND id=? AND removed_at IS NULL`, now, latest.PolicyVersion, evidenceRef, workspaceID, domainID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domain_risk_evaluations SET state='stale',reason_category='decision-stale',updated_at=? WHERE id=?`, now, latest.ID); err != nil {
		return false, err
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, workspaceID, domainID, "stale", latest.PolicyVersion, now, nil, evidenceRef, correlationID, map[string]any{"p16_state": DomainRiskStale}); err != nil {
		return false, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, workspaceID, domainID, &latest.ID, actorID, "domain-risk.expire", "denied", "decision-stale", correlationID, map[string]any{"expired_valid_until": latest.ValidUntil.UTC().Format(time.RFC3339Nano)}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *DomainRiskService) appendDomainSecurityAudit(ctx context.Context, input domains.DomainSecuritySuspensionInput, updated domains.Domain) error {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := appendDomainRiskAuditTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), input.DomainID, nil, strings.TrimSpace(input.ActorID), "domain-risk.security-suspend", "success", "security-suspended", strings.TrimSpace(input.CorrelationID), map[string]any{"category": input.Category, "routing_state": updated.RoutingState, "grace": false}); err != nil {
		return err
	}
	return tx.Commit()
}

func loadDomainRiskSnapshot(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, now time.Time) (domainRiskSnapshot, error) {
	var snapshot domainRiskSnapshot
	var removedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
SELECT hostname_ascii,ownership_status,ingress_dns_status,https_status,routing_state,removed_at
FROM custom_domains WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, domainID).Scan(&snapshot.hostname, &snapshot.ownership, &snapshot.ingressDNS, &snapshot.https, &snapshot.routing, &removedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainRiskSnapshot{}, ErrNotFound
		}
		return domainRiskSnapshot{}, err
	}
	if removedAt.Valid {
		return domainRiskSnapshot{}, ErrNotFound
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=? AND status='active' AND starts_at<=? AND (expires_at IS NULL OR expires_at>?)`, workspaceID, now, now).Scan(&active); err != nil {
		return domainRiskSnapshot{}, err
	}
	snapshot.entitlement = "no-active-source"
	if active > 0 {
		snapshot.entitlement = "active-source-present"
	}
	return snapshot, nil
}

func loadDomainRiskByIdempotency(ctx context.Context, query domainRiskQueryer, workspaceID, idempotencyKey string) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+` WHERE workspace_id=? AND idempotency_key=?`, workspaceID, idempotencyKey))
}

func loadDomainRiskEvaluationByID(ctx context.Context, query domainRiskQueryer, workspaceID string, id uint64, forUpdate bool) (DomainRiskEvaluation, error) {
	suffix := ` WHERE workspace_id=? AND id=?`
	if forUpdate {
		suffix += ` FOR UPDATE`
	}
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+suffix, workspaceID, id))
}

func loadLatestDomainRiskEvaluation(ctx context.Context, query domainRiskQueryer, workspaceID string, domainID uint64, forUpdate bool) (DomainRiskEvaluation, error) {
	suffix := ` WHERE workspace_id=? AND domain_id=? ORDER BY id DESC LIMIT 1`
	if forUpdate {
		suffix += ` FOR UPDATE`
	}
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+suffix, workspaceID, domainID))
}

func scanDomainRiskEvaluation(row domainRiskRowScanner) (DomainRiskEvaluation, error) {
	var evaluation DomainRiskEvaluation
	var requestKind, state string
	var validUntil, checkedAt, nextDueAt sql.NullTime
	if err := row.Scan(&evaluation.ID, &evaluation.WorkspaceID, &evaluation.DomainID, &evaluation.HostnameASCII, &evaluation.PolicyVersion, &requestKind, &evaluation.IdempotencyKey, &state, &evaluation.ReasonCategory, &evaluation.CorrelationID, &evaluation.ActorID, &validUntil, &checkedAt, &nextDueAt, &evaluation.EntitlementSnapshot, &evaluation.OwnershipSnapshot, &evaluation.IngressDNSSnapshot, &evaluation.HTTPSSnapshot, &evaluation.RoutingSnapshot, &evaluation.CreatedAt, &evaluation.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DomainRiskEvaluation{}, ErrNotFound
		}
		return DomainRiskEvaluation{}, err
	}
	evaluation.RequestKind = DomainRiskRequestKind(requestKind)
	evaluation.State = DomainRiskState(state)
	if validUntil.Valid {
		value := validUntil.Time.UTC()
		evaluation.ValidUntil = &value
	}
	if checkedAt.Valid {
		value := checkedAt.Time.UTC()
		evaluation.CheckedAt = &value
	}
	if nextDueAt.Valid {
		value := nextDueAt.Time.UTC()
		evaluation.NextDueAt = &value
	}
	return evaluation, nil
}

func (s *DomainRiskService) loadDomainRiskObservations(ctx context.Context, evaluationID uint64) ([]ProviderObservation, error) {
	rows, err := s.Store.db.QueryContext(ctx, `SELECT provider,outcome,signal_code,evidence_json,observed_at FROM domain_risk_provider_observations WHERE evaluation_id=? ORDER BY provider`, evaluationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	observations := []ProviderObservation{}
	for rows.Next() {
		var observation ProviderObservation
		var outcome string
		var raw []byte
		if err := rows.Scan(&observation.Provider, &outcome, &observation.SignalCode, &raw, &observation.ObservedAt); err != nil {
			return nil, err
		}
		observation.Outcome = ProviderOutcome(outcome)
		if err := json.Unmarshal(raw, &observation.Evidence); err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	return observations, rows.Err()
}

func appendDomainRiskAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, evaluationID *uint64, actorID, action, result, reason, correlationID string, metadata map[string]any) error {
	if tx == nil || strings.TrimSpace(workspaceID) == "" || domainID == 0 || strings.TrimSpace(actorID) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(correlationID) == "" {
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
INSERT INTO domain_risk_audit_events
(workspace_id,domain_id,evaluation_id,actor_id,action,result,reason_category,correlation_id,metadata_json)
VALUES (?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(workspaceID), domainID, evaluationID, strings.TrimSpace(actorID), strings.TrimSpace(action), result, safeDomainRiskReason(reason), strings.TrimSpace(correlationID), string(raw))
	return err
}

func appendP06RiskRevalidationTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, result, policyVersion string, checkedAt time.Time, nextDueAt *time.Time, evidenceRef, correlationID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var next any
	if nextDueAt != nil {
		next = nextDueAt.UTC()
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO custom_domain_revalidations
(domain_id,workspace_id,axis,result,policy_version,checked_at,next_due_at,evidence_ref,correlation_id,metadata_json)
VALUES (?,?,'risk',?,?,?,?,?,?,?)`, domainID, workspaceID, result, policyVersion, checkedAt.UTC(), next, evidenceRef, correlationID, string(raw))
	return err
}

func appendP06DomainAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, actorID, action, result, reason, correlationID string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO custom_domain_audit_events
(workspace_id,domain_id,entitlement_source_id,actor_id,action,result,reason,correlation_id,metadata_json)
VALUES (?,?,NULL,?,?,?,?,?,?)`, workspaceID, domainID, actorID, action, result, strings.TrimSpace(reason), correlationID, string(raw))
	return err
}

func isDomainRiskDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
