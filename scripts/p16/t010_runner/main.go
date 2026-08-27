package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
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
		out.Case = "P16-T010"
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
		Case:         "P16-T010",
		Status:       "FAIL",
		Fixture:      "real native redirectengine with P06-ready custom-domain axes proving official/custom parity over primary, routing and link-scoped A/B targets",
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
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		return out, err
	}

	runtime := links.NewRedisRiskStore(redisClient)
	workspace := "p16-t010-workspace"
	customHost := "safe-t010.example.com"
	if err := runtimefixture.CreateReadyCustomDomain(ctx, db, workspace, customHost, time.Now().UTC()); err != nil {
		return out, err
	}

	type scenario struct {
		name    string
		primary string
		routing []links.RoutingRule
		ab      []links.ABVariant
		headers http.Header
		kind    string
	}
	scenarios := []scenario{
		{name: "primary", primary: "https://primary.example/t010-primary", kind: "primary"},
		{
			name:    "routing",
			primary: "https://primary.example/t010-routing",
			routing: []links.RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route.example/t010-us", Enabled: true}},
			headers: http.Header{"X-GoJet-Test-Country": []string{"US"}, "User-Agent": []string{"GoJet-P16-T010/1.0"}, "Accept-Language": []string{"en-US,en;q=0.9"}},
			kind:    "routing",
		},
		{
			name:    "ab",
			primary: "https://primary.example/t010-ab",
			ab: []links.ABVariant{
				{ID: "a", Destination: "https://a.example/t010-ab", Weight: 50, Enabled: true},
				{ID: "b", Destination: "https://b.example/t010-ab", Weight: 50, Enabled: true},
			},
			headers: http.Header{"User-Agent": []string{"GoJet-P16-T010/1.0"}, "Accept-Language": []string{"en-US,en;q=0.9"}},
			kind:    "ab",
		},
	}

	fingerprintParity := true
	reviewParity := true
	allowEnforcementParity := true
	selectedKindCorrect := true
	reachableMembership := true
	customAxesUsed := true
	pairCount := 0
	redirectCount := 0

	for _, sc := range scenarios {
		official, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t010-"+sc.name+"-official", sc.primary, sc.routing, sc.ab)
		if err != nil {
			return out, err
		}
		custom, err := runtimefixture.CreateLink(ctx, db, workspace, customHost, "custom", "t010-"+sc.name+"-custom", sc.primary, sc.routing, sc.ab)
		if err != nil {
			return out, err
		}
		pairCount++
		fingerprintParity = fingerprintParity && official.Fingerprint == custom.Fingerprint && strings.Join(official.Targets, "\n") == strings.Join(custom.Targets, "\n")

		if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, official, links.RiskReview, time.Minute); err != nil {
			return out, err
		}
		if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, custom, links.RiskReview, time.Minute); err != nil {
			return out, err
		}
		officialReview, err := runtimefixture.RequestRedirectWithHeaders(ctx, official.Hostname, official.Code, sc.headers)
		if err != nil {
			return out, err
		}
		customReview, err := runtimefixture.RequestRedirectWithHeaders(ctx, custom.Hostname, custom.Code, sc.headers)
		if err != nil {
			return out, err
		}
		reviewParity = reviewParity && officialReview.Location == "" && customReview.Location == "" && officialReview.Status == customReview.Status
		for _, target := range official.Targets {
			reviewParity = reviewParity && !strings.Contains(officialReview.Body, target) && !strings.Contains(customReview.Body, target)
		}

		if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, official, links.RiskAllow, time.Minute); err != nil {
			return out, err
		}
		if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, custom, links.RiskAllow, time.Minute); err != nil {
			return out, err
		}
		officialAllow, err := runtimefixture.RequestRedirectWithHeaders(ctx, official.Hostname, official.Code, sc.headers)
		if err != nil {
			return out, err
		}
		customAllow, err := runtimefixture.RequestRedirectWithHeaders(ctx, custom.Hostname, custom.Code, sc.headers)
		if err != nil {
			return out, err
		}
		redirectCount += 2
		allowEnforcementParity = allowEnforcementParity && officialAllow.Status == 302 && customAllow.Status == 302 && officialAllow.Location != "" && customAllow.Location != ""

		officialParsed, err := url.Parse(officialAllow.Location)
		if err != nil {
			return out, err
		}
		customParsed, err := url.Parse(customAllow.Location)
		if err != nil {
			return out, err
		}
		officialMember, err := locationIsMember(officialParsed, official.Targets)
		if err != nil {
			return out, err
		}
		customMember, err := locationIsMember(customParsed, custom.Targets)
		if err != nil {
			return out, err
		}
		reachableMembership = reachableMembership && officialMember && customMember
		selectedKindCorrect = selectedKindCorrect && targetKindMatches(sc.kind, officialParsed) && targetKindMatches(sc.kind, customParsed)
	}

	domainRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domains WHERE workspace_id=? AND hostname_ascii=? AND routing_state='enabled' AND ownership_status='verified' AND ingress_dns_status='valid' AND https_status='active' AND risk_status='allow'`, workspace, customHost)
	if err != nil {
		return out, err
	}
	entitlementRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=? AND status='active'`, workspace)
	if err != nil {
		return out, err
	}
	customAxesUsed = customAxesUsed && domainRows == 1 && entitlementRows >= 1

	out.RecordCounts = map[string]int{
		"official_custom_pairs":  pairCount,
		"native_allow_redirects": redirectCount,
		"ready_custom_domains":   domainRows,
	}
	out.Checks = map[string]bool{
		"official_custom_exact_fingerprint_parity":                  fingerprintParity && pairCount == 3,
		"review_fails_closed_identically_on_both_hosts":             reviewParity,
		"allow_enforcement_releases_both_hosts_only_after_approval": allowEnforcementParity,
		"primary_routing_ab_selection_is_valid_on_both_hosts":       selectedKindCorrect,
		"every_selected_target_on_both_hosts_is_fingerprint_member": reachableMembership,
		"custom_host_used_real_independent_p06_axes":                customAxesUsed,
		"all_three_frozen_target_classes_are_covered":               pairCount == 3 && redirectCount == 6,
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func locationIsMember(parsed *url.URL, targets []string) (bool, error) {
	normalized, err := links.NormalizeDestination(stripRuntimeUTM(parsed))
	if err != nil {
		return false, err
	}
	for _, target := range targets {
		if target == normalized {
			return true, nil
		}
	}
	return false, nil
}

func targetKindMatches(kind string, parsed *url.URL) bool {
	switch kind {
	case "primary":
		return parsed.Hostname() == "primary.example"
	case "routing":
		return parsed.Hostname() == "route.example"
	case "ab":
		return parsed.Hostname() == "a.example" || parsed.Hostname() == "b.example"
	default:
		return false
	}
}

func stripRuntimeUTM(u *url.URL) string {
	clone := *u
	query := clone.Query()
	for key := range query {
		if strings.HasPrefix(strings.ToLower(key), "utm_") {
			query.Del(key)
		}
	}
	clone.RawQuery = query.Encode()
	return clone.String()
}
