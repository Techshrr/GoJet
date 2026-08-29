package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT020(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL append-only audit plus authorized HTTP query response redaction for raw tokens and provider evidence")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, login, err := bootstrapCaseRoot(ctx, runtime, "T020", []string{adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		return out, err
	}
	other, otherLogin, err := createScopedMFAAdmin(ctx, service, root, "T020", "content-only", adminaccess.PermissionContentManage, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	_, err = runtime.DB.ExecContext(ctx, `INSERT INTO admin_audit_events(actor_kind,actor_id,action,resource_type,resource_id,result,request_correlation_id,reason,before_json,after_json,metadata_json,created_at) VALUES ('system','','fixture.sensitive','security_fixture','fixture-1','success','p17-t020-sensitive','authorization: bearer raw-audit-token',JSON_OBJECT('session_token','raw-session-value'),JSON_OBJECT('status','safe'),JSON_OBJECT('provider_evidence',JSON_OBJECT('secret','provider-raw-evidence')),?)`, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	api, err := adminaccess.NewHTTPAPI(service)
	if err != nil {
		return out, err
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	query := func(token string) (int, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/admin/audit?limit=100", nil)
		if err != nil {
			return 0, "", err
		}
		req.AddCookie(&http.Cookie{Name: adminaccess.AdminSessionCookie, Value: token})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, "", err
		}
		defer resp.Body.Close()
		var raw json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
			return resp.StatusCode, "", err
		}
		return resp.StatusCode, string(raw), nil
	}
	status, body, err := query(login.Token)
	if err != nil {
		return out, err
	}
	deniedStatus, _, err := query(otherLogin.Token)
	if err != nil {
		return out, err
	}
	_, updateErr := runtime.DB.ExecContext(ctx, `UPDATE admin_audit_events SET result='failed' WHERE resource_id='fixture-1'`)
	_, deleteErr := runtime.DB.ExecContext(ctx, `DELETE FROM admin_audit_events WHERE resource_id='fixture-1'`)
	lower := strings.ToLower(body)
	out.RecordCounts["audit_query_status"] = status
	out.Checks["platform_read_authorizes_query"] = status == http.StatusOK
	out.Checks["unrelated_permission_denied"] = deniedStatus == http.StatusForbidden && !other.Has(adminaccess.PermissionPlatformRead)
	out.Checks["raw_session_token_redacted"] = !strings.Contains(body, "raw-session-value") && strings.Contains(body, "[redacted]")
	out.Checks["provider_evidence_redacted"] = !strings.Contains(body, "provider-raw-evidence")
	out.Checks["authorization_reason_redacted"] = !strings.Contains(lower, "raw-audit-token")
	out.Checks["history_update_rejected"] = updateErr != nil
	out.Checks["history_delete_rejected"] = deleteErr != nil
	out.Checks["trigger_failures_are_not_false_conflict"] = !errors.Is(updateErr, adminaccess.ErrConflict) && !errors.Is(deleteErr, adminaccess.ErrConflict)
	pass(&out)
	return out, nil
}
