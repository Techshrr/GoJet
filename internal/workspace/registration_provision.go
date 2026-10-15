package workspace

import (
	"context"
	"database/sql"
	"strings"
)

// ProvisionInitialWorkspaceTx creates the first Workspace authority for a newly
// registered account inside the caller-owned transaction. It deliberately
// reuses the P12 Workspace schema/RBAC semantics without introducing a second
// identity authority or committing independently from account registration.
func ProvisionInitialWorkspaceTx(ctx context.Context, tx *sql.Tx, principal Principal, name, correlationID string) (string, error) {
	if tx == nil {
		return "", ErrInvalid
	}
	name = strings.TrimSpace(name)
	correlationID = strings.TrimSpace(correlationID)
	email := normalizeEmail(principal.Email)
	if principal.UserID == "" || email == "" || name == "" || len(name) > 160 || !validWorkspaceCorrelationID(correlationID) {
		return "", ErrInvalid
	}

	workspaceID, err := newOpaqueID("ws_", 18)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspaces (id,name,status,version,created_by) VALUES (?,?, 'active',1,?)`,
		workspaceID, name, principal.UserID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES (?,?,?,?, 'owner')`,
		workspaceID, principal.UserID, email, strings.TrimSpace(principal.DisplayName)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_organizations (workspace_id,name,description,version) VALUES (?,?,'',1)`,
		workspaceID, name); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO workspace_notification_state (workspace_id,status,data_through_at,state_reason) VALUES (?,'complete',CURRENT_TIMESTAMP(6),'current')`,
		workspaceID); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO workspace_audit_events
(workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json)
VALUES (?,?,'workspace.create','workspace',?,NULL,?,'success',JSON_OBJECT('source','registration'))`,
		workspaceID, principal.UserID, workspaceID, correlationID); err != nil {
		return "", err
	}
	return workspaceID, nil
}

func validWorkspaceCorrelationID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}
