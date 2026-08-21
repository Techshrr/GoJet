package domains

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"
)

const entitlementRevalidationPolicyVersion = "entitlement-resolver-v1"

type PeriodicRevalidationInput struct {
	WorkspaceID   string
	DomainID      uint64
	CorrelationID string
	Now           time.Time
}

type PeriodicAxisResult struct {
	Result    string    `json:"result"`
	NextDueAt time.Time `json:"next_due_at"`
}

type PeriodicRevalidationResult struct {
	Domain      Domain                         `json:"domain"`
	Entitlement ResolvedEntitlement            `json:"entitlement"`
	Axes        map[RevalidationAxis]PeriodicAxisResult `json:"axes"`
}

type PeriodicRevalidator struct {
	store     *MySQLStore
	ownership *OwnershipVerifier
	ingress   *IngressDNSVerifier
	https     *HTTPSVerifier
	risk      *DomainRiskVerifier
	policy    RevalidationSchedulePolicy
}

func NewPeriodicRevalidator(
	store *MySQLStore,
	ownership *OwnershipVerifier,
	ingress *IngressDNSVerifier,
	https *HTTPSVerifier,
	risk *DomainRiskVerifier,
	policy RevalidationSchedulePolicy,
) (*PeriodicRevalidator, error) {
	if store == nil || ownership == nil || ingress == nil || https == nil || risk == nil ||
		ownership.store != store || ingress.store != store || https.store != store || risk.store != store {
		return nil, ErrInvalidDomainMutation
	}
	checkedAt := time.Unix(1, 0).UTC()
	for _, axis := range []RevalidationAxis{RevalidationEntitlement, RevalidationOwnership, RevalidationIngressDNS, RevalidationHTTPS, RevalidationRisk} {
		if _, err := policy.schedule(axis, checkedAt); err != nil {
			return nil, err
		}
	}
	return &PeriodicRevalidator{store: store, ownership: ownership, ingress: ingress, https: https, risk: risk, policy: policy}, nil
}

// Run performs the scheduled security observation set independently of a user
// mutation. Network observations are gathered first, then one transaction
// row-locks the domain, re-resolves entitlement and re-reads the current
// ownership verifier before any observation can become authoritative.
func (r *PeriodicRevalidator) Run(ctx context.Context, input PeriodicRevalidationInput) (PeriodicRevalidationResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if r == nil || r.store == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.CorrelationID == "" || input.Now.IsZero() {
		return PeriodicRevalidationResult{}, ErrInvalidDomainMutation
	}
	checkedAt := input.Now.UTC()
	snapshot, err := r.store.GetDomain(ctx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}

	ownershipRecords, ownershipLookupErr := r.ownership.resolver.LookupTXT(ctx, OwnershipTXTName(snapshot.HostnameASCII))
	if isTXTNotFound(ownershipLookupErr) {
		ownershipRecords = nil
		ownershipLookupErr = nil
	}
	ingressTarget, ingressLookupErr := r.ingress.resolver.LookupCNAME(ctx, snapshot.HostnameASCII)
	if isDNSNotFound(ingressLookupErr) {
		ingressTarget = ""
		ingressLookupErr = nil
	}
	tlsObservation, tlsProbeErr := r.https.probe.Probe(ctx, snapshot.HostnameASCII)
	riskObservation, riskEvaluationErr := r.risk.evaluator.Evaluate(ctx, snapshot.HostnameASCII)

	tx, err := r.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return PeriodicRevalidationResult{}, ErrInvalidDomainMutation
	}
	entitlement, err := r.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, checkedAt)
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}
	ownershipVerifier, err := loadOwnershipVerifierTx(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}

	ownershipOutcome := OwnershipVerificationMissing
	ownershipStatus := OwnershipPending
	ownershipResult := "pending"
	if ownershipLookupErr != nil {
		ownershipOutcome = OwnershipVerificationMismatch
		ownershipStatus = OwnershipFailed
		ownershipResult = "error"
	} else {
		ownershipOutcome, ownershipStatus = classifyOwnershipTXT(ownershipRecords, ownershipVerifier)
		switch ownershipOutcome {
		case OwnershipVerificationVerified:
			ownershipResult = "pass"
		case OwnershipVerificationMismatch:
			ownershipResult = "fail"
		}
	}
	if domain.OwnershipStatus == OwnershipVerified && ownershipStatus != OwnershipVerified {
		ownershipStatus = OwnershipLost
	}

	ingressOutcome := IngressDNSMissing
	ingressStatus := IngressInvalid
	ingressResult := "fail"
	if ingressLookupErr != nil {
		ingressResult = "error"
	} else if ingressTarget != "" {
		canonicalObserved, canonicalErr := canonicalDNSTarget(ingressTarget)
		if canonicalErr == nil && canonicalObserved == r.ingress.expectedTarget {
			ingressOutcome = IngressDNSValid
			ingressStatus = IngressValid
			ingressResult = "pass"
		} else {
			ingressOutcome = IngressDNSMismatch
		}
	}
	if domain.IngressDNSStatus == IngressValid && ingressStatus != IngressValid {
		ingressOutcome = IngressDNSDrift
	}

	httpsStatus := HTTPSPending
	httpsResult := "pending"
	if tlsProbeErr != nil {
		httpsStatus = HTTPSError
		httpsResult = "error"
	} else {
		switch tlsObservation.Outcome {
		case TLSProbeActive:
			httpsStatus = HTTPSActive
			httpsResult = "pass"
		case TLSProbeError:
			httpsStatus = HTTPSError
			httpsResult = "fail"
		case TLSProbePending:
			httpsStatus = HTTPSPending
		default:
			httpsStatus = HTTPSError
			httpsResult = "error"
		}
	}

	riskStatus := RiskStale
	riskResult := "error"
	if riskEvaluationErr == nil && validDomainRiskObservation(riskObservation) {
		riskStatus = riskObservation.Status
		riskResult, _, _ = domainRiskEvidenceSemantics(riskStatus)
	} else {
		riskObservation = DomainRiskObservation{
			Status:        RiskStale,
			PolicyVersion: "domain-risk-evaluation-error",
			EvidenceRef:   "risk:evaluation:error",
		}
	}

	var ownershipVerifiedAt any = domain.OwnershipVerifiedAt
	if ownershipStatus == OwnershipVerified {
		ownershipVerifiedAt = checkedAt
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status = ?, ownership_verified_at = ?,
		    ingress_dns_status = ?, ingress_dns_checked_at = ?,
		    https_status = ?, https_checked_at = ?,
		    risk_status = ?, risk_checked_at = ?, risk_policy_version = ?, risk_evidence_ref = ?
		WHERE workspace_id = ? AND id = ?`,
		ownershipStatus, ownershipVerifiedAt,
		ingressStatus, checkedAt,
		httpsStatus, checkedAt,
		riskStatus, checkedAt, riskObservation.PolicyVersion, riskObservation.EvidenceRef,
		input.WorkspaceID, input.DomainID); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return PeriodicRevalidationResult{}, err
	}

	axes := map[RevalidationAxis]PeriodicAxisResult{}
	appendAxis := func(axis RevalidationAxis, result, policyVersion, evidenceRef string, metadata map[string]any) error {
		schedule, err := r.policy.schedule(axis, checkedAt)
		if err != nil {
			return err
		}
		correlationID := input.CorrelationID + ":" + string(axis)
		if err := appendDomainRevalidationTx(ctx, tx, updated, axis, result, policyVersion, checkedAt, schedule, evidenceRef, correlationID, metadata); err != nil {
			return err
		}
		axes[axis] = PeriodicAxisResult{Result: result, NextDueAt: schedule.NextDueAt}
		return nil
	}

	entitlementResult := "fail"
	if entitlement.MutationAllowed || entitlement.ExistingRoutingAllowed {
		entitlementResult = "pass"
	}
	if err := appendAxis(RevalidationEntitlement, entitlementResult, entitlementRevalidationPolicyVersion, "entitlement:resolver", map[string]any{
		"source": entitlement.Source,
		"status": entitlement.Status,
		"mutation_allowed": entitlement.MutationAllowed,
		"existing_routing_allowed": entitlement.ExistingRoutingAllowed,
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if err := appendAxis(RevalidationOwnership, ownershipResult, ownershipTXTPolicyVersion, "dns:txt:"+OwnershipTXTName(updated.HostnameASCII), map[string]any{
		"outcome": ownershipOutcome,
		"ownership_status": ownershipStatus,
		"records_observed": len(ownershipRecords),
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if err := appendAxis(RevalidationIngressDNS, ingressResult, ingressDNSPolicyVersion, "dns:cname:"+updated.HostnameASCII, map[string]any{
		"outcome": ingressOutcome,
		"ingress_status": ingressStatus,
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if err := appendAxis(RevalidationHTTPS, httpsResult, httpsReadinessPolicyVersion, "tls:handshake:"+updated.HostnameASCII, map[string]any{
		"outcome": tlsObservation.Outcome,
		"https_status": httpsStatus,
		"handshake_complete": tlsObservation.HandshakeComplete,
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if err := appendAxis(RevalidationRisk, riskResult, riskObservation.PolicyVersion, riskObservation.EvidenceRef, map[string]any{
		"risk_status": riskStatus,
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}

	readiness := updated.Readiness(entitlement)
	periodicResult := "success"
	periodicCode := "periodic_revalidation_complete"
	if !readiness.ReadyForRouting {
		periodicCode = "periodic_revalidation_not_ready"
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, "system:p06-domain-revalidator", "domain.revalidation.periodic", periodicResult, "scheduled domain authority revalidation", input.CorrelationID, map[string]any{
		"code": periodicCode,
		"entitlement_status": entitlement.Status,
		"ownership_status": ownershipStatus,
		"ingress_dns_status": ingressStatus,
		"https_status": httpsStatus,
		"risk_status": riskStatus,
		"ready_for_new_links": readiness.ReadyForNewLinks,
		"ready_for_routing": readiness.ReadyForRouting,
	}); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return PeriodicRevalidationResult{}, err
	}
	return PeriodicRevalidationResult{Domain: updated, Entitlement: entitlement, Axes: axes}, nil
}

func periodicDNSLookupError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr)
}
