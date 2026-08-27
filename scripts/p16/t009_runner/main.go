package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
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
		out.Case = "P16-T009"
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
		Case:         "P16-T009",
		Status:       "FAIL",
		Fixture:      "real native redirectengine with real MySQL and Redis exercising every frozen runtime non-allow state",
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
	store := trust.NewStore(db)
	policy := trust.DestinationPolicy{Version: "p16-runtime-policy-v1", RequiredProviders: []string{"semantic-fixture"}, AllowTTL: 10 * time.Minute}
	workspace := "p16-t009-workspace"
	nonAllow := map[string]bool{}

	makeLink := func(name string) (runtimefixture.LinkFixture, error) {
		return runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t009-"+name, "https://customer.example/"+name, nil, nil)
	}
	requestClosed := func(name string, link runtimefixture.LinkFixture) error {
		result, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
		if err != nil {
			return err
		}
		nonAllow[name] = result.Location == "" && result.Status != 301 && result.Status != 302 && result.Status != 307 && result.Status != 308
		return nil
	}

	missing, err := makeLink("missing")
	if err != nil {
		return out, err
	}
	if err := requestClosed("missing", missing); err != nil {
		return out, err
	}

	malformed, err := makeLink("malformed")
	if err != nil {
		return out, err
	}
	if err := runtimefixture.PutMalformedRuntime(ctx, redisClient, malformed, `{`, time.Minute); err != nil {
		return out, err
	}
	if err := requestClosed("malformed", malformed); err != nil {
		return out, err
	}

	stale, err := makeLink("stale")
	if err != nil {
		return out, err
	}
	if err := runtimefixture.PutStaleRuntime(ctx, redisClient, stale, time.Now().UTC()); err != nil {
		return out, err
	}
	if err := requestClosed("stale", stale); err != nil {
		return out, err
	}

	review, err := makeLink("review")
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, review, links.RiskReview, time.Minute); err != nil {
		return out, err
	}
	if err := requestClosed("review", review); err != nil {
		return out, err
	}

	block, err := makeLink("block")
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.PutRuntimeDecision(ctx, runtime, block, links.RiskBlock, time.Minute); err != nil {
		return out, err
	}
	if err := requestClosed("block", block); err != nil {
		return out, err
	}

	pending, err := makeLink("pending")
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.InsertRawDecision(ctx, db, store, pending, policy.Version, "pending", trust.DecisionPending, "provider-pending", nil); err != nil {
		return out, err
	}
	_, pendingProjectionErr := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, pending.WorkspaceID, pending.ID, policy.Version, time.Now().UTC(), time.Minute)
	if err := requestClosed("pending", pending); err != nil {
		return out, err
	}

	unknown, err := makeLink("unknown")
	if err != nil {
		return out, err
	}
	if _, err := runtimefixture.InsertRawDecision(ctx, db, store, unknown, policy.Version, "unknown", trust.DecisionUnknown, "provider-unknown", nil); err != nil {
		return out, err
	}
	_, unknownProjectionErr := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, unknown.WorkspaceID, unknown.ID, policy.Version, time.Now().UTC(), time.Minute)
	if err := requestClosed("unknown", unknown); err != nil {
		return out, err
	}

	unavailable, err := makeLink("provider-unavailable")
	if err != nil {
		return out, err
	}
	unavailableDecision, err := runtimefixture.FinalizeDecision(ctx, store, unavailable, policy, "provider-unavailable", trust.ProviderUnavailable)
	if err != nil {
		return out, err
	}
	if _, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, unavailable.WorkspaceID, unavailable.ID, policy.Version, time.Now().UTC(), time.Minute); err != nil {
		return out, err
	}
	if err := requestClosed("provider-unavailable", unavailable); err != nil {
		return out, err
	}

	allow, err := makeLink("allow")
	if err != nil {
		return out, err
	}
	allowDecision, err := runtimefixture.FinalizeDecision(ctx, store, allow, policy, "allow", trust.ProviderAllow)
	if err != nil {
		return out, err
	}
	allowProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, allow.WorkspaceID, allow.ID, policy.Version, time.Now().UTC(), time.Minute)
	if err != nil {
		return out, err
	}
	allowResult, err := runtimefixture.RequestRedirect(ctx, allow.Hostname, allow.Code)
	if err != nil {
		return out, err
	}

	allClosed := true
	for _, state := range []string{"missing", "malformed", "stale", "review", "block", "pending", "unknown", "provider-unavailable"} {
		allClosed = allClosed && nonAllow[state]
	}
	out.RecordCounts = map[string]int{
		"runtime_non_allow_states": len(nonAllow),
		"durable_decisions":        4,
	}
	out.Checks = map[string]bool{
		"missing_fails_closed":                           nonAllow["missing"],
		"malformed_fails_closed":                         nonAllow["malformed"],
		"stale_fails_closed":                             nonAllow["stale"],
		"review_fails_closed":                            nonAllow["review"],
		"block_fails_closed":                             nonAllow["block"],
		"pending_is_not_projectable_or_redirectable":     errors.Is(pendingProjectionErr, trust.ErrConflict) && nonAllow["pending"],
		"unknown_is_not_projectable_or_redirectable":     errors.Is(unknownProjectionErr, trust.ErrConflict) && nonAllow["unknown"],
		"provider_unavailable_is_non_allow":              unavailableDecision.State == trust.DecisionReview && unavailableDecision.ReasonCategory == "provider-unavailable" && nonAllow["provider-unavailable"],
		"all_frozen_non_allow_states_fail_closed":        allClosed && len(nonAllow) == 8,
		"exact_current_allow_is_sole_redirect_authority": allowDecision.State == trust.DecisionAllow && allowProjection.Runtime.Decision == links.RiskAllow && allowResult.Status == 302 && strings.HasPrefix(allowResult.Location, "https://customer.example/allow"),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
