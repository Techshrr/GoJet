package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/links"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

type apiErrorEnvelope struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func main() {
	caseFlag := flag.String("case", "P06-T018", "P06 custom-domain Link assignment case ID")
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
	if *caseFlag != "P06-T018" {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	} else if err := caseT018(ctx, db, &result); err != nil {
		result.Status = "FAIL"
		result.Errors = append(result.Errors, err.Error())
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT018(ctx context.Context, db *sql.DB, out *caseResult) error {
	now := time.Now().UTC().Truncate(time.Second)
	domainStore := domains.NewMySQLStore(db)
	linkStore := links.NewMySQLStoreWithCustomDomainAuthority(db, domainStore)
	api := links.NewAPI(linkStore, true).Handler()
	defaultAPI := links.NewAPI(links.NewMySQLStore(db), true).Handler()

	workspace := "p06-t018-links"
	otherWorkspace := "p06-t018-other"
	actor := "owner-t018"
	if _, err := domainStore.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "business-t018",
		Status: domains.EntitlementActive,
		DomainLimit: 8,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T018 active entitlement fixture",
	}, "corr-p06-t018-plan"); err != nil {
		return err
	}
	if _, err := domainStore.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: otherWorkspace,
		SourceKey: "business-t018-other",
		Status: domains.EntitlementActive,
		DomainLimit: 2,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T018 other Workspace fixture",
	}, "corr-p06-t018-other-plan"); err != nil {
		return err
	}

	created, err := domainStore.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t018-domain",
		Reason: "create domain for Link assignment authority",
		Hostname: "assign-t018.example.com",
		Now: now,
	})
	if err != nil {
		return err
	}
	domainID := created.Domain.ID
	if err := setDomainAxes(ctx, db, workspace, domainID, "verified", "valid", "active", "allow", "pending", ""); err != nil {
		return err
	}

	// Existing P05 store without the P06 authority must remain fail closed.
	defaultDenied := doJSON(defaultAPI, http.MethodPost, "/api/workspaces/"+workspace+"/links", workspace, actor, "owner", "corr-p06-t018-default", createBody("assign-t018.example.com", "custom", "default-denied", "https://destination.example/path"))
	if err := requireAPIError(defaultDenied, http.StatusConflict, "domain_unavailable"); err != nil {
		return fmt.Errorf("default store custom-domain denial: %w", err)
	}

	// Request-level authorization remains ahead of domain authority.
	viewerDenied := doJSON(api, http.MethodPost, "/api/workspaces/"+workspace+"/links", workspace, "viewer-t018", "viewer", "corr-p06-t018-viewer", createBody("assign-t018.example.com", "custom", "viewer-denied", "https://destination.example/path"))
	if err := requireAPIError(viewerDenied, http.StatusForbidden, "read_only"); err != nil {
		return fmt.Errorf("viewer crafted create: %w", err)
	}

	// The same hostname from another Workspace is indistinguishable from any
	// unavailable custom domain and must not disclose the owning Workspace.
	crossWorkspace := doJSON(api, http.MethodPost, "/api/workspaces/"+otherWorkspace+"/links", otherWorkspace, actor, "owner", "corr-p06-t018-cross", createBody("assign-t018.example.com", "custom", "cross-denied", "https://destination.example/path"))
	if err := requireAPIError(crossWorkspace, http.StatusConflict, "domain_unavailable"); err != nil {
		return fmt.Errorf("cross-Workspace assignment denial: %w", err)
	}
	if strings.Contains(strings.ToLower(crossWorkspace.Body.String()), strings.ToLower(workspace)) {
		return fmt.Errorf("cross-Workspace denial leaked owning Workspace")
	}

	baselineLinks, baselineVersions, baselineAudits, err := linkCounts(ctx, db, workspace)
	if err != nil {
		return err
	}

	axisCases := []struct {
		name      string
		ownership string
		ingress   string
		https     string
		risk      string
	}{
		{"ownership", "pending", "valid", "active", "allow"},
		{"ingress", "verified", "pending", "active", "allow"},
		{"https", "verified", "valid", "pending", "allow"},
		{"risk", "verified", "valid", "active", "missing"},
	}
	for index, tc := range axisCases {
		if err := setDomainAxes(ctx, db, workspace, domainID, tc.ownership, tc.ingress, tc.https, tc.risk, "pending", ""); err != nil {
			return err
		}
		response := doJSON(api, http.MethodPost, "/api/workspaces/"+workspace+"/links", workspace, actor, "owner", fmt.Sprintf("corr-p06-t018-axis-%d", index), createBody("ASSIGN-T018.EXAMPLE.COM.", "custom", "axis-"+tc.name, "https://destination.example/path"))
		if err := requireAPIError(response, http.StatusConflict, "domain_unavailable"); err != nil {
			return fmt.Errorf("%s axis assignment: %w", tc.name, err)
		}
		linksCount, versionsCount, auditsCount, err := linkCounts(ctx, db, workspace)
		if err != nil {
			return err
		}
		if linksCount != baselineLinks || versionsCount != baselineVersions || auditsCount != baselineAudits {
			return fmt.Errorf("%s denied create mutated Links persistence: links=%d/%d versions=%d/%d audits=%d/%d", tc.name, linksCount, baselineLinks, versionsCount, baselineVersions, auditsCount, baselineAudits)
		}
	}

	// Active entitlement is a distinct mandatory gate even when all trust axes
	// are ready. A separate Workspace avoids modifying the successful fixture.
	expiredWorkspace := "p06-t018-expired"
	expiredStarts := now.Add(-48 * time.Hour)
	expiredAt := now.Add(-time.Hour)
	if _, err := domainStore.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: expiredWorkspace,
		SourceKey: "business-t018-expired",
		Status: domains.EntitlementActive,
		DomainLimit: 2,
		StartsAt: expiredStarts,
		ExpiresAt: &expiredAt,
		DecisionReason: "T018 expired entitlement fixture",
	}, "corr-p06-t018-expired-plan"); err != nil {
		return err
	}
	// Create fixture while entitlement was still active at a historical instant.
	expiredCreated, err := domainStore.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: expiredWorkspace,
		ActorID: actor,
		CorrelationID: "corr-p06-t018-expired-domain",
		Reason: "historical domain before entitlement expiry",
		Hostname: "expired-assign-t018.example.com",
		Now: expiredAt.Add(-time.Minute),
	})
	if err != nil {
		return err
	}
	if err := setDomainAxes(ctx, db, expiredWorkspace, expiredCreated.Domain.ID, "verified", "valid", "active", "allow", "pending", ""); err != nil {
		return err
	}
	expiredDenied := doJSON(api, http.MethodPost, "/api/workspaces/"+expiredWorkspace+"/links", expiredWorkspace, actor, "owner", "corr-p06-t018-expired", createBody("expired-assign-t018.example.com", "custom", "expired-denied", "https://destination.example/path"))
	if err := requireAPIError(expiredDenied, http.StatusConflict, "domain_unavailable"); err != nil {
		return fmt.Errorf("expired entitlement assignment: %w", err)
	}
	var expiredLinkCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links WHERE workspace_id=?`, expiredWorkspace).Scan(&expiredLinkCount); err != nil {
		return err
	}
	if expiredLinkCount != 0 {
		return fmt.Errorf("expired entitlement created a Link")
	}

	// Fully ready custom domain succeeds and the stored hostname is the P06
	// canonical ASCII identity, not the raw crafted request spelling.
	if err := setDomainAxes(ctx, db, workspace, domainID, "verified", "valid", "active", "allow", "pending", ""); err != nil {
		return err
	}
	readyCreate := doJSON(api, http.MethodPost, "/api/workspaces/"+workspace+"/links", workspace, actor, "owner", "corr-p06-t018-ready-create", createBody("ASSIGN-T018.EXAMPLE.COM.", "custom", "ready-custom", "https://destination.example/path"))
	if readyCreate.Code != http.StatusCreated {
		return fmt.Errorf("ready custom create status=%d body=%s", readyCreate.Code, readyCreate.Body.String())
	}
	var readyPayload map[string]any
	if err := json.Unmarshal(readyCreate.Body.Bytes(), &readyPayload); err != nil {
		return err
	}
	if readyPayload["domain_kind"] != "custom" || readyPayload["hostname"] != "assign-t018.example.com" {
		return fmt.Errorf("ready custom create returned noncanonical identity: %v", readyPayload)
	}

	// Official Links remain unaffected by the custom-domain authority.
	officialCreate := doJSON(api, http.MethodPost, "/api/workspaces/"+workspace+"/links", workspace, actor, "owner", "corr-p06-t018-official", createBody("gojet.cc", "official", "official-t018", "https://official-destination.example/path"))
	if officialCreate.Code != http.StatusCreated {
		return fmt.Errorf("official create regressed: status=%d body=%s", officialCreate.Code, officialCreate.Body.String())
	}
	var officialPayload map[string]any
	if err := json.Unmarshal(officialCreate.Body.Bytes(), &officialPayload); err != nil {
		return err
	}
	officialID, ok := numberToUint64(officialPayload["id"])
	if !ok {
		return fmt.Errorf("official response missing id: %v", officialPayload)
	}
	officialVersion, ok := numberToUint64(officialPayload["version"])
	if !ok {
		return fmt.Errorf("official response missing version: %v", officialPayload)
	}

	beforeUpdate, err := linkStore.GetByID(ctx, workspace, officialID)
	if err != nil {
		return err
	}
	beforeHistory, err := linkStore.History(ctx, workspace, officialID)
	if err != nil {
		return err
	}
	if err := setDomainAxes(ctx, db, workspace, domainID, "verified", "valid", "active", "review", "pending", ""); err != nil {
		return err
	}
	deniedUpdate := doJSON(api, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/links/%d", workspace, officialID), workspace, actor, "owner", "corr-p06-t018-update-denied", updateBody(officialVersion, "assign-t018.example.com", "custom", "official-t018", "https://changed.example/path"))
	if err := requireAPIError(deniedUpdate, http.StatusConflict, "domain_unavailable"); err != nil {
		return fmt.Errorf("non-ready custom update: %w", err)
	}
	afterDeniedUpdate, err := linkStore.GetByID(ctx, workspace, officialID)
	if err != nil {
		return err
	}
	afterDeniedHistory, err := linkStore.History(ctx, workspace, officialID)
	if err != nil {
		return err
	}
	if afterDeniedUpdate.Version != beforeUpdate.Version || afterDeniedUpdate.Hostname != beforeUpdate.Hostname || afterDeniedUpdate.DomainKind != beforeUpdate.DomainKind || afterDeniedUpdate.PrimaryDestination != beforeUpdate.PrimaryDestination || afterDeniedUpdate.RiskFingerprint != beforeUpdate.RiskFingerprint || len(afterDeniedHistory) != len(beforeHistory) {
		return fmt.Errorf("denied update mutated Link: before=%+v after=%+v history=%d/%d", beforeUpdate, afterDeniedUpdate, len(beforeHistory), len(afterDeniedHistory))
	}

	if err := setDomainAxes(ctx, db, workspace, domainID, "verified", "valid", "active", "allow", "pending", ""); err != nil {
		return err
	}
	allowedUpdate := doJSON(api, http.MethodPatch, fmt.Sprintf("/api/workspaces/%s/links/%d", workspace, officialID), workspace, actor, "owner", "corr-p06-t018-update-allowed", updateBody(officialVersion, "ASSIGN-T018.EXAMPLE.COM.", "custom", "official-t018", "https://changed.example/path"))
	if allowedUpdate.Code != http.StatusOK {
		return fmt.Errorf("ready custom update status=%d body=%s", allowedUpdate.Code, allowedUpdate.Body.String())
	}
	updated, err := linkStore.GetByID(ctx, workspace, officialID)
	if err != nil {
		return err
	}
	if updated.Version != beforeUpdate.Version+1 || updated.DomainKind != "custom" || updated.Hostname != "assign-t018.example.com" || updated.PrimaryDestination == beforeUpdate.PrimaryDestination {
		return fmt.Errorf("allowed custom update did not commit expected mutation: before=%+v after=%+v", beforeUpdate, updated)
	}

	finalLinks, finalVersions, finalAudits, err := linkCounts(ctx, db, workspace)
	if err != nil {
		return err
	}
	out.Details = map[string]any{
		"default_store_fail_closed": true,
		"viewer_crafted_create_denied": true,
		"cross_workspace_generic_denial": true,
		"independent_axis_denials": []string{"ownership", "ingress_dns", "https", "risk"},
		"denied_create_zero_persistence_mutation": true,
		"expired_entitlement_denied": true,
		"ready_custom_create_succeeded": true,
		"ready_custom_hostname_canonicalized": readyPayload["hostname"],
		"official_link_path_unchanged": true,
		"denied_update_preserved_link": true,
		"denied_update_preserved_history": true,
		"ready_custom_update_succeeded": true,
		"ready_custom_update_version": updated.Version,
		"workspace_link_rows": finalLinks,
		"workspace_link_versions": finalVersions,
		"workspace_link_audits": finalAudits,
	}
	return nil
}

func createBody(hostname, domainKind, code, destination string) map[string]any {
	return map[string]any{
		"hostname": hostname,
		"domain_kind": domainKind,
		"code": code,
		"title": "T018",
		"primary_destination": destination,
		"redirect_status": 302,
		"routing": []any{},
		"ab": []any{},
		"utm": map[string]any{},
		"access": map[string]any{},
		"one_time": false,
		"change_reason": "P06-T018 assignment evidence",
	}
}

func updateBody(version uint64, hostname, domainKind, code, destination string) map[string]any {
	body := createBody(hostname, domainKind, code, destination)
	body["expected_version"] = version
	body["status"] = "active"
	return body
}

func doJSON(handler http.Handler, method, path, workspace, actor, role, correlation string, body map[string]any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GoJet-Test-Actor", actor)
	req.Header.Set("X-GoJet-Test-Workspace", workspace)
	req.Header.Set("X-GoJet-Test-Workspace-Role", role)
	req.Header.Set("X-Request-ID", correlation)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func requireAPIError(response *httptest.ResponseRecorder, status int, code string) error {
	if response.Code != status {
		return fmt.Errorf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	var envelope apiErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		return err
	}
	if envelope.Error.Code != code {
		return fmt.Errorf("error code=%q want=%q body=%s", envelope.Error.Code, code, response.Body.String())
	}
	return nil
}

func setDomainAxes(ctx context.Context, db *sql.DB, workspace string, domainID uint64, ownership, ingress, httpsState, risk, routing, securityCategory string) error {
	var category any
	if strings.TrimSpace(securityCategory) != "" {
		category = securityCategory
	}
	_, err := db.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status=?, ingress_dns_status=?, https_status=?, risk_status=?, routing_state=?, security_category=?,
		    ownership_verified_at=CASE WHEN ?='verified' THEN CURRENT_TIMESTAMP(6) ELSE NULL END,
		    ingress_dns_checked_at=CURRENT_TIMESTAMP(6), https_checked_at=CURRENT_TIMESTAMP(6), risk_checked_at=CURRENT_TIMESTAMP(6),
		    risk_policy_version='t018-fixture', risk_evidence_ref='risk:t018:fixture'
		WHERE workspace_id=? AND id=?`, ownership, ingress, httpsState, risk, routing, category, ownership, workspace, domainID)
	return err
}

func linkCounts(ctx context.Context, db *sql.DB, workspace string) (int, int, int, error) {
	var linksCount, versionsCount, auditsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links WHERE workspace_id=?`, workspace).Scan(&linksCount); err != nil {
		return 0, 0, 0, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM link_versions WHERE workspace_id=?`, workspace).Scan(&versionsCount); err != nil {
		return 0, 0, 0, err
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM link_audit_events WHERE workspace_id=?`, workspace).Scan(&auditsCount); err != nil {
		return 0, 0, 0, err
	}
	return linksCount, versionsCount, auditsCount, nil
}

func numberToUint64(value any) (uint64, bool) {
	number, ok := value.(float64)
	if !ok || number <= 0 || number != float64(uint64(number)) {
		return 0, false
	}
	return uint64(number), true
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
