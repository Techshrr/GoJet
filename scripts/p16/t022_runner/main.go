package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

const destinationPolicy = "p16-t022-policy-v1"

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type permissionFixture struct {
	allow bool
	calls int
}

func (p *permissionFixture) Authorize(_ context.Context, actorID, permission string) error {
	p.calls++
	if !p.allow || strings.TrimSpace(actorID) == "" || permission != trust.SecurityManagePermission {
		return trust.ErrUnauthorized
	}
	return nil
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T022"
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T022",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native redirectengine proving security.manage abuse block/suspend, active-hold projection, safety-authorized recovery and immutable destination/domain/abuse audit",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	db, redisClient, err := runtimefixture.Open()
	if err != nil {
		return out, err
	}
	defer db.Close()
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		return out, err
	}
	out.MySQLVersion, err = domainfixture.MySQLVersion(ctx, db)
	if err != nil {
		return out, err
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		return out, err
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	store := trust.NewStore(db)
	runtime := links.NewRedisRiskStore(redisClient)
	service, err := trust.NewAbuseActionService(store, runtime, destinationPolicy, 20*time.Minute)
	if err != nil {
		return out, err
	}
	allowed := &permissionFixture{allow: true}
	denied := &permissionFixture{allow: false}

	// Short-link risk action: exact fingerprint is the resource authority.
	link, err := runtimefixture.CreateLink(ctx, db, "p16-t022-link-workspace", "go.example.test", "official", "t022-link", "https://safe.example/t022", nil, nil)
	if err != nil {
		return out, err
	}
	allowUntil := now.Add(30 * time.Minute)
	if _, err := runtimefixture.InsertRawDecision(ctx, db, store, link, destinationPolicy, "t022-initial-allow", trust.DecisionAllow, "policy-allow", &allowUntil); err != nil {
		return out, err
	}
	if _, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, link.WorkspaceID, link.ID, destinationPolicy, now, 20*time.Minute); err != nil {
		return out, err
	}
	beforeBlock, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}
	linkReportID, err := seedAbuseReport(ctx, db, link.WorkspaceID, trust.AbuseShortLinkRisk, strconv.FormatUint(link.ID, 10), link.Hostname, link.Code, link.Fingerprint, "t022-link")
	if err != nil {
		return out, err
	}
	_, deniedErr := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: linkReportID, Action: trust.AbuseActionBlock, ExactFingerprint: link.Fingerprint,
		Reason: "validated destination abuse", ActorID: "p16-t022-denied", CorrelationID: "p16-t022-denied", IdempotencyKey: "p16-t022-denied-idem", Now: now,
	}, denied)
	blocked, err := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: linkReportID, Action: trust.AbuseActionBlock, ExactFingerprint: link.Fingerprint,
		Reason: "validated destination abuse", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-block", IdempotencyKey: "p16-t022-block-idem", Now: now.Add(time.Second),
	}, allowed)
	if err != nil {
		return out, err
	}
	afterBlock, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}
	projectedWhileHeld, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, link.WorkspaceID, link.ID, destinationPolicy, now.Add(2*time.Second), 20*time.Minute)
	if err != nil {
		return out, err
	}

	reviewUntil := now.Add(25 * time.Minute)
	if _, err := runtimefixture.InsertRawDecision(ctx, db, store, link, destinationPolicy, "t022-review", trust.DecisionReview, "policy-review", &reviewUntil); err != nil {
		return out, err
	}
	_, unsafeRestoreErr := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: linkReportID, Action: trust.AbuseActionRestore, ExactFingerprint: link.Fingerprint,
		Reason: "request restore while policy review remains", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-restore-denied", IdempotencyKey: "p16-t022-restore-denied-idem", Now: now.Add(3 * time.Second),
	}, allowed)
	latestAllowUntil := now.Add(40 * time.Minute)
	if _, err := runtimefixture.InsertRawDecision(ctx, db, store, link, destinationPolicy, "t022-final-allow", trust.DecisionAllow, "policy-allow", &latestAllowUntil); err != nil {
		return out, err
	}
	restoredLink, err := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: linkReportID, Action: trust.AbuseActionRestore, ExactFingerprint: link.Fingerprint,
		Reason: "current destination authority is allow", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-restore", IdempotencyKey: "p16-t022-restore-idem", Now: now.Add(4 * time.Second),
	}, allowed)
	if err != nil {
		return out, err
	}
	afterRestore, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}

	// Custom-domain risk action: P06 security suspension remains the kill switch.
	domain, err := domainfixture.CreateReadyDomain(ctx, db, "t022-domain", now)
	if err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE custom_domains SET grace_started_at=?,grace_until=? WHERE id=?`, now.Add(-time.Minute), now.Add(24*time.Hour), domain.ID); err != nil {
		return out, err
	}
	if _, err := insertDomainEvaluation(ctx, db, domain, "review", "p16-domain-fixture-v1", now.Add(20*time.Minute), "t022-domain-review"); err != nil {
		return out, err
	}
	domainReportID, err := seedAbuseReport(ctx, db, domain.WorkspaceID, trust.AbuseCustomDomainRisk, strconv.FormatUint(domain.ID, 10), domain.Hostname, "", "", "t022-domain")
	if err != nil {
		return out, err
	}
	suspended, err := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: domainReportID, Action: trust.AbuseActionSuspend,
		Reason: "validated custom-domain abuse", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-domain-suspend", IdempotencyKey: "p16-t022-domain-suspend-idem", Now: now.Add(5 * time.Second),
	}, allowed)
	if err != nil {
		return out, err
	}
	domainAfterSuspend, err := domains.NewMySQLStore(db).GetDomain(ctx, domain.WorkspaceID, domain.ID)
	if err != nil {
		return out, err
	}
	_, unsafeDomainRestoreErr := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: domainReportID, Action: trust.AbuseActionRestore,
		Reason: "restore requested while durable domain risk is review", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-domain-restore-denied", IdempotencyKey: "p16-t022-domain-restore-denied-idem", Now: now.Add(6 * time.Second),
	}, allowed)
	if _, err := insertDomainEvaluation(ctx, db, domain, "allow", "p16-domain-fixture-v1", now.Add(45*time.Minute), "t022-domain-allow"); err != nil {
		return out, err
	}
	restoredDomain, err := service.Apply(ctx, trust.AbuseResourceActionInput{
		ReportID: domainReportID, Action: trust.AbuseActionRestore,
		Reason: "current domain safety and all independent axes are ready", ActorID: "p16-t022-admin", CorrelationID: "p16-t022-domain-restore", IdempotencyKey: "p16-t022-domain-restore-idem", Now: now.Add(7 * time.Second),
	}, allowed)
	if err != nil {
		return out, err
	}
	domainAfterRestore, err := domains.NewMySQLStore(db).GetDomain(ctx, domain.WorkspaceID, domain.ID)
	if err != nil {
		return out, err
	}

	activeLinkHolds, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_resource_holds WHERE workspace_id=? AND resource_type='short-link-risk' AND resource_id=? AND state='active'`, link.WorkspaceID, strconv.FormatUint(link.ID, 10))
	if err != nil {
		return out, err
	}
	activeDomainHolds, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_resource_holds WHERE workspace_id=? AND resource_type='custom-domain-risk' AND resource_id=? AND state='active'`, domain.WorkspaceID, strconv.FormatUint(domain.ID, 10))
	if err != nil {
		return out, err
	}
	abuseSuccessEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE report_id IN (?,?) AND action IN ('abuse.resource-block','abuse.resource-suspend','abuse.resource-restore') AND result='success'`, linkReportID, domainReportID)
	if err != nil {
		return out, err
	}
	abuseDeniedEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE report_id=? AND action='abuse.resource-block' AND result='denied'`, linkReportID)
	if err != nil {
		return out, err
	}
	destinationAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE link_id=? AND action IN ('destination-risk.abuse-block','destination-risk.abuse-restore') AND result='success'`, link.ID)
	if err != nil {
		return out, err
	}
	p06DomainAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_audit_events WHERE domain_id=? AND action IN ('domain.security.suspend','domain.security.restore') AND result='success'`, domain.ID)
	if err != nil {
		return out, err
	}
	p16DomainAudits, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=? AND action IN ('domain-risk.security-suspend','domain-risk.abuse-restore') AND result='success'`, domain.ID)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"active_link_holds_after_restore":   activeLinkHolds,
		"active_domain_holds_after_restore": activeDomainHolds,
		"abuse_success_action_events":       abuseSuccessEvents,
		"abuse_permission_denied_events":    abuseDeniedEvents,
		"destination_risk_action_audits":    destinationAudits,
		"p06_domain_security_action_audits": p06DomainAudits,
		"p16_domain_risk_action_audits":     p16DomainAudits,
	}
	out.Checks = map[string]bool{
		"security_manage_denial_is_fail_closed_and_audited":  errors.Is(deniedErr, trust.ErrUnauthorized) && denied.calls == 1 && abuseDeniedEvents == 1,
		"short_link_was_reachable_before_abuse_block":        beforeBlock.Status == 302 && beforeBlock.Location != "",
		"short_link_block_creates_exact_active_hold":         blocked.Changed && blocked.Hold.State == "active" && blocked.Hold.ExactFingerprint == link.Fingerprint,
		"short_link_block_immediately_fails_redirect_closed": afterBlock.Status != 302 && afterBlock.Location == "",
		"background_projection_cannot_overwrite_abuse_hold":  projectedWhileHeld.Source == "abuse-hold" && projectedWhileHeld.Runtime.Decision == links.RiskBlock,
		"short_link_restore_rejects_non_allow_safety":        errors.Is(unsafeRestoreErr, trust.ErrConflict),
		"short_link_restore_requires_current_allow":          restoredLink.Changed && restoredLink.Hold.State == "released" && activeLinkHolds == 0 && afterRestore.Status == 302 && afterRestore.Location != "",
		"domain_suspend_is_immediate_and_clears_grace":       suspended.Changed && domainAfterSuspend.RoutingState == domains.RoutingSuspended && domainAfterSuspend.SecurityCategory == string(domains.DomainSecurityAbuse) && domainAfterSuspend.GraceStartedAt == nil && domainAfterSuspend.GraceUntil == nil,
		"domain_restore_rejects_review_authority":            errors.Is(unsafeDomainRestoreErr, trust.ErrConflict),
		"domain_restore_rechecks_all_p06_axes":               restoredDomain.Changed && restoredDomain.Hold.State == "released" && domainAfterRestore.RoutingState == domains.RoutingEnabled && domainAfterRestore.SecurityCategory == "" && domainAfterRestore.OwnershipStatus == domains.OwnershipVerified && domainAfterRestore.IngressDNSStatus == domains.IngressValid && domainAfterRestore.HTTPSStatus == domains.HTTPSActive && domainAfterRestore.RiskStatus == domains.RiskAllow && activeDomainHolds == 0,
		"before_after_actions_have_immutable_abuse_audit":    abuseSuccessEvents == 4,
		"destination_actions_have_risk_audit":                destinationAudits == 2,
		"domain_actions_have_p06_and_p16_audit":              p06DomainAudits == 2 && p16DomainAudits == 2,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func seedAbuseReport(ctx context.Context, db *sql.DB, workspace string, resourceType trust.AbuseResourceType, resourceID, hostname, safeCode, fingerprint, suffix string) (uint64, error) {
	publicID := "abr_" + suffix
	var codeValue, fingerprintValue any
	if resourceType == trust.AbuseShortLinkRisk {
		codeValue = safeCode
		fingerprintValue = fingerprint
	}
	result, err := db.ExecContext(ctx, `
INSERT INTO abuse_reports
(public_id,workspace_id,resource_type,resource_id,hostname_ascii,safe_code,destination_fingerprint,category,details_redacted,request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref)
VALUES (?,?,?,?,?,?,?,'phishing','validated fixture report',?,?,'investigating',2,?,?)`,
		publicID, workspace, string(resourceType), resourceID, hostname, codeValue, fingerprintValue,
		hash64("request-"+suffix), hash64("idem-"+suffix), "p16-t022-seed-"+suffix, "abuse-report:"+publicID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func insertDomainEvaluation(ctx context.Context, db *sql.DB, domain domainfixture.DomainFixture, state, policy string, validUntil time.Time, suffix string) (uint64, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	result, err := db.ExecContext(ctx, `
INSERT INTO domain_risk_evaluations
(workspace_id,domain_id,hostname_ascii,policy_version,request_kind,idempotency_key,state,reason_category,correlation_id,actor_id,valid_until,checked_at,next_due_at,
 entitlement_snapshot,ownership_snapshot,ingress_dns_snapshot,https_snapshot,routing_snapshot)
VALUES (?,?,?,?, 'revalidation', ?,?,?,?,'p16-t022-admin',?,?,?,'active','verified','valid','active','enabled')`,
		domain.WorkspaceID, domain.ID, domain.Hostname, policy, suffix, state, "p16-t022-"+state, "p16-t022-"+suffix,
		validUntil.UTC(), now, now.Add(10*time.Minute))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func hash64(value string) string {
	fingerprint, _, _ := links.RiskFingerprint("https://hash.example/"+value, nil, nil)
	return fingerprint
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func allTrue(checks map[string]bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return true
}
