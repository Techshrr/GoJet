package trust

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
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
	if !DestinationPolicy{Version: p.Version, RequiredProviders: p.RequiredProviders, AllowTTL: p.AllowTTL}.Validate() {
		return false
	}
	if p.RevalidateAfter <= 0 || p.RevalidateAfter > 7*24*time.Hour {
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
	WorkspaceID   string
	DomainID      uint64
	RequestKind   DomainRiskRequestKind
	IdempotencyKey string
	ActorID       string
	Reason        string
	CorrelationID string
	Now           time.Time
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

func (s *DomainRiskService) Evaluate(ctx context.Context, in EvaluateDomainRiskInput) (EvaluateDomainRiskResult, error) {
	if s == nil || s.Store == nil || s.Store.db == nil || !s.Policy.Validate() || !validDomainRiskInput(in) {
		return EvaluateDomainRiskResult{}, ErrInvalid
	}
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.Reason = strings.TrimSpace(in.Reason)
	in.CorrelationID = strings.TrimSpace(in.CorrelationID)
	now := in.Now.UTC().Truncate(time.Microsecond)

	started, replay, err := s.beginEvaluation(ctx, in, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	if replay {
		observations, obsErr := s.loadDomainObservations(ctx, started.ID)
		if obsErr != nil {
			return EvaluateDomainRiskResult{}, obsErr
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
		if !validDomainProviderObservation(observation) {
			observation = normalizedProviderFailure("domain-provider", ProviderUnknown, "provider-malformed", "invalid-observation", now)
		}
		observations = append(observations, observation)
	}

	state, reasonCategory, validUntil, nextDueAt, err := evaluateDomainSignals(s.Policy, observations, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	completed, err := s.finishEvaluation(ctx, in, started, observations, state, reasonCategory, validUntil, nextDueAt, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	return EvaluateDomainRiskResult{Evaluation: completed, Observations: observations, Created: true}, nil
}

func (s *DomainRiskService) beginEvaluation(ctx context.Context, in EvaluateDomainRiskInput, now time.Time) (DomainRiskEvaluation, bool, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if existing, existingErr := getDomainEvaluationByIdempotency(ctx, tx, in.WorkspaceID, in.IdempotencyKey); existingErr == nil {
		if existing.DomainID != in.DomainID || existing.PolicyVersion != strings.TrimSpace(s.Policy.Version) || existing.RequestKind != in.RequestKind {
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskEvaluation{}, false, err
		}
		return existing, true, nil
	} else if !errors.Is(existingErr, ErrNotFound) {
		return DomainRiskEvaluation{}, false, existingErr
	}

	snapshot, err := loadDomainRiskSnapshotTx(ctx, tx, in.WorkspaceID, in.DomainID, now)
	if err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if in.RequestKind == DomainRiskRevalidation {
		latest, latestErr := getLatestDomainEvaluationTx(ctx, tx, in.WorkspaceID, in.DomainID)
		if latestErr == nil && latest.NextDueAt != nil && now.Before(latest.NextDueAt.UTC()) {
			if auditErr := appendDomainRiskAuditTx(ctx, tx, in.WorkspaceID, in.DomainID, nil, in.ActorID, "domain-risk.revalidate", "conflict", "revalidation-rate-limited", in.CorrelationID, map[string]any{
				"next_due_at": latest.NextDueAt.UTC().Format(time.RFC3339Nano),
			}); auditErr != nil {
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

	state := DomainRiskPending
	if in.RequestKind == DomainRiskRevalidation {
		state = DomainRiskRevalidating
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO domain_risk_evaluations
(workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,
 entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.WorkspaceID, in.DomainID, snapshot.hostnameASCII, strings.TrimSpace(s.Policy.Version), string(in.RequestKind), in.IdempotencyKey,
		string(state), "evaluation-started", in.CorrelationID, in.ActorID,
		snapshot.entitlement, snapshot.ownership, snapshot.ingressDNS, snapshot.https, snapshot.routing, now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return DomainRiskEvaluation{}, false, ErrConflict
		}
		return DomainRiskEvaluation{}, false, err
	}
	idRaw, err := res.LastInsertId()
	if err != nil || idRaw <= 0 {
		return DomainRiskEvaluation{}, false, fmt.Errorf("domain risk evaluation insert: %w", err)
	}
	id := uint64(idRaw)
	evidenceRef := fmt.Sprintf("domain-risk:%d:revalidating", id)
	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status='stale',risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=?`, now, strings.TrimSpace(s.Policy.Version), evidenceRef, in.WorkspaceID, in.DomainID); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, in.WorkspaceID, in.DomainID, "pending", strings.TrimSpace(s.Policy.Version), now, nil, evidenceRef, in.CorrelationID+":begin", map[string]any{
		"p16_state": state,
		"reason_category": "evaluation-started",
	}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := appendP06DomainAuditTx(ctx, tx, in.WorkspaceID, in.DomainID, in.ActorID, "domain.risk.p16.begin", "success", in.Reason, in.CorrelationID, map[string]any{
		"evaluation_id": id,
		"p16_state": state,
		"risk_projection": "stale",
	}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, in.WorkspaceID, in.DomainID, &id, in.ActorID, "domain-risk.evaluate.begin", "success", "evaluation-started", in.CorrelationID, map[string]any{
		"request_kind": in.RequestKind,
		"policy_version": strings.TrimSpace(s.Policy.Version),
	}); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return DomainRiskEvaluation{}, false, err
	}
	checked := now
	return DomainRiskEvaluation{
		ID: id, WorkspaceID: in.WorkspaceID, DomainID: in.DomainID, HostnameASCII: snapshot.hostnameASCII,
		PolicyVersion: strings.TrimSpace(s.Policy.Version), RequestKind: in.RequestKind, IdempotencyKey: in.IdempotencyKey,
		State: state, ReasonCategory: "evaluation-started", CorrelationID: in.CorrelationID, ActorID: in.ActorID,
		CheckedAt: &checked, EntitlementSnapshot: snapshot.entitlement, OwnershipSnapshot: snapshot.ownership,
		IngressDNSSnapshot: snapshot.ingressDNS, HTTPSSnapshot: snapshot.https, RoutingSnapshot: snapshot.routing,
		CreatedAt: now, UpdatedAt: now,
	}, false, nil
}

func (s *DomainRiskService) finishEvaluation(
	ctx context.Context,
	in EvaluateDomainRiskInput,
	started DomainRiskEvaluation,
	observations []ProviderObservation,
	state DomainRiskState,
	reasonCategory string,
	validUntil *time.Time,
	nextDueAt time.Time,
	now time.Time,
) (DomainRiskEvaluation, error) {
	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	defer func() { _ = tx.Rollback() }()

	current, err := getDomainEvaluationByIDTx(ctx, tx, in.WorkspaceID, started.ID)
	if err != nil {
		return DomainRiskEvaluation{}, err
	}
	if current.DomainID != in.DomainID || current.HostnameASCII != started.HostnameASCII || current.PolicyVersion != strings.TrimSpace(s.Policy.Version) || (current.State != DomainRiskPending && current.State != DomainRiskRevalidating) {
		return DomainRiskEvaluation{}, ErrConflict
	}
	var hostname string
	if err := tx.QueryRowContext(ctx, `SELECT hostname_ascii FROM custom_domains WHERE workspace_id=? AND id=? AND removed_at IS NULL FOR UPDATE`, in.WorkspaceID, in.DomainID).Scan(&hostname); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DomainRiskEvaluation{}, ErrNotFound
		}
		return DomainRiskEvaluation{}, err
	}
	if hostname != started.HostnameASCII {
		return DomainRiskEvaluation{}, ErrConflict
	}

	seenProviders := map[string]struct{}{}
	for _, observation := range observations {
		provider := strings.TrimSpace(observation.Provider)
		if _, exists := seenProviders[provider]; exists {
			return DomainRiskEvaluation{}, ErrConflict
		}
		seenProviders[provider] = struct{}{}
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
WHERE id=? AND workspace_id=?`, string(state), reasonCategory, validUntil, now, nextDueAt.UTC(), now, started.ID, in.WorkspaceID); err != nil {
		return DomainRiskEvaluation{}, err
	}
	projection := projectDomainRiskState(state)
	evidenceRef := fmt.Sprintf("domain-risk:%d", started.ID)
	if _, err := tx.ExecContext(ctx, `
UPDATE custom_domains
SET risk_status=?,risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=?
WHERE workspace_id=? AND id=?`, string(projection), now, strings.TrimSpace(s.Policy.Version), evidenceRef, in.WorkspaceID, in.DomainID); err != nil {
		return DomainRiskEvaluation{}, err
	}
	p06Result := "fail"
	if projection == domains.RiskAllow {
		p06Result = "pass"
	} else if projection == domains.RiskStale {
		p06Result = "stale"
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, in.WorkspaceID, in.DomainID, p06Result, strings.TrimSpace(s.Policy.Version), now, &nextDueAt, evidenceRef, in.CorrelationID+":complete", map[string]any{
		"p16_state": state,
		"reason_category": reasonCategory,
		"provider_count": len(observations),
	}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	auditResult := "denied"
	if state == DomainRiskAllow {
		auditResult = "success"
	}
	if err := appendP06DomainAuditTx(ctx, tx, in.WorkspaceID, in.DomainID, in.ActorID, "domain.risk.p16.complete", auditResult, in.Reason, in.CorrelationID, map[string]any{
		"evaluation_id": started.ID,
		"p16_state": state,
		"reason_category": reasonCategory,
		"risk_projection": projection,
	}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, in.WorkspaceID, in.DomainID, &started.ID, in.ActorID, "domain-risk.evaluate.complete", auditResult, reasonCategory, in.CorrelationID, map[string]any{
		"p16_state": state,
		"risk_projection": projection,
		"provider_count": len(observations),
	}); err != nil {
		return DomainRiskEvaluation{}, err
	}
	if err := tx.Commit(); err != nil {
		return DomainRiskEvaluation{}, err
	}
	return s.GetDomainRiskEvaluation(ctx, in.WorkspaceID, started.ID)
}

func (s *DomainRiskService) GetDomainRiskEvaluation(ctx context.Context, workspaceID string, evaluationID uint64) (DomainRiskEvaluation, error) {
	if s == nil || s.Store == nil || s.Store.db == nil || strings.TrimSpace(workspaceID) == "" || evaluationID == 0 {
		return DomainRiskEvaluation{}, ErrInvalid
	}
	return scanDomainRiskEvaluation(s.Store.db.QueryRowContext(ctx, domainRiskEvaluationSelect+` WHERE workspace_id=? AND id=?`, strings.TrimSpace(workspaceID), evaluationID))
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
	defer func() { _ = tx.Rollback() }()
	latest, err := getLatestDomainEvaluationTx(ctx, tx, workspaceID, domainID)
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
	if _, err := tx.ExecContext(ctx, `UPDATE custom_domains SET risk_status='stale',risk_checked_at=?,risk_policy_version=?,risk_evidence_ref=? WHERE workspace_id=? AND id=?`,
		now, latest.PolicyVersion, fmt.Sprintf("domain-risk:%d:stale", latest.ID), workspaceID, domainID); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE domain_risk_evaluations SET state='stale',reason_category='decision-stale',updated_at=? WHERE id=?`, now, latest.ID); err != nil {
		return false, err
	}
	if err := appendP06RiskRevalidationTx(ctx, tx, workspaceID, domainID, "stale", latest.PolicyVersion, now, nil, fmt.Sprintf("domain-risk:%d:stale", latest.ID), correlationID, map[string]any{
		"p16_state": DomainRiskStale,
		"reason_category": "decision-stale",
	}); err != nil {
		return false, err
	}
	if err := appendDomainRiskAuditTx(ctx, tx, workspaceID, domainID, &latest.ID, actorID, "domain-risk.expire", "denied", "decision-stale", correlationID, map[string]any{
		"expired_valid_until": latest.ValidUntil.UTC().Format(time.RFC3339Nano),
	}); err != nil {
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
	defer func() { _ = tx.Rollback() }()
	if err := appendDomainRiskAuditTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), input.DomainID, nil, strings.TrimSpace(input.ActorID), "domain-risk.security-suspend", "success", "security-suspended", strings.TrimSpace(input.CorrelationID), map[string]any{
		"category": input.Category,
		"routing_state": updated.RoutingState,
		"grace": false,
	}); err != nil {
		return domains.Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return domains.Domain{}, err
	}
	return updated, nil
}

type domainRiskSnapshot struct {
	hostnameASCII string
	entitlement   string
	ownership     string
	ingressDNS    string
	https         string
	routing       string
}

func loadDomainRiskSnapshotTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, now time.Time) (domainRiskSnapshot, error) {
	var snapshot domainRiskSnapshot
	var removedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
SELECT hostname_ascii,ownership_status,ingress_dns_status,https_status,routing_state,removed_at
FROM custom_domains
WHERE workspace_id=? AND id=?
FOR UPDATE`, workspaceID, domainID).Scan(&snapshot.hostnameASCII, &snapshot.ownership, &snapshot.ingressDNS, &snapshot.https, &snapshot.routing, &removedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainRiskSnapshot{}, ErrNotFound
		}
		return domainRiskSnapshot{}, err
	}
	if removedAt.Valid {
		return domainRiskSnapshot{}, ErrNotFound
	}
	var activeSources int
	if err := tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM custom_domain_entitlement_sources
WHERE workspace_id=? AND status='active' AND starts_at<=? AND (expires_at IS NULL OR expires_at>?)`, workspaceID, now, now).Scan(&activeSources); err != nil {
		return domainRiskSnapshot{}, err
	}
	snapshot.entitlement = "no-active-source"
	if activeSources > 0 {
		snapshot.entitlement = "active-source-present"
	}
	return snapshot, nil
}

func evaluateDomainSignals(policy DomainRiskPolicy, observations []ProviderObservation, now time.Time) (DomainRiskState, string, *time.Time, time.Time, error) {
	evaluation, err := EvaluateDestinationPolicy(DestinationPolicy{
		Version: policy.Version, RequiredProviders: policy.RequiredProviders, AllowTTL: policy.AllowTTL,
	}, observations, true, now)
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
	var validUntil *time.Time
	nextDue := now.Add(policy.RetryAfter).UTC().Truncate(time.Microsecond)
	if state == DomainRiskAllow {
		validUntil = evaluation.ValidUntil
		nextDue = now.Add(policy.RevalidateAfter).UTC().Truncate(time.Microsecond)
	}
	return state, safeDomainReasonCategory(reason), validUntil, nextDue, nil
}

func safeDomainReasonCategory(value string) string {
	switch strings.TrimSpace(value) {
	case "policy-allow", "provider-block", "provider-pending", "provider-review", "provider-unknown", "provider-unavailable", "provider-incomplete", "provider-malformed", "provider-partial", "local-safety-not-approved", "decision-stale", "evaluation-started", "security-suspended", "revalidation-rate-limited":
		return strings.TrimSpace(value)
	default:
		return "provider-review"
	}
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

func validDomainProviderObservation(observation ProviderObservation) bool {
	if !validProviderName(observation.Provider) || normalizeSignalCode(observation.SignalCode) == "" || observation.ObservedAt.IsZero() {
		return false
	}
	switch observation.Outcome {
	case ProviderAllow, ProviderReview, ProviderBlock, ProviderUnknown, ProviderUnavailable:
		return true
	default:
		return false
	}
}

func validDomainRiskInput(in EvaluateDomainRiskInput) bool {
	if strings.TrimSpace(in.WorkspaceID) == "" || len(strings.TrimSpace(in.WorkspaceID)) > 64 || in.DomainID == 0 || strings.TrimSpace(in.IdempotencyKey) == "" || len(strings.TrimSpace(in.IdempotencyKey)) > 128 || strings.TrimSpace(in.ActorID) == "" || len(strings.TrimSpace(in.ActorID)) > 128 || strings.TrimSpace(in.Reason) == "" || strings.TrimSpace(in.CorrelationID) == "" || len(strings.TrimSpace(in.CorrelationID)) > 128 || in.Now.IsZero() {
		return false
	}
	return in.RequestKind == DomainRiskInitial || in.RequestKind == DomainRiskRevalidation
}

const domainRiskEvaluationSelect = `
SELECT id,workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,
       valid_until,checked_at,next_due_at,entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at
FROM domain_risk_evaluations`

func getDomainEvaluationByIdempotency(ctx context.Context, q interface{ QueryRowContext(context.Context, string, ...any) *sql.Row }, workspaceID, idempotencyKey string) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(q.QueryRowContext(ctx, domainRiskEvaluationSelect+` WHERE workspace_id=? AND idempotency_key=?`, workspaceID, idempotencyKey))
}

func getDomainEvaluationByIDTx(ctx context.Context, tx *sql.Tx, workspaceID string, id uint64) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(tx.QueryRowContext(ctx, domainRiskEvaluationSelect+` WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, id))
}

func getLatestDomainEvaluationTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64) (DomainRiskEvaluation, error) {
	return scanDomainRiskEvaluation(tx.QueryRowContext(ctx, domainRiskEvaluationSelect+` WHERE workspace_id=? AND domain_id=? ORDER BY id DESC LIMIT 1 FOR UPDATE`, workspaceID, domainID))
}

func scanDomainRiskEvaluation(row rowScanner) (DomainRiskEvaluation, error) {
	var evaluation DomainRiskEvaluation
	var requestKind, state string
	var validUntil, checkedAt, nextDueAt sql.NullTime
	if err := row.Scan(
		&evaluation.ID, &evaluation.WorkspaceID, &evaluation.DomainID, &evaluation.HostnameASCII, &evaluation.PolicyVersion,
		&requestKind, &evaluation.IdempotencyKey, &state, &evaluation.ReasonCategory, &evaluation.CorrelationID, &evaluation.ActorID,
		&validUntil, &checkedAt, &nextDueAt, &evaluation.EntitlementSnapshot, &evaluation.OwnershipSnapshot, &evaluation.IngressDNSSnapshot,
		&evaluation.HTTPSSnapshot, &evaluation.RoutingSnapshot, &evaluation.CreatedAt, &evaluation.UpdatedAt,
	); err != nil {
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

func (s *DomainRiskService) loadDomainObservations(ctx context.Context, evaluationID uint64) ([]ProviderObservation, error) {
	rows, err := s.Store.db.QueryContext(ctx, `
SELECT provider,outcome,signal_code,evidence_json,observed_at
FROM domain_risk_provider_observations
WHERE evaluation_id=? ORDER BY provider`, evaluationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderObservation{}
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
		out = append(out, observation)
	}
	return out, rows.Err()
}

func appendDomainRiskAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64, evaluationID *uint64, actorID, action, result, reasonCategory, correlationID string, metadata map[string]any) error {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	action = strings.TrimSpace(action)
	reasonCategory = safeDomainReasonCategory(reasonCategory)
	correlationID = strings.TrimSpace(correlationID)
	if tx == nil || workspaceID == "" || domainID == 0 || actorID == "" || action == "" || correlationID == "" {
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
VALUES (?,?,?,?,?,?,?,?,?)`, workspaceID, domainID, evaluationID, actorID, action, result, reasonCategory, correlationID, string(raw))
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

func requiredProviderNames(policy DomainRiskPolicy) []string {
	providers := append([]string(nil), policy.RequiredProviders...)
	for i := range providers {
		providers[i] = strings.TrimSpace(providers[i])
	}
	sort.Strings(providers)
	return providers
}
