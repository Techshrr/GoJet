package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		out.Case = "P16-T014"
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
		Case:         "P16-T014",
		Status:       "FAIL",
		Fixture:      "real native redirectengine observable side effects proving domain/link authority then exact risk allow before routing/A-B/UTM/password/counters and safety non-disclosure",
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
	if err := redisClient.FlushDB(ctx).Err(); err != nil {
		return out, err
	}
	runtime := links.NewRedisRiskStore(redisClient)
	workspace := "p16-t014-workspace"

	customTarget := "https://unsafe-custom-target.example/private?marker=t014-custom-secret"
	customLink, err := runtimefixture.CreateLink(ctx, db, workspace, "not-ready-t014.example.com", "custom", "t014-domain", customTarget, nil, nil)
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, customLink, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	customResult, err := runtimefixture.RequestRedirect(ctx, customLink.Hostname, customLink.Code)
	if err != nil {
		return out, err
	}

	pausedTarget := "https://unsafe-paused-target.example/private?marker=t014-paused-secret"
	pausedLink, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t014-paused", pausedTarget, nil, nil)
	if err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE links SET status='paused',updated_at=NOW(6) WHERE id=?`, pausedLink.ID); err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, pausedLink, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	pausedResult, err := runtimefixture.RequestRedirect(ctx, pausedLink.Hostname, pausedLink.Code)
	if err != nil {
		return out, err
	}

	routing := []links.RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route-secret-t014.example/private?existing=1", Enabled: true}}
	variants := []links.ABVariant{
		{ID: "a", Destination: "https://a-secret-t014.example/private", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b-secret-t014.example/private", Weight: 50, Enabled: true},
	}
	protected, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t014-ordered", "https://primary-secret-t014.example/private", routing, variants)
	if err != nil {
		return out, err
	}
	password := "P16-T014-Ordering-Password!"
	passwordHash, err := links.HashLinkPassword(password)
	if err != nil {
		return out, err
	}
	accessRaw, _ := json.Marshal(links.AccessConfig{PasswordHash: passwordHash})
	if _, err := db.ExecContext(ctx, `UPDATE links SET access_json=?,click_limit=1,one_time=1,updated_at=NOW(6) WHERE id=?`, string(accessRaw), protected.ID); err != nil {
		return out, err
	}
	headers := http.Header{
		"X-GoJet-Test-Country": []string{"US"},
		"User-Agent":           []string{"GoJet-P16-T014/1.0"},
		"Accept-Language":      []string{"en-US,en;q=0.9"},
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, protected, links.RiskReview, 10*time.Minute); err != nil {
		return out, err
	}
	riskReview, err := runtimefixture.RequestRedirectWithHeaders(ctx, protected.Hostname, protected.Code, headers)
	if err != nil {
		return out, err
	}
	clicksAfterRisk, err := runtimefixture.ScalarInt(ctx, db, `SELECT click_count FROM links WHERE id=?`, protected.ID)
	if err != nil {
		return out, err
	}

	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, protected, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	passwordChallenge, err := runtimefixture.RequestRedirectWithHeaders(ctx, protected.Hostname, protected.Code, headers)
	if err != nil {
		return out, err
	}
	clicksAfterChallenge, err := runtimefixture.ScalarInt(ctx, db, `SELECT click_count FROM links WHERE id=?`, protected.ID)
	if err != nil {
		return out, err
	}

	accepted, err := requestPassword(ctx, protected.Hostname, protected.Code, password, headers)
	if err != nil {
		return out, err
	}
	clicksAfterAccepted, err := runtimefixture.ScalarInt(ctx, db, `SELECT click_count FROM links WHERE id=?`, protected.ID)
	if err != nil {
		return out, err
	}
	exhausted, err := requestPassword(ctx, protected.Hostname, protected.Code, password, headers)
	if err != nil {
		return out, err
	}
	clicksAfterExhausted, err := runtimefixture.ScalarInt(ctx, db, `SELECT click_count FROM links WHERE id=?`, protected.ID)
	if err != nil {
		return out, err
	}

	acceptedURL, err := url.Parse(accepted.Location)
	if err != nil {
		return out, err
	}
	acceptedQuery := acceptedURL.Query()
	allSafetyBodies := []string{customResult.Body, pausedResult.Body, riskReview.Body, passwordChallenge.Body, exhausted.Body}
	forbiddenFragments := []string{
		"unsafe-custom-target.example", "t014-custom-secret",
		"unsafe-paused-target.example", "t014-paused-secret",
		"primary-secret-t014.example", "route-secret-t014.example", "a-secret-t014.example", "b-secret-t014.example",
		"semantic-fixture", "provider evidence", "provider threshold", "continue-anyway", "continue anyway", "bypass safety",
	}
	nonDisclosure := true
	for _, body := range allSafetyBodies {
		lower := strings.ToLower(body)
		for _, fragment := range forbiddenFragments {
			nonDisclosure = nonDisclosure && !strings.Contains(lower, strings.ToLower(fragment))
		}
	}

	out.RecordCounts = map[string]int{
		"safety_surfaces_scanned":  len(allSafetyBodies),
		"clicks_after_final_allow": clicksAfterAccepted,
	}
	out.Checks = map[string]bool{
		"custom_domain_axis_blocks_before_destination_risk":                          customResult.Status == http.StatusServiceUnavailable && customResult.Location == "" && strings.Contains(strings.ToLower(customResult.Body), "domain unavailable"),
		"link_state_blocks_before_destination_risk":                                  pausedResult.Location == "" && clicksAfterRisk == 0 && !strings.Contains(pausedResult.Body, pausedTarget),
		"non_allow_risk_blocks_before_routing_ab_utm_password_and_counters":          riskReview.Location == "" && clicksAfterRisk == 0 && !strings.Contains(strings.ToLower(riskReview.Body), "password required") && strings.Contains(strings.ToLower(riskReview.Body), "under review"),
		"access_challenge_occurs_only_after_exact_risk_allow":                        passwordChallenge.Location == "" && clicksAfterChallenge == 0 && strings.Contains(strings.ToLower(passwordChallenge.Body), "password required"),
		"routing_and_utm_reach_location_only_after_password_and_counter_authority":   accepted.Status == 302 && acceptedURL.Hostname() == "route-secret-t014.example" && acceptedQuery.Get("existing") == "1" && acceptedQuery.Get("utm_source") == "p16-runtime" && acceptedQuery.Get("utm_campaign") == "safety-order" && clicksAfterAccepted == 1,
		"one_time_click_limit_is_after_risk_and_access_and_prevents_second_location": exhausted.Location == "" && clicksAfterExhausted == 1 && (exhausted.Status == http.StatusGone || exhausted.Status == http.StatusOK),
		"safety_surfaces_are_no_store_noindex":                                       strings.Contains(strings.ToLower(riskReview.Headers.Get("Cache-Control")), "no-store") && strings.Contains(strings.ToLower(riskReview.Headers.Get("X-Robots-Tag")), "noindex") && strings.Contains(strings.ToLower(customResult.Headers.Get("Cache-Control")), "no-store"),
		"safety_responses_disclose_no_target_provider_threshold_or_bypass":           nonDisclosure,
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func requestPassword(ctx context.Context, hostname, code, password string, headers http.Header) (runtimefixture.HTTPResult, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_REDIRECT_URL")), "/")
	if base == "" {
		return runtimefixture.HTTPResult{}, fmt.Errorf("GOJET_REDIRECT_URL is required")
	}
	form := url.Values{"password": []string{password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/"+code, strings.NewReader(form.Encode()))
	if err != nil {
		return runtimefixture.HTTPResult{}, err
	}
	req.Host = hostname
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return runtimefixture.HTTPResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return runtimefixture.HTTPResult{}, err
	}
	return runtimefixture.HTTPResult{Status: resp.StatusCode, Location: resp.Header.Get("Location"), Body: string(raw), Headers: resp.Header.Clone()}, nil
}
