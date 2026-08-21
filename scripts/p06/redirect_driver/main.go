package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

func main() {
	caseFlag := flag.String("case", "P06-T019", "P06 custom-host redirect case ID")
	flag.Parse()
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	if dsn == "" || redisAddr == "" {
		failFatal("GOJET_MYSQL_DSN and GOJET_REDIS_ADDR are required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		failFatal(err.Error())
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		failFatal(fmt.Sprintf("ping MySQL: %v", err))
	}
	redisClient := links.NewRedisClient(redisAddr, "", 0)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		failFatal(fmt.Sprintf("ping Redis: %v", err))
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		failFatal(fmt.Sprintf("flush Redis: %v", err))
	}

	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	if *caseFlag != "P06-T019" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT019(ctx, db, redisClient, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT019(ctx context.Context, db *sql.DB, redisClient *redis.Client, out *caseResult) error {
	now := time.Now().UTC().Truncate(time.Second)
	workspace := "p06-t019-redirect"
	actor := "owner-t019"
	hostname := "route-t019.example.com"
	code := "same-code-t019"
	customerDestination := "https://customer-t019.example/target?t=t019"
	officialDestination := "https://official-fallback-t019.example/target"

	domainStore := domains.NewMySQLStore(db)
	linkStore := links.NewMySQLStoreWithCustomDomainAuthority(db, domainStore)
	riskStore := links.NewRedisRiskStore(redisClient)
	handler := links.NewRedirectHandler(linkStore, riskStore, true)

	if _, err := domainStore.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "business-t019",
		Status: domains.EntitlementActive,
		DomainLimit: 4,
		StartsAt: now.Add(-30 * 24 * time.Hour),
		DecisionReason: "T019 active entitlement fixture",
	}, "corr-p06-t019-plan"); err != nil {
		return err
	}
	createdDomain, err := domainStore.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t019-domain",
		Reason: "create redirect authority fixture",
		Hostname: hostname,
		Now: now,
	})
	if err != nil {
		return err
	}
	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "verified", "valid", "active", "allow", "enabled", ""); err != nil {
		return err
	}

	customLink, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t019-custom-link",
		ChangeReason: "T019 custom redirect fixture",
		Hostname: hostname,
		DomainKind: "custom",
		Code: code,
		Title: "T019 custom",
		PrimaryDestination: customerDestination,
		RedirectStatus: http.StatusFound,
		Routing: []links.RoutingRule{},
		AB: []links.ABVariant{},
		UTM: links.UTMConfig{},
		Access: links.AccessConfig{},
	})
	if err != nil {
		return err
	}
	officialLink, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t019-official-link",
		ChangeReason: "T019 official fallback decoy",
		Hostname: "gojet.cc",
		DomainKind: "official",
		Code: code,
		Title: "T019 official decoy",
		PrimaryDestination: officialDestination,
		RedirectStatus: http.StatusFound,
		Routing: []links.RoutingRule{},
		AB: []links.ABVariant{},
		UTM: links.UTMConfig{},
		Access: links.AccessConfig{},
	})
	if err != nil {
		return err
	}
	if _, err := riskStore.PutDecision(ctx, customLink.ID, customLink.RiskFingerprint, links.RiskAllow, "t019-destination-policy", 30*time.Minute); err != nil {
		return err
	}
	if _, err := riskStore.PutDecision(ctx, officialLink.ID, officialLink.RiskFingerprint, links.RiskAllow, "t019-official-policy", 30*time.Minute); err != nil {
		return err
	}

	ready := doRedirect(handler, hostname, code)
	if ready.Code != http.StatusFound || ready.Header().Get("Location") != customerDestination {
		return fmt.Errorf("ready custom redirect failed: status=%d location=%q body=%s", ready.Code, ready.Header().Get("Location"), ready.Body.String())
	}

	deniedStates := []struct {
		name      string
		ownership string
		ingress   string
		https     string
		risk      string
		routing   string
		security  string
	}{
		{"routing_pending", "verified", "valid", "active", "allow", "pending", ""},
		{"ownership", "lost", "valid", "active", "allow", "enabled", ""},
		{"ingress_dns", "verified", "invalid", "active", "allow", "enabled", ""},
		{"https", "verified", "valid", "error", "allow", "enabled", ""},
		{"domain_risk", "verified", "valid", "active", "review", "enabled", ""},
		{"security", "verified", "valid", "active", "allow", "suspended", "security"},
	}
	for _, tc := range deniedStates {
		if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, tc.ownership, tc.ingress, tc.https, tc.risk, tc.routing, tc.security); err != nil {
			return err
		}
		response := doRedirect(handler, hostname, code)
		if err := requireNoDestination(response, http.StatusServiceUnavailable, customerDestination, officialDestination); err != nil {
			return fmt.Errorf("%s fail-closed redirect: %w", tc.name, err)
		}
	}

	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "verified", "valid", "active", "allow", "enabled", ""); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET status='expired', degraded_at=NULL, grace_until=NULL, expires_at=?
		WHERE workspace_id=? AND source='plan' AND source_key='business-t019'`, now.Add(-time.Minute), workspace); err != nil {
		return err
	}
	expired := doRedirect(handler, hostname, code)
	if err := requireNoDestination(expired, http.StatusServiceUnavailable, customerDestination, officialDestination); err != nil {
		return fmt.Errorf("expired entitlement fail closed: %w", err)
	}

	degradedAt := now.Add(-24 * time.Hour)
	graceUntil := degradedAt.Add(domains.NormalDowngradeGrace)
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET status='active', expires_at=?, degraded_at=?, grace_until=?
		WHERE workspace_id=? AND source='plan' AND source_key='business-t019'`, degradedAt, degradedAt, graceUntil, workspace); err != nil {
		return err
	}
	graceResponse := doRedirect(handler, hostname, code)
	if graceResponse.Code != http.StatusFound || graceResponse.Header().Get("Location") != customerDestination {
		return fmt.Errorf("valid normal-downgrade grace did not preserve routing: status=%d location=%q", graceResponse.Code, graceResponse.Header().Get("Location"))
	}

	expiredDegradedAt := now.Add(-8 * 24 * time.Hour)
	expiredGraceUntil := expiredDegradedAt.Add(domains.NormalDowngradeGrace)
	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET status='active', expires_at=?, degraded_at=?, grace_until=?
		WHERE workspace_id=? AND source='plan' AND source_key='business-t019'`, expiredDegradedAt, expiredDegradedAt, expiredGraceUntil, workspace); err != nil {
		return err
	}
	graceExpired := doRedirect(handler, hostname, code)
	if err := requireNoDestination(graceExpired, http.StatusServiceUnavailable, customerDestination, officialDestination); err != nil {
		return fmt.Errorf("expired grace fail closed: %w", err)
	}

	if _, err := db.ExecContext(ctx, `
		UPDATE custom_domain_entitlement_sources
		SET status='active', expires_at=NULL, degraded_at=NULL, grace_until=NULL
		WHERE workspace_id=? AND source='plan' AND source_key='business-t019'`, workspace); err != nil {
		return err
	}
	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "verified", "valid", "active", "allow", "enabled", ""); err != nil {
		return err
	}
	if _, err := riskStore.PutDecision(ctx, customLink.ID, customLink.RiskFingerprint, links.RiskBlock, "t019-destination-policy", 30*time.Minute); err != nil {
		return err
	}
	riskBlocked := doRedirect(handler, hostname, code)
	if err := requireNoDestination(riskBlocked, http.StatusOK, customerDestination, officialDestination); err != nil {
		return fmt.Errorf("destination-risk block fail closed: %w", err)
	}
	if _, err := riskStore.PutDecision(ctx, customLink.ID, customLink.RiskFingerprint, links.RiskAllow, "t019-destination-policy", 30*time.Minute); err != nil {
		return err
	}

	lookup, err := linkStore.GetByHostCodeForRedirect(ctx, hostname, code, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("pre-race current authority lookup: %w", err)
	}
	beforeClicks, err := clickCount(ctx, db, customLink.ID)
	if err != nil {
		return err
	}
	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "verified", "valid", "active", "allow", "suspended", "security"); err != nil {
		return err
	}
	_, claimState, err := linkStore.ClaimRedirectAccessCurrentAuthority(ctx, lookup.ID, lookup.Version, lookup.RiskFingerprint, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("final claim authority returned error: %w", err)
	}
	if claimState != links.AccessClaimDomainUnavailable {
		return fmt.Errorf("final claim did not catch raced domain suspension: state=%s", claimState)
	}
	afterClicks, err := clickCount(ctx, db, customLink.ID)
	if err != nil {
		return err
	}
	if afterClicks != beforeClicks {
		return fmt.Errorf("raced domain suspension consumed click count: before=%d after=%d", beforeClicks, afterClicks)
	}

	out.Details = map[string]any{
		"ready_custom_redirect_location": ready.Header().Get("Location"),
		"runtime_denied_authorities": []string{"routing_pending", "ownership", "ingress_dns", "https", "domain_risk", "security", "entitlement_expired", "grace_expired"},
		"normal_downgrade_grace_preserved_existing_routing": true,
		"destination_risk_block_fail_closed": true,
		"official_same_code_decoy_present": officialLink.ID > 0,
		"official_host_fallback_observed": false,
		"denied_responses_exposed_destination": false,
		"final_claim_recheck_caught_raced_suspension": true,
		"raced_suspension_click_count_unchanged": true,
		"custom_link_id": customLink.ID,
		"official_decoy_link_id": officialLink.ID,
	}
	return nil
}

func doRedirect(handler http.Handler, host, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "https://"+host+"/"+code, nil)
	req.Host = host
	req.RemoteAddr = "203.0.113.19:41019"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func requireNoDestination(response *httptest.ResponseRecorder, expectedStatus int, customerDestination, officialDestination string) error {
	if response.Code != expectedStatus {
		return fmt.Errorf("status=%d want=%d body=%s", response.Code, expectedStatus, response.Body.String())
	}
	if location := strings.TrimSpace(response.Header().Get("Location")); location != "" {
		return fmt.Errorf("denied response exposed Location=%q", location)
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{
		strings.ToLower(customerDestination),
		strings.ToLower(officialDestination),
		"customer-t019.example",
		"official-fallback-t019.example",
		"https://gojet.cc/" + strings.ToLower("same-code-t019"),
	} {
		if forbidden != "" && strings.Contains(body, forbidden) {
			return fmt.Errorf("denied safety surface exposed forbidden destination/fallback %q", forbidden)
		}
	}
	return nil
}

func setDomainState(ctx context.Context, db *sql.DB, workspace string, domainID uint64, ownership, ingress, httpsState, risk, routing, securityCategory string) error {
	var category any
	if strings.TrimSpace(securityCategory) != "" {
		category = securityCategory
	}
	_, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status=?, ingress_dns_status=?, https_status=?, risk_status=?, routing_state=?, security_category=?,
		    ownership_verified_at=CASE WHEN ?='verified' THEN CURRENT_TIMESTAMP(6) ELSE NULL END,
		    ingress_dns_checked_at=CURRENT_TIMESTAMP(6), https_checked_at=CURRENT_TIMESTAMP(6), risk_checked_at=CURRENT_TIMESTAMP(6),
		    risk_policy_version='t019-fixture', risk_evidence_ref='risk:t019:fixture'
		WHERE workspace_id=? AND id=?`, ownership, ingress, httpsState, risk, routing, category, ownership, workspace, domainID)
	return err
}

func clickCount(ctx context.Context, db *sql.DB, linkID uint64) (uint64, error) {
	var count uint64
	if err := db.QueryRowContext(ctx, `SELECT click_count FROM links WHERE id=?`, linkID).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		failFatal(err.Error())
	}
}

func failFatal(message string) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message})
	os.Exit(2)
}
