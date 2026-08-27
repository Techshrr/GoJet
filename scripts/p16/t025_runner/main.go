package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/adminfixture"
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
		out.Case = "P16-T025"
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
		Case:         "P16-T025",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native platformapi with inherited P15 session/origin/CSRF authority proving domains.risk.manage list/detail/revalidate RBAC, idempotency/rate rules, independent P06 axes and provider-evidence non-disclosure",
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
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}

	const (
		domainActor   = "p16-t025-domain-admin"
		securityActor = "p16-t024-security-admin"
		deniedActor   = "p16-t025-denied"
		secretMarker  = "p16-t025-domain-provider-secret"
		unsafeTarget  = "https://unsafe-domain-evidence.example/private"
	)
	domainSession, err := adminfixture.EnsureSession(ctx, db, domainActor)
	if err != nil {
		return out, err
	}
	securitySession, err := adminfixture.EnsureSession(ctx, db, securityActor)
	if err != nil {
		return out, err
	}
	deniedSession, err := adminfixture.EnsureSession(ctx, db, deniedActor)
	if err != nil {
		return out, err
	}

	seedNow := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Microsecond)
	domain, err := domainfixture.CreateReadyDomain(ctx, db, "t025", seedNow)
	if err != nil {
		return out, err
	}
	seedService, err := trust.NewDomainRiskService(trust.NewStore(db), trust.DomainRiskPolicy{
		Version:           "p16-t025-seed-v1",
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          10 * time.Minute,
		RevalidateAfter:   30 * time.Second,
		RetryAfter:        15 * time.Second,
	}, domainfixture.Provider{
		Name:       "domain-reputation",
		Outcome:    trust.ProviderAllow,
		SignalCode: "domain-allow",
		Evidence:   map[string]any{"fixture": "t025-seed"},
	})
	if err != nil {
		return out, err
	}
	initial, err := seedService.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID:    domain.WorkspaceID,
		DomainID:       domain.ID,
		RequestKind:    trust.DomainRiskInitial,
		IdempotencyKey: "p16-t025-initial-evaluation",
		ActorID:        "p16-t025-seed-worker",
		Reason:         "seed current domain reputation authority",
		CorrelationID:  "p16-t025-seed-correlation",
		Now:            seedNow,
	})
	if err != nil {
		return out, err
	}
	if initial.Evaluation.State != trust.DomainRiskAllow {
		return out, fmt.Errorf("T025 seed evaluation did not allow: %s", initial.Evaluation.State)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE domain_risk_provider_observations
SET evidence_json=?
WHERE evaluation_id=?`,
		`{"authorization":"Bearer `+secretMarker+`","target":"`+unsafeTarget+`"}`,
		initial.Evaluation.ID,
	); err != nil {
		return out, err
	}

	unauthenticated, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", "", "", "", "", nil)
	if err != nil {
		return out, err
	}
	denied, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", deniedSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	securityPermissionDenied, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	list, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks?limit=100", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	csrf := adminfixture.CSRF(list)
	if csrf == "" {
		return out, fmt.Errorf("T025 list did not issue CSRF token")
	}
	detailPath := "/api/admin/domain-risks/" + strconv.FormatUint(domain.ID, 10)
	detail, err := adminfixture.Request(ctx, http.MethodGet, detailPath, domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	crossPermission, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	invalidLimit, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks?limit=501", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}

	revalidateKey := "p16-t025-revalidate-idempotency-0001"
	revalidate, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/revalidate", domainSession, csrf, revalidateKey, "p16-t025-revalidate-0001", map[string]any{
		"reason": "security reviewer requested current domain reputation revalidation",
	})
	if err != nil {
		return out, err
	}
	revalidateCreated, _ := revalidate.Body["created"].(bool)

	freshList, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	replay, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/revalidate", domainSession, adminfixture.CSRF(freshList), revalidateKey, "p16-t025-revalidate-replay", map[string]any{
		"reason": "security reviewer requested current domain reputation revalidation",
	})
	if err != nil {
		return out, err
	}
	replayCreated, _ := replay.Body["created"].(bool)

	missingCSRF, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/revalidate", domainSession, "", "p16-t025-revalidate-no-csrf", "p16-t025-no-csrf", map[string]any{
		"reason": "must be rejected without CSRF authority",
	})
	if err != nil {
		return out, err
	}

	rateList, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	rateLimited, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/revalidate", domainSession, adminfixture.CSRF(rateList), "p16-t025-revalidate-rate-0002", "p16-t025-rate-correlation", map[string]any{
		"reason": "second immediate domain revalidation must be rate limited",
	})
	if err != nil {
		return out, err
	}

	evaluationRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=?`, domain.ID)
	if err != nil {
		return out, err
	}
	idempotentRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=? AND idempotency_key=?`, domain.ID, revalidateKey)
	if err != nil {
		return out, err
	}
	rateAuditRows, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_audit_events WHERE domain_id=? AND action='domain-risk.revalidate' AND result='conflict' AND reason_category='revalidation-rate-limited'`, domain.ID)
	if err != nil {
		return out, err
	}
	axesUnchanged, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE id=? AND workspace_id=? AND ownership_status='verified' AND ingress_dns_status='valid' AND https_status='active' AND routing_state='enabled'`, domain.ID, domain.WorkspaceID)
	if err != nil {
		return out, err
	}
	latestAllow, err := domainfixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM domain_risk_evaluations WHERE domain_id=? AND request_kind='revalidation' AND state='allow'`, domain.ID)
	if err != nil {
		return out, err
	}

	allHTTP := strings.Join([]string{list.Raw, detail.Raw, revalidate.Raw, replay.Raw, rateLimited.Raw}, "\n")
	out.RecordCounts = map[string]int{
		"domain_risk_evaluations":      evaluationRows,
		"idempotent_revalidation_rows": idempotentRows,
		"rate_limited_audit_events":    rateAuditRows,
		"domains_with_axes_unchanged":  axesUnchanged,
		"successful_revalidations":     latestAllow,
	}
	out.Checks = map[string]bool{
		"p15_session_is_required":                                  unauthenticated.Status == http.StatusUnauthorized,
		"domains_risk_manage_rejects_unprivileged_session":         denied.Status == http.StatusForbidden,
		"security_manage_does_not_grant_domain_risk_access":        securityPermissionDenied.Status == http.StatusForbidden,
		"domain_risk_permission_does_not_grant_destination_access": crossPermission.Status == http.StatusForbidden,
		"domain_list_is_private_and_noindex":                       list.Status == http.StatusOK && adminfixture.NoStoreNoIndex(list),
		"domain_detail_is_private_and_noindex":                     detail.Status == http.StatusOK && adminfixture.NoStoreNoIndex(detail),
		"invalid_admin_query_is_consistent_bad_request":            invalidLimit.Status == http.StatusBadRequest,
		"domain_control_dto_preserves_independent_axes":            strings.Contains(detail.Raw, domain.Hostname) && strings.Contains(detail.Raw, "verified") && strings.Contains(detail.Raw, "valid") && strings.Contains(detail.Raw, "active") && strings.Contains(detail.Raw, "enabled") && axesUnchanged == 1,
		"provider_evidence_never_leaves_server":                    !strings.Contains(allHTTP, secretMarker) && !strings.Contains(allHTTP, unsafeTarget) && !strings.Contains(strings.ToLower(allHTTP), "authorization"),
		"authorized_revalidation_completes_current_allow":          revalidate.Status == http.StatusOK && revalidateCreated && latestAllow == 1,
		"revalidation_idempotency_replays_without_duplicate":       replay.Status == http.StatusOK && !replayCreated && idempotentRows == 1,
		"unsafe_mutation_requires_p15_csrf":                        missingCSRF.Status == http.StatusForbidden && evaluationRows == 2,
		"immediate_second_revalidation_is_rate_limited":            rateLimited.Status == http.StatusConflict && rateAuditRows == 1,
		"admin_responses_do_not_offer_bypass":                      !strings.Contains(strings.ToLower(allHTTP), "continue anyway") && !strings.Contains(strings.ToLower(allHTTP), "bypass"),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
