package bio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/redis/go-redis/v9"
)

type RiskAuthority interface {
	Resolve(ctx context.Context, childID uint64, fingerprint string, now time.Time) (links.RiskDecision, links.RiskState, error)
}

type RedisRiskAuthority struct {
	client *redis.Client
}

func NewRedisRiskAuthority(client *redis.Client) *RedisRiskAuthority {
	return &RedisRiskAuthority{client: client}
}

func (r *RedisRiskAuthority) Ping(ctx context.Context) error {
	if r == nil || r.client == nil {
		return errors.New("bio risk authority unavailable")
	}
	return r.client.Ping(ctx).Err()
}

func BioRiskDecisionKey(childID uint64, fingerprint string) string {
	return fmt.Sprintf("risk:bio-child:%d:%s", childID, fingerprint)
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validDecisionState(state links.RiskState) bool {
	return state == links.RiskAllow || state == links.RiskReview || state == links.RiskBlock
}

// PutDecision is an internal integration/scanner seam. No Bio HTTP route exposes
// it, so browser/API clients cannot self-approve a child destination.
func (r *RedisRiskAuthority) PutDecision(ctx context.Context, childID uint64, fingerprint string, state links.RiskState, policyVersion string, ttl time.Duration) (links.RiskDecision, error) {
	if r == nil || r.client == nil || childID == 0 || !validFingerprint(fingerprint) || !validDecisionState(state) || strings.TrimSpace(policyVersion) == "" || ttl <= 0 {
		return links.RiskDecision{}, ErrInvalidInput
	}
	now := time.Now().UTC()
	decision := links.RiskDecision{
		SchemaVersion: 1,
		Decision:      state,
		Fingerprint:   fingerprint,
		CheckedAt:     now,
		ValidUntil:    now.Add(ttl),
		PolicyVersion: strings.TrimSpace(policyVersion),
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		return links.RiskDecision{}, err
	}
	if err := r.client.Set(ctx, BioRiskDecisionKey(childID, fingerprint), raw, ttl).Err(); err != nil {
		return links.RiskDecision{}, err
	}
	return decision, nil
}

// Resolve mirrors the P05 current-fingerprint risk semantics. Only an exact,
// unexpired allow is navigable. Missing, malformed, stale and transport errors
// remain fail-closed at the caller.
func (r *RedisRiskAuthority) Resolve(ctx context.Context, childID uint64, fingerprint string, now time.Time) (links.RiskDecision, links.RiskState, error) {
	if r == nil || r.client == nil || childID == 0 || !validFingerprint(fingerprint) {
		return links.RiskDecision{}, links.RiskMalformed, ErrInvalidInput
	}
	raw, err := r.client.Get(ctx, BioRiskDecisionKey(childID, fingerprint)).Bytes()
	if errors.Is(err, redis.Nil) {
		return links.RiskDecision{}, links.RiskMissing, nil
	}
	if err != nil {
		return links.RiskDecision{}, links.RiskMissing, err
	}

	var decision links.RiskDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return links.RiskDecision{}, links.RiskMalformed, nil
	}
	if decision.SchemaVersion != 1 ||
		decision.Fingerprint != fingerprint ||
		!validDecisionState(decision.Decision) ||
		strings.TrimSpace(decision.PolicyVersion) == "" ||
		decision.CheckedAt.IsZero() ||
		decision.ValidUntil.IsZero() {
		return decision, links.RiskMalformed, nil
	}
	if !decision.ValidUntil.After(now.UTC()) {
		return decision, links.RiskStale, nil
	}
	return decision, decision.Decision, nil
}

func mapRiskState(state links.RiskState) string {
	switch state {
	case links.RiskAllow:
		return "allowed"
	case links.RiskBlock:
		return "blocked"
	default:
		return "review"
	}
}
