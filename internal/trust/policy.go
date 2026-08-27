package trust

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrPolicyMismatch = errors.New("destination risk policy mismatch")

// DestinationPolicy is local decision authority. Provider observations are
// inputs only; they cannot independently produce an allow decision.
type DestinationPolicy struct {
	Version           string
	RequiredProviders []string
	AllowTTL          time.Duration
}

type PolicyEvaluation struct {
	State          DecisionState
	ReasonCategory string
	ValidUntil     *time.Time
	Metadata       map[string]any
}

func (p DestinationPolicy) Validate() bool {
	version := strings.TrimSpace(p.Version)
	if version == "" || len(version) > 64 || p.AllowTTL <= 0 || p.AllowTTL > 24*time.Hour {
		return false
	}
	if len(p.RequiredProviders) == 0 || len(p.RequiredProviders) > 16 {
		return false
	}
	seen := map[string]struct{}{}
	for _, raw := range p.RequiredProviders {
		provider := strings.TrimSpace(raw)
		if provider == "" || len(provider) > 64 {
			return false
		}
		if _, exists := seen[provider]; exists {
			return false
		}
		seen[provider] = struct{}{}
	}
	return true
}

// EvaluateDestinationPolicy deterministically maps durable provider signals
// into local policy authority. Any missing/unknown/unavailable/review signal is
// non-allow, any block wins, and provider allow still requires the independent
// localSafetyPassed gate.
func EvaluateDestinationPolicy(policy DestinationPolicy, observations []ProviderObservation, localSafetyPassed bool, now time.Time) (PolicyEvaluation, error) {
	if !policy.Validate() {
		return PolicyEvaluation{}, ErrInvalid
	}
	now = now.UTC().Truncate(time.Microsecond)
	required := make([]string, 0, len(policy.RequiredProviders))
	for _, provider := range policy.RequiredProviders {
		required = append(required, strings.TrimSpace(provider))
	}
	sort.Strings(required)

	byProvider := make(map[string]ProviderObservation, len(observations))
	for _, observation := range observations {
		provider := strings.TrimSpace(observation.Provider)
		if provider == "" {
			return PolicyEvaluation{}, ErrInvalid
		}
		if _, duplicate := byProvider[provider]; duplicate {
			return PolicyEvaluation{}, ErrConflict
		}
		byProvider[provider] = observation
	}

	missing := make([]string, 0)
	counts := map[string]int{
		"allow":       0,
		"review":      0,
		"block":       0,
		"unknown":     0,
		"unavailable": 0,
	}
	for _, provider := range required {
		observation, exists := byProvider[provider]
		if !exists {
			missing = append(missing, provider)
			continue
		}
		switch observation.Outcome {
		case ProviderAllow:
			counts["allow"]++
		case ProviderReview:
			counts["review"]++
		case ProviderBlock:
			counts["block"]++
		case ProviderUnknown:
			counts["unknown"]++
		case ProviderUnavailable:
			counts["unavailable"]++
		default:
			return PolicyEvaluation{}, ErrInvalid
		}
	}

	metadata := map[string]any{
		"policy_version":             strings.TrimSpace(policy.Version),
		"required_provider_count":    len(required),
		"observed_provider_count":    len(required) - len(missing),
		"missing_provider_count":     len(missing),
		"provider_allow_count":       counts["allow"],
		"provider_review_count":      counts["review"],
		"provider_block_count":       counts["block"],
		"provider_unknown_count":     counts["unknown"],
		"provider_unavailable_count": counts["unavailable"],
		"local_safety_passed":        localSafetyPassed,
	}

	evaluation := PolicyEvaluation{Metadata: metadata}
	switch {
	case counts["block"] > 0:
		evaluation.State = DecisionBlock
		evaluation.ReasonCategory = "provider-block"
	case len(missing) > 0:
		evaluation.State = DecisionPending
		evaluation.ReasonCategory = "provider-pending"
	case counts["review"] > 0:
		evaluation.State = DecisionReview
		evaluation.ReasonCategory = "provider-review"
	case counts["unknown"] > 0:
		evaluation.State = DecisionReview
		evaluation.ReasonCategory = "provider-unknown"
	case counts["unavailable"] > 0:
		evaluation.State = DecisionReview
		evaluation.ReasonCategory = "provider-unavailable"
	case counts["allow"] != len(required):
		evaluation.State = DecisionReview
		evaluation.ReasonCategory = "provider-incomplete"
	case !localSafetyPassed:
		evaluation.State = DecisionReview
		evaluation.ReasonCategory = "local-safety-not-approved"
	default:
		evaluation.State = DecisionAllow
		evaluation.ReasonCategory = "policy-allow"
		validUntil := now.Add(policy.AllowTTL).UTC().Truncate(time.Microsecond)
		evaluation.ValidUntil = &validUntil
	}
	return evaluation, nil
}
