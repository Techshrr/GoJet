package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

func appendDomainAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	workspaceID string,
	domainID *uint64,
	entitlementSourceID *uint64,
	actorID string,
	action string,
	result string,
	reason string,
	correlationID string,
	metadata map[string]any,
) error {
	workspaceID = strings.TrimSpace(workspaceID)
	actorID = strings.TrimSpace(actorID)
	action = strings.TrimSpace(action)
	correlationID = strings.TrimSpace(correlationID)
	if workspaceID == "" || actorID == "" || action == "" || correlationID == "" {
		return ErrInvalidEntitlementSource
	}
	switch result {
	case "success", "denied", "conflict", "failed":
	default:
		return ErrInvalidEntitlementSource
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_audit_events (
			workspace_id, domain_id, entitlement_source_id, actor_id, action,
			result, reason, correlation_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		workspaceID, domainID, entitlementSourceID, actorID, action,
		result, strings.TrimSpace(reason), correlationID, string(raw),
	)
	return err
}
