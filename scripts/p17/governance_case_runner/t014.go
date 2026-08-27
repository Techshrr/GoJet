package main

import (
	"context"
	"net/http"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT014(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real P16 destination-risk retry/failed queue inspected and safely requeued by operations.manage with impact/reason/idempotency/correlation and P17 immutable audit")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, rootLogin, err := bootstrapCaseRoot(ctx, runtime, "T014", []string{adminaccess.PermissionOperationsManage}, now)
	if err != nil {
		return out, err
	}
	_, nonOpsLogin, err := createScopedMFAAdmin(ctx, service, root, "T014", "security", adminaccess.PermissionSecurityManage, now.Add(15*time.Second))
	if err != nil {
		return out, err
	}
	const ws = "ws_p17_t014"
	res, err := runtime.DB.ExecContext(ctx, `INSERT INTO links(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json,created_at,updated_at) VALUES (?,'jobs.p17.test','official','t014','P16 Job Link','https://example.test/job',302,'active',1,REPEAT('f',64),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),?,?)`, ws, now, now)
	if err != nil {
		return out, err
	}
	linkID, _ := res.LastInsertId()
	seedJob := func(key, status string, attempts, max int, errCode string) (uint64, error) {
		res, err := runtime.DB.ExecContext(ctx, `INSERT INTO destination_risk_scans(workspace_id,link_id,risk_fingerprint,policy_version,request_kind,idempotency_key,status,attempts,max_attempts,available_at,correlation_id,last_error_code,created_at,updated_at) VALUES (?,?,REPEAT('f',64),'p16-policy','rescan',?,?,?,?,?,'p16-worker-correlation',?,?,?)`, ws, linkID, key, status, attempts, max, now, errCode, now, now)
		if err != nil {
			return 0, err
		}
		id, err := res.LastInsertId()
		return uint64(id), err
	}
	retryID, err := seedJob("t014-retry", "retry", 1, 5, "provider-unavailable")
	if err != nil {
		return out, err
	}
	failedID, err := seedJob("t014-failed", "failed", 2, 5, "provider-timeout")
	if err != nil {
		return out, err
	}
	exhaustedID, err := seedJob("t014-exhausted", "failed", 5, 5, "max-attempts")
	if err != nil {
		return out, err
	}

	ops, err := adminaccess.NewOperationsGovernance(service, deterministicOpsProbe{}, nil)
	if err != nil {
		return out, err
	}
	server, err := adminfixture.NewExtendedHTTPServer(service, ops)
	if err != nil {
		return out, err
	}
	defer server.Close()
	list, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/operations/jobs", "", rootLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	nonOpsList, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/operations/jobs", "", nonOpsLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	badImpact, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-bad-impact-key", "p17-t014-bad-impact", map[string]any{"reason": "retry durable P16 job", "impact_confirmation": "wrong impact"})
	if err != nil {
		return out, err
	}
	missingReason, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-missing-reason-key", "p17-t014-missing-reason", map[string]any{"reason": "", "impact_confirmation": "requeue destination-risk job " + itoa64(retryID)})
	if err != nil {
		return out, err
	}
	nonOpsMutation, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, nonOpsLogin.Token, nonOpsLogin.CSRFToken, "p17-t014-nonops-key", "p17-t014-nonops", map[string]any{"reason": "must remain forbidden", "impact_confirmation": "requeue destination-risk job " + itoa64(retryID)})
	if err != nil {
		return out, err
	}

	body := map[string]any{"reason": "requeue recoverable P16 destination-risk job without fabricating verdict", "impact_confirmation": "requeue destination-risk job " + itoa64(retryID)}
	requeue, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-requeue-key", "p17-t014-requeue", body)
	if err != nil {
		return out, err
	}
	replay, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-requeue-key", "p17-t014-requeue", body)
	if err != nil {
		return out, err
	}
	conflict, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(retryID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-requeue-key", "p17-t014-requeue-conflict", map[string]any{"reason": "different reason must conflict", "impact_confirmation": "requeue destination-risk job " + itoa64(retryID)})
	if err != nil {
		return out, err
	}
	exhausted, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/operations/jobs/"+itoa64(exhaustedID)+"/requeue", adminfixture.AllowedOrigin, rootLogin.Token, rootLogin.CSRFToken, "p17-t014-exhausted-key", "p17-t014-exhausted", map[string]any{"reason": "do not bypass maximum attempts", "impact_confirmation": "requeue destination-risk job " + itoa64(exhaustedID)})
	if err != nil {
		return out, err
	}

	state, err := scalarString(ctx, runtime.DB, `SELECT CONCAT(status,':',attempts,':',max_attempts,':',COALESCE(lease_owner,'none')) FROM destination_risk_scans WHERE id=?`, retryID)
	if err != nil {
		return out, err
	}
	failedState, err := scalarString(ctx, runtime.DB, `SELECT status FROM destination_risk_scans WHERE id=?`, failedID)
	if err != nil {
		return out, err
	}
	exhaustedState, err := scalarString(ctx, runtime.DB, `SELECT CONCAT(status,':',attempts) FROM destination_risk_scans WHERE id=?`, exhaustedID)
	if err != nil {
		return out, err
	}
	decisionCount, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM destination_risk_decisions WHERE scan_id=?`, retryID)
	if err != nil {
		return out, err
	}
	auditN, err := auditCount(ctx, runtime.DB, "admin.operations.job.requeue", itoa64(retryID))
	if err != nil {
		return out, err
	}
	var reason, before, after string
	var impact bool
	err = runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(reason,''),CAST(before_json AS CHAR),CAST(after_json AS CHAR),JSON_EXTRACT(metadata_json,'$.impact_confirmation')=true FROM admin_audit_events WHERE action='admin.operations.job.requeue' AND resource_id=? AND request_correlation_id='p17-t014-requeue'`, itoa64(retryID)).Scan(&reason, &before, &after, &impact)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{"operational_jobs": 3, "requeue_audit_events": auditN}
	out.Checks = map[string]bool{
		"operations_manage_required_for_inspect_and_requeue":                         list.Status == http.StatusOK && nonOpsList.Status == http.StatusForbidden && nonOpsMutation.Status == http.StatusForbidden && adminfixture.NoStoreNoIndex(list),
		"reason_and_exact_impact_confirmation_are_mandatory":                         badImpact.Status == http.StatusBadRequest && missingReason.Status == http.StatusUnprocessableEntity,
		"recoverable_job_requeue_is_bounded_and_does_not_fabricate_business_success": requeue.Status == http.StatusOK && state == "queued:1:5:none" && decisionCount == 0 && failedState == "failed",
		"requeue_is_idempotent_and_conflicting_reuse_is_rejected":                    replay.Status == http.StatusOK && conflict.Status == http.StatusConflict && auditN == 1,
		"maximum_attempts_remains_fail_closed":                                       exhausted.Status == http.StatusConflict && exhaustedState == "failed:5",
		"requeue_audit_contains_reason_correlation_before_after_and_impact":          reason != "" && before != "{}" && after != "{}" && impact,
	}
	pass(&out)
	return out, nil
}
