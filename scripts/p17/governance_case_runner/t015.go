package main

import (
	"context"
	"net/http"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT015(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("exact eight-service runtime governance with operations.manage, explicit restart impact/reason, allowlisted restarter and immutable idempotent P17 audit")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, rootLogin, err := bootstrapCaseRoot(ctx, runtime, "T015", []string{adminaccess.PermissionOperationsManage}, now)
	if err != nil {
		return out, err
	}
	_, nonOpsLogin, err := createScopedMFAAdmin(ctx, service, root, "T015", "security", adminaccess.PermissionSecurityManage, now.Add(15*time.Second))
	if err != nil {
		return out, err
	}
	probe := deterministicOpsProbe{states: map[string]map[string]bool{
		"redirectengine":      {"unit": true, "mysql": true, "redis": true},
		"analyticsworker":     {"unit": true, "mysql": true, "redis": true},
		"analyticsreconciler": {"unit": true, "mysql": true},
		"platformapi":         {"unit": true, "mysql": true, "redis": true},
		"mailworker":          {"unit": true, "mysql": true},
		"fileworker":          {"unit": true, "mysql": true, "clamav": true},
		"operationsmonitor":   {"unit": true, "mysql": true, "redis": true},
		"logreceiver":         {"unit": true},
	}}
	restarter := &recordingRestarter{}
	ops, err := adminaccess.NewOperationsGovernance(service, probe, restarter)
	if err != nil {
		return out, err
	}
	server, err := adminfixture.NewExtendedHTTPServer(service, ops)
	if err != nil {
		return out, err
	}
	defer server.Close()
	list, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/operations/services", "", rootLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	nonOpsList, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/operations/services", "", nonOpsLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	ids := make([]string, 0, 8)
	if rawItems, ok := list.Body["items"].([]any); ok {
		for _, raw := range rawItems {
			if item, ok := raw.(map[string]any); ok {
				if id, ok := item["id"].(string); ok {
					ids = append(ids, id)
				}
			}
		}
	}
	expected := strings.Join([]string{"redirectengine", "analyticsworker", "analyticsreconciler", "platformapi", "mailworker", "fileworker", "operationsmonitor", "logreceiver"}, ",")

	invalidService, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/imaginary/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-invalid-service-key", "p17-t015-invalid-service", map[string]any{"reason": "unknown service must be refused", "impact_confirmation": "restart service imaginary"})
	if err != nil {
		return out, err
	}
	badImpact, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-bad-impact-key", "p17-t015-bad-impact", map[string]any{"reason": "restart file worker", "impact_confirmation": "restart everything"})
	if err != nil {
		return out, err
	}
	missingReason, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-missing-reason-key", "p17-t015-missing-reason", map[string]any{"reason": "", "impact_confirmation": "restart service fileworker"})
	if err != nil {
		return out, err
	}
	nonOps, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, nonOpsLogin.Token, nonOpsLogin.CSRFToken, "p17-t015-nonops-key", "p17-t015-nonops", map[string]any{"reason": "must remain forbidden", "impact_confirmation": "restart service fileworker"})
	if err != nil {
		return out, err
	}
	body := map[string]any{"reason": "restart fileworker after explicit operator impact acknowledgement", "impact_confirmation": "restart service fileworker"}
	restart, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-restart-key", "p17-t015-restart", body)
	if err != nil {
		return out, err
	}
	replay, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-restart-key", "p17-t015-restart", body)
	if err != nil {
		return out, err
	}
	conflict, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/services/fileworker/restart", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t015-restart-key", "p17-t015-restart-conflict", map[string]any{"reason": "different reason is different request", "impact_confirmation": "restart service fileworker"})
	if err != nil {
		return out, err
	}
	auditN, err := auditCount(ctx, runtime.DB, "admin.operations.service.restart", "fileworker")
	if err != nil {
		return out, err
	}
	var reason, before, after string
	var allowlisted, shellInput, impact bool
	err = runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(reason,''),CAST(before_json AS CHAR),CAST(after_json AS CHAR),JSON_EXTRACT(metadata_json,'$.allowlisted')=true,JSON_EXTRACT(metadata_json,'$.shell_input')=true,JSON_EXTRACT(metadata_json,'$.impact_confirmation')=true FROM admin_audit_events WHERE action='admin.operations.service.restart' AND resource_id='fileworker' AND request_correlation_id='p17-t015-restart'`).Scan(&reason, &before, &after, &allowlisted, &shellInput, &impact)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{"service_identities": len(ids), "restarter_calls": len(restarter.calls), "restart_audits": auditN}
	out.Checks = map[string]bool{
		"service_surface_contains_exactly_frozen_eight_identities":            list.Status == http.StatusOK && strings.Join(ids, ",") == expected && len(adminaccess.AdminRuntimeServiceIDs()) == 8 && strings.Join(adminaccess.AdminRuntimeServiceIDs(), ",") == expected && adminfixture.NoStoreNoIndex(list),
		"operations_manage_is_required_for_service_health_and_restart":        nonOpsList.Status == http.StatusForbidden && nonOps.Status == http.StatusForbidden,
		"unknown_service_and_wrong_impact_fail_before_restart":                invalidService.Status == http.StatusBadRequest && badImpact.Status == http.StatusBadRequest && len(restarter.calls) == 1,
		"restart_reason_is_mandatory":                                         missingReason.Status == http.StatusUnprocessableEntity,
		"allowlisted_restart_executes_once_and_replay_does_not_restart_again": restart.Status == http.StatusOK && replay.Status == http.StatusOK && len(restarter.calls) == 1 && restarter.calls[0] == "fileworker" && auditN == 1,
		"conflicting_idempotency_reuse_is_rejected":                           conflict.Status == http.StatusConflict,
		"restart_audit_records_impact_and_no_shell_input":                     reason != "" && before != "{}" && after != "{}" && allowlisted && !shellInput && impact,
	}
	pass(&out)
	return out, nil
}
