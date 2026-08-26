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

type DomainRiskState string
type DomainRiskRequestKind string

const (
	DomainRiskPending         DomainRiskState = "pending"
	DomainRiskRevalidating    DomainRiskState = "revalidating"
	DomainRiskAllow           DomainRiskState = "allow"
	DomainRiskReview          DomainRiskState = "review"
	DomainRiskBlock           DomainRiskState = "block"
	DomainRiskMalformed       DomainRiskState = "malformed"
	DomainRiskStale           DomainRiskState = "stale"
	DomainRiskProviderPartial DomainRiskState = "provider_partial"

	DomainRiskInitial      DomainRiskRequestKind = "initial"
	DomainRiskRevalidation DomainRiskRequestKind = "revalidation"
)

type DomainRiskPolicy struct {
	Version           string
	RequiredProviders []string
	AllowTTL          time.Duration
	RevalidateAfter   time.Duration
	RetryAfter        time.Duration
}

func (p DomainRiskPolicy) Validate() bool {
	destination := DestinationPolicy{
		Version:           p.Version,
		RequiredProviders: p.RequiredProviders,
		AllowTTL:          p.AllowTTL,
	}
	if !destination.Validate() {
		return false
	}
	if p.RevalidateAfter <= 0 || p.RevalidateAfter > p.AllowTTL {
		return false
	}
	if p.RetryAfter <= 0 || p.RetryAfter > 24*time.Hour {
		return false
	}
	return true
}

type DomainReputationProvider interface {
	Observe(ctx context.Context, hostnameASCII string) (ProviderObservation, error)
}

type DomainRiskEvaluation struct {
	ID                  uint64
	WorkspaceID         string
	DomainID            uint64
	HostnameASCII       string
	PolicyVersion       string
	RequestKind         DomainRiskRequestKind
	IdempotencyKey      string
	State               DomainRiskState
	ReasonCategory      string
	CorrelationID       string
	ActorID             string
	ValidUntil          *time.Time
	CheckedAt           *time.Time
	NextDueAt           *time.Time
	EntitlementSnapshot string
	OwnershipSnapshot   string
	IngressDNSSnapshot  string
	HTTPSSnapshot       string
	RoutingSnapshot     string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type EvaluateDomainRiskInput struct {
	WorkspaceID    string
	DomainID       uint64
	RequestKind    DomainRiskRequestKind
	IdempotencyKey string
	ActorID        string
	Reason         string
	CorrelationID  string
	Now            time.Time
}

type EvaluateDomainRiskResult struct {
	Evaluation   DomainRiskEvaluation
	Observations []ProviderObservation
	Created      bool
}

type DomainRiskService struct {
	Store     *Store
	Policy    DomainRiskPolicy
	Providers []DomainReputationProvider
}

func NewDomainRiskService(store *Store, policy DomainRiskPolicy, providers ...DomainReputationProvider) (*DomainRiskService, error) {
	if store == nil || store.db == nil || !policy.Validate() || len(providers) == 0 || len(providers) > 16 {
		return nil, ErrInvalid
	}
	for _, provider := range providers {
		if provider == nil {
			return nil, ErrInvalid
		}
	}
	return &DomainRiskService{Store: store, Policy: policy, Providers: append([]DomainReputationProvider(nil), providers...)}, nil
}

func (s *DomainRiskService) Evaluate(ctx context.Context, input EvaluateDomainRiskInput) (EvaluateDomainRiskResult, error) {
	if s == nil || s.Store == nil || s.Store.db == nil || !s.Policy.Validate() || !validDomainRiskInput(input) {
		return EvaluateDomainRiskResult{}, ErrInvalid
	}
	input = normalizeDomainRiskInput(input)
	now := input.Now.UTC().Truncate(time.Microsecond)

	started, replay, err := s.beginEvaluation(ctx, input, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	if replay {
		observations, err := s.loadObservations(ctx, started.ID)
		if err != nil {
			return EvaluateDomainRiskResult{}, err
		}
		return EvaluateDomainRiskResult{Evaluation: started, Observations: observations, Created: false}, nil
	}

	observations := make([]ProviderObservation, 0, len(s.Providers))
	for _, provider := range s.Providers {
		observation, observeErr := provider.Observe(ctx, started.HostnameASCII)
		if observeErr != nil {
			observation = normalizedProviderFailure("domain-provider", ProviderUnavailable, "provider-unavailable", "provider-error", now)
		}
		observation.Provider = strings.TrimSpace(observation.Provider)
		observation.SignalCode = normalizeSignalCode(observation.SignalCode)
		if observation.ObservedAt.IsZero() {
			observation.ObservedAt = now
		}
		observation.ObservedAt = observation.ObservedAt.UTC().Truncate(time.Microsecond)
		observation.Evidence = SanitizeProviderEvidence(observation.Evidence)
		if !validDomainObservation(observation) {
			observation = normalizedProviderFailure("domain-provider", ProviderUnknown, "provider-malformed", "invalid-observation", now)
		}
		observations = append(observations, observation)
	}

	state, reason, validUntil, nextDueAt, err := evaluateDomainSignals(s.Policy, observations, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	completed, err := s.finishEvaluation(ctx, input, started, observations, state, reason, validUntil, nextDueAt, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	return EvaluateDomainRiskResult{Evaluation: completed, Observations: observations, Created: true}, nil
}

func (s *DomainRiskService) beginEvaluation(ctx context.Context, input EvaluateDomainRiskInput, now time.Time) (DomainRiskEvaluation, bool, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	defer tx.Rollback()

	existing, err := getDomainEvaluationByIdempotency(ctx, tx, input.WorkspaceID, input.IdempotencyKey)
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
		latest, latestErr := getLatestDomainEvaluation(ctx, tx, input.WorkspaceID, input.DomainID)
		if latestErr == nil && latest.NextDueAt != nil && now.Before(latest.NextDueAt.UTC()) {
			if auditErr := appendDomainRiskAuditTx(ctx, tx, input.WorkspaceID, input.DomainID, nil, input.ActorID, "domain-risk.revalidate", "conflict", "revalidation-rate-limited", input.CorrelationID, map[string]any{"next_due_at": latest.NextDueAt.UTC().Format(time.RFC3339Nano)}); auditErr != nil {
				return DomainRiskEvaluation{}, false, auditErr
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
		if isDomainDuplicate(err) {
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		return DomainRiskEvaluation{}, false, err
	}
	idRaw, err := result.LastInsertId()
	if err != nil || idRaw <= 0 {
		return DomainRiskEvaluation{}, false, fmt.Errorf("domain risk insert: %w", err)
	}
	id := uint64(idRaw)
	evidenceRef := fmt.Sprintf("domain-risk:%d:revalidating", id)
	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status='stale',risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=? AND removed_at IS NULL`, now, strings.TrimSpace(s.Policy.Version), evidenceRef, input.WorkspaceID, input.DomainID); err != nil {
		return DomainRiskEvaluation{}, false, err
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
	return s.GetDomainRiskEvaluation(ctx, input.WorkspaceID, id)
}

func (s *DomainRiskService) finishEvaluation(ctx context.Context, input EvaluateDomainRiskInput, started DomainRiskEvaluation, observations []ProviderObservation, state DomainRiskState, reason string, validUntil *time.Time, nextDueAt time.Time, now time.Time) (DomainRiskEvaluation, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	defer tx.Rollback()

	current, err := getDomainEvaluationByID(ctx, tx, input.WorkspaceID, started.ID)
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
	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status=?,risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=? AND removed_at IS NULL`, string(projection), now, strings.TrimSpace(s.Policy.Version), evidenceRef, input.WorkspaceID, input.DomainID); err != nil {
		return DomainRiskEvaluation{}, err
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

func (s *DomainRiskService) GetDomainRiskEvaluation(ctx context.Context, workspaceID string, evaluationID uint64) (DomainRiskEvaluation, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.Store == nil || s.Store.db == nil || workspaceID == "" || evaluationID == 0 {
		return DomainRiskEvaluation{}, ErrInvalid
	}
	return scanDomainRiskEvaluation(s.Store.db.QueryRowContext(ctx, domainRiskSelect+` WHERE workspace_id=? AND id=?`, workspaceID, evaluationID))
}

func (s *DomainRiskService) ExpireAllowIfStale(ctx context.Context, workspaceID string, domainID uint64, actorID, correlationID string, now time.Time) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.Store == nil || s.Store.db == nil || workspaceID == "" || domainID == 0 || actorID == "" || correlationID == "" || now.IsZero() {
		return false, ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	latest, err := getLatestDomainEvaluation(ctx, tx, workspaceID, domainID)
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

func (s *DomainRiskService) ApplySecuritySuspension(ctx context.Context, input domains.DomainSecuritySuspensionInput) (domains.Domain, error) {
	if s == nil || s.Store == nil || s.Store.db == nil {
		return domains.Domain{}, ErrInvalid
	}
	updated, err := domains.NewMySQLStore(s.Store.db).ApplyDomainSecuritySuspension(ctx, input)
	if err != nil {
		return domains.Domain{}, err
	}
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domains.Domain{}, err
	}
	defer tx.Rollback()
	if err := appendDomainRiskAuditTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), input.DomainID, nil, strings.TrimSpace(input.ActorID), "domain-risk.security-suspend", "success", "security-suspended", strings.TrimSpace(input.CorrelationID), map[string]any{"category": input.Category, "routing_state": updated.RoutingState, "grace": false}); err != nil {
		return domains.Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return domains.Domain{}, err
	}
	return updated, nil
}

type domainRiskSnapshot struct {
	hostname    string
	entitlement string
	ownership   string
	ingressDNS  string
	https       string
	routing     string
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

func evaluateDomainSignals(policy DomainRiskPolicy, observations []ProviderObservation, now time.Time) (DomainRiskState, string, *time.Time, time.Time, error) {
	evaluation, err := EvaluateDestinationPolicy(DestinationPolicy{Version: policy.Version, RequiredProviders: policy.RequiredProviders, AllowTTL: policy.AllowTTL}, observations, true, now)
	if err != nil {
		return "", "", nil, time.Time{}, err
	}
	state := DomainRiskReview
	reason := evaluation.ReasonCategory
	for _, observation := range observations {
		switch observation.SignalCode {
		case "provider-malformed":
			state = DomainRiskMalformed
			reason = "provider-malformed"
		case "provider-partial":
			if state != DomainRiskMalformed {
				state = DomainRiskProviderPartial
				reason = "provider-partial"
			}
		}
	}
	if state != DomainRiskMalformed && state != DomainRiskProviderPartial {
		switch evaluation.State {
		case DecisionAllow:
			state = DomainRiskAllow
		case DecisionBlock:
			state = DomainRiskBlock
		case DecisionPending:
			state = DomainRiskPending
		case DecisionReview, DecisionUnknown:
			state = DomainRiskReview
		default:
			return "", "", nil, time.Time{}, ErrInvalid
		}
	}
	nextDue := now.Add(policy.RetryAfter).UTC().Truncate(time.Microsecond)
	var validUntil *time.Time
	if state == DomainRiskAllow {
		validUntil = evaluation.ValidUntil
		nextDue = now.Add(policy.RevalidateAfter).UTC().Truncate(time.Microsecond)
	}
	return state, safeDomainReason(reason), validUntil, nextDue, nil
}

func projectDomainRiskState(state DomainRiskState) domains.DomainRiskStatus {
	switch state {
	case DomainRiskAllow:
		return domains.RiskAllow
	case DomainRiskBlock:
		return domains.RiskBlock
	case DomainRiskMalformed:
		return domains.RiskMalformed
	case DomainRiskStale:
		return domains.RiskStale
	case DomainRiskPending, DomainRiskRevalidating, DomainRiskReview, DomainRiskProviderPartial:
		return domains.RiskReview
	default:
		return domains.RiskStale
	}
}

func validDomainObservation(observation ProviderObservation) bool {
	if !validProviderName(observation.Provider) || observation.SignalCode == "" || observation.ObservedAt.IsZero() {
		return false
	}
	switch observation.Outcome {
	case ProviderAllow, ProviderReview, ProviderBlock, ProviderUnknown, ProviderUnavailable:
		return true
	default:
		return false
	}
}

func validDomainRiskInput(input EvaluateDomainRiskInput) bool {
	if strings.TrimSpace(input.WorkspaceID) == "" || len(strings.TrimSpace(input.WorkspaceID)) > 64 || input.DomainID == 0 || strings.TrimSpace(input.IdempotencyKey) == "" || len(strings.TrimSpace(input.IdempotencyKey)) > 128 || strings.TrimSpace(input.ActorID) == "" || len(strings.TrimSpace(input.ActorID)) > 128 || strings.TrimSpace(input.Reason) == "" || strings.TrimSpace(input.CorrelationID) == "" || len(strings.TrimSpace(input.CorrelationID)) > 128 || input.Now.IsZero() {
		return false
	}
	return input.RequestKind == DomainRiskInitial || input.RequestKind == DomainRiskRevalidation
}

func normalizeDomainRiskInput(input EvaluateDomainRiskInput) EvaluateDomainRiskInput {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Now = input.Now.UTC().Truncate(time.Microsecond)
	return input
}

func safeDomainReason(value string) string {
	switch strings.TrimSpace(value) {
	case "policy-allow", "provider-block", "provider-pending", "provider-review", "provider-unknown", "provider-unavailable", "provider-incomplete", "provider-malformed", "provider-partial", "local-safety-not-approved", "decision-stale", "evaluation-started", "security-suspended", "revalidation-rate-limited":
		return strings.TrimSpace(value)
	default:
		return "provider-review"
	}
}

const domainRiskSelect = `
SELECT id,workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,
       valid_until,checked_at,next_due_at,entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at
FROM domain_risk_evaluations`

type domainRiskRowScanner interface {
	Scan(...any) error
}

type domainRiskQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getDomainEvaluationByIdempotency(ctx context.Context, query domainRiskQueryer, workspaceID, idempotencyKey string) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+` WHERE workspace_id=? AND idempotency_key=?`, workspaceID, idempotencyKey))
}

func getDomainEvaluationByID(ctx context.Context, query domainRiskQueryer, workspaceID string, id uint64) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+` WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, id))
}

func getLatestDomainEvaluation(ctx context.Context, query domainRiskQueryer, workspaceID string, domainID uint64) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(query.QueryRowContext(ctx, domainRiskSelect+` WHERE workspace_id=? AND domain_id=? ORDER BY id DESC LIMIT 1 FOR UPDATE`, workspaceID, domainID))
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

func (s *DomainRiskService) loadObservations(ctx context.Context, evaluationID uint64) ([]ProviderObservation, error) {
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
VALUES (?,?,?,?,?,?,?,?,?)`, strings.TrimSpace(workspaceID), domainID, evaluationID, strings.TrimSpace(actorID), strings.TrimSpace(action), result, safeDomainReason(reason), strings.TrimSpace(correlationID), string(raw))
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

func isDomainDuplicate(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
