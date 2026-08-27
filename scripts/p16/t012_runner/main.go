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
		out.Case = "P16-T012"
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
		Case:         "P16-T012",
		Status:       "FAIL",
		Fixture:      "real MySQL durable override authority, permission consumer, immutable audit, Redis projection and native redirectengine",
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

	store := trust.NewStore(db)
	runtime := links.NewRedisRiskStore(redisClient)
	workspace := "p16-t012-workspace"
	policyVersion := "p16-runtime-policy-v1"
	link, err := runtimefixture.CreateLink(ctx, db, workspace, "go.example.test", "official", "t012-override", "https://customer.example/t012", nil, nil)
	if err != nil {
		return out, err
	}
	base, err := runtimefixture.InsertRawDecision(ctx, db, store, link, policyVersion, "t012-base-review", trust.DecisionReview, "manual-review-required", nil)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	policyContext := trust.DestinationOverridePolicyContext{
		AuthorityVersion:  trust.OverrideAuthorityVersion,
		PolicyVersion:     policyVersion,
		BaseDecisionID:    base.ID,
		BaseDecisionState: base.State,
	}
	valid := trust.CreateDestinationOverrideInput{
		WorkspaceID:     workspace,
		LinkID:          link.ID,
		RiskFingerprint: link.Fingerprint,
		PolicyVersion:   policyVersion,
		Decision:        trust.DecisionAllow,
		Reason:          "security reviewer verified destination ownership and current evidence",
		PolicyContext:   policyContext,
		ActorID:         "p16-security-reviewer",
		CorrelationID:   "p16-t012-valid-override",
		ExpiresAt:       now.Add(5 * time.Minute),
	}

	denyAuthorizer := &runtimefixture.PermissionFixture{Allow: false}
	_, unauthorizedErr := store.CreateDestinationOverride(ctx, valid, denyAuthorizer, now)

	allowAuthorizer := &runtimefixture.PermissionFixture{Allow: true}
	wrongFingerprint := valid
	wrongFingerprint.CorrelationID = "p16-t012-wrong-fingerprint"
	wrongFingerprint.RiskFingerprint = strings.Repeat("a", 64)
	if wrongFingerprint.RiskFingerprint == link.Fingerprint {
		wrongFingerprint.RiskFingerprint = strings.Repeat("b", 64)
	}
	_, wrongFingerprintErr := store.CreateDestinationOverride(ctx, wrongFingerprint, allowAuthorizer, now)

	wrongPolicyContext := valid
	wrongPolicyContext.CorrelationID = "p16-t012-wrong-context"
	wrongPolicyContext.PolicyContext.BaseDecisionID = base.ID + 1
	_, wrongContextErr := store.CreateDestinationOverride(ctx, wrongPolicyContext, allowAuthorizer, now)

	missingReason := valid
	missingReason.CorrelationID = "p16-t012-missing-reason"
	missingReason.Reason = "   "
	_, missingReasonErr := store.CreateDestinationOverride(ctx, missingReason, allowAuthorizer, now)

	unbounded := valid
	unbounded.CorrelationID = "p16-t012-unbounded"
	unbounded.ExpiresAt = now.Add(24*time.Hour + time.Microsecond)
	_, unboundedErr := store.CreateDestinationOverride(ctx, unbounded, allowAuthorizer, now)

	override, err := store.CreateDestinationOverride(ctx, valid, allowAuthorizer, now)
	if err != nil {
		return out, err
	}
	replayed, err := store.CreateDestinationOverride(ctx, valid, allowAuthorizer, now)
	if err != nil {
		return out, err
	}
	authority, err := store.CurrentDestinationAuthority(ctx, workspace, link.ID, policyVersion, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	projection, err := trust.ProjectCurrentDestinationDecision(ctx, store, runtime, workspace, link.ID, policyVersion, now.Add(time.Second), 10*time.Minute)
	if err != nil {
		return out, err
	}
	redirect, err := runtimefixture.RequestRedirect(ctx, link.Hostname, link.Code)
	if err != nil {
		return out, err
	}

	overrideRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_overrides WHERE workspace_id=? AND link_id=?`, workspace, link.ID)
	if err != nil {
		return out, err
	}
	auditRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND link_id=? AND action='destination-risk.override-create' AND result='success' AND correlation_id=?`, workspace, link.ID, valid.CorrelationID)
	if err != nil {
		return out, err
	}
	auditBoundRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND link_id=? AND action='destination-risk.override-create' AND actor_id=? AND reason=? AND correlation_id=? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.risk_fingerprint'))=? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.policy_version'))=?`, workspace, link.ID, valid.ActorID, valid.Reason, valid.CorrelationID, link.Fingerprint, policyVersion)
	if err != nil {
		return out, err
	}

	permissionExact := len(allowAuthorizer.Permissions) > 0
	for _, permission := range allowAuthorizer.Permissions {
		permissionExact = permissionExact && permission == trust.SecurityManagePermission
	}

	out.RecordCounts = map[string]int{
		"durable_overrides":          overrideRows,
		"successful_override_audits": auditRows,
		"permission_checks":          len(allowAuthorizer.Permissions) + len(denyAuthorizer.Permissions),
	}
	out.Checks = map[string]bool{
		"security_manage_is_required":                                       errors.Is(unauthorizedErr, trust.ErrUnauthorized) && len(denyAuthorizer.Permissions) == 1 && denyAuthorizer.Permissions[0] == trust.SecurityManagePermission,
		"crafted_fingerprint_fails_closed":                                  errors.Is(wrongFingerprintErr, trust.ErrStaleFingerprint),
		"crafted_policy_context_fails_closed":                               errors.Is(wrongContextErr, trust.ErrConflict),
		"non_empty_reason_is_required":                                      errors.Is(missingReasonErr, trust.ErrInvalid),
		"override_validity_is_bounded":                                      errors.Is(unboundedErr, trust.ErrInvalid),
		"permission_consumer_uses_only_security_manage":                     permissionExact,
		"valid_override_is_exact_authority_bound":                           override.ID > 0 && override.RiskFingerprint == link.Fingerprint && override.PolicyVersion == policyVersion && override.BaseDecisionID == base.ID && override.ActorID == valid.ActorID && override.CorrelationID == valid.CorrelationID,
		"duplicate_same_correlation_is_idempotent":                          replayed.ID == override.ID && overrideRows == 1 && auditRows == 1,
		"immutable_audit_binds_actor_reason_correlation_fingerprint_policy": auditBoundRows == 1,
		"effective_authority_is_manual_override":                            authority.Source == "manual-override" && authority.Override != nil && authority.Override.ID == override.ID && authority.State == trust.DecisionAllow,
		"override_projects_through_existing_exact_redis_authority":          projection.Runtime.Decision == links.RiskAllow && projection.Runtime.Fingerprint == link.Fingerprint && projection.Runtime.PolicyVersion == policyVersion,
		"valid_override_can_reach_customer_only_through_native_redirect":    redirect.Status == 302 && strings.HasPrefix(redirect.Location, "https://customer.example/t012"),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}
