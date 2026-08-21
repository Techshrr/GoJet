package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type staticTXTResolver struct {
	records []string
}

func (r staticTXTResolver) LookupTXT(context.Context, string) ([]string, error) {
	return append([]string(nil), r.records...), nil
}

func main() {
	caseFlag := flag.String("case", "P06-T016", "P06 immediate safety-suspension case ID")
	flag.Parse()
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		failFatal("GOJET_MYSQL_DSN is required")
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
	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	if *caseFlag != "P06-T016" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT016(ctx, db, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT016(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t016-safety"
	store := domains.NewMySQLStore(db)
	now := time.Date(2026, 8, 21, 18, 0, 0, 0, time.UTC)
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t016",
		Status: domains.EntitlementActive,
		DomainLimit: 4,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T016 active entitlement fixture",
	}, "corr-p06-t016-plan"); err != nil {
		return err
	}

	type fixture struct {
		name     string
		category domains.DomainSecurityCategory
		created  domains.CreatedDomain
	}
	securityCases := []struct {
		name     string
		category domains.DomainSecurityCategory
	}{
		{"abuse", domains.DomainSecurityAbuse},
		{"fraud", domains.DomainSecurityFraud},
		{"security", domains.DomainSecurityGeneral},
	}
	fixtures := make([]fixture, 0, len(securityCases))
	for index, item := range securityCases {
		created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
			WorkspaceID: workspace,
			ActorID: "actor-t016",
			CorrelationID: fmt.Sprintf("corr-p06-t016-create-%s", item.name),
			Reason: "create ready domain before safety suspension",
			Hostname: fmt.Sprintf("%s-t016.example.com", item.name),
			Now: now.Add(time.Duration(index) * time.Second),
		})
		if err != nil {
			return err
		}
		if err := makeReady(ctx, db, workspace, created.Domain.ID, now); err != nil {
			return err
		}
		fixtures = append(fixtures, fixture{name: item.name, category: item.category, created: created})
	}

	entitlement, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if !entitlement.MutationAllowed || !entitlement.ExistingRoutingAllowed {
		return fmt.Errorf("active entitlement fixture not authoritative: %+v", entitlement)
	}

	mutationKinds := []domains.DomainMutationKind{
		domains.DomainMutationActivate,
		domains.DomainMutationRestore,
		domains.DomainMutationRotate,
		domains.DomainMutationAssignLink,
	}
	for _, item := range fixtures {
		before, err := store.GetDomain(ctx, workspace, item.created.Domain.ID)
		if err != nil {
			return err
		}
		if !before.Readiness(entitlement).ReadyForRouting || !before.Readiness(entitlement).ReadyForNewLinks {
			return fmt.Errorf("%s precondition not ready: %+v", item.name, before.Readiness(entitlement))
		}
		suspended, err := store.ApplyDomainSecuritySuspension(ctx, domains.DomainSecuritySuspensionInput{
			WorkspaceID: workspace,
			DomainID: item.created.Domain.ID,
			ActorID: "security-reviewer-t016",
			Category: item.category,
			Reason: "internal safety suspension evidence",
			CorrelationID: fmt.Sprintf("corr-p06-t016-suspend-%s", item.name),
			Now: now.Add(10 * time.Minute),
		})
		if err != nil {
			return err
		}
		if suspended.RoutingState != domains.RoutingSuspended || suspended.SecurityCategory != string(item.category) || suspended.GraceStartedAt != nil || suspended.GraceUntil != nil {
			return fmt.Errorf("%s suspension state incorrect: %+v", item.name, suspended)
		}
		if suspended.OwnershipStatus != domains.OwnershipVerified || suspended.IngressDNSStatus != domains.IngressValid || suspended.HTTPSStatus != domains.HTTPSActive || suspended.RiskStatus != domains.RiskAllow {
			return fmt.Errorf("%s safety event collapsed independent axes: %+v", item.name, suspended)
		}
		readiness := suspended.Readiness(entitlement)
		if readiness.ReadyForRouting || readiness.ReadyForNewLinks {
			return fmt.Errorf("%s suspension did not fail closed: %+v", item.name, readiness)
		}
		for _, kind := range mutationKinds {
			decision, checkErr := store.CheckDomainMutationAuthority(ctx, workspace, suspended.ID, kind, now.Add(11*time.Minute))
			if !errors.Is(checkErr, domains.ErrDomainSecuritySuspended) || decision.Allowed || decision.Code != "security_suspended" {
				return fmt.Errorf("%s mutation %s bypassed suspension: decision=%+v err=%v", item.name, kind, decision, checkErr)
			}
		}
		if _, err := store.ApplyDomainSecuritySuspension(ctx, domains.DomainSecuritySuspensionInput{
			WorkspaceID: workspace,
			DomainID: suspended.ID,
			ActorID: "security-reviewer-t016",
			Category: item.category,
			Reason: "idempotent replay",
			CorrelationID: fmt.Sprintf("corr-p06-t016-suspend-%s-replay", item.name),
			Now: now.Add(12 * time.Minute),
		}); err != nil {
			return fmt.Errorf("%s suspension replay: %w", item.name, err)
		}
		var auditCount int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_domain_audit_events WHERE workspace_id=? AND domain_id=? AND action='domain.security.suspend'`, workspace, suspended.ID).Scan(&auditCount); err != nil {
			return err
		}
		if auditCount != 1 {
			return fmt.Errorf("%s suspension audit count=%d want=1", item.name, auditCount)
		}
	}

	ownershipCreated, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t016",
		CorrelationID: "corr-p06-t016-create-ownership",
		Reason: "create ready domain before ownership loss",
		Hostname: "ownership-loss-t016.example.com",
		Now: now.Add(4 * time.Second),
	})
	if err != nil {
		return err
	}
	if err := makeReady(ctx, db, workspace, ownershipCreated.Domain.ID, now); err != nil {
		return err
	}
	lost, err := store.ApplyOwnershipLoss(ctx, domains.OwnershipLossInput{
		WorkspaceID: workspace,
		DomainID: ownershipCreated.Domain.ID,
		ActorID: "security-reviewer-t016",
		Reason: "authoritative ownership control lost",
		CorrelationID: "corr-p06-t016-ownership-loss",
		Now: now.Add(20 * time.Minute),
	})
	if err != nil {
		return err
	}
	if lost.OwnershipStatus != domains.OwnershipLost || lost.RoutingState != domains.RoutingSuspended || lost.SecurityCategory != string(domains.DomainSecurityOwnershipLoss) || lost.GraceStartedAt != nil || lost.GraceUntil != nil {
		return fmt.Errorf("ownership loss did not immediately suspend: %+v", lost)
	}
	if lost.IngressDNSStatus != domains.IngressValid || lost.HTTPSStatus != domains.HTTPSActive || lost.RiskStatus != domains.RiskAllow {
		return fmt.Errorf("ownership loss collapsed unrelated axes: %+v", lost)
	}
	for _, kind := range mutationKinds {
		decision, checkErr := store.CheckDomainMutationAuthority(ctx, workspace, lost.ID, kind, now.Add(21*time.Minute))
		if !errors.Is(checkErr, domains.ErrDomainSecuritySuspended) || decision.Allowed || decision.Code != "security_suspended" {
			return fmt.Errorf("ownership-loss mutation %s bypassed suspension: decision=%+v err=%v", kind, decision, checkErr)
		}
	}

	// A later valid TXT proof may restore only the Ownership axis. It must not
	// clear the persisted safety suspension or become a self-reactivation path.
	verifier := domains.NewOwnershipVerifier(store, staticTXTResolver{records: []string{ownershipCreated.OwnershipTXTValue}})
	reverified, err := verifier.VerifyTXT(ctx, domains.VerifyOwnershipTXTInput{
		WorkspaceID: workspace,
		DomainID: lost.ID,
		ActorID: "actor-t016",
		CorrelationID: "corr-p06-t016-ownership-reverify",
		Reason: "reverify ownership after safety suspension",
		Now: now.Add(22 * time.Minute),
	})
	if err != nil {
		return err
	}
	if reverified.Domain.OwnershipStatus != domains.OwnershipVerified || reverified.Domain.RoutingState != domains.RoutingSuspended || reverified.Domain.SecurityCategory != string(domains.DomainSecurityOwnershipLoss) {
		return fmt.Errorf("ownership reverify cleared safety state: %+v", reverified.Domain)
	}
	postRecoveryDecision, postRecoveryErr := store.CheckDomainMutationAuthority(ctx, workspace, lost.ID, domains.DomainMutationRestore, now.Add(23*time.Minute))
	if !errors.Is(postRecoveryErr, domains.ErrDomainSecuritySuspended) || postRecoveryDecision.Allowed {
		return fmt.Errorf("ownership proof recovery self-reactivated domain: decision=%+v err=%v", postRecoveryDecision, postRecoveryErr)
	}

	// Workspace security authority also has zero billing grace and overrides the
	// otherwise-active plan source.
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "security-overlay-t016",
		Status: domains.EntitlementSuspended,
		DomainLimit: 4,
		StartsAt: now.Add(24 * time.Minute),
		DecisionReason: "workspace security suspension",
		SecurityCategory: "security",
	}, "corr-p06-t016-workspace-security"); err != nil {
		return err
	}
	securityEntitlement, err := store.ResolveEntitlement(ctx, workspace, now.Add(25*time.Minute))
	if err != nil {
		return err
	}
	if securityEntitlement.Status != domains.EntitlementSuspended || securityEntitlement.MutationAllowed || securityEntitlement.ExistingRoutingAllowed || securityEntitlement.GracePeriod || securityEntitlement.GraceUntil != nil {
		return fmt.Errorf("workspace security suspension received grace: %+v", securityEntitlement)
	}

	var ownershipLossAudits int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_domain_audit_events WHERE workspace_id=? AND domain_id=? AND action='domain.ownership.loss'`, workspace, lost.ID).Scan(&ownershipLossAudits); err != nil {
		return err
	}
	if ownershipLossAudits != 1 {
		return fmt.Errorf("ownership loss audit count=%d want=1", ownershipLossAudits)
	}

	out.Details = map[string]any{
		"security_categories": []string{"abuse", "fraud", "security"},
		"security_domains_suspended": len(fixtures),
		"security_grace_applied": false,
		"security_mutations_denied": true,
		"security_axes_preserved": true,
		"ownership_loss_status": lost.OwnershipStatus,
		"ownership_loss_routing_state": lost.RoutingState,
		"ownership_loss_category": lost.SecurityCategory,
		"ownership_loss_grace_applied": false,
		"ownership_loss_mutations_denied": true,
		"ownership_reverify_status": reverified.Domain.OwnershipStatus,
		"ownership_reverify_kept_suspended": reverified.Domain.RoutingState == domains.RoutingSuspended,
		"ownership_reverify_self_reactivated": false,
		"workspace_security_status": securityEntitlement.Status,
		"workspace_security_grace_applied": securityEntitlement.GracePeriod,
		"ownership_loss_audit_events": ownershipLossAudits,
	}
	return nil
}

func makeReady(ctx context.Context, db *sql.DB, workspace string, domainID uint64, now time.Time) error {
	_, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET routing_state='enabled', ownership_status='verified', ingress_dns_status='valid',
		    https_status='active', risk_status='allow', ownership_verified_at=?,
		    ingress_dns_checked_at=?, https_checked_at=?, risk_checked_at=?,
		    risk_policy_version='t016-ready-fixture', risk_evidence_ref='risk:t016:ready',
		    security_category=NULL, grace_started_at=NULL, grace_until=NULL
		WHERE workspace_id=? AND id=?`, now, now, now, now, workspace, domainID)
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
