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
	"reflect"
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
	caseFlag := flag.String("case", "P06-T020", "P06 official/custom risk parity case ID")
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
	if *caseFlag != "P06-T020" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT020(ctx, db, redisClient, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT020(ctx context.Context, db *sql.DB, redisClient *redis.Client, out *caseResult) error {
	now := time.Now().UTC().Truncate(time.Second)
	workspace := "p06-t020-parity"
	actor := "owner-t020"
	customHost := "parity-t020.example.com"
	allCode := "all-targets-t020"
	primaryCode := "primary-t020"

	domainStore := domains.NewMySQLStore(db)
	linkStore := links.NewMySQLStoreWithCustomDomainAuthority(db, domainStore)
	riskStore := links.NewRedisRiskStore(redisClient)
	handler := links.NewRedirectHandler(linkStore, riskStore, true)

	if _, err := domainStore.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "business-t020",
		Status: domains.EntitlementActive,
		DomainLimit: 2,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T020 active entitlement fixture",
	}, "corr-p06-t020-plan"); err != nil {
		return err
	}
	createdDomain, err := domainStore.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t020-domain",
		Reason: "create parity custom domain",
		Hostname: customHost,
		Now: now,
	})
	if err != nil {
		return err
	}
	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "enabled", ""); err != nil {
		return err
	}

	primaryRaw := "HTTPS://Primary-T020.EXAMPLE:443/root?z=9&a=1#ignored"
	routing := []links.RoutingRule{
		{ID: "us", MatchType: "country", MatchValue: "US", Destination: "HTTPS://Route-T020.EXAMPLE:443/us?b=2&a=1#ignored", Enabled: true},
		{ID: "disabled", MatchType: "country", MatchValue: "DE", Destination: "https://disabled-t020.example/never", Enabled: false},
	}
	variants := []links.ABVariant{
		{ID: "a", Destination: "https://A-T020.EXAMPLE:443/variant", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b-t020.example/variant", Weight: 50, Enabled: true},
	}

	officialAll, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace, ActorID: actor, CorrelationID: "corr-p06-t020-official-all", ChangeReason: "T020 official all-target parity fixture",
		Hostname: "gojet.cc", DomainKind: "official", Code: allCode, Title: "T020 official all",
		PrimaryDestination: primaryRaw, RedirectStatus: http.StatusFound, Routing: routing, AB: variants,
	})
	if err != nil {
		return err
	}
	customAll, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace, ActorID: actor, CorrelationID: "corr-p06-t020-custom-all", ChangeReason: "T020 custom all-target parity fixture",
		Hostname: customHost, DomainKind: "custom", Code: allCode, Title: "T020 custom all",
		PrimaryDestination: primaryRaw, RedirectStatus: http.StatusFound, Routing: routing, AB: variants,
	})
	if err != nil {
		return err
	}

	officialFingerprint, officialTargets, err := links.RiskFingerprint(officialAll.PrimaryDestination, officialAll.Routing, officialAll.AB)
	if err != nil {
		return err
	}
	customFingerprint, customTargets, err := links.RiskFingerprint(customAll.PrimaryDestination, customAll.Routing, customAll.AB)
	if err != nil {
		return err
	}
	if officialAll.RiskFingerprint != officialFingerprint || customAll.RiskFingerprint != customFingerprint || officialFingerprint != customFingerprint || !reflect.DeepEqual(officialTargets, customTargets) {
		return fmt.Errorf("official/custom reachable target fingerprint parity failed: official=%s custom=%s targets=%v/%v", officialFingerprint, customFingerprint, officialTargets, customTargets)
	}
	expectedTargets := []string{
		"https://a-t020.example/variant",
		"https://b-t020.example/variant",
		"https://primary-t020.example/root?a=1&z=9",
		"https://route-t020.example/us?a=1&b=2",
	}
	if !reflect.DeepEqual(officialTargets, expectedTargets) {
		return fmt.Errorf("normalized reachable targets=%v want=%v", officialTargets, expectedTargets)
	}

	// Missing risk must block both paths before the A/B selection that would
	// otherwise occur for a request without a matching routing rule.
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "")
		if err := requireSafety(response, http.StatusOK, expectedTargets); err != nil {
			return fmt.Errorf("%s missing-risk parity: %w", tc.name, err)
		}
	}

	for _, link := range []links.Link{officialAll, customAll} {
		if _, err := riskStore.PutDecision(ctx, link.ID, link.RiskFingerprint, links.RiskReview, "t020-shared-policy", 30*time.Minute); err != nil {
			return err
		}
	}
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "")
		if err := requireSafety(response, http.StatusOK, expectedTargets); err != nil {
			return fmt.Errorf("%s review-before-ab parity: %w", tc.name, err)
		}
	}

	for _, link := range []links.Link{officialAll, customAll} {
		if _, err := riskStore.PutDecision(ctx, link.ID, link.RiskFingerprint, links.RiskBlock, "t020-shared-policy", 30*time.Minute); err != nil {
			return err
		}
	}
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "US")
		if err := requireSafety(response, http.StatusOK, expectedTargets); err != nil {
			return fmt.Errorf("%s block-before-routing parity: %w", tc.name, err)
		}
	}

	for _, link := range []links.Link{officialAll, customAll} {
		if err := redisClient.Set(ctx, links.RiskDecisionKey(link.ID, link.RiskFingerprint), []byte("{"), 30*time.Minute).Err(); err != nil {
			return err
		}
	}
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "US")
		if err := requireSafety(response, http.StatusOK, expectedTargets); err != nil {
			return fmt.Errorf("%s malformed-risk parity: %w", tc.name, err)
		}
	}

	staleNow := time.Now().UTC()
	for _, link := range []links.Link{officialAll, customAll} {
		stale := links.RiskDecision{SchemaVersion: 1, Decision: links.RiskAllow, Fingerprint: link.RiskFingerprint, CheckedAt: staleNow.Add(-2 * time.Hour), ValidUntil: staleNow.Add(-time.Hour), PolicyVersion: "t020-shared-policy"}
		raw, err := json.Marshal(stale)
		if err != nil {
			return err
		}
		if err := redisClient.Set(ctx, links.RiskDecisionKey(link.ID, link.RiskFingerprint), raw, 30*time.Minute).Err(); err != nil {
			return err
		}
	}
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "US")
		if err := requireSafety(response, http.StatusOK, expectedTargets); err != nil {
			return fmt.Errorf("%s stale-risk parity: %w", tc.name, err)
		}
	}

	for _, link := range []links.Link{officialAll, customAll} {
		if _, err := riskStore.PutDecision(ctx, link.ID, link.RiskFingerprint, links.RiskAllow, "t020-shared-policy", 30*time.Minute); err != nil {
			return err
		}
	}

	// With identical current allow decisions, a matching routing rule is selected
	// only after risk allow on both official and custom paths.
	routeExpected := "https://route-t020.example/us?a=1&b=2"
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, allCode, "US")
		if response.Code != http.StatusFound || response.Header().Get("Location") != routeExpected {
			return fmt.Errorf("%s allow-routing parity status=%d location=%q", tc.name, response.Code, response.Header().Get("Location"))
		}
	}

	// No routing match falls through to the same A/B algorithm. Link IDs are part
	// of the deterministic bucket seed, so the chosen variant may differ, but both
	// selections must be A/B members of the exact same fingerprint target set.
	resolveContext := links.ResolveContext{ABSeed: "203.0.113.20\nGoJet-T020\n"}
	officialSelected, err := links.SelectTarget(officialAll, resolveContext)
	if err != nil {
		return err
	}
	customSelected, err := links.SelectTarget(customAll, resolveContext)
	if err != nil {
		return err
	}
	if officialSelected.Source != "ab" || customSelected.Source != "ab" {
		return fmt.Errorf("A/B parity source official=%s custom=%s", officialSelected.Source, customSelected.Source)
	}
	if err := links.VerifySelectedTargetIsFingerprintMember(officialAll, officialSelected.Destination); err != nil {
		return err
	}
	if err := links.VerifySelectedTargetIsFingerprintMember(customAll, customSelected.Destination); err != nil {
		return err
	}
	officialAB := doRedirect(handler, "gojet.cc", allCode, "")
	customAB := doRedirect(handler, customHost, allCode, "")
	if officialAB.Code != http.StatusFound || officialAB.Header().Get("Location") != officialSelected.Destination {
		return fmt.Errorf("official A/B runtime parity location=%q selected=%q", officialAB.Header().Get("Location"), officialSelected.Destination)
	}
	if customAB.Code != http.StatusFound || customAB.Header().Get("Location") != customSelected.Destination {
		return fmt.Errorf("custom A/B runtime parity location=%q selected=%q", customAB.Header().Get("Location"), customSelected.Destination)
	}

	// Separate equivalent primary-only pair proves the same risk-before-primary
	// ordering without changing the all-target fingerprint fixture.
	primaryOnlyRaw := "HTTPS://Primary-Only-T020.EXAMPLE:443/root?b=2&a=1#fragment"
	officialPrimary, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace, ActorID: actor, CorrelationID: "corr-p06-t020-official-primary", ChangeReason: "T020 official primary parity fixture",
		Hostname: "gojet.cc", DomainKind: "official", Code: primaryCode, Title: "T020 official primary", PrimaryDestination: primaryOnlyRaw, RedirectStatus: http.StatusFound,
	})
	if err != nil {
		return err
	}
	customPrimary, err := linkStore.Create(ctx, links.CreateInput{
		WorkspaceID: workspace, ActorID: actor, CorrelationID: "corr-p06-t020-custom-primary", ChangeReason: "T020 custom primary parity fixture",
		Hostname: customHost, DomainKind: "custom", Code: primaryCode, Title: "T020 custom primary", PrimaryDestination: primaryOnlyRaw, RedirectStatus: http.StatusFound,
	})
	if err != nil {
		return err
	}
	if officialPrimary.RiskFingerprint != customPrimary.RiskFingerprint {
		return fmt.Errorf("primary-only fingerprints differ official=%s custom=%s", officialPrimary.RiskFingerprint, customPrimary.RiskFingerprint)
	}
	primaryExpected := "https://primary-only-t020.example/root?a=1&b=2"
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, primaryCode, "")
		if err := requireSafety(response, http.StatusOK, []string{primaryExpected}); err != nil {
			return fmt.Errorf("%s missing-risk-before-primary parity: %w", tc.name, err)
		}
	}
	for _, link := range []links.Link{officialPrimary, customPrimary} {
		if _, err := riskStore.PutDecision(ctx, link.ID, link.RiskFingerprint, links.RiskAllow, "t020-shared-policy", 30*time.Minute); err != nil {
			return err
		}
	}
	for _, tc := range []struct{ name, host string }{{"official", "gojet.cc"}, {"custom", customHost}} {
		response := doRedirect(handler, tc.host, primaryCode, "")
		if response.Code != http.StatusFound || response.Header().Get("Location") != primaryExpected {
			return fmt.Errorf("%s allow-primary parity status=%d location=%q", tc.name, response.Code, response.Header().Get("Location"))
		}
	}

	// Custom-domain authority is an extra gate, not an alternate risk policy:
	// keep identical allow decisions/fingerprints, suspend only the custom domain,
	// and prove official routing continues while custom routing fails closed.
	fingerprintBeforeSuspension := customAll.RiskFingerprint
	if err := setDomainState(ctx, db, workspace, createdDomain.Domain.ID, "suspended", "security"); err != nil {
		return err
	}
	officialAfterDomainSuspension := doRedirect(handler, "gojet.cc", allCode, "US")
	if officialAfterDomainSuspension.Code != http.StatusFound || officialAfterDomainSuspension.Header().Get("Location") != routeExpected {
		return fmt.Errorf("official path was incorrectly coupled to custom-domain gate: status=%d location=%q", officialAfterDomainSuspension.Code, officialAfterDomainSuspension.Header().Get("Location"))
	}
	customAfterDomainSuspension := doRedirect(handler, customHost, allCode, "US")
	if customAfterDomainSuspension.Code != http.StatusServiceUnavailable || customAfterDomainSuspension.Header().Get("Location") != "" {
		return fmt.Errorf("custom-domain extra gate did not fail closed: status=%d location=%q", customAfterDomainSuspension.Code, customAfterDomainSuspension.Header().Get("Location"))
	}
	persistedCustom, err := linkStore.GetByID(ctx, workspace, customAll.ID)
	if err != nil {
		return err
	}
	if persistedCustom.RiskFingerprint != fingerprintBeforeSuspension || persistedCustom.RiskFingerprint != officialAll.RiskFingerprint {
		return fmt.Errorf("custom-domain gate altered/bypassed destination fingerprint")
	}

	out.Details = map[string]any{
		"shared_all_targets_fingerprint": officialFingerprint,
		"shared_all_targets": officialTargets,
		"shared_primary_only_fingerprint": officialPrimary.RiskFingerprint,
		"non_allow_states_fail_closed_on_both_hosts": []string{"missing", "review", "block", "malformed", "stale"},
		"risk_precedes_routing_selection": true,
		"risk_precedes_ab_selection": true,
		"risk_precedes_primary_selection": true,
		"allow_routing_location": routeExpected,
		"official_allow_ab_location": officialAB.Header().Get("Location"),
		"custom_allow_ab_location": customAB.Header().Get("Location"),
		"allow_primary_location": primaryExpected,
		"custom_domain_authority_is_additional_gate": true,
		"custom_domain_gate_did_not_change_fingerprint": true,
		"official_path_continues_when_only_custom_domain_is_suspended": true,
		"custom_path_fails_closed_when_domain_suspended": true,
	}
	return nil
}

func doRedirect(handler http.Handler, host, code, country string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "https://"+host+"/"+code, nil)
	req.Host = host
	req.RemoteAddr = "203.0.113.20:42020"
	req.Header.Set("User-Agent", "GoJet-T020")
	if country != "" {
		req.Header.Set("X-GoJet-Test-Country", country)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func requireSafety(response *httptest.ResponseRecorder, expectedStatus int, forbiddenTargets []string) error {
	if response.Code != expectedStatus {
		return fmt.Errorf("status=%d want=%d body=%s", response.Code, expectedStatus, response.Body.String())
	}
	if location := strings.TrimSpace(response.Header().Get("Location")); location != "" {
		return fmt.Errorf("non-allow response exposed Location=%q", location)
	}
	body := strings.ToLower(response.Body.String())
	for _, target := range forbiddenTargets {
		if target != "" && strings.Contains(body, strings.ToLower(target)) {
			return fmt.Errorf("non-allow safety surface exposed target=%q", target)
		}
	}
	return nil
}

func setDomainState(ctx context.Context, db *sql.DB, workspace string, domainID uint64, routing, securityCategory string) error {
	var category any
	if strings.TrimSpace(securityCategory) != "" {
		category = securityCategory
	}
	_, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status='verified', ingress_dns_status='valid', https_status='active', risk_status='allow', routing_state=?, security_category=?,
		    ownership_verified_at=CURRENT_TIMESTAMP(6), ingress_dns_checked_at=CURRENT_TIMESTAMP(6), https_checked_at=CURRENT_TIMESTAMP(6), risk_checked_at=CURRENT_TIMESTAMP(6),
		    risk_policy_version='t020-domain-policy', risk_evidence_ref='risk:t020:domain'
		WHERE workspace_id=? AND id=?`, routing, category, workspace, domainID)
	return err
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
