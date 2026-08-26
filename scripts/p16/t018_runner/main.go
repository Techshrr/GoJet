package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T018"
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
		Case:         "P16-T018",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native redirectengine proving inherited P06 security-abuse kill switch clears grace immediately and denies the affected custom link without official-host fallback",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	db, redisClient, err := runtimefixture.Open()
	if err != nil {
		return out, err
	}
	defer db.Close()
	defer redisClient.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	domain, err := domainfixture.CreateReadyDomain(ctx, db, "t018", now)
	if err != nil {
		return out, err
	}
	link, err := runtimefixture.CreateLink(ctx, db, domain.WorkspaceID, domain.Hostname, "custom", "t018-custom", "https://safe.example/t018-destination", nil, nil)
	if err != nil {
		return out, err
	}
	runtime := links.NewRedisRiskStore(redisClient)
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, link, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	before, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE custom_domains SET grace_started_at=?,grace_until=? WHERE workspace_id=? AND id=?`, now.Add(-time.Minute), now.Add(24*time.Hour), domain.WorkspaceID, domain.ID); err != nil {
		return out, err
	}

	service, err := trust.NewDomainRiskService(trust.NewStore(db), trust.DomainRiskPolicy{
		Version:           "p16-domain-policy-t018",
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          20 * time.Minute,
		RevalidateAfter:   10 * time.Minute,
		RetryAfter:        time.Minute,
	}, domainfixture.Provider{Name: "domain-reputation", Outcome: trust.ProviderAllow, SignalCode: "domain-allow"})
	if err != nil {
		return out, err
	}
	updated, err := service.ApplySecuritySuspension(ctx, domains.DomainSecuritySuspensionInput{
		WorkspaceID:   domain.WorkspaceID,
		DomainID:      domain.ID,
		ActorID:       "p16-t018-security",
		Category:      domains.DomainSecurityAbuse,
		Reason:        "validated abuse report",
		CorrelationID: "p16-t018-abuse-correlation",
		Now:           now.Add(time.Second),
	})
	if err != nil {
		return out, err
	}

	after, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}
	officialFallback, err := runtimefixture.RequestRedirect(ctx, "go.example.test", link.Code)
	if err != nil {
		return out, err
	}
	stored, _, readiness, err := domains.NewMySQLStore(db).ResolveDomainReadiness(ctx, domain.WorkspaceID, domain.ID, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}

	graceCleared, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id=? AND routing_state='suspended' AND security_category='abuse' AND grace_started_at IS NULL AND grace_until IS NULL`, domain.ID)
	if err != nil {
		return out, err
	}
	p06AuditRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_audit_events WHERE domain_id=? AND action='domain.security.suspend' AND result='success' AND correlation_id='p16-t018-abuse-correlation'`, domain.ID)
	if err != nil {
		return out, err
	}
	p16AuditRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=? AND action='domain-risk.security-suspend' AND result='success' AND correlation_id='p16-t018-abuse-correlation'`, domain.ID)
	if err != nil {
		return out, err
	}
	allowProjectionRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id=? AND risk_status='allow'`, domain.ID)
	if err != nil {
		return out, err
	}

	targetLeaked := strings.Contains(after.Body, "https://safe.example/t018-destination") || strings.Contains(officialFallback.Body, "https://safe.example/t018-destination")
	out.RecordCounts = map[string]int{
		"grace_cleared_suspended_domains": graceCleared,
		"p06_security_audit_events":       p06AuditRows,
		"p16_security_audit_events":       p16AuditRows,
		"preserved_risk_allow_projection": allowProjectionRows,
	}
	out.Checks = map[string]bool{
		"custom_link_is_reachable_before_security_action":         before.Status == 302 && before.Location != "",
		"security_abuse_action_suspends_immediately":              updated.RoutingState == domains.RoutingSuspended && updated.SecurityCategory == string(domains.DomainSecurityAbuse),
		"billing_domain_grace_is_cleared":                         graceCleared == 1 && updated.GraceStartedAt == nil && updated.GraceUntil == nil,
		"affected_custom_link_fails_closed_after_suspension":      after.Status != 302 && after.Location == "",
		"same_code_has_no_official_host_fallback":                 officialFallback.Status != 302 && officialFallback.Location == "",
		"suspension_overrides_even_preserved_risk_allow_signal":   stored.RiskStatus == domains.RiskAllow && allowProjectionRows == 1 && !readiness.ReadyForRouting,
		"security_action_is_audited_in_inherited_and_p16_ledgers": p06AuditRows == 1 && p16AuditRows == 1,
		"safety_response_does_not_disclose_target":                !targetLeaked,
	}
	if domainfixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
