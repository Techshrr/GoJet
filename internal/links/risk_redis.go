package links

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const riskDecisionSchemaVersion = 1

type RiskState string

const (
	RiskAllow     RiskState = "allow"
	RiskReview    RiskState = "review"
	RiskBlock     RiskState = "block"
	RiskMissing   RiskState = "missing"
	RiskMalformed RiskState = "malformed"
	RiskStale     RiskState = "stale"
)

type RiskDecision struct {
	SchemaVersion int       `json:"schema_version"`
	Decision      RiskState `json:"decision"`
	Fingerprint   string    `json:"fingerprint"`
	CheckedAt     time.Time `json:"checked_at"`
	ValidUntil    time.Time `json:"valid_until"`
	PolicyVersion string    `json:"policy_version"`
}

type RedisRiskStore struct {
	client *redis.Client
}

func NewRedisClient(addr, password string, db int) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
}

func NewRedisRiskStore(client *redis.Client) *RedisRiskStore {
	return &RedisRiskStore{client: client}
}

func (s *RedisRiskStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func RiskDecisionKey(linkID uint64, fingerprint string) string {
	return fmt.Sprintf("risk:link:%d:%s", linkID, fingerprint)
}

func validateFingerprint(fingerprint string) bool {
	if len(fingerprint) != 64 {
		return false
	}
	for _, r := range fingerprint {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func validateDecisionState(state RiskState) bool {
	return state == RiskAllow || state == RiskReview || state == RiskBlock
}

// PutDecision records a scanner/policy decision bound to the exact current
// fingerprint. P05 integration tests call this method directly; a public client
// cannot set risk decisions through the Links API.
func (s *RedisRiskStore) PutDecision(ctx context.Context, linkID uint64, fingerprint string, state RiskState, policyVersion string, ttl time.Duration) (RiskDecision, error) {
	if linkID == 0 || !validateFingerprint(fingerprint) || !validateDecisionState(state) || strings.TrimSpace(policyVersion) == "" || ttl <= 0 {
		return RiskDecision{}, ErrInvalidInput
	}
	now := time.Now().UTC()
	decision := RiskDecision{
		SchemaVersion: riskDecisionSchemaVersion,
		Decision:      state,
		Fingerprint:   fingerprint,
		CheckedAt:     now,
		ValidUntil:    now.Add(ttl),
		PolicyVersion: strings.TrimSpace(policyVersion),
	}
	raw, err := json.Marshal(decision)
	if err != nil {
		return RiskDecision{}, err
	}
	if err := s.client.Set(ctx, RiskDecisionKey(linkID, fingerprint), raw, ttl).Err(); err != nil {
		return RiskDecision{}, err
	}
	return decision, nil
}

// Resolve classifies every runtime state explicitly. Only RiskAllow may proceed
// to routing/A-B/UTM/access behavior. All other states are fail-closed.
func (s *RedisRiskStore) Resolve(ctx context.Context, linkID uint64, fingerprint string, now time.Time) (RiskDecision, RiskState, error) {
	if linkID == 0 || !validateFingerprint(fingerprint) {
		return RiskDecision{}, RiskMalformed, ErrInvalidInput
	}
	raw, err := s.client.Get(ctx, RiskDecisionKey(linkID, fingerprint)).Bytes()
	if errors.Is(err, redis.Nil) {
		return RiskDecision{}, RiskMissing, nil
	}
	if err != nil {
		return RiskDecision{}, RiskMissing, err
	}

	var decision RiskDecision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return RiskDecision{}, RiskMalformed, nil
	}
	if decision.SchemaVersion != riskDecisionSchemaVersion || decision.Fingerprint != fingerprint || !validateDecisionState(decision.Decision) || strings.TrimSpace(decision.PolicyVersion) == "" || decision.CheckedAt.IsZero() || decision.ValidUntil.IsZero() {
		return decision, RiskMalformed, nil
	}
	if !decision.ValidUntil.After(now.UTC()) {
		return decision, RiskStale, nil
	}
	return decision, decision.Decision, nil
}

// StoreRawForTest is intentionally not provided: malformed/missing/stale cases
// are produced by the integration harness through its isolated Redis instance,
// so production code cannot accidentally expose a malformed-decision writer.
