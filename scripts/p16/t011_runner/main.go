package main

import (
	"context"
	"encoding/json"
	"fmt"
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
		out.Case = "P16-T011"
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
		Case:         "P16-T011",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native redirectengine target mutation plus semantically equivalent routing/A-B reorder and duplicate-target normalization",
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
	workspace := "p16-t011-workspace"

	mutationRouting := []links.RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route.example/original", Enabled: true}}
	mutationAB := []links.ABVariant{
		{ID: "a", Destination: "https://a.example/original", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b.example/original", Weight: 50, Enabled: true},
	}
	mutated, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t011-mutation", "https://primary.example/original", mutationRouting, mutationAB)
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, mutated, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	beforeMutation, err := runtimefixture.RequestRedirect(ctx, mutated.Hostname, mutated.Code)
	if err != nil {
		return out, err
	}
	oldKey := links.RiskDecisionKey(mutated.ID, mutated.Fingerprint)
	oldKeyBefore, err := redisClient.Exists(ctx, oldKey).Result()
	if err != nil {
		return out, err
	}

	newRouting := []links.RoutingRule{{ID: "us", MatchType: "country", MatchValue: "US", Destination: "https://route.example/changed", Enabled: true}}
	newFingerprint, newTargets, err := links.RiskFingerprint(mutated.Primary, newRouting, mutationAB)
	if err != nil {
		return out, err
	}
	newRoutingRaw, _ := json.Marshal(newRouting)
	if _, err := db.ExecContext(ctx, `UPDATE links SET routing_json=?,risk_fingerprint=?,version=version+1,updated_at=NOW(6) WHERE id=?`, string(newRoutingRaw), newFingerprint, mutated.ID); err != nil {
		return out, err
	}
	oldKeyAfter, err := redisClient.Exists(ctx, oldKey).Result()
	if err != nil {
		return out, err
	}
	newKeyExists, err := redisClient.Exists(ctx, links.RiskDecisionKey(mutated.ID, newFingerprint)).Result()
	if err != nil {
		return out, err
	}
	afterMutation, err := runtimefixture.RequestRedirect(ctx, mutated.Hostname, mutated.Code)
	if err != nil {
		return out, err
	}

	duplicateRouting := []links.RoutingRule{
		{ID: "us-a", MatchType: "country", MatchValue: "US", Destination: "https://route.example/stable", Enabled: true},
		{ID: "us-b", MatchType: "country", MatchValue: "US", Destination: "https://route.example/stable", Enabled: true},
	}
	stableAB := []links.ABVariant{
		{ID: "a", Destination: "https://a.example/stable", Weight: 50, Enabled: true},
		{ID: "b", Destination: "https://b.example/stable", Weight: 50, Enabled: true},
	}
	stable, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t011-stable", "https://primary.example/stable", duplicateRouting, stableAB)
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, stable, links.RiskAllow, 10*time.Minute); err != nil {
		return out, err
	}
	stableBefore, err := runtimefixture.RequestRedirect(ctx, stable.Hostname, stable.Code)
	if err != nil {
		return out, err
	}

	reorderedRouting := []links.RoutingRule{{ID: "us-b", MatchType: "country", MatchValue: "US", Destination: "https://route.example/stable", Enabled: true}}
	reorderedAB := []links.ABVariant{
		{ID: "b", Destination: "https://b.example/stable", Weight: 50, Enabled: true},
		{ID: "a", Destination: "https://a.example/stable", Weight: 50, Enabled: true},
	}
	stableFingerprintAfter, stableTargetsAfter, err := links.RiskFingerprint(stable.Primary, reorderedRouting, reorderedAB)
	if err != nil {
		return out, err
	}
	routingRaw, _ := json.Marshal(reorderedRouting)
	abRaw, _ := json.Marshal(reorderedAB)
	if _, err := db.ExecContext(ctx, `UPDATE links SET routing_json=?,ab_json=?,risk_fingerprint=?,version=version+1,updated_at=NOW(6) WHERE id=?`, string(routingRaw), string(abRaw), stableFingerprintAfter, stable.ID); err != nil {
		return out, err
	}
	stableAfter, err := runtimefixture.RequestRedirect(ctx, stable.Hostname, stable.Code)
	if err != nil {
		return out, err
	}
	stableKeyExists, err := redisClient.Exists(ctx, links.RiskDecisionKey(stable.ID, stable.Fingerprint)).Result()
	if err != nil {
		return out, err
	}

	mutationBodySafe := true
	for _, target := range append(append([]string{}, mutated.Targets...), newTargets...) {
		mutationBodySafe = mutationBodySafe && !strings.Contains(afterMutation.Body, target)
	}

	out.RecordCounts = map[string]int{
		"old_projection_keys_preserved_for_non_authoritative_history": int(oldKeyAfter),
		"new_projection_keys_without_rescan":                       int(newKeyExists),
		"stable_projection_keys":                                   int(stableKeyExists),
	}
	out.Checks = map[string]bool{
		"pre_mutation_exact_allow_redirects": beforeMutation.Status == 302 && beforeMutation.Location != "" && oldKeyBefore == 1,
		"reachable_target_mutation_changes_fingerprint": newFingerprint != mutated.Fingerprint,
		"old_allow_key_cannot_authorize_new_fingerprint": oldKeyAfter == 1 && newKeyExists == 0 && afterMutation.Location == "" && afterMutation.Status != 301 && afterMutation.Status != 302 && afterMutation.Status != 307 && afterMutation.Status != 308,
		"mutation_safety_response_discloses_no_old_or_new_target": mutationBodySafe,
		"duplicate_target_dedup_and_reorder_preserve_fingerprint": stableFingerprintAfter == stable.Fingerprint && strings.Join(stableTargetsAfter, "\n") == strings.Join(stable.Targets, "\n"),
		"semantically_equivalent_change_keeps_exact_allow_authority": stableBefore.Status == 302 && stableAfter.Status == 302 && stableBefore.Location != "" && stableAfter.Location != "" && stableKeyExists == 1,
		"stable_redirect_remains_within_exact_target_set": locationMatchesTarget(stableAfter.Location, stable.Targets),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func locationMatchesTarget(location string, targets []string) bool {
	normalized, err := links.NormalizeDestination(stripUTM(location))
	if err != nil {
		return false
	}
	for _, target := range targets {
		if normalized == target {
			return true
		}
	}
	return false
}

func stripUTM(raw string) string {
	marker := "?"
	if !strings.Contains(raw, marker) {
		return raw
	}
	parts := strings.SplitN(raw, marker, 2)
	pairs := strings.Split(parts[1], "&")
	kept := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		key := strings.ToLower(strings.SplitN(pair, "=", 2)[0])
		if strings.HasPrefix(key, "utm_") {
			continue
		}
		kept = append(kept, pair)
	}
	if len(kept) == 0 {
		return parts[0]
	}
	return parts[0] + "?" + strings.Join(kept, "&")
}
