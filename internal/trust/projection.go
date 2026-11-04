package trust

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

type ProjectionCandidate struct {
	WorkspaceID string
	LinkID      uint64
	Fingerprint string
}

type ProjectionResult struct {
	Decision DestinationDecision
	Override *DestinationOverride
	Source   string
	Runtime  links.RiskDecision
}

func (s *Store) CurrentDestinationDecision(ctx context.Context, workspaceID string, linkID uint64, policyVersion string) (DestinationDecision, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	policyVersion = strings.TrimSpace(policyVersion)
	if s == nil || s.db == nil || workspaceID == "" || linkID == 0 || policyVersion == "" {
		return DestinationDecision{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DestinationDecision{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var storedWorkspace, primary, storedFingerprint string
	var routingRaw, abRaw []byte
	err = tx.QueryRowContext(ctx, `
SELECT workspace_id,primary_destination,routing_json,ab_json,risk_fingerprint
FROM links
WHERE id=? AND deleted_at IS NULL`, linkID).Scan(&storedWorkspace, &primary, &routingRaw, &abRaw, &storedFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return DestinationDecision{}, ErrNotFound
	}
	if err != nil {
		return DestinationDecision{}, err
	}
	if storedWorkspace != workspaceID {
		return DestinationDecision{}, ErrNotFound
	}
	var routing []links.RoutingRule
	var variants []links.ABVariant
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return DestinationDecision{}, fmt.Errorf("decode projection routing: %w", err)
	}
	if err := json.Unmarshal(abRaw, &variants); err != nil {
		return DestinationDecision{}, fmt.Errorf("decode projection A/B: %w", err)
	}
	fingerprint, _, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return DestinationDecision{}, err
	}
	if strings.ToLower(strings.TrimSpace(storedFingerprint)) != fingerprint {
		return DestinationDecision{}, ErrStaleFingerprint
	}

	decision, err := scanDestinationDecision(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,
       decision_metadata_json,valid_until,decided_at,created_at
FROM destination_risk_decisions
WHERE workspace_id=? AND link_id=? AND risk_fingerprint=? AND policy_version=?
ORDER BY decided_at DESC,id DESC
LIMIT 1`, workspaceID, linkID, fingerprint, policyVersion))
	if err != nil {
		return DestinationDecision{}, err
	}
	if err := tx.Commit(); err != nil {
		return DestinationDecision{}, err
	}
	return decision, nil
}

func (s *Store) ListProjectionCandidates(ctx context.Context, policyVersion string, limit int) ([]ProjectionCandidate, error) {
	return s.listProjectionPage(ctx, policyVersion, 0, ^uint64(0), limit)
}

func (s *Store) listProjectionPage(ctx context.Context, policyVersion string, after, through uint64, limit int) ([]ProjectionCandidate, error) {
	policyVersion = strings.TrimSpace(policyVersion)
	if s == nil || s.db == nil || policyVersion == "" || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT d.workspace_id,d.link_id,l.risk_fingerprint
FROM destination_risk_decisions d
JOIN links l ON l.id=d.link_id AND l.workspace_id=d.workspace_id AND l.deleted_at IS NULL
WHERE d.policy_version=? AND d.risk_fingerprint=l.risk_fingerprint AND l.id>? AND l.id<=?
  AND d.state IN ('allow','review','block','unknown')
ORDER BY d.link_id
LIMIT ?`, policyVersion, after, through, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ProjectionCandidate, 0)
	for rows.Next() {
		var candidate ProjectionCandidate
		if err := rows.Scan(&candidate.WorkspaceID, &candidate.LinkID, &candidate.Fingerprint); err != nil {
			return nil, err
		}
		out = append(out, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func ProjectCurrentDestinationDecision(ctx context.Context, store *Store, runtime *links.RedisRiskStore, workspaceID string, linkID uint64, policyVersion string, now time.Time, maxTTL time.Duration) (ProjectionResult, error) {
	if store == nil || runtime == nil || maxTTL <= 0 || maxTTL > 24*time.Hour {
		return ProjectionResult{}, ErrInvalid
	}
	now = now.UTC()
	authority, err := store.EffectiveDestinationAuthority(ctx, workspaceID, linkID, policyVersion, now)
	if err != nil {
		return ProjectionResult{}, err
	}
	var state links.RiskState
	var ttl time.Duration
	switch authority.State {
	case DecisionAllow:
		state = links.RiskAllow
		if authority.ValidUntil == nil || !authority.ValidUntil.After(now) {
			return ProjectionResult{}, ErrStaleFingerprint
		}
		ttl = authority.ValidUntil.Sub(now)
	case DecisionReview:
		state = links.RiskReview
		ttl = maxTTL
	case DecisionBlock:
		state = links.RiskBlock
		ttl = maxTTL
	default:
		return ProjectionResult{}, ErrConflict
	}
	if authority.ValidUntil != nil {
		remaining := authority.ValidUntil.Sub(now)
		if remaining <= 0 {
			return ProjectionResult{}, ErrStaleFingerprint
		}
		if remaining < ttl {
			ttl = remaining
		}
	}
	if ttl > maxTTL {
		ttl = maxTTL
	}
	if ttl <= 0 {
		return ProjectionResult{}, ErrStaleFingerprint
	}
	runtimeDecision, err := runtime.PutDecisionUntil(ctx, authority.Decision.LinkID, authority.Fingerprint, state, authority.PolicyVersion, now.Add(ttl))
	if err != nil {
		return ProjectionResult{}, err
	}
	return ProjectionResult{
		Decision: authority.Decision,
		Override: authority.Override,
		Source:   authority.Source,
		Runtime:  runtimeDecision,
	}, nil
}

type RiskProjector struct {
	Store         *Store
	Runtime       *links.RedisRiskStore
	PolicyVersion string
	MaxTTL        time.Duration
	BatchSize     int
	cursor        uint64
}

func (p *RiskProjector) RunOnce(ctx context.Context) (int, error) {
	if p == nil || p.Store == nil || p.Runtime == nil || strings.TrimSpace(p.PolicyVersion) == "" || p.MaxTTL <= 0 || p.BatchSize < 1 || p.BatchSize > 500 || p.MaxTTL > 24*time.Hour {
		return 0, ErrInvalid
	}
	candidates, err := p.Store.listProjectionPage(ctx, p.PolicyVersion, p.cursor, ^uint64(0), p.BatchSize)
	if err != nil {
		return 0, err
	}
	projected := 0
	now := time.Now().UTC()
	for _, candidate := range candidates {
		_, err := ProjectCurrentDestinationDecision(ctx, p.Store, p.Runtime, candidate.WorkspaceID, candidate.LinkID, p.PolicyVersion, now, p.MaxTTL)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrStaleFingerprint) || errors.Is(err, ErrConflict) {
			if err := p.Runtime.DeleteDecision(ctx, candidate.LinkID, candidate.Fingerprint); err != nil {
				return projected, err
			}
			p.cursor = candidate.LinkID
			continue
		}
		if err != nil {
			return projected, err
		}
		projected++
		p.cursor = candidate.LinkID
	}
	if len(candidates) < p.BatchSize {
		p.cursor = 0
	}
	return projected, nil
}

// ProjectionRestore reports only a completed bounded scan of durable authority.
// The caller must stop all writers and invalidate old projection keys first.
type ProjectionRestore struct {
	Complete  bool `json:"complete"`
	Visited   int  `json:"visited"`
	Projected int  `json:"projected"`
	Withheld  int  `json:"withheld"`
}

// Rebuild does not call providers, consume jobs or deliver webhooks. Unknown,
// expired and stale authority is withheld, never converted into an allow.
func (p *RiskProjector) Rebuild(ctx context.Context, maximum int) (ProjectionRestore, error) {
	result := ProjectionRestore{}
	if p == nil || p.Store == nil || p.Store.db == nil || p.Runtime == nil ||
		strings.TrimSpace(p.PolicyVersion) == "" || p.MaxTTL <= 0 || p.MaxTTL > 24*time.Hour ||
		p.BatchSize < 1 || p.BatchSize > 500 || maximum < 1 || maximum > 100000 {
		return result, ErrInvalid
	}
	var through uint64
	if err := p.Store.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM links").Scan(&through); err != nil {
		return result, err
	}
	var after uint64
	for {
		candidates, err := p.Store.listProjectionPage(ctx, p.PolicyVersion, after, through, p.BatchSize)
		if err != nil {
			return result, err
		}
		for _, candidate := range candidates {
			if result.Visited >= maximum {
				return result, ErrInvalid
			}
			_, err := ProjectCurrentDestinationDecision(ctx, p.Store, p.Runtime, candidate.WorkspaceID,
				candidate.LinkID, p.PolicyVersion, time.Now().UTC(), p.MaxTTL)
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrStaleFingerprint) || errors.Is(err, ErrConflict) {
				if err := p.Runtime.DeleteDecision(ctx, candidate.LinkID, candidate.Fingerprint); err != nil {
					return result, err
				}
				result.Withheld++
			} else if err != nil {
				return result, err
			} else {
				result.Projected++
			}
			result.Visited++
			after = candidate.LinkID
		}
		if len(candidates) < p.BatchSize {
			result.Complete = true
			return result, nil
		}
	}
}
