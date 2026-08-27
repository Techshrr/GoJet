package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT013(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real P16 destination/domain-risk and abuse authority consumed through P17 exact permissions without verdict mutation or provider/user-evidence exposure")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T013", []string{adminaccess.PermissionSecurityManage, adminaccess.PermissionDomainsRiskManage}, now)
	if err != nil {
		return out, err
	}
	securityPrincipal, _, err := createScopedMFAAdmin(ctx, service, root, "T013", "security", adminaccess.PermissionSecurityManage, now.Add(10*time.Second))
	if err != nil {
		return out, err
	}
	domainPrincipal, _, err := createScopedMFAAdmin(ctx, service, root, "T013", "domain-risk", adminaccess.PermissionDomainsRiskManage, now.Add(20*time.Second))
	if err != nil {
		return out, err
	}

	const ws = "ws_p17_t013"
	res, err := runtime.DB.ExecContext(ctx, `INSERT INTO links(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json,created_at,updated_at) VALUES (?,'risk.p17.test','official','t013','P16 Risk Link','https://blocked.example/private-target',302,'active',1,REPEAT('b',64),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),?,?)`, ws, now, now)
	if err != nil {
		return out, err
	}
	linkID, _ := res.LastInsertId()
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO destination_risk_scans(workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,attempts,max_attempts,available_at,correlation_id,completed_at,created_at,updated_at) VALUES (?,?,REPEAT('b',64),'p16-policy','initial','t013-risk-initial','completed',1,5,?,'p16-t013-risk',?,?,?)`, ws, linkID, now, now, now, now)
	if err != nil {
		return out, err
	}
	riskID, _ := res.LastInsertId()
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO destination_risk_scan_targets(scan_id,target_order,normalized_url,target_hash,created_at) VALUES (?,1,'https://P16_PRIVATE_TARGET_MUST_NOT_EXPOSE.test/path',REPEAT('c',64),?)`, riskID, now); err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO destination_risk_provider_observations(scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at) VALUES (?,'provider-private','block','provider-block',JSON_OBJECT('P16_PRIVATE_PROVIDER_EVIDENCE_MUST_NOT_EXPOSE','yes'),?,?)`, riskID, now, now); err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO destination_risk_decisions(workspace_id,link_id,scan_id,risk_fingerprint,policy_version,state,reason_category,decision_metadata_json,valid_until,decided_at,created_at) VALUES (?,?,?,REPEAT('b',64),'p16-policy','block','provider-block',JSON_OBJECT('P16_PRIVATE_DECISION_METADATA_MUST_NOT_EXPOSE','yes'),?,?,?)`, ws, linkID, riskID, now.Add(time.Hour), now, now); err != nil {
		return out, err
	}

	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO custom_domains(workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,ownership_secret_hash,ownership_secret_issued_at,risk_policy_version,risk_evidence_ref,created_at,updated_at) VALUES (?,'domain-t013.example','domain-t013.example','suspended','verified','valid','active','block',UNHEX(REPEAT('12',32)),?,'p16-domain-policy','P16_PRIVATE_DOMAIN_EVIDENCE_MUST_NOT_EXPOSE',?,?)`, ws, now, now, now)
	if err != nil {
		return out, err
	}
	domainID, _ := res.LastInsertId()
	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO domain_risk_evaluations(workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,valid_until,checked_at,next_due_at,entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot,created_at,updated_at) VALUES (?,?,'domain-t013.example','p16-domain-policy','initial','t013-domain-initial','block','provider-block','p16-t013-domain','p16-worker',?,?,?,'active','verified','valid','active','suspended',?,?)`, ws, domainID, now.Add(time.Hour), now, now.Add(30*time.Minute), now, now)
	if err != nil {
		return out, err
	}
	evaluationID, _ := res.LastInsertId()
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO domain_risk_provider_observations(evaluation_id,provider,outcome,signal_code,evidence_json,observed_at,created_at) VALUES (?,'provider-private','block','domain-provider-block',JSON_OBJECT('P16_PRIVATE_DOMAIN_PROVIDER_EVIDENCE_MUST_NOT_EXPOSE','yes'),?,?)`, evaluationID, now, now); err != nil {
		return out, err
	}

	res, err = runtime.DB.ExecContext(ctx, `INSERT INTO abuse_reports(public_id,workspace_id,resource_type,resource_id,hostname_ascii,safe_code,destination_fingerprint,category,details_redacted,request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref,created_at,updated_at) VALUES ('abr_t013_public',?,'short-link-risk',?,'risk.p17.test','t013',REPEAT('b',64),'phishing','P16_PRIVATE_ABUSE_DETAILS_MUST_NOT_EXPOSE',REPEAT('d',64),REPEAT('e',64),'investigating',2,'p16-t013-abuse','abuse-report:t013',?,?)`, ws, fmt.Sprintf("%d", linkID), now, now)
	if err != nil {
		return out, err
	}
	reportID, _ := res.LastInsertId()

	before, err := scalarString(ctx, runtime.DB, `SELECT CONCAT((SELECT state FROM destination_risk_decisions WHERE scan_id=?),':',(SELECT state FROM domain_risk_evaluations WHERE id=?),':',(SELECT status FROM abuse_reports WHERE id=?))`, riskID, evaluationID, reportID)
	if err != nil {
		return out, err
	}
	dest, err := service.P16DestinationRisk(ctx, securityPrincipal, uint64(riskID))
	if err != nil {
		return out, err
	}
	abuse, err := service.P16Abuse(ctx, securityPrincipal, uint64(reportID))
	if err != nil {
		return out, err
	}
	domainDeniedForSecurity := false
	if _, err := service.P16DomainRisk(ctx, securityPrincipal, uint64(domainID)); errors.Is(err, adminaccess.ErrForbidden) {
		domainDeniedForSecurity = true
	}
	domainSnap, err := service.P16DomainRisk(ctx, domainPrincipal, uint64(domainID))
	if err != nil {
		return out, err
	}
	destDeniedForDomain := false
	if _, err := service.P16DestinationRisk(ctx, domainPrincipal, uint64(riskID)); errors.Is(err, adminaccess.ErrForbidden) {
		destDeniedForDomain = true
	}
	authSecurity := adminaccess.NewP16TrustAuthorizer(securityPrincipal)
	authDomain := adminaccess.NewP16TrustAuthorizer(domainPrincipal)
	exactSecurityOK := authSecurity.Authorize(ctx, securityPrincipal.Administrator.ID, trust.SecurityManagePermission) == nil
	exactDomainOK := authDomain.Authorize(ctx, domainPrincipal.Administrator.ID, trust.DomainsRiskManagePermission) == nil
	wrongPermDenied := errors.Is(authSecurity.Authorize(ctx, securityPrincipal.Administrator.ID, trust.DomainsRiskManagePermission), trust.ErrUnauthorized)
	wrongActorDenied := errors.Is(authSecurity.Authorize(ctx, "another-admin", trust.SecurityManagePermission), trust.ErrUnauthorized)
	raw, _ := json.Marshal([]any{dest, domainSnap, abuse})
	lower := strings.ToLower(string(raw))
	after, err := scalarString(ctx, runtime.DB, `SELECT CONCAT((SELECT state FROM destination_risk_decisions WHERE scan_id=?),':',(SELECT state FROM domain_risk_evaluations WHERE id=?),':',(SELECT status FROM abuse_reports WHERE id=?))`, riskID, evaluationID, reportID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{"destination_provider_observations": 1, "domain_provider_observations": 1, "abuse_reports": 1}
	out.Checks = map[string]bool{
		"p17_security_manage_consumes_p16_destination_and_abuse_authority": dest.DecisionState == "block" && dest.ScanStatus == "completed" && dest.ProviderCount == 1 && abuse.Status == "investigating" && abuse.Version == 2,
		"p17_domains_risk_manage_consumes_latest_p16_domain_authority":     domainSnap.State == "block" && domainSnap.ProviderCount == 1 && domainSnap.RoutingStatus == "suspended",
		"security_and_domain_risk_permissions_remain_independent":          domainDeniedForSecurity && destDeniedForDomain,
		"p16_authorizer_binds_exact_actor_and_permission":                  exactSecurityOK && exactDomainOK && wrongPermDenied && wrongActorDenied,
		"p16_provider_target_abuse_and_domain_evidence_remains_redacted":   !strings.Contains(lower, "p16_private_target_must_not_expose") && !strings.Contains(lower, "p16_private_provider_evidence_must_not_expose") && !strings.Contains(lower, "p16_private_decision_metadata_must_not_expose") && !strings.Contains(lower, "p16_private_domain_evidence_must_not_expose") && !strings.Contains(lower, "p16_private_domain_provider_evidence_must_not_expose") && !strings.Contains(lower, "p16_private_abuse_details_must_not_expose"),
		"p17_bridge_never_mutates_p16_verdicts":                            before == "block:block:investigating" && after == before,
	}
	pass(&out)
	return out, nil
}
