package trust

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

const (
	SecurityManagePermission      = "security.manage"
	OverrideAuthorityVersion      = "p16-destination-risk-v1"
	maximumDestinationOverrideTTL = 24 * time.Hour
)

type PermissionAuthorizer interface {
	Authorize(context.Context, string, string) error
}

type DestinationOverridePolicyContext struct {
	AuthorityVersion  string        `json:"authority_version"`
	PolicyVersion     string        `json:"policy_version"`
	BaseDecisionID    uint64        `json:"base_decision_id"`
	BaseDecisionState DecisionState `json:"base_decision_state"`
}

type DestinationOverride struct {
	ID                 uint64                           `json:"id"`
	WorkspaceID        string                           `json:"workspace_id"`
	LinkID             uint64                           `json:"link_id"`
	RiskFingerprint    string                           `json:"risk_fingerprint"`
	PolicyVersion      string                           `json:"policy_version"`
	BaseDecisionID     uint64                           `json:"base_decision_id"`
	BaseDecisionState  DecisionState                    `json:"base_decision_state"`
	Decision           DecisionState                    `json:"decision"`
	Reason             string                           `json:"reason"`
	PolicyContext      DestinationOverridePolicyContext `json:"policy_context"`
	ActorID            string                           `json:"actor_id"`
	CorrelationID      string                           `json:"correlation_id"`
	ExpiresAt          time.Time                        `json:"expires_at"`
	InvalidatedAt      *time.Time                       `json:"invalidated_at,omitempty"`
	InvalidatedBy      string                           `json:"invalidated_by,omitempty"`
	InvalidationReason string                           `json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time                        `json:"created_at"`
}

type CreateDestinationOverrideInput struct {
	WorkspaceID     string
	LinkID          uint64
	RiskFingerprint string
	PolicyVersion   string
	Decision        DecisionState
	Reason          string
	PolicyContext   DestinationOverridePolicyContext
	ActorID         string
	CorrelationID   string
	ExpiresAt       time.Time
}

type InvalidateDestinationOverrideInput struct {
	WorkspaceID   string
	OverrideID    uint64
	ActorID       string
	Reason        string
	CorrelationID string
}

type DestinationAuthority struct {
	Decision      DestinationDecision  `json:"decision"`
	Override      *DestinationOverride `json:"override,omitempty"`
	State         DecisionState        `json:"state"`
	Fingerprint   string               `json:"fingerprint"`
	PolicyVersion string               `json:"policy_version"`
	ValidUntil    *time.Time           `json:"valid_until,omitempty"`
	Source        string               `json:"source"`
}

func (s *Store) CreateDestinationOverride(ctx context.Context, in CreateDestinationOverrideInput, authorizer PermissionAuthorizer, now time.Time) (DestinationOverride, error) {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.RiskFingerprint = strings.TrimSpace(in.RiskFingerprint)
	in.PolicyVersion = strings.TrimSpace(in.PolicyVersion)
	in.Reason = strings.TrimSpace(in.Reason)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.CorrelationID = strings.TrimSpace(in.CorrelationID)
	now = now.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if s == nil || s.db == nil || authorizer == nil || !validCreateOverrideInput(in, now) {
		return DestinationOverride{}, ErrInvalid
	}
	if err := authorizer.Authorize(ctx, in.ActorID, SecurityManagePermission); err != nil {
		return DestinationOverride{}, ErrUnauthorized
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DestinationOverride{}, err
	}
	defer func() { _ = tx.Rollback() }()

	currentFingerprint, err := currentLinkFingerprintTx(ctx, tx, in.WorkspaceID, in.LinkID)
	if err != nil {
		return DestinationOverride{}, err
	}
	if currentFingerprint != in.RiskFingerprint {
		return DestinationOverride{}, ErrStaleFingerprint
	}
	base, err := latestDestinationDecisionTx(ctx, tx, in.WorkspaceID, in.LinkID, currentFingerprint, in.PolicyVersion)
	if err != nil {
		return DestinationOverride{}, err
	}
	if in.PolicyContext.AuthorityVersion != OverrideAuthorityVersion ||
		in.PolicyContext.PolicyVersion != in.PolicyVersion ||
		in.PolicyContext.BaseDecisionID != base.ID ||
		in.PolicyContext.BaseDecisionState != base.State ||
		base.State == DecisionPending {
		return DestinationOverride{}, ErrConflict
	}

	policyRaw, err := json.Marshal(in.PolicyContext)
	if err != nil {
		return DestinationOverride{}, ErrInvalid
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO destination_risk_overrides
(workspace_id,link_id,risk_fingerprint,policy_version,base_decision_id,base_decision_state,decision,reason,policy_context_json,actor_id,correlation_id,expires_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.WorkspaceID,
		in.LinkID,
		in.RiskFingerprint,
		in.PolicyVersion,
		base.ID,
		string(base.State),
		string(in.Decision),
		in.Reason,
		string(policyRaw),
		in.ActorID,
		in.CorrelationID,
		in.ExpiresAt,
		now,
	)
	if err != nil {
		if !mysqlDuplicate(err) {
			return DestinationOverride{}, err
		}
		existing, existingErr := getDestinationOverrideByCorrelationTx(ctx, tx, in.WorkspaceID, in.CorrelationID)
		if existingErr != nil {
			return DestinationOverride{}, existingErr
		}
		if !sameOverrideRequest(existing, in) {
			return DestinationOverride{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return DestinationOverride{}, err
		}
		return existing, nil
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		return DestinationOverride{}, ErrConflict
	}
	override := DestinationOverride{
		ID:                uint64(id),
		WorkspaceID:       in.WorkspaceID,
		LinkID:            in.LinkID,
		RiskFingerprint:   in.RiskFingerprint,
		PolicyVersion:     in.PolicyVersion,
		BaseDecisionID:    base.ID,
		BaseDecisionState: base.State,
		Decision:          in.Decision,
		Reason:            in.Reason,
		PolicyContext:     in.PolicyContext,
		ActorID:           in.ActorID,
		CorrelationID:     in.CorrelationID,
		ExpiresAt:         in.ExpiresAt,
		CreatedAt:         now,
	}
	if err := appendRiskAuditTx(ctx, tx, in.WorkspaceID, &in.LinkID, nil, in.ActorID,
		"destination-risk.override-create", "success", in.Reason, in.CorrelationID,
		map[string]any{
			"override_id":         override.ID,
			"risk_fingerprint":    override.RiskFingerprint,
			"policy_version":      override.PolicyVersion,
			"base_decision_id":    override.BaseDecisionID,
			"base_decision_state": string(override.BaseDecisionState),
			"override_decision":   string(override.Decision),
			"expires_at":          override.ExpiresAt.Format(time.RFC3339Nano),
			"authority_version":   override.PolicyContext.AuthorityVersion,
		}); err != nil {
		return DestinationOverride{}, err
	}
	if err := tx.Commit(); err != nil {
		return DestinationOverride{}, err
	}
	return override, nil
}

func (s *Store) InvalidateDestinationOverride(ctx context.Context, in InvalidateDestinationOverrideInput, authorizer PermissionAuthorizer, now time.Time) (DestinationOverride, error) {
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.Reason = strings.TrimSpace(in.Reason)
	in.CorrelationID = strings.TrimSpace(in.CorrelationID)
	now = now.UTC().Truncate(time.Microsecond)
	if s == nil || s.db == nil || authorizer == nil || in.WorkspaceID == "" || in.OverrideID == 0 || in.ActorID == "" || len(in.ActorID) > 128 || in.Reason == "" || len(in.Reason) > 500 || in.CorrelationID == "" || len(in.CorrelationID) > 128 {
		return DestinationOverride{}, ErrInvalid
	}
	if err := authorizer.Authorize(ctx, in.ActorID, SecurityManagePermission); err != nil {
		return DestinationOverride{}, ErrUnauthorized
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DestinationOverride{}, err
	}
	defer func() { _ = tx.Rollback() }()
	override, err := getDestinationOverrideForUpdateTx(ctx, tx, in.WorkspaceID, in.OverrideID)
	if err != nil {
		return DestinationOverride{}, err
	}
	if override.InvalidatedAt != nil {
		return DestinationOverride{}, ErrConflict
	}
	res, err := tx.ExecContext(ctx, `
UPDATE destination_risk_overrides
SET invalidated_at=?,invalidated_by=?,invalidation_reason=?
WHERE id=? AND workspace_id=? AND invalidated_at IS NULL`, now, in.ActorID, in.Reason, override.ID, override.WorkspaceID)
	if err != nil {
		return DestinationOverride{}, err
	}
	rows, err := res.RowsAffected()
	if err != nil || rows != 1 {
		return DestinationOverride{}, ErrConflict
	}
	if err := appendRiskAuditTx(ctx, tx, override.WorkspaceID, &override.LinkID, nil, in.ActorID,
		"destination-risk.override-invalidate", "success", in.Reason, in.CorrelationID,
		map[string]any{
			"override_id":       override.ID,
			"risk_fingerprint":  override.RiskFingerprint,
			"policy_version":    override.PolicyVersion,
			"previous_decision": string(override.Decision),
		}); err != nil {
		return DestinationOverride{}, err
	}
	if err := tx.Commit(); err != nil {
		return DestinationOverride{}, err
	}
	override.InvalidatedAt = &now
	override.InvalidatedBy = in.ActorID
	override.InvalidationReason = in.Reason
	return override, nil
}

func (s *Store) CurrentDestinationAuthority(ctx context.Context, workspaceID string, linkID uint64, policyVersion string, now time.Time) (DestinationAuthority, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	policyVersion = strings.TrimSpace(policyVersion)
	now = now.UTC().Truncate(time.Microsecond)
	if s == nil || s.db == nil || workspaceID == "" || linkID == 0 || policyVersion == "" {
		return DestinationAuthority{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DestinationAuthority{}, err
	}
	defer func() { _ = tx.Rollback() }()
	fingerprint, err := currentLinkFingerprintTx(ctx, tx, workspaceID, linkID)
	if err != nil {
		return DestinationAuthority{}, err
	}
	decision, err := latestDestinationDecisionTx(ctx, tx, workspaceID, linkID, fingerprint, policyVersion)
	if err != nil {
		return DestinationAuthority{}, err
	}
	authority := DestinationAuthority{
		Decision:      decision,
		State:         decision.State,
		Fingerprint:   fingerprint,
		PolicyVersion: policyVersion,
		ValidUntil:    decision.ValidUntil,
		Source:        "policy",
	}
	override, err := activeDestinationOverrideTx(ctx, tx, workspaceID, linkID, fingerprint, policyVersion, decision, now)
	if err == nil {
		authority.Override = &override
		authority.State = override.Decision
		v := override.ExpiresAt
		authority.ValidUntil = &v
		authority.Source = "manual-override"
	} else if !errors.Is(err, ErrNotFound) {
		return DestinationAuthority{}, err
	}
	if err := tx.Commit(); err != nil {
		return DestinationAuthority{}, err
	}
	return authority, nil
}

func validCreateOverrideInput(in CreateDestinationOverrideInput, now time.Time) bool {
	if in.WorkspaceID == "" || in.LinkID == 0 || !validFingerprint(in.RiskFingerprint) || in.PolicyVersion == "" || len(in.PolicyVersion) > 64 || !validOverrideDecision(in.Decision) || in.Reason == "" || len(in.Reason) > 500 || in.ActorID == "" || len(in.ActorID) > 128 || in.CorrelationID == "" || len(in.CorrelationID) > 128 {
		return false
	}
	if !in.ExpiresAt.After(now) || in.ExpiresAt.Sub(now) > maximumDestinationOverrideTTL {
		return false
	}
	return in.PolicyContext.AuthorityVersion == OverrideAuthorityVersion &&
		in.PolicyContext.PolicyVersion == in.PolicyVersion &&
		in.PolicyContext.BaseDecisionID > 0 &&
		validBaseDecisionState(in.PolicyContext.BaseDecisionState)
}

func validOverrideDecision(state DecisionState) bool {
	return state == DecisionAllow || state == DecisionReview || state == DecisionBlock
}

func validBaseDecisionState(state DecisionState) bool {
	return state == DecisionAllow || state == DecisionReview || state == DecisionBlock || state == DecisionUnknown
}

func currentLinkFingerprintTx(ctx context.Context, tx *sql.Tx, workspaceID string, linkID uint64) (string, error) {
	var storedWorkspace, primary, storedFingerprint string
	var routingRaw, abRaw []byte
	err := tx.QueryRowContext(ctx, `
SELECT workspace_id,primary_destination,routing_json,ab_json,risk_fingerprint
FROM links
WHERE id=? AND deleted_at IS NULL
FOR UPDATE`, linkID).Scan(&storedWorkspace, &primary, &routingRaw, &abRaw, &storedFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if storedWorkspace != workspaceID {
		return "", ErrNotFound
	}
	var routing []links.RoutingRule
	var variants []links.ABVariant
	if err := json.Unmarshal(routingRaw, &routing); err != nil {
		return "", err
	}
	if err := json.Unmarshal(abRaw, &variants); err != nil {
		return "", err
	}
	fingerprint, _, err := links.RiskFingerprint(primary, routing, variants)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(storedFingerprint) != fingerprint {
		return "", ErrStaleFingerprint
	}
	return fingerprint, nil
}

func latestDestinationDecisionTx(ctx context.Context, tx *sql.Tx, workspaceID string, linkID uint64, fingerprint, policyVersion string) (DestinationDecision, error) {
	return scanDestinationDecision(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,
       decision_metadata_json,valid_until,decided_at,created_at
FROM destination_risk_decisions
WHERE workspace_id=? AND link_id=? AND risk_fingerprint=? AND policy_version=?
ORDER BY decided_at DESC,id DESC
LIMIT 1`, workspaceID, linkID, fingerprint, policyVersion))
}

func activeDestinationOverrideTx(ctx context.Context, tx *sql.Tx, workspaceID string, linkID uint64, fingerprint, policyVersion string, base DestinationDecision, now time.Time) (DestinationOverride, error) {
	return scanDestinationOverride(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,base_decision_id,base_decision_state,decision,reason,
       policy_context_json,actor_id,correlation_id,expires_at,invalidated_at,invalidated_by,invalidation_reason,created_at
FROM destination_risk_overrides
WHERE workspace_id=? AND link_id=? AND risk_fingerprint=? AND policy_version=?
  AND base_decision_id=? AND invalidated_at IS NULL AND expires_at>?
ORDER BY created_at DESC,id DESC
LIMIT 1`, workspaceID, linkID, fingerprint, policyVersion, base.ID, now))
}

func getDestinationOverrideByCorrelationTx(ctx context.Context, tx *sql.Tx, workspaceID, correlationID string) (DestinationOverride, error) {
	return scanDestinationOverride(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,base_decision_id,base_decision_state,decision,reason,
       policy_context_json,actor_id,correlation_id,expires_at,invalidated_at,invalidated_by,invalidation_reason,created_at
FROM destination_risk_overrides
WHERE workspace_id=? AND correlation_id=?`, workspaceID, correlationID))
}

func getDestinationOverrideForUpdateTx(ctx context.Context, tx *sql.Tx, workspaceID string, overrideID uint64) (DestinationOverride, error) {
	return scanDestinationOverride(tx.QueryRowContext(ctx, `
SELECT id,workspace_id,link_id,risk_fingerprint,policy_version,base_decision_id,base_decision_state,decision,reason,
       policy_context_json,actor_id,correlation_id,expires_at,invalidated_at,invalidated_by,invalidation_reason,created_at
FROM destination_risk_overrides
WHERE id=? AND workspace_id=?
FOR UPDATE`, overrideID, workspaceID))
}

func scanDestinationOverride(row rowScanner) (DestinationOverride, error) {
	var out DestinationOverride
	var baseState, decision string
	var policyRaw []byte
	var invalidatedAt sql.NullTime
	var invalidatedBy, invalidationReason sql.NullString
	err := row.Scan(
		&out.ID,
		&out.WorkspaceID,
		&out.LinkID,
		&out.RiskFingerprint,
		&out.PolicyVersion,
		&out.BaseDecisionID,
		&baseState,
		&decision,
		&out.Reason,
		&policyRaw,
		&out.ActorID,
		&out.CorrelationID,
		&out.ExpiresAt,
		&invalidatedAt,
		&invalidatedBy,
		&invalidationReason,
		&out.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return DestinationOverride{}, ErrNotFound
	}
	if err != nil {
		return DestinationOverride{}, err
	}
	out.BaseDecisionState = DecisionState(baseState)
	out.Decision = DecisionState(decision)
	if err := json.Unmarshal(policyRaw, &out.PolicyContext); err != nil {
		return DestinationOverride{}, err
	}
	if invalidatedAt.Valid {
		v := invalidatedAt.Time
		out.InvalidatedAt = &v
	}
	if invalidatedBy.Valid {
		out.InvalidatedBy = invalidatedBy.String
	}
	if invalidationReason.Valid {
		out.InvalidationReason = invalidationReason.String
	}
	return out, nil
}

func sameOverrideRequest(existing DestinationOverride, in CreateDestinationOverrideInput) bool {
	return existing.LinkID == in.LinkID &&
		existing.RiskFingerprint == in.RiskFingerprint &&
		existing.PolicyVersion == in.PolicyVersion &&
		existing.Decision == in.Decision &&
		existing.Reason == in.Reason &&
		existing.ActorID == in.ActorID &&
		existing.ExpiresAt.Equal(in.ExpiresAt) &&
		reflect.DeepEqual(existing.PolicyContext, in.PolicyContext)
}
