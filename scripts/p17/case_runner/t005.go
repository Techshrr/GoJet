package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT005(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL 8.x high-risk administrator mutation authority proving reason/idempotency/correlation, replay safety, immutable append-only secret-safe before/after audit")
	now := time.Now().UTC().Truncate(time.Second)
	service, err := adminfixture.NewService(runtime, "t005", 100)
	if err != nil {
		return out, err
	}
	_, err = adminfixture.Bootstrap(ctx, service, rootEmail, rootPassword, []string{adminaccess.PermissionAdminsManage, adminaccess.PermissionPlatformRead}, now)
	if err != nil {
		return out, err
	}
	root, rootLogin, totpSecret, err := adminfixture.LoginAndConfirmMFA(ctx, service, rootEmail, rootPassword, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	input := adminaccess.CreateRoleInput{Name: "Audited operators", Description: "P17 T005 audit fixture", Permissions: []string{adminaccess.PermissionOperationsManage}}
	_, _, missingReasonErr := service.CreateRole(ctx, root, input, adminaccess.MutationAuthority{CorrelationID: "p17-t005-missing-reason", IdempotencyKey: "p17-t005-missing-reason-key"}, now.Add(10*time.Second))
	_, _, missingKeyErr := service.CreateRole(ctx, root, input, adminaccess.MutationAuthority{Reason: "reason present", CorrelationID: "p17-t005-missing-key"}, now.Add(11*time.Second))
	authority := adminaccess.MutationAuthority{Reason: "approve bounded operations role for audited fixture", CorrelationID: "p17-t005-role", IdempotencyKey: "p17-t005-role-key"}
	role, replay, err := service.CreateRole(ctx, root, input, authority, now.Add(12*time.Second))
	if err != nil || replay {
		return out, fmt.Errorf("create audited role replay=%v err=%w", replay, err)
	}
	replayedRole, replay, err := service.CreateRole(ctx, root, input, authority, now.Add(13*time.Second))
	if err != nil || !replay {
		return out, fmt.Errorf("replay audited role replay=%v err=%w", replay, err)
	}
	_, _, mismatchErr := service.CreateRole(ctx, root, adminaccess.CreateRoleInput{Name: "Different request", Permissions: []string{adminaccess.PermissionOperationsManage}}, authority, now.Add(14*time.Second))
	childPassword := "P17-T005-CHILD-RAW-PASSWORD-MARKER"
	_, _, err = service.CreateAdministrator(ctx, root, adminaccess.CreateAdministratorInput{Email: "audit-child@p17.test", DisplayName: "Audit Child", Password: childPassword, RoleIDs: []string{role.ID}}, adminaccess.MutationAuthority{Reason: "create audited administrator without secret disclosure", CorrelationID: "p17-t005-admin", IdempotencyKey: "p17-t005-admin-key"}, now.Add(15*time.Second))
	if err != nil {
		return out, err
	}
	events, err := service.ListAudit(ctx, root, 500)
	if err != nil {
		return out, err
	}
	rawEvents, err := json.Marshal(events)
	if err != nil {
		return out, err
	}
	var roleAuditCount int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.role.create' AND request_correlation_id='p17-t005-role'`).Scan(&roleAuditCount); err != nil {
		return out, err
	}
	var reason, requestID, beforeJSON, afterJSON string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COALESCE(reason,''),request_correlation_id,CAST(before_json AS CHAR),CAST(after_json AS CHAR) FROM admin_audit_events WHERE action='admin.role.create' AND request_correlation_id='p17-t005-role' LIMIT 1`).Scan(&reason, &requestID, &beforeJSON, &afterJSON); err != nil {
		return out, err
	}
	_, updateErr := runtime.DB.ExecContext(ctx, `UPDATE admin_audit_events SET result='failed' WHERE action='admin.role.create' AND request_correlation_id='p17-t005-role'`)
	_, deleteErr := runtime.DB.ExecContext(ctx, `DELETE FROM admin_audit_events WHERE action='admin.role.create' AND request_correlation_id='p17-t005-role'`)
	idempotencyRows, err := adminfixture.ScalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_idempotency_records WHERE actor_id=? AND action='admin.role.create' AND idempotency_key_hash IS NOT NULL`, root.Administrator.ID)
	if err != nil {
		return out, err
	}
	out.RecordCounts = map[string]int{"audit_events": len(events), "role_create_audit_events": roleAuditCount, "role_idempotency_records": idempotencyRows}
	out.Checks = map[string]bool{
		"high_risk_mutation_requires_reason":                     errors.Is(missingReasonErr, adminaccess.ErrReasonRequired),
		"high_risk_mutation_requires_idempotency":                errors.Is(missingKeyErr, adminaccess.ErrInvalid),
		"approved_mutation_records_exact_reason_and_correlation": reason == authority.Reason && requestID == authority.CorrelationID,
		"approved_mutation_records_before_and_after":             strings.TrimSpace(beforeJSON) == "{}" && strings.Contains(afterJSON, role.ID) && strings.Contains(afterJSON, adminaccess.PermissionOperationsManage),
		"exact_replay_returns_same_resource_without_duplication": replay && replayedRole.ID == role.ID && roleAuditCount == 1 && idempotencyRows == 1,
		"idempotency_key_reuse_with_other_request_is_rejected":   errors.Is(mismatchErr, adminaccess.ErrReplayMismatch),
		"audit_update_is_database_rejected":                      updateErr != nil,
		"audit_delete_is_database_rejected":                      deleteErr != nil,
		"audit_output_excludes_raw_password_totp_and_session":    !bytes.Contains(rawEvents, []byte(rootPassword)) && !bytes.Contains(rawEvents, []byte(childPassword)) && !bytes.Contains(rawEvents, []byte(totpSecret)) && !bytes.Contains(rawEvents, []byte(rootLogin.Token)) && !bytes.Contains(rawEvents, []byte(rootLogin.CSRFToken)),
	}
	pass(&out)
	return out, nil
}
