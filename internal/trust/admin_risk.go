package trust

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

const DomainsRiskManagePermission = "domains.risk.manage"

type AdminDestinationRiskRecord struct {
	ID                uint64          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	LinkID            uint64          `json:"link_id"`
	RiskFingerprint   string          `json:"risk_fingerprint"`
	PolicyVersion     string          `json:"policy_version"`
	RequestKind       ScanRequestKind `json:"request_kind"`
	ScanStatus        ScanStatus      `json:"scan_status"`
	DecisionState     DecisionState   `json:"decision_state"`
	ReasonCategory    string          `json:"reason_category"`
	Attempts          uint32          `json:"attempts"`
	MaxAttempts       uint32          `json:"max_attempts"`
	TargetCount       uint32          `json:"target_count"`
	ProviderCount     uint32          `json:"provider_count"`
	HasActiveOverride bool            `json:"has_active_override"`
	ValidUntil        *time.Time      `json:"valid_until,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type AdminDomainRiskRecord struct {
	EvaluationID      uint64                `json:"evaluation_id"`
	WorkspaceID       string                `json:"workspace_id"`
	DomainID          uint64                `json:"domain_id"`
	HostnameASCII     string                `json:"hostname_ascii"`
	PolicyVersion     string                `json:"policy_version"`
	RequestKind       DomainRiskRequestKind `json:"request_kind"`
	State             DomainRiskState       `json:"state"`
	ReasonCategory    string                `json:"reason_category"`
	EntitlementStatus string                `json:"entitlement_status"`
	OwnershipStatus   string                `json:"ownership_status"`
	IngressDNSStatus  string                `json:"ingress_dns_status"`
	HTTPSStatus       string                `json:"https_status"`
	RoutingStatus     string                `json:"routing_status"`
	ProviderCount     uint32                `json:"provider_count"`
	ValidUntil        *time.Time            `json:"valid_until,omitempty"`
	CheckedAt         *time.Time            `json:"checked_at,omitempty"`
	NextDueAt         *time.Time            `json:"next_due_at,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

type AdminDestinationRescanInput struct {
	RiskID         uint64
	ActorID        string
	CorrelationID  string
	IdempotencyKey string
}

type AdminDestinationOverrideInput struct {
	RiskID        uint64
	Decision      DecisionState
	Reason        string
	ActorID       string
	CorrelationID string
	ExpiresAt     time.Time
}

type AdminDomainRevalidateInput struct {
	DomainID       uint64
	ActorID        string
	Reason         string
	CorrelationID  string
	IdempotencyKey string
	Now            time.Time
}

// ListAdminDestinationRisks intentionally returns only normalized control-plane
// state. Reachable targets and provider evidence stay server-side.
func (s *Store) ListAdminDestinationRisks(ctx context.Context, limit int) ([]AdminDestinationRiskRecord, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, adminDestinationRiskSelect+`
ORDER BY s.created_at DESC,s.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminDestinationRiskRecord, 0)
	for rows.Next() {
		record, err := scanAdminDestinationRisk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetAdminDestinationRisk(ctx context.Context, riskID uint64) (AdminDestinationRiskRecord, error) {
	if s == nil || s.db == nil || riskID == 0 {
		return AdminDestinationRiskRecord{}, ErrInvalid
	}
	return scanAdminDestinationRisk(s.db.QueryRowContext(ctx, adminDestinationRiskSelect+` WHERE s.id=?`, riskID))
}

func (s *Store) AdminRescanDestinationRisk(ctx context.Context, input AdminDestinationRescanInput, authorizer PermissionAuthorizer) (EnqueueDestinationScanResult, error) {
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if s == nil || s.db == nil || authorizer == nil || input.RiskID == 0 || input.ActorID == "" || input.CorrelationID == "" || len(input.CorrelationID) > 128 || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 {
		return EnqueueDestinationScanResult{}, ErrInvalid
	}
	if err := authorizer.Authorize(ctx, input.ActorID, SecurityManagePermission); err != nil {
		return EnqueueDestinationScanResult{}, ErrUnauthorized
	}
	record, err := s.GetAdminDestinationRisk(ctx, input.RiskID)
	if err != nil {
		return EnqueueDestinationScanResult{}, err
	}
	return s.EnqueueDestinationScan(ctx, EnqueueDestinationScanInput{
		WorkspaceID:     record.WorkspaceID,
		LinkID:          record.LinkID,
		RiskFingerprint: record.RiskFingerprint,
		PolicyVersion:   record.PolicyVersion,
		RequestKind:     ScanRequestRescan,
		IdempotencyKey:  input.IdempotencyKey,
		CorrelationID:   input.CorrelationID,
		ActorID:         input.ActorID,
		MaxAttempts:     5,
	})
}

func (s *Store) AdminOverrideDestinationRisk(ctx context.Context, input AdminDestinationOverrideInput, authorizer PermissionAuthorizer, now time.Time) (DestinationOverride, error) {
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	now = now.UTC().Truncate(time.Microsecond)
	if s == nil || s.db == nil || authorizer == nil || input.RiskID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.ExpiresAt.IsZero() {
		return DestinationOverride{}, ErrInvalid
	}
	if err := authorizer.Authorize(ctx, input.ActorID, SecurityManagePermission); err != nil {
		return DestinationOverride{}, ErrUnauthorized
	}
	record, err := s.GetAdminDestinationRisk(ctx, input.RiskID)
	if err != nil {
		return DestinationOverride{}, err
	}
	authority, err := s.CurrentDestinationAuthority(ctx, record.WorkspaceID, record.LinkID, record.PolicyVersion, now)
	if err != nil {
		return DestinationOverride{}, err
	}
	if authority.Fingerprint != record.RiskFingerprint || authority.Decision.ID == 0 {
		return DestinationOverride{}, ErrStaleFingerprint
	}
	return s.CreateDestinationOverride(ctx, CreateDestinationOverrideInput{
		WorkspaceID:     record.WorkspaceID,
		LinkID:          record.LinkID,
		RiskFingerprint: record.RiskFingerprint,
		PolicyVersion:   record.PolicyVersion,
		Decision:        input.Decision,
		Reason:          input.Reason,
		PolicyContext: DestinationOverridePolicyContext{
			AuthorityVersion:  OverrideAuthorityVersion,
			PolicyVersion:     record.PolicyVersion,
			BaseDecisionID:    authority.Decision.ID,
			BaseDecisionState: authority.Decision.State,
		},
		ActorID:       input.ActorID,
		CorrelationID: input.CorrelationID,
		ExpiresAt:     input.ExpiresAt.UTC(),
	}, authorizer, now)
}

func (s *Store) ListAdminDomainRisks(ctx context.Context, limit int) ([]AdminDomainRiskRecord, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, adminDomainRiskSelect+`
WHERE e.id=(SELECT e2.id FROM domain_risk_evaluations e2 WHERE e2.domain_id=e.domain_id ORDER BY e2.id DESC LIMIT 1)
ORDER BY e.updated_at DESC,e.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminDomainRiskRecord, 0)
	for rows.Next() {
		record, err := scanAdminDomainRisk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetAdminDomainRisk(ctx context.Context, domainID uint64) (AdminDomainRiskRecord, error) {
	if s == nil || s.db == nil || domainID == 0 {
		return AdminDomainRiskRecord{}, ErrInvalid
	}
	return scanAdminDomainRisk(s.db.QueryRowContext(ctx, adminDomainRiskSelect+`
WHERE e.domain_id=?
ORDER BY e.id DESC
LIMIT 1`, domainID))
}

func (s *DomainRiskService) AdminRevalidateDomainRisk(ctx context.Context, input AdminDomainRevalidateInput, authorizer PermissionAuthorizer) (EvaluateDomainRiskResult, error) {
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Reason = strings.TrimSpace(input.Reason)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.Now = input.Now.UTC().Truncate(time.Microsecond)
	if s == nil || s.Store == nil || s.Store.db == nil || authorizer == nil || input.DomainID == 0 || input.ActorID == "" || input.Reason == "" || input.CorrelationID == "" || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || input.Now.IsZero() {
		return EvaluateDomainRiskResult{}, ErrInvalid
	}
	if err := authorizer.Authorize(ctx, input.ActorID, DomainsRiskManagePermission); err != nil {
		return EvaluateDomainRiskResult{}, ErrUnauthorized
	}
	record, err := s.Store.GetAdminDomainRisk(ctx, input.DomainID)
	if err != nil {
		return EvaluateDomainRiskResult{}, err
	}
	return s.Evaluate(ctx, EvaluateDomainRiskInput{
		WorkspaceID:    record.WorkspaceID,
		DomainID:       record.DomainID,
		RequestKind:    DomainRiskRevalidation,
		IdempotencyKey: input.IdempotencyKey,
		ActorID:        input.ActorID,
		Reason:         input.Reason,
		CorrelationID:  input.CorrelationID,
		Now:            input.Now,
	})
}

const adminDestinationRiskSelect = `
SELECT s.id,s.workspace_id,s.link_id,s.risk_fingerprint,s.policy_version,s.request_kind,s.status,
       COALESCE(d.state,'pending'),COALESCE(d.reason_category,'evaluation-started'),s.attempts,s.max_attempts,
       (SELECT COUNT(*) FROM destination_risk_scan_targets t WHERE t.scan_id=s.id),
       (SELECT COUNT(*) FROM destination_risk_provider_observations p WHERE p.scan_id=s.id),
       CASE WHEN EXISTS(
           SELECT 1 FROM destination_risk_overrides o
           WHERE o.workspace_id=s.workspace_id AND o.link_id=s.link_id AND o.risk_fingerprint=s.risk_fingerprint
             AND o.policy_version=s.policy_version AND o.invalidated_at IS NULL AND o.expires_at>CURRENT_TIMESTAMP(6)
       ) THEN 1 ELSE 0 END,
       d.valid_until,s.created_at,s.updated_at
FROM destination_risk_scans s
LEFT JOIN destination_risk_decisions d ON d.scan_id=s.id`

const adminDomainRiskSelect = `
SELECT e.id,e.workspace_id,e.domain_id,e.hostname_ascii,e.policy_version,e.request_kind,e.state,e.reason_category,
       e.entitlement_snapshot,e.ownership_snapshot,e.ingress_dns_snapshot,e.https_snapshot,e.routing_snapshot,
       (SELECT COUNT(*) FROM domain_risk_provider_observations p WHERE p.evaluation_id=e.id),
       e.valid_until,e.checked_at,e.next_due_at,e.created_at,e.updated_at
FROM domain_risk_evaluations e`

type adminRiskScanner interface {
	Scan(...any) error
}

func scanAdminDestinationRisk(row adminRiskScanner) (AdminDestinationRiskRecord, error) {
	var record AdminDestinationRiskRecord
	var requestKind, scanStatus, decisionState string
	var activeOverride uint8
	var validUntil sql.NullTime
	if err := row.Scan(&record.ID, &record.WorkspaceID, &record.LinkID, &record.RiskFingerprint, &record.PolicyVersion,
		&requestKind, &scanStatus, &decisionState, &record.ReasonCategory, &record.Attempts, &record.MaxAttempts,
		&record.TargetCount, &record.ProviderCount, &activeOverride, &validUntil, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AdminDestinationRiskRecord{}, ErrNotFound
		}
		return AdminDestinationRiskRecord{}, err
	}
	record.RequestKind = ScanRequestKind(requestKind)
	record.ScanStatus = ScanStatus(scanStatus)
	record.DecisionState = DecisionState(decisionState)
	record.HasActiveOverride = activeOverride == 1
	if validUntil.Valid {
		v := validUntil.Time.UTC()
		record.ValidUntil = &v
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}

func scanAdminDomainRisk(row adminRiskScanner) (AdminDomainRiskRecord, error) {
	var record AdminDomainRiskRecord
	var requestKind, state string
	var validUntil, checkedAt, nextDueAt sql.NullTime
	if err := row.Scan(&record.EvaluationID, &record.WorkspaceID, &record.DomainID, &record.HostnameASCII, &record.PolicyVersion,
		&requestKind, &state, &record.ReasonCategory, &record.EntitlementStatus, &record.OwnershipStatus,
		&record.IngressDNSStatus, &record.HTTPSStatus, &record.RoutingStatus, &record.ProviderCount,
		&validUntil, &checkedAt, &nextDueAt, &record.CreatedAt, &record.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AdminDomainRiskRecord{}, ErrNotFound
		}
		return AdminDomainRiskRecord{}, err
	}
	record.RequestKind = DomainRiskRequestKind(requestKind)
	record.State = DomainRiskState(state)
	if validUntil.Valid {
		v := validUntil.Time.UTC()
		record.ValidUntil = &v
	}
	if checkedAt.Valid {
		v := checkedAt.Time.UTC()
		record.CheckedAt = &v
	}
	if nextDueAt.Valid {
		v := nextDueAt.Time.UTC()
		record.NextDueAt = &v
	}
	record.CreatedAt = record.CreatedAt.UTC()
	record.UpdatedAt = record.UpdatedAt.UTC()
	return record, nil
}
