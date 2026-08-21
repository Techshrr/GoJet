package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type DomainRiskObservation struct {
	Status        DomainRiskStatus `json:"status"`
	PolicyVersion string           `json:"policy_version"`
	EvidenceRef   string           `json:"evidence_ref"`
}

type DomainRiskEvaluator interface {
	Evaluate(ctx context.Context, hostnameASCII string) (DomainRiskObservation, error)
}

type VerifyDomainRiskInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

type DomainRiskVerificationResult struct {
	Domain      Domain                `json:"domain"`
	Observation DomainRiskObservation `json:"observation"`
}

type DomainRiskVerifier struct {
	store     *MySQLStore
	evaluator DomainRiskEvaluator
}

// NewDomainRiskVerifier binds a server-owned evaluator. The caller of Verify
// never supplies a desired risk state, policy version or provider evidence.
func NewDomainRiskVerifier(store *MySQLStore, evaluator DomainRiskEvaluator) *DomainRiskVerifier {
	return &DomainRiskVerifier{store: store, evaluator: evaluator}
}

func (v *DomainRiskVerifier) Verify(ctx context.Context, input VerifyDomainRiskInput) (DomainRiskVerificationResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if v == nil || v.store == nil || v.evaluator == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return DomainRiskVerificationResult{}, ErrInvalidDomainMutation
	}
	now := input.Now.UTC()

	snapshot, err := v.preflight(ctx, input, now)
	if err != nil {
		return DomainRiskVerificationResult{}, err
	}
	observation, err := v.evaluator.Evaluate(ctx, snapshot.HostnameASCII)
	if err != nil {
		if recordErr := v.recordEvaluationFailure(ctx, input, snapshot, now); recordErr != nil {
			return DomainRiskVerificationResult{}, recordErr
		}
		return DomainRiskVerificationResult{}, ErrDomainRiskEvaluation
	}
	observation.PolicyVersion = strings.TrimSpace(observation.PolicyVersion)
	observation.EvidenceRef = strings.TrimSpace(observation.EvidenceRef)
	if !validDomainRiskObservation(observation) {
		if recordErr := v.recordEvaluationFailure(ctx, input, snapshot, now); recordErr != nil {
			return DomainRiskVerificationResult{}, recordErr
		}
		return DomainRiskVerificationResult{}, ErrDomainRiskEvaluation
	}

	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return DomainRiskVerificationResult{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return DomainRiskVerificationResult{}, err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return DomainRiskVerificationResult{}, ErrInvalidDomainMutation
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return DomainRiskVerificationResult{}, err
	}
	if !entitlement.MutationAllowed {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		return DomainRiskVerificationResult{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		return DomainRiskVerificationResult{}, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "ingress_dns_required", "valid ingress DNS required"); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		return DomainRiskVerificationResult{}, ErrIngressDNSRequired
	}
	if domain.HTTPSStatus != HTTPSActive {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "https_required", "active HTTPS required"); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return DomainRiskVerificationResult{}, err
		}
		return DomainRiskVerificationResult{}, ErrHTTPSRequired
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET risk_status = ?, risk_checked_at = ?, risk_policy_version = ?, risk_evidence_ref = ?
		WHERE workspace_id = ? AND id = ?`,
		observation.Status, now, observation.PolicyVersion, observation.EvidenceRef,
		input.WorkspaceID, input.DomainID); err != nil {
		return DomainRiskVerificationResult{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return DomainRiskVerificationResult{}, err
	}

	revalidationResult, auditResult, auditCode := domainRiskEvidenceSemantics(observation.Status)
	metadata := map[string]any{"risk_status": observation.Status}
	if err := appendRiskRevalidationTx(ctx, tx, updated, revalidationResult, now, input.CorrelationID, observation, metadata); err != nil {
		return DomainRiskVerificationResult{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.risk.verify", auditResult, input.Reason, input.CorrelationID, map[string]any{
		"code":        auditCode,
		"risk_status": observation.Status,
	}); err != nil {
		return DomainRiskVerificationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return DomainRiskVerificationResult{}, err
	}
	return DomainRiskVerificationResult{Domain: updated, Observation: observation}, nil
}

func validDomainRiskObservation(observation DomainRiskObservation) bool {
	if observation.PolicyVersion == "" || observation.EvidenceRef == "" {
		return false
	}
	switch observation.Status {
	case RiskMissing, RiskAllow, RiskReview, RiskBlock, RiskMalformed, RiskStale:
		return true
	default:
		return false
	}
}

func domainRiskEvidenceSemantics(status DomainRiskStatus) (revalidationResult, auditResult, auditCode string) {
	switch status {
	case RiskAllow:
		return "pass", "success", "domain_risk_allow"
	case RiskMissing:
		return "pending", "denied", "domain_risk_missing"
	case RiskStale:
		return "stale", "denied", "domain_risk_stale"
	case RiskReview:
		return "fail", "denied", "domain_risk_review"
	case RiskBlock:
		return "fail", "denied", "domain_risk_block"
	case RiskMalformed:
		return "fail", "denied", "domain_risk_malformed"
	default:
		return "error", "failed", "domain_risk_error"
	}
}

func (v *DomainRiskVerifier) preflight(ctx context.Context, input VerifyDomainRiskInput, now time.Time) (Domain, error) {
	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return Domain{}, err
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return Domain{}, err
	}
	if !entitlement.MutationAllowed {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "entitlement_required", entitlement.DecisionReason); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrEntitlementRequired
	}
	if domain.OwnershipStatus != OwnershipVerified {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "ownership_required", "ownership verification required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrOwnershipRequired
	}
	if domain.IngressDNSStatus != IngressValid {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "ingress_dns_required", "valid ingress DNS required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrIngressDNSRequired
	}
	if domain.HTTPSStatus != HTTPSActive {
		if err := appendRiskDenialAuditTx(ctx, tx, domain, input, "https_required", "active HTTPS required"); err != nil {
			return Domain{}, err
		}
		if err := tx.Commit(); err != nil {
			return Domain{}, err
		}
		return Domain{}, ErrHTTPSRequired
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return domain, nil
}

func appendRiskDenialAuditTx(ctx context.Context, tx *sql.Tx, domain Domain, input VerifyDomainRiskInput, code, reason string) error {
	return appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.risk.verify", "denied", reason, input.CorrelationID, map[string]any{"code": code})
}

func appendRiskRevalidationTx(ctx context.Context, tx *sql.Tx, domain Domain, result string, checkedAt time.Time, correlationID string, observation DomainRiskObservation, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_revalidations (
			domain_id, workspace_id, axis, result, policy_version, checked_at,
			next_due_at, evidence_ref, correlation_id, metadata_json
		) VALUES (?, ?, 'risk', ?, ?, ?, NULL, ?, ?, ?)`,
		domain.ID, domain.WorkspaceID, result, observation.PolicyVersion, checkedAt,
		observation.EvidenceRef, correlationID, string(raw),
	)
	return err
}

func (v *DomainRiskVerifier) recordEvaluationFailure(ctx context.Context, input VerifyDomainRiskInput, snapshot Domain, checkedAt time.Time) error {
	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return ErrInvalidDomainMutation
	}
	observation := DomainRiskObservation{Status: RiskMissing, PolicyVersion: "domain-risk-evaluation-error", EvidenceRef: "risk:evaluation:error"}
	if err := appendRiskRevalidationTx(ctx, tx, domain, "error", checkedAt, input.CorrelationID, observation, map[string]any{"risk_status": "evaluation_error"}); err != nil {
		return err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.risk.verify", "failed", "domain risk evaluation failed", input.CorrelationID, map[string]any{"code": "domain_risk_evaluation_failed"}); err != nil {
		return err
	}
	return tx.Commit()
}
