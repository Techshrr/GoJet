package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/analytics"
	"github.com/Techshrr/GoJet/internal/links"
	"github.com/redis/go-redis/v9"
)

type result struct {
	Errors  []string       `json:"errors"`
	Details map[string]any `json:"details"`
}

type redirectResult struct {
	Status   int
	Location string
	Body     string
}

type failClosedSnapshot struct {
	Clicks int
	Outbox int
	Stream int64
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	redisAddr := strings.TrimSpace(os.Getenv("GOJET_REDIS_ADDR"))
	redirectBase := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_P20_REDIRECT_BASE")), "/")
	exactHead := strings.TrimSpace(os.Getenv("P20_EXACT_HEAD"))
	if dsn == "" || redisAddr == "" || redirectBase == "" || len(exactHead) < 12 {
		writeResult(result{Errors: []string{"T013 runtime configuration is incomplete"}, Details: baseDetails()})
		return
	}

	db, err := links.OpenMySQL(dsn)
	if err != nil {
		writeResult(result{Errors: []string{"T013 could not open MySQL"}, Details: baseDetails()})
		return
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		writeResult(result{Errors: []string{"T013 could not reach MySQL"}, Details: baseDetails()})
		return
	}

	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		writeResult(result{Errors: []string{"T013 could not reach Redis"}, Details: baseDetails()})
		return
	}
	riskStore := links.NewRedisRiskStore(redisClient)
	linkStore := links.NewMySQLStore(db)

	suffix := strings.ToLower(exactHead[:12])
	email := "p20-t009-" + suffix + "@example.test"
	code := "p20t012" + suffix
	details := baseDetails()
	errors := make([]string, 0, 1)

	var userID, userStatus string
	if err := db.QueryRowContext(ctx, "SELECT id,status FROM auth_users WHERE email_normalized=?", email).Scan(&userID, &userStatus); err != nil {
		fail(&errors, details, "T013 could not bind to the T009-T012 auth user")
		return
	}
	var workspaceID, workspaceRole string
	if err := db.QueryRowContext(ctx, "SELECT workspace_id,role FROM workspace_memberships WHERE user_id=?", userID).Scan(&workspaceID, &workspaceRole); err != nil {
		fail(&errors, details, "T013 could not bind to the T012 Workspace")
		return
	}
	details["user_id"] = userID
	details["workspace_id"] = workspaceID
	if userStatus != "active" || workspaceRole != "owner" {
		fail(&errors, details, "T013 predecessor identity/Workspace state is not active owner authority")
		return
	}

	link, err := linkStore.GetByHostCode(ctx, "gojet.cc", code)
	if err != nil || link.WorkspaceID != workspaceID {
		fail(&errors, details, "T013 could not bind to the same T012 Link")
		return
	}
	details["link_id"] = link.ID
	details["t012_link_version_before_fixture"] = link.Version
	if link.Version != 2 || link.ClickCount != 0 || !validFingerprint(link.RiskFingerprint) {
		fail(&errors, details, "T013 predecessor Link is not the exact unclicked T012 version-2 state")
		return
	}

	primaryDestination := "https://primary.example/p20/" + suffix + "?base=primary"
	countryDestination := "https://country.example/p20/" + suffix + "?base=country"
	routingDestination := "https://route.example/p20/" + suffix + "?base=routing"
	abADestination := "https://ab-a.example/p20/" + suffix + "?base=ab-a"
	abBDestination := "https://ab-b.example/p20/" + suffix + "?base=ab-b"
	routing := []links.RoutingRule{
		{ID: "country-test-header-must-not-authorize", MatchType: "country", MatchValue: "SG", Destination: countryDestination, Enabled: true},
		{ID: "language-en", MatchType: "language", MatchValue: "en", Destination: routingDestination, Enabled: true},
	}
	variants := []links.ABVariant{
		{ID: "a", Destination: abADestination, Weight: 50, Enabled: true},
		{ID: "b", Destination: abBDestination, Weight: 50, Enabled: true},
	}
	utm := links.UTMConfig{
		Source:   "p20",
		Medium:   "redirect",
		Campaign: "t013-" + suffix,
		Content:  "routing-order",
	}
	updated, err := linkStore.Update(ctx, link.ID, links.UpdateInput{
		WorkspaceID:        workspaceID,
		ActorID:            userID,
		CorrelationID:      "p20-t013-fixture-" + suffix,
		ChangeReason:       "P20 T013 redirect routing fixture",
		ExpectedVersion:    link.Version,
		Hostname:           link.Hostname,
		DomainKind:         link.DomainKind,
		Code:               link.Code,
		Title:              "P20 T013 Redirect Link",
		PrimaryDestination: primaryDestination,
		RedirectStatus:     http.StatusTemporaryRedirect,
		Status:             "active",
		Routing:            routing,
		AB:                 variants,
		UTM:                utm,
		Access:             link.Access,
		ExpiresAt:          link.ExpiresAt,
		ClickLimit:         link.ClickLimit,
		OneTime:            link.OneTime,
	})
	if err != nil {
		fail(&errors, details, "T013 could not establish the real P05 redirect fixture")
		return
	}
	details["link_fixture_version"] = updated.Version
	details["link_fixture_fingerprint_changed"] = updated.Version == 3 && updated.RiskFingerprint != link.RiskFingerprint && validFingerprint(updated.RiskFingerprint)
	if details["link_fixture_fingerprint_changed"] != true {
		fail(&errors, details, "T013 routing fixture did not produce version-3 fingerprint authority")
		return
	}

	_, preScanState, err := riskStore.Resolve(ctx, updated.ID, updated.RiskFingerprint, time.Now().UTC())
	if err != nil || preScanState != links.RiskMissing {
		fail(&errors, details, "T013 routing mutation incorrectly inherited a risk decision")
		return
	}
	details["fixture_risk_state_before_scan"] = string(preScanState)
	if _, err := riskStore.PutDecision(ctx, updated.ID, updated.RiskFingerprint, links.RiskAllow, "p20-t013-allow-v1", 10*time.Minute); err != nil {
		fail(&errors, details, "T013 could not establish exact-current allow risk authority")
		return
	}

	before, err := snapshot(ctx, db, redisClient, updated.ID)
	if err != nil {
		fail(&errors, details, "T013 could not capture pre-redirect authority state")
		return
	}
	if before.Clicks != 0 || before.Outbox != 0 {
		fail(&errors, details, "T013 predecessor state already contains a redirect click identity")
		return
	}

	accepted, err := requestRedirect(ctx, redirectBase, code)
	if err != nil {
		fail(&errors, details, "T013 real redirectengine request failed")
		return
	}
	expectedLocation, err := expectedUTMLocation(routingDestination, utm)
	if err != nil {
		fail(&errors, details, "T013 could not construct the independent redirect oracle")
		return
	}
	details["redirect_http_status"] = accepted.Status
	details["redirect_status_semantics"] = accepted.Status == http.StatusTemporaryRedirect
	details["routing_precedes_ab"] = accepted.Location == expectedLocation
	details["utm_finalization"] = accepted.Location == expectedLocation
	details["spoofed_test_country_ignored"] = accepted.Location == expectedLocation
	details["customer_location_emitted_after_allow"] = accepted.Location != ""
	if accepted.Status != http.StatusTemporaryRedirect || accepted.Location != expectedLocation {
		fail(&errors, details, "T013 allow path did not preserve routing-before-A/B, UTM and redirect-status semantics")
		return
	}

	after, err := snapshot(ctx, db, redisClient, updated.ID)
	if err != nil {
		fail(&errors, details, "T013 could not inspect accepted click state")
		return
	}
	if after.Clicks != 1 || after.Outbox != 1 || after.Stream != before.Stream+1 {
		fail(&errors, details, "T013 accepted redirect did not create exactly one correlated click identity")
		return
	}

	var eventID, eventWorkspace, publishedStreamID string
	var eventLinkID, clickSequence uint64
	var published int
	if err := db.QueryRowContext(ctx, `
		SELECT event_id,workspace_id,link_id,click_sequence,COALESCE(published_stream_id,''),published_at IS NOT NULL
		FROM analytics_outbox WHERE link_id=? AND click_sequence=1`, updated.ID).Scan(
		&eventID, &eventWorkspace, &eventLinkID, &clickSequence, &publishedStreamID, &published,
	); err != nil {
		fail(&errors, details, "T013 could not inspect the durable click outbox identity")
		return
	}
	expectedEventID := deterministicClickEventID(workspaceID, updated.ID, 1)
	streamMatches, err := streamContainsEvent(ctx, redisClient, eventID, publishedStreamID)
	if err != nil {
		fail(&errors, details, "T013 could not inspect the real Redis click transport")
		return
	}
	clickIdentityCorrelated := eventID == expectedEventID && eventWorkspace == workspaceID && eventLinkID == updated.ID && clickSequence == 1 && published == 1 && publishedStreamID != "" && streamMatches
	details["click_event_id"] = eventID
	details["click_sequence"] = clickSequence
	details["click_identity_correlated"] = clickIdentityCorrelated
	details["analytics_outbox_published"] = published == 1
	details["analytics_redis_transport_seeded"] = streamMatches
	if !clickIdentityCorrelated {
		fail(&errors, details, "T013 click identity is not correlated across Link/MySQL outbox/Redis transport")
		return
	}

	baseline := after
	blocked, err := probeDecision(ctx, riskStore, redisClient, db, redirectBase, updated, links.RiskBlock, "p20-t013-block", baseline)
	if err != nil {
		fail(&errors, details, "T013 blocked risk probe could not execute")
		return
	}
	details["blocked_fail_closed"] = blocked

	review, err := probeDecision(ctx, riskStore, redisClient, db, redirectBase, updated, links.RiskReview, "p20-t013-review", baseline)
	if err != nil {
		fail(&errors, details, "T013 review risk probe could not execute")
		return
	}
	details["review_fail_closed"] = review

	staleRaw, err := json.Marshal(links.RiskDecision{
		SchemaVersion: 1,
		Decision:      links.RiskAllow,
		Fingerprint:   updated.RiskFingerprint,
		CheckedAt:     time.Now().UTC().Add(-2 * time.Minute),
		ValidUntil:    time.Now().UTC().Add(-time.Minute),
		PolicyVersion: "p20-t013-stale",
	})
	if err != nil {
		fail(&errors, details, "T013 could not encode isolated stale risk fixture")
		return
	}
	if err := redisClient.Set(ctx, links.RiskDecisionKey(updated.ID, updated.RiskFingerprint), staleRaw, 5*time.Minute).Err(); err != nil {
		fail(&errors, details, "T013 could not write isolated stale risk fixture")
		return
	}
	_, staleState, err := riskStore.Resolve(ctx, updated.ID, updated.RiskFingerprint, time.Now().UTC())
	if err != nil || staleState != links.RiskStale {
		fail(&errors, details, "T013 stale risk fixture did not resolve as stale")
		return
	}
	staleResponse, err := requestRedirect(ctx, redirectBase, code)
	if err != nil {
		fail(&errors, details, "T013 stale risk redirect request failed")
		return
	}
	staleSnapshot, err := snapshot(ctx, db, redisClient, updated.ID)
	if err != nil {
		fail(&errors, details, "T013 could not inspect stale fail-closed state")
		return
	}
	staleClosed := isFailClosed(staleResponse, staleSnapshot, baseline)
	details["stale_fail_closed"] = staleClosed

	if err := redisClient.Del(ctx, links.RiskDecisionKey(updated.ID, updated.RiskFingerprint)).Err(); err != nil {
		fail(&errors, details, "T013 could not establish missing/unknown risk fixture")
		return
	}
	_, missingState, err := riskStore.Resolve(ctx, updated.ID, updated.RiskFingerprint, time.Now().UTC())
	if err != nil || missingState != links.RiskMissing {
		fail(&errors, details, "T013 missing risk fixture did not resolve as unknown/missing")
		return
	}
	unknownResponse, err := requestRedirect(ctx, redirectBase, code)
	if err != nil {
		fail(&errors, details, "T013 unknown risk redirect request failed")
		return
	}
	unknownSnapshot, err := snapshot(ctx, db, redisClient, updated.ID)
	if err != nil {
		fail(&errors, details, "T013 could not inspect unknown fail-closed state")
		return
	}
	unknownClosed := isFailClosed(unknownResponse, unknownSnapshot, baseline)
	details["unknown_fail_closed"] = unknownClosed

	allFailClosed := blocked && review && staleClosed && unknownClosed
	details["risk_before_routing_and_click_identity"] = allFailClosed
	details["risk_fail_closed_no_destination_leak"] = allFailClosed
	if !allFailClosed {
		fail(&errors, details, "T013 non-allow risk state exposed a destination or created a click identity")
		return
	}

	if _, err := riskStore.PutDecision(ctx, updated.ID, updated.RiskFingerprint, links.RiskAllow, "p20-t013-restore-v1", 10*time.Minute); err != nil {
		fail(&errors, details, "T013 could not restore allow authority for the next timeline case")
		return
	}
	_, restoredState, err := riskStore.Resolve(ctx, updated.ID, updated.RiskFingerprint, time.Now().UTC())
	if err != nil || restoredState != links.RiskAllow {
		fail(&errors, details, "T013 did not leave the current Link fingerprint in a valid allow state")
		return
	}
	details["risk_authority_restored"] = true
	details["real_redirect_flow"] = details["redirect_status_semantics"] == true && details["routing_precedes_ab"] == true && details["utm_finalization"] == true && clickIdentityCorrelated && allFailClosed

	writeResult(result{Errors: errors, Details: details})
}

func baseDetails() map[string]any {
	return map[string]any{
		"real_redirectengine":      true,
		"real_mysql":               true,
		"real_redis":               true,
		"real_p05_links":           true,
		"mock_authority":           false,
		"test_header_authority":    false,
		"secret_material_recorded": false,
	}
}

func snapshot(ctx context.Context, db *sql.DB, redisClient *redis.Client, linkID uint64) (failClosedSnapshot, error) {
	var out failClosedSnapshot
	if err := db.QueryRowContext(ctx, "SELECT click_count FROM links WHERE id=?", linkID).Scan(&out.Clicks); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM analytics_outbox WHERE link_id=?", linkID).Scan(&out.Outbox); err != nil {
		return out, err
	}
	stream, err := redisClient.XLen(ctx, analytics.ClickStreamKey).Result()
	if err != nil && err != redis.Nil {
		return out, err
	}
	out.Stream = stream
	return out, nil
}

func probeDecision(ctx context.Context, riskStore *links.RedisRiskStore, redisClient *redis.Client, db *sql.DB, redirectBase string, link links.Link, state links.RiskState, policy string, baseline failClosedSnapshot) (bool, error) {
	if _, err := riskStore.PutDecision(ctx, link.ID, link.RiskFingerprint, state, policy, 5*time.Minute); err != nil {
		return false, err
	}
	_, resolved, err := riskStore.Resolve(ctx, link.ID, link.RiskFingerprint, time.Now().UTC())
	if err != nil || resolved != state {
		return false, fmt.Errorf("risk state mismatch")
	}
	response, err := requestRedirect(ctx, redirectBase, link.Code)
	if err != nil {
		return false, err
	}
	after, err := snapshot(ctx, db, redisClient, link.ID)
	if err != nil {
		return false, err
	}
	return isFailClosed(response, after, baseline), nil
}

func isFailClosed(response redirectResult, after, baseline failClosedSnapshot) bool {
	if response.Status != http.StatusOK || response.Location != "" {
		return false
	}
	body := strings.ToLower(response.Body)
	for _, leaked := range []string{"primary.example", "country.example", "route.example", "ab-a.example", "ab-b.example"} {
		if strings.Contains(body, leaked) {
			return false
		}
	}
	return after.Clicks == baseline.Clicks && after.Outbox == baseline.Outbox && after.Stream == baseline.Stream
}

func requestRedirect(ctx context.Context, base, code string) (redirectResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+url.PathEscape(code), nil)
	if err != nil {
		return redirectResult{}, err
	}
	req.Host = "gojet.cc"
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	req.Header.Set("User-Agent", "GoJet-P20-T013/1.0")
	// This deliberately attempts to spoof the predecessor test-only country
	// adapter. Production redirectengine is started with test routing headers off.
	req.Header.Set("X-GoJet-Test-Country", "SG")
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return redirectResult{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return redirectResult{}, err
	}
	return redirectResult{Status: resp.StatusCode, Location: resp.Header.Get("Location"), Body: string(raw)}, nil
}

func expectedUTMLocation(raw string, config links.UTMConfig) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	query := u.Query()
	for key, value := range map[string]string{
		"utm_source":   config.Source,
		"utm_medium":   config.Medium,
		"utm_campaign": config.Campaign,
		"utm_term":     config.Term,
		"utm_content":  config.Content,
	} {
		if strings.TrimSpace(value) != "" {
			query.Set(key, strings.TrimSpace(value))
		}
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func deterministicClickEventID(workspaceID string, linkID, clickSequence uint64) string {
	identity := fmt.Sprintf("gojet.analytics.click.v1\n%s\n%d\n%d", workspaceID, linkID, clickSequence)
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func streamContainsEvent(ctx context.Context, client *redis.Client, eventID, expectedStreamID string) (bool, error) {
	messages, err := client.XRange(ctx, analytics.ClickStreamKey, "-", "+").Result()
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if fmt.Sprint(message.Values["event_id"]) == eventID {
			return expectedStreamID == "" || message.ID == expectedStreamID, nil
		}
	}
	return false, nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func fail(errors *[]string, details map[string]any, message string) {
	*errors = append(*errors, message)
	writeResult(result{Errors: *errors, Details: details})
}

func writeResult(value result) {
	if value.Details == nil {
		value.Details = baseDetails()
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "T013 could not encode safe evidence")
	}
}
