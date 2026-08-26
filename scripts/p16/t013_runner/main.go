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

type authorityFixture struct {
	Link       runtimefixture.LinkFixture
	Base       trust.DestinationDecision
	Override   trust.DestinationOverride
	Authorizer *runtimefixture.PermissionFixture
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T013"
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
		Case:         "P16-T013",
		Status:       "FAIL",
		Fixture:      "real durable override invalidation over fingerprint mutation, policy change, expiry, newer policy decision and explicit revocation with Redis/native redirect enforcement",
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

	store := trust.NewStore(db)
	runtime := links.NewRedisRiskStore(redisClient)
	workspace := "p16-t013-workspace"
	policyV1 := "p16-runtime-policy-v1"
	now := time.Now().UTC().Truncate(time.Microsecond)

	makeFixture := func(name string, expiresAt time.Time) (authorityFixture, error) {
		link, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t013-"+name, "https://customer.example/t013-"+name, nil, nil)
		if err != nil {
			return authorityFixture{}, err
		}
		base, err := runtimefixture.InsertRawDecision(ctx, db, store, link, policyV1, "t013-"+name+"-base", trust.DecisionReview, "review-required", nil)
		if err != nil {
			return authorityFixture{}, err
		}
		authorizer := &runtimefixture.PermissionFixture{Allow: true}
		override, err := store.CreateDestinationOverride(ctx, trust.CreateDestinationOverrideInput{
			WorkspaceID: workspace, LinkID: link.ID, RiskFingerprint: link.Fingerprint, PolicyVersion: policyV1,
			Decision: trust.DecisionAllow, Reason: "bounded security review override for invalidation evidence",
			PolicyContext: trust.DestinationOverridePolicyContext{
				AuthorityVersion: trust.OverrideAuthorityVersion, PolicyVersion: policyV1,
				BaseDecisionID: base.ID, BaseDecisionState: base.State,
			},
			ActorID: "p16-security-reviewer", CorrelationID: "p16-t013-" + name + "-override", ExpiresAt: expiresAt,
		}, authorizer, now)
		if err != nil {
			return authorityFixture{}, err
		}
		if _, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, link.ID, policyV1, now, 10*time.Minute); err != nil {
			return authorityFixture{}, err
		}
		before, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
		if err != nil || before.Status != 302 || before.Location == "" {
			if err != nil {
				return authorityFixture{}, err
			}
			return authorityFixture{}, fmt.Errorf("fixture %s did not establish override allow", name)
		}
		return authorityFixture{Link: link, Base: base, Override: override, Authorizer: authorizer}, nil
	}

	fingerprintFixture, err := makeFixture("fingerprint", now.Add(10*time.Minute))
	if err != nil {
		return out, err
	}
	newPrimary := "https://customer.example/t013-fingerprint-mutated"
	newFingerprint, _, err := links.RiskFingerprint(newPrimary, nil, nil)
	if err != nil {
		return out, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE links SET primary_destination=?,risk_fingerprint=?,version=version+1,updated_at=NOW(6) WHERE id=?`, newPrimary, newFingerprint, fingerprintFixture.Link.ID); err != nil {
		return out, err
	}
	_, fingerprintAuthorityErr := store.CurrentDestinationAuthority(ctx, workspace, fingerprintFixture.Link.ID, policyV1, now.Add(time.Second))
	oldFingerprintKeyExists, err := redisClient.Exists(ctx, links.RiskDecisionKey(fingerprintFixture.Link.ID, fingerprintFixture.Link.Fingerprint)).Result()
	if err != nil {
		return out, err
	}
	fingerprintRedirect, err := runtimefixture.RequestRedirect(ctx, fingerprintFixture.Link.Hostname, fingerprintFixture.Link.Code)
	if err != nil {
		return out, err
	}

	policyFixture, err := makeFixture("policy", now.Add(10*time.Minute))
	if err != nil {
		return out, err
	}
	policyV2 := "p16-runtime-policy-v2"
	newPolicyDecision, err := runtimefixture.InsertRawDecision(ctx, db, store, policyFixture.Link, policyV2, "t013-policy-v2", trust.DecisionReview, "policy-v2-review", nil)
	if err != nil {
		return out, err
	}
	policyAuthority, err := store.CurrentDestinationAuthority(ctx, workspace, policyFixture.Link.ID, policyV2, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	policyProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, policyFixture.Link.ID, policyV2, now.Add(time.Second), 10*time.Minute)
	if err != nil {
		return out, err
	}
	policyRedirect, err := runtimefixture.RequestRedirect(ctx, policyFixture.Link.Hostname, policyFixture.Link.Code)
	if err != nil {
		return out, err
	}

	expiryFixture, err := makeFixture("expiry", now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	future := now.Add(3 * time.Second)
	expiryAuthority, err := store.CurrentDestinationAuthority(ctx, workspace, expiryFixture.Link.ID, policyV1, future)
	if err != nil {
		return out, err
	}
	expiryProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, expiryFixture.Link.ID, policyV1, future, 10*time.Minute)
	if err != nil {
		return out, err
	}
	expiryRedirect, err := runtimefixture.RequestRedirect(ctx, expiryFixture.Link.Hostname, expiryFixture.Link.Code)
	if err != nil {
		return out, err
	}

	newDecisionFixture, err := makeFixture("new-decision", now.Add(10*time.Minute))
	if err != nil {
		return out, err
	}
	newerDecision, err := runtimefixture.InsertRawDecision(ctx, db, store, newDecisionFixture.Link, policyV1, "t013-newer-decision", trust.DecisionBlock, "new-policy-block", nil)
	if err != nil {
		return out, err
	}
	newDecisionAuthority, err := store.CurrentDestinationAuthority(ctx, workspace, newDecisionFixture.Link.ID, policyV1, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	newDecisionProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, newDecisionFixture.Link.ID, policyV1, now.Add(time.Second), 10*time.Minute)
	if err != nil {
		return out, err
	}
	newDecisionRedirect, err := runtimefixture.RequestRedirect(ctx, newDecisionFixture.Link.Hostname, newDecisionFixture.Link.Code)
	if err != nil {
		return out, err
	}

	explicitFixture, err := makeFixture("explicit", now.Add(10*time.Minute))
	if err != nil {
		return out, err
	}
	invalidated, err := store.InvalidateDestinationOverride(ctx, trust.InvalidateDestinationOverrideInput{
		WorkspaceID: workspace, OverrideID: explicitFixture.Override.ID, ActorID: "p16-security-reviewer",
		Reason: "provider authority changed and requires fresh review", CorrelationID: "p16-t013-explicit-invalidate",
	}, explicitFixture.Authorizer, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	explicitAuthority, err := store.CurrentDestinationAuthority(ctx, workspace, explicitFixture.Link.ID, policyV1, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	explicitProjection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, explicitFixture.Link.ID, policyV1, now.Add(2*time.Second), 10*time.Minute)
	if err != nil {
		return out, err
	}
	explicitRedirect, err := runtimefixture.RequestRedirect(ctx, explicitFixture.Link.Hostname, explicitFixture.Link.Code)
	if err != nil {
		return out, err
	}
	invalidateAuditRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND link_id=? AND action='destination-risk.override-invalidate' AND result='success' AND correlation_id=?`, workspace, explicitFixture.Link.ID, "p16-t013-explicit-invalidate")
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"invalidation_classes":        5,
		"explicit_invalidation_audits": invalidateAuditRows,
	}
	out.Checks = map[string]bool{
		"fingerprint_change_invalidates_override_even_if_old_redis_key_remains": errors.Is(fingerprintAuthorityErr, trust.ErrNotFound) && oldFingerprintKeyExists == 1 && newFingerprint != fingerprintFixture.Link.Fingerprint && closed(fingerprintRedirect),
		"incompatible_policy_version_does_not_reuse_override": newPolicyDecision.ID > 0 && policyAuthority.Source == "policy" && policyAuthority.Override == nil && policyAuthority.State == trust.DecisionReview && policyProjection.Runtime.Decision == links.RiskReview && closed(policyRedirect),
		"override_expiry_falls_back_to_current_policy_authority": expiryAuthority.Source == "policy" && expiryAuthority.Override == nil && expiryAuthority.State == trust.DecisionReview && expiryProjection.Runtime.Decision == links.RiskReview && closed(expiryRedirect),
		"newer_same_policy_decision_invalidates_old_base_binding": newerDecision.ID != newDecisionFixture.Base.ID && newDecisionAuthority.Source == "policy" && newDecisionAuthority.Override == nil && newDecisionAuthority.State == trust.DecisionBlock && newDecisionProjection.Runtime.Decision == links.RiskBlock && closed(newDecisionRedirect),
		"explicit_invalidation_is_durable_audited_and_non_allow": invalidated.InvalidatedAt != nil && invalidated.InvalidationReason != "" && explicitAuthority.Source == "policy" && explicitAuthority.Override == nil && explicitProjection.Runtime.Decision == links.RiskReview && invalidateAuditRows == 1 && closed(explicitRedirect),
		"all_five_invalidation_classes_remove_customer_location": closed(fingerprintRedirect) && closed(policyRedirect) && closed(expiryRedirect) && closed(newDecisionRedirect) && closed(explicitRedirect),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func closed(result runtimefixture.HTTPResult) bool {
	if result.Location != "" {
		return false
	}
	return result.Status != 301 && result.Status != 302 && result.Status != 307 && result.Status != 308 && !strings.Contains(result.Body, "https://customer.example/")
}
