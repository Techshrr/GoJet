package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
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

func main() {
	caseFlag := flag.String("case", "all", "P06 case ID or all")
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

	caseIDs := []string{"P06-T001", "P06-T002", "P06-T003", "P06-T004", "P06-T005", "P06-T006", "P06-T007", "P06-T008"}
	if *caseFlag != "all" {
		caseIDs = []string{*caseFlag}
	}

	results := map[string]caseResult{}
	for _, caseID := range caseIDs {
		result := runCase(ctx, db, caseID)
		results[caseID] = result
		if result.Status != "PASS" {
			writeJSON(results)
			os.Exit(1)
		}
	}
	writeJSON(results)
}

func runCase(ctx context.Context, db *sql.DB, caseID string) caseResult {
	result := caseResult{CaseID: caseID, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	var err error
	switch caseID {
	case "P06-T001":
		err = caseT001(ctx, db, &result)
	case "P06-T002":
		err = caseT002(ctx, db, &result)
	case "P06-T003":
		err = caseT003(ctx, db, &result)
	case "P06-T004":
		err = caseT004(ctx, db, &result)
	case "P06-T005":
		err = caseT005(ctx, db, &result)
	case "P06-T006":
		err = caseT006(ctx, db, &result)
	case "P06-T007":
		err = caseT007(ctx, db, &result)
	case "P06-T008":
		err = caseT008(ctx, db, &result)
	default:
		err = fmt.Errorf("unsupported case %s", caseID)
	}
	if err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	return result
}

func caseT001(ctx context.Context, db *sql.DB, out *caseResult) error {
	requiredTables := []string{
		"custom_domain_entitlement_requests",
		"custom_domain_entitlement_sources",
		"custom_domain_usage",
		"custom_domains",
		"custom_domain_revalidations",
		"custom_domain_audit_events",
	}
	rows, err := db.QueryContext(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name LIKE 'custom_domain%'
		ORDER BY table_name`)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		found = append(found, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, required := range requiredTables {
		if !contains(found, required) {
			return fmt.Errorf("missing table %s; found=%v", required, found)
		}
	}

	axisColumns := []string{"routing_state", "ownership_status", "ingress_dns_status", "https_status", "risk_status"}
	columnRows, err := db.QueryContext(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = 'custom_domains'
		ORDER BY ordinal_position`)
	if err != nil {
		return err
	}
	defer columnRows.Close()
	columns := []string{}
	for columnRows.Next() {
		var name string
		if err := columnRows.Scan(&name); err != nil {
			return err
		}
		columns = append(columns, name)
	}
	for _, axis := range axisColumns {
		if !contains(columns, axis) {
			return fmt.Errorf("missing independent axis column %s", axis)
		}
	}
	if contains(columns, "is_verified") {
		return errors.New("collapsed is_verified authority column exists")
	}

	var hostnameUnique int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name='custom_domains'
		AND index_name='uq_custom_domains_hostname' AND non_unique=0`).Scan(&hostnameUnique); err != nil {
		return err
	}
	if hostnameUnique != 1 {
		return fmt.Errorf("global hostname uniqueness missing: count=%d", hostnameUnique)
	}
	out.Details = map[string]any{
		"tables": found,
		"independent_axes": axisColumns,
		"collapsed_verified_boolean": false,
		"global_hostname_unique_constraint": true,
	}
	return nil
}

func caseT002(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t002-requested"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	_, err := store.ProjectAccessRequest(ctx, domains.AccessRequestInput{
		WorkspaceID: workspace, SupportTicketID: "ticket-p06-t002", SubmittedAt: now.Add(-time.Hour), CorrelationID: "corr-p06-t002-request",
	})
	if err != nil {
		return err
	}
	resolved, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if resolved.Source != domains.SourceNone || resolved.Status != domains.EntitlementRequested || resolved.MutationAllowed || resolved.DomainLimit != 0 {
		return fmt.Errorf("support request incorrectly became authority: %+v", resolved)
	}
	_, err = store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace, ActorID: "actor-t002", CorrelationID: "corr-p06-t002-create", Reason: "crafted create while requested", Hostname: "requested-t002.example.com", Now: now,
	})
	if !errors.Is(err, domains.ErrEntitlementRequired) {
		return fmt.Errorf("requested create error=%v, want entitlement required", err)
	}
	count, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id = ?", workspace)
	if err != nil {
		return err
	}
	usage, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domain_usage WHERE workspace_id = ?", workspace)
	if err != nil {
		return err
	}
	if count != 0 || usage != 0 {
		return fmt.Errorf("denied request created persistent domain/usage state: domains=%d usage_rows=%d", count, usage)
	}
	out.Details = map[string]any{
		"source": resolved.Source, "status": resolved.Status, "domain_limit": resolved.DomainLimit,
		"mutation_allowed": resolved.MutationAllowed, "domain_rows_after_denial": count, "usage_rows_after_denial": usage,
		"support_ticket_is_authority": false,
	}
	return nil
}

func caseT003(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t003-business"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	source, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace, SourceKey: "subscription-business-t003", Status: domains.EntitlementActive,
		DomainLimit: 3, StartsAt: now.Add(-24 * time.Hour), DecisionReason: "active business plan fixture",
	}, "corr-p06-t003-plan")
	if err != nil {
		return err
	}
	resolved, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if source.Source != domains.SourcePlan || resolved.Source != domains.SourcePlan || resolved.Status != domains.EntitlementActive || resolved.DomainLimit != 3 || !resolved.MutationAllowed {
		return fmt.Errorf("business plan did not resolve authority: source=%+v resolved=%+v", source, resolved)
	}
	out.Details = map[string]any{"source": resolved.Source, "status": resolved.Status, "domain_limit": resolved.DomainLimit, "mutation_allowed": resolved.MutationAllowed}
	return nil
}

func caseT004(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t004-manual"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	expires := now.Add(30 * 24 * time.Hour)
	approval, err := store.CreateManualApproval(ctx, domains.ManualApprovalInput{
		WorkspaceID: workspace, SourceKey: "approval-t004", DomainLimit: 4, StartsAt: now.Add(-time.Hour), ExpiresAt: expires,
		GrantedBy: "admin-entitlements-t004", SupportTicketID: "ticket-p06-t004", DecisionReason: "independent manual approval", CorrelationID: "corr-p06-t004-approval",
	})
	if err != nil {
		return err
	}
	resolved, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if approval.Source != domains.SourceManualApproval || resolved.Source != domains.SourceManualApproval || resolved.DomainLimit != 4 || !resolved.MutationAllowed {
		return fmt.Errorf("manual approval did not resolve: %+v", resolved)
	}

	_, malformedErr := db.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources (
			workspace_id, source, source_key, status, domain_limit, starts_at, expires_at
		) VALUES (?, 'manual_approval', 'malformed-t004', 'active', 1, ?, ?)`, workspace, now, expires)
	if malformedErr == nil {
		return errors.New("database accepted manual approval without grant/ticket/reason")
	}
	out.Details = map[string]any{
		"source": resolved.Source, "domain_limit": resolved.DomainLimit, "support_ticket_id": resolved.SupportTicketID,
		"granted_by": resolved.GrantedBy, "bounded_expiry": resolved.ExpiresAt != nil, "db_constraint_rejected_malformed_approval": true,
	}
	return nil
}

func caseT005(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t005-coexist"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	_, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace, SourceKey: "subscription-business-t005", Status: domains.EntitlementActive,
		DomainLimit: 3, StartsAt: now.Add(-24 * time.Hour), DecisionReason: "active plan",
	}, "corr-p06-t005-plan")
	if err != nil {
		return err
	}
	_, err = store.CreateManualApproval(ctx, domains.ManualApprovalInput{
		WorkspaceID: workspace, SourceKey: "approval-t005", DomainLimit: 7, StartsAt: now.Add(-time.Hour), ExpiresAt: now.Add(30 * 24 * time.Hour),
		GrantedBy: "admin-entitlements-t005", SupportTicketID: "ticket-p06-t005", DecisionReason: "manual expansion", CorrelationID: "corr-p06-t005-manual",
	})
	if err != nil {
		return err
	}
	resolved, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if resolved.Source != domains.SourceManualApproval || resolved.DomainLimit != 7 || len(resolved.ValidSources) != 2 {
		return fmt.Errorf("highest valid source not selected: %+v", resolved)
	}

	_, err = store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace, SourceKey: "security-overlay-t005", Status: domains.EntitlementSuspended,
		DomainLimit: 1, StartsAt: now.Add(-time.Minute), DecisionReason: "security hold", SecurityCategory: "security",
	}, "corr-p06-t005-security")
	if err != nil {
		return err
	}
	suspended, err := store.ResolveEntitlement(ctx, workspace, now)
	if err != nil {
		return err
	}
	if suspended.Status != domains.EntitlementSuspended || suspended.MutationAllowed || suspended.ExistingRoutingAllowed {
		return fmt.Errorf("security state did not override valid entitlement sources: %+v", suspended)
	}
	out.Details = map[string]any{
		"coexisting_valid_sources": 2, "highest_valid_domain_limit": 7, "highest_valid_source": domains.SourceManualApproval,
		"security_override_status": suspended.Status, "security_override_mutation_allowed": suspended.MutationAllowed,
	}
	return nil
}

func caseT006(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t006-inactive"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	_, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace, ActorID: "crafted-client-t006", CorrelationID: "corr-p06-t006", Reason: "crafted bypass", Hostname: "inactive-t006.example.com", Now: now,
	})
	if !errors.Is(err, domains.ErrEntitlementRequired) {
		return fmt.Errorf("inactive create error=%v, want entitlement required", err)
	}
	domainsCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id = ?", workspace)
	if err != nil {
		return err
	}
	usageRows, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domain_usage WHERE workspace_id = ?", workspace)
	if err != nil {
		return err
	}
	secretRows, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id = ? AND ownership_secret_hash IS NOT NULL", workspace)
	if err != nil {
		return err
	}
	if domainsCount != 0 || usageRows != 0 || secretRows != 0 {
		return fmt.Errorf("inactive bypass persisted forbidden state: domains=%d usage=%d secret=%d", domainsCount, usageRows, secretRows)
	}
	denials, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domain_audit_events WHERE workspace_id = ? AND action='domain.create' AND result='denied'", workspace)
	if err != nil {
		return err
	}
	if denials != 1 {
		return fmt.Errorf("expected one denied audit event, got %d", denials)
	}
	out.Details = map[string]any{"domain_rows": 0, "usage_rows": 0, "secret_rows": 0, "denied_audit_events": denials}
	return nil
}

func caseT007(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t007-limit"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	_, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace, SourceKey: "subscription-business-t007", Status: domains.EntitlementActive,
		DomainLimit: 1, StartsAt: now.Add(-24 * time.Hour), DecisionReason: "one-slot concurrency fixture",
	}, "corr-p06-t007-plan")
	if err != nil {
		return err
	}

	type outcome struct {
		host string
		err  error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, 2)
	var wg sync.WaitGroup
	for index := 0; index < 2; index++ {
		index := index
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			host := fmt.Sprintf("limit-%d-t007.example.com", index+1)
			_, createErr := store.CreateDomain(ctx, domains.CreateDomainInput{
				WorkspaceID: workspace, ActorID: fmt.Sprintf("actor-t007-%d", index+1), CorrelationID: fmt.Sprintf("corr-p06-t007-%d", index+1),
				Reason: "concurrent allocation", Hostname: host, Now: now,
			})
			outcomes <- outcome{host: host, err: createErr}
		}()
	}
	close(start)
	wg.Wait()
	close(outcomes)

	successes := 0
	limitDenials := 0
	resultStrings := []string{}
	for outcome := range outcomes {
		switch {
		case outcome.err == nil:
			successes++
			resultStrings = append(resultStrings, outcome.host+":success")
		case errors.Is(outcome.err, domains.ErrDomainLimitReached):
			limitDenials++
			resultStrings = append(resultStrings, outcome.host+":domain_limit_reached")
		default:
			return fmt.Errorf("unexpected concurrent create error for %s: %v", outcome.host, outcome.err)
		}
	}
	sort.Strings(resultStrings)
	if successes != 1 || limitDenials != 1 {
		return fmt.Errorf("concurrency allocation mismatch success=%d denied=%d outcomes=%v", successes, limitDenials, resultStrings)
	}
	allocated, _, err := store.Usage(ctx, workspace)
	if err != nil {
		return err
	}
	domainCount, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id = ?", workspace)
	if err != nil {
		return err
	}
	if allocated != 1 || domainCount != 1 {
		return fmt.Errorf("domain limit over-allocated: allocated=%d domain_count=%d", allocated, domainCount)
	}
	out.Details = map[string]any{"domain_limit": 1, "successes": successes, "domain_limit_denials": limitDenials, "allocated_count": allocated, "domain_rows": domainCount, "outcomes": resultStrings}
	return nil
}

func caseT008(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspaceA := "p06-t008-owner-a"
	workspaceB := "p06-t008-owner-b"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	for _, fixture := range []struct {
		workspace string
		key       string
		corr      string
	}{
		{workspace: workspaceA, key: "subscription-business-t008-a", corr: "corr-p06-t008-plan-a"},
		{workspace: workspaceB, key: "subscription-business-t008-b", corr: "corr-p06-t008-plan-b"},
	} {
		if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
			WorkspaceID: fixture.workspace, SourceKey: fixture.key, Status: domains.EntitlementActive,
			DomainLimit: 2, StartsAt: now.Add(-24 * time.Hour), DecisionReason: "T008 active entitlement fixture",
		}, fixture.corr); err != nil {
			return err
		}
	}

	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspaceA, ActorID: "actor-t008-a", CorrelationID: "corr-p06-t008-create-a",
		Reason: "claim canonical IDNA hostname", Hostname: "bücher.example.com", Now: now,
	})
	if err != nil {
		return err
	}
	if created.Domain.HostnameASCII != "xn--bcher-kva.example.com" || created.Domain.DisplayHostname != "bücher.example.com" {
		return fmt.Errorf("unexpected canonical hostname identity: ascii=%q display=%q", created.Domain.HostnameASCII, created.Domain.DisplayHostname)
	}

	_, err = store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspaceB, ActorID: "actor-t008-b", CorrelationID: "corr-p06-t008-conflict",
		Reason: "attempt IDNA alias conflict", Hostname: "XN--BCHER-KVA.EXAMPLE.COM.", Now: now,
	})
	if !errors.Is(err, domains.ErrHostnameConflict) {
		return fmt.Errorf("cross-workspace IDNA alias create error=%v, want hostname conflict", err)
	}
	workspaceBDomains, err := scalarInt(ctx, db, "SELECT COUNT(*) FROM custom_domains WHERE workspace_id = ?", workspaceB)
	if err != nil {
		return err
	}
	allocatedB, _, err := store.Usage(ctx, workspaceB)
	if err != nil {
		return err
	}
	if workspaceBDomains != 0 || allocatedB != 0 {
		return fmt.Errorf("conflict mutated losing workspace allocation: domains=%d allocated=%d", workspaceBDomains, allocatedB)
	}

	var conflictReason, conflictMetadata string
	if err := db.QueryRowContext(ctx, `
		SELECT COALESCE(reason, ''), CAST(metadata_json AS CHAR)
		FROM custom_domain_audit_events
		WHERE workspace_id = ? AND correlation_id = ? AND action = 'domain.create' AND result = 'conflict'
		ORDER BY id DESC LIMIT 1`, workspaceB, "corr-p06-t008-conflict").Scan(&conflictReason, &conflictMetadata); err != nil {
		return err
	}
	var conflictPayload map[string]any
	if err := json.Unmarshal([]byte(conflictMetadata), &conflictPayload); err != nil {
		return fmt.Errorf("parse conflict metadata: %w", err)
	}
	if conflictReason != "hostname unavailable" || len(conflictPayload) != 1 || conflictPayload["code"] != "hostname_unavailable" {
		return fmt.Errorf("conflict response/audit is not allowlisted and generic: reason=%q metadata=%v", conflictReason, conflictPayload)
	}
	if strings.Contains(conflictMetadata, workspaceA) || strings.Contains(strings.ToLower(conflictMetadata), "provider") || strings.Contains(conflictMetadata, created.Domain.HostnameASCII) {
		return fmt.Errorf("conflict audit leaked tenant/provider/hostname evidence: %s", conflictMetadata)
	}

	_, err = store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspaceB, ActorID: "actor-t008-b", CorrelationID: "corr-p06-t008-platform",
		Reason: "attempt platform-host claim", Hostname: "assets.gojet.cc", Now: now,
	})
	if !errors.Is(err, domains.ErrInvalidHostname) {
		return fmt.Errorf("platform hostname create error=%v, want invalid hostname", err)
	}
	platformDenials, err := scalarInt(ctx, db, `
		SELECT COUNT(*) FROM custom_domain_audit_events
		WHERE workspace_id = ? AND correlation_id = ? AND action='domain.create' AND result='denied'
		AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.code'))='invalid_hostname'`, workspaceB, "corr-p06-t008-platform")
	if err != nil {
		return err
	}
	if platformDenials != 1 {
		return fmt.Errorf("platform-host rejection audit count=%d, want 1", platformDenials)
	}

	out.Details = map[string]any{
		"unicode_input":                    "bücher.example.com",
		"canonical_ascii":                 created.Domain.HostnameASCII,
		"safe_display":                    created.Domain.DisplayHostname,
		"punycode_alias_conflict":          true,
		"losing_workspace_domain_rows":     workspaceBDomains,
		"losing_workspace_allocated_count": allocatedB,
		"conflict_code":                    conflictPayload["code"],
		"cross_tenant_details_disclosed":   false,
		"platform_descendant_rejected":     true,
		"platform_rejection_audited":       platformDenials == 1,
	}
	return nil
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
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
