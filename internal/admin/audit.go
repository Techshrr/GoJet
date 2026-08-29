package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

var safeAuditKeys = map[string]struct{}{
	"status": {}, "mfa_enabled": {}, "role_ids": {}, "permissions": {}, "version": {},
	"session_id": {}, "target_session_id": {}, "attempts": {}, "locked": {},
	"replayed": {}, "role_id": {}, "administrator_id": {},
	"user_id": {}, "workspace_id": {}, "email_verified": {}, "revoked_sessions": {},
	"member_count": {}, "owner_count": {},
	"scan_state": {}, "scan_generation": {}, "published": {}, "expires_at": {}, "deleted": {}, "clamav_bypass": {},
	"state": {}, "authority": {}, "p16_verdict_mutated": {},
	"max_attempts": {}, "last_error_code": {}, "allowlisted": {}, "shell_input": {},
	"service_status": {}, "unit": {}, "mysql": {}, "redis": {}, "clamav": {},
	"resource_kind": {}, "impact_confirmation": {},
	"configured": {}, "hostname": {}, "action": {}, "enabled": {}, "default": {},
}

func safeAuditJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		return []byte("{}"), nil
	}
	clean := make(map[string]any, len(value))
	for key, item := range value {
		if _, ok := safeAuditKeys[key]; !ok {
			return nil, ErrInvalid
		}
		switch v := item.(type) {
		case nil, string, bool, float64, int, int64, uint64:
			clean[key] = v
		case []string:
			clean[key] = append([]string(nil), v...)
		default:
			return nil, ErrInvalid
		}
	}
	return json.Marshal(clean)
}

type auditInput struct {
	ActorKind     string
	ActorID       string
	Action        string
	ResourceType  string
	ResourceID    string
	Result        string
	CorrelationID string
	Reason        string
	Before        map[string]any
	After         map[string]any
	Metadata      map[string]any
	CreatedAt     time.Time
}

func recordAuditTx(ctx context.Context, tx *sql.Tx, input auditInput) (uint64, error) {
	if tx == nil || strings.TrimSpace(input.Action) == "" || strings.TrimSpace(input.ResourceType) == "" || strings.TrimSpace(input.CorrelationID) == "" {
		return 0, ErrInvalid
	}
	if input.ActorKind != "anonymous" && input.ActorKind != "administrator" && input.ActorKind != "system" {
		return 0, ErrInvalid
	}
	if input.Result != "success" && input.Result != "denied" && input.Result != "conflict" && input.Result != "failed" {
		return 0, ErrInvalid
	}
	before, err := safeAuditJSON(input.Before)
	if err != nil {
		return 0, err
	}
	after, err := safeAuditJSON(input.After)
	if err != nil {
		return 0, err
	}
	metadata, err := safeAuditJSON(input.Metadata)
	if err != nil {
		return 0, err
	}
	var reason any
	if trimmed := strings.TrimSpace(input.Reason); trimmed != "" {
		if len(trimmed) > 500 {
			return 0, ErrInvalid
		}
		reason = trimmed
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO admin_audit_events
(actor_kind,actor_id,action,resource_type,resource_id,result,request_correlation_id,reason,before_json,after_json,metadata_json,created_at)
VALUES (?,?,?,?,?,?,?,?,CAST(? AS JSON),CAST(? AS JSON),CAST(? AS JSON),?)`,
		input.ActorKind, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.Action), strings.TrimSpace(input.ResourceType), strings.TrimSpace(input.ResourceID), input.Result, strings.TrimSpace(input.CorrelationID), reason, string(before), string(after), string(metadata), input.CreatedAt.UTC().Truncate(time.Microsecond))
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}
