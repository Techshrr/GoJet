package trust

import (
	"context"
	"errors"
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
	destination := DestinationPolicy{
		Version:           p.Version,
		RequiredProviders: p.RequiredProviders,
		AllowTTL:          p.AllowTTL,
	}
	if !destination.Validate() {
		return false
	}
	return p.RevalidateAfter > 0 && p.RevalidateAfter <= p.AllowTTL && p.RetryAfter > 0 && p.RetryAfter <= 24*time.Hour
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
	now := input.Now

	started, replay, err := s.beginDomainRiskEvaluation(ctx, input, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	if replay {
		observations, err := s.loadDomainRiskObservations(ctx, started.ID)
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
		if !validDomainRiskObservation(observation) {
			observation = normalizedProviderFailure("domain-provider", ProviderUnknown, "provider-malformed", "invalid-observation", now)
		}
		observations = append(observations, observation)
	}

	state, reason, validUntil, nextDueAt, err := evaluateDomainRiskSignals(s.Policy, observations, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	completed, err := s.finishDomainRiskEvaluation(ctx, input, started, observations, state, reason, validUntil, nextDueAt, now)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	return EvaluateDomainRiskResult{Evaluation: completed, Observations: observations, Created: true}, nil
}

func (s *DomainRiskService) GetDomainRiskEvaluation(ctx context.Context, workspaceID string, evaluationID uint64) (DomainRiskEvaluation, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.Store == nil || s.Store.db == nil || workspaceID == "" || evaluationID == 0 {
		return DomainRiskEvaluation{}, ErrInvalid
	}
	return loadDomainRiskEvaluationByID(ctx, s.Store.db, workspaceID, evaluationID, false)
}

func (s *DomainRiskService) ExpireAllowIfStale(ctx context.Context, workspaceID string, domainID uint64, actorID, correlationID string, now time.Time) (bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.Store == nil || s.Store.db == nil || workspaceID == "" || domainID == 0 || actorID == "" || correlationID == "" || now.IsZero() {
		return false, ErrInvalid
	}
	return s.expireDomainRiskAllow(ctx, workspaceID, domainID, actorID, correlationID, now.UTC().Truncate(time.Microsecond))
}

func (s *DomainRiskService) ApplySecuritySuspension(ctx context.Context, input domains.DomainSecuritySuspensionInput) (domains.Domain, error) {
	if s == nil || s.Store == nil || s.Store.db == nil {
		return domains.Domain{}, ErrInvalid
	}
	updated, err := domains.NewMySQLStore(s.Store.db).ApplyDomainSecuritySuspension(ctx, input)
	if err != nil {
		return domains.Domain{}, err
	}
	if err := s.appendDomainSecurityAudit(ctx, input, updated); err != nil {
		return domains.Domain{}, err
	}
	return updated, nil
}

func evaluateDomainRiskSignals(policy DomainRiskPolicy, observations []ProviderObservation, now time.Time) (DomainRiskState, string, *time.Time, time.Time, error) {
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

	nextDueAt := now.Add(policy.RetryAfter).UTC().Truncate(time.Microsecond)
	var validUntil *time.Time
	if state == DomainRiskAllow {
		validUntil = evaluation.ValidUntil
		nextDueAt = now.Add(policy.RevalidateAfter).UTC().Truncate(time.Microsecond)
	}
	return state, safeDomainRiskReason(reason), validUntil, nextDueAt, nil
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

func validDomainRiskObservation(observation ProviderObservation) bool {
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

func safeDomainRiskReason(value string) string {
	switch strings.TrimSpace(value) {
	case "policy-allow", "provider-block", "provider-pending", "provider-review", "provider-unknown", "provider-unavailable", "provider-incomplete", "provider-malformed", "provider-partial", "local-safety-not-approved", "decision-stale", "evaluation-started", "security-suspended", "revalidation-rate-limited":
		return strings.TrimSpace(value)
	default:
		return "provider-review"
	}
}

var errDomainRiskRateLimited = errors.New("domain risk revalidation rate limited")
