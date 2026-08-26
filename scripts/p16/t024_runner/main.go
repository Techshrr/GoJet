package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/adminfixture"
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
		out.Case = "P16-T024"
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
		Case:         "P16-T024",
		Status:       "FAIL",
		Fixture:      "real MySQL/Redis/native platformapi with inherited P15 session/origin/CSRF authority proving security.manage destination-risk list/detail/rescan/override RBAC and provider/target non-disclosure",
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

	const (
		securityActor = "p16-t024-security-admin"
		deniedActor   = "p16-t024-denied"
		domainActor   = "p16-t025-domain-admin"
		policyVersion = "p16-admin-destination-v1"
		secretMarker  = "p16-t024-provider-secret"
		unsafeTarget  = "https://unsafe-admin-leak.example/private"
	)
	securitySession, err := adminfixture.EnsureSession(ctx, db, securityActor)
	if err != nil {
		return out, err
	}
	deniedSession, err := adminfixture.EnsureSession(ctx, db, deniedActor)
	if err != nil {
		return out, err
	}
	domainSession, err := adminfixture.EnsureSession(ctx, db, domainActor)
	if err != nil {
		return out, err
	}

	store := trust.NewStore(db)
	link, err := runtimefixture.CreateLink(ctx, db, "p16-t024-workspace", "go.example.test", "official", "t024-admin", "https://customer.example/t024-sensitive-destination", nil, nil)
	if err != nil {
		return out, err
	}
	decision, err := runtimefixture.InsertRawDecision(ctx, db, store, link, policyVersion, "t024-base", trust.DecisionReview, "manual-review-required", nil)
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
INSERT INTO destination_risk_provider_observations
(scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at)
VALUES (?,'semantic-sensitive-fixture','review','admin-sensitive-fixture',?,?,?)`,
		decision.ScanID,
		`{"authorization":"Bearer `+secretMarker+`","target":"`+unsafeTarget+`"}`,
		now,
		now,
	); err != nil {
		return out, err
	}

	unauthenticated, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", "", "", "", "", nil)
	if err != nil {
		return out, err
	}
	denied, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", deniedSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	domainPermissionDenied, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", domainSession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	list, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks?limit=100", securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	csrf := adminfixture.CSRF(list)
	if csrf == "" {
		return out, fmt.Errorf("T024 list did not issue CSRF token")
	}
	detailPath := "/api/admin/destination-risks/" + strconv.FormatUint(decision.ScanID, 10)
	detail, err := adminfixture.Request(ctx, http.MethodGet, detailPath, securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	crossPermission, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/domain-risks", securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}

	rescanKey := "p16-t024-rescan-idempotency-0001"
	rescan, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/rescan", securitySession, csrf, rescanKey, "p16-t024-rescan-0001", map[string]any{})
	if err != nil {
		return out, err
	}
	rescanID := uint64(number(rescan.Body["risk_id"]))
	rescanCreated, _ := rescan.Body["created"].(bool)

	freshList, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	rescanReplay, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/rescan", securitySession, adminfixture.CSRF(freshList), rescanKey, "p16-t024-rescan-replay", map[string]any{})
	if err != nil {
		return out, err
	}
	replayID := uint64(number(rescanReplay.Body["risk_id"]))
	replayCreated, _ := rescanReplay.Body["created"].(bool)

	missingCSRF, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/rescan", securitySession, "", "p16-t024-rescan-no-csrf", "p16-t024-no-csrf", map[string]any{})
	if err != nil {
		return out, err
	}

	overrideList, err := adminfixture.Request(ctx, http.MethodGet, "/api/admin/destination-risks", securitySession, "", "", "", nil)
	if err != nil {
		return out, err
	}
	override, err := adminfixture.Request(ctx, http.MethodPost, detailPath+"/override", securitySession, adminfixture.CSRF(overrideList), "", "p16-t024-override-0001", map[string]any{
		"decision":   "allow",
		"reason":     "security reviewer approved current exact destination authority",
		"expires_at": time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		return out, err
	}
	overrideID := uint64(number(override.Body["override_id"]))

	scans, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND link_id=? AND policy_version=?`, link.WorkspaceID, link.ID, policyVersion)
	if err != nil {
		return out, err
	}
	rescanRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_scans WHERE workspace_id=? AND link_id=? AND policy_version=? AND request_kind='rescan' AND idempotency_key=?`, link.WorkspaceID, link.ID, policyVersion, rescanKey)
	if err != nil {
		return out, err
	}
	overrideRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_overrides WHERE workspace_id=? AND link_id=? AND policy_version=? AND correlation_id='p16-t024-override-0001'`, link.WorkspaceID, link.ID, policyVersion)
	if err != nil {
		return out, err
	}
	auditRows, err := runtimefixture.ScalarInt(ctx, db, `SELECT COUNT(*) FROM destination_risk_audit_events WHERE workspace_id=? AND link_id=? AND action='destination-risk.override-create' AND correlation_id='p16-t024-override-0001'`, link.WorkspaceID, link.ID)
	if err != nil {
		return out, err
	}

	allHTTP := strings.Join([]string{list.Raw, detail.Raw, rescan.Raw, rescanReplay.Raw, override.Raw}, "\n")
	out.RecordCounts = map[string]int{
		"destination_risk_scans": scans,
		"idempotent_rescan_rows": rescanRows,
		"destination_overrides":  overrideRows,
		"override_audit_events":  auditRows,
	}
	out.Checks = map[string]bool{
		"p15_session_is_required":                                  unauthenticated.Status == http.StatusUnauthorized,
		"security_manage_rejects_unprivileged_session":             denied.Status == http.StatusForbidden,
		"domain_risk_permission_does_not_grant_destination_access": domainPermissionDenied.Status == http.StatusForbidden,
		"security_manage_does_not_grant_domain_risk_access":        crossPermission.Status == http.StatusForbidden,
		"destination_list_is_private_and_noindex":                  list.Status == http.StatusOK && adminfixture.NoStoreNoIndex(list),
		"destination_detail_is_private_and_noindex":                detail.Status == http.StatusOK && adminfixture.NoStoreNoIndex(detail),
		"admin_dto_contains_control_state_not_reachable_targets":    strings.Contains(list.Raw, strconv.FormatUint(decision.ScanID, 10)) && strings.Contains(detail.Raw, link.Fingerprint) && !strings.Contains(allHTTP, link.Primary),
		"provider_evidence_and_unsafe_target_never_leave_server":   !strings.Contains(allHTTP, secretMarker) && !strings.Contains(allHTTP, unsafeTarget) && !strings.Contains(strings.ToLower(allHTTP), "authorization"),
		"rescan_requires_exact_security_authority":                 rescan.Status == http.StatusAccepted && rescanCreated && rescanID > 0 && rescanID != decision.ScanID,
		"rescan_idempotency_replays_without_duplicate_queue_row":   rescanReplay.Status == http.StatusAccepted && !replayCreated && replayID == rescanID && rescanRows == 1,
		"unsafe_mutation_requires_p15_csrf":                        missingCSRF.Status == http.StatusForbidden && scans == 2,
		"manual_override_uses_existing_exact_bound_authority":      override.Status == http.StatusOK && overrideID > 0 && overrideRows == 1 && auditRows == 1,
		"admin_responses_do_not_offer_continue_anyway_bypass":      !strings.Contains(strings.ToLower(allHTTP), "continue anyway") && !strings.Contains(strings.ToLower(allHTTP), "bypass"),
	}
	if runtimefixture.AllTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func number(value any) float64 {
	n, _ := value.(float64)
	return n
}
