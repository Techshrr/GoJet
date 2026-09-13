package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// QueueLinkEvents reconciles committed product audit events into the durable
// delivery queue. Links writes and their audit records share a transaction.
// Delivery uniqueness is the checkpoint: a crash or concurrent iteration can
// retry the same event without producing another delivery record.
func (a *WorkspaceWebhookAuthority) QueueLinkEvents(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		return 0, ErrInvalid
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT e.id,e.workspace_id,w.id,e.link_id,
		       JSON_UNQUOTE(JSON_EXTRACT(e.metadata_json,'$.version')),
		       CASE e.action WHEN 'link.create' THEN 'link.created'
		         WHEN 'link.delete' THEN 'link.deleted' ELSE 'link.updated' END,
		       e.request_correlation_id,e.created_at
		FROM link_audit_events e
		JOIN workspaces ws ON ws.id=e.workspace_id AND ws.status='active'
		JOIN workspace_webhooks w ON w.workspace_id=e.workspace_id
		  AND w.status='active' AND w.created_at<=e.created_at
		WHERE e.result='success' AND e.link_id IS NOT NULL
		  AND e.action IN ('link.create','link.update','link.delete','link.restore')
		  AND JSON_CONTAINS(w.events_json,JSON_QUOTE(
		    CASE e.action WHEN 'link.create' THEN 'link.created'
		      WHEN 'link.delete' THEN 'link.deleted' ELSE 'link.updated' END))
		  AND NOT EXISTS (SELECT 1 FROM workspace_webhook_deliveries d
		    WHERE d.webhook_id=w.id AND d.event_id=CONCAT('link-audit-',e.id))
		ORDER BY e.id,w.id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	type event struct {
		id          uint64
		workspaceID string
		webhookID   string
		linkID      uint64
		version     uint64
		kind        string
		correlation string
		createdAt   time.Time
	}
	var events []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.id, &e.workspaceID, &e.webhookID, &e.linkID, &e.version, &e.kind, &e.correlation, &e.createdAt); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, e := range events {
		// Do not serialize audit metadata, actor, destination, title or reason.
		payload, err := linkWebhookPayload(e.linkID, e.version)
		if err != nil {
			return queued, err
		}
		_, err = a.QueueDelivery(ctx, e.workspaceID, e.webhookID,
			fmt.Sprintf("link-audit-%d", e.id), e.kind, payload, e.correlation, e.createdAt)
		if errors.Is(err, ErrConflict) || errors.Is(err, ErrForbidden) {
			// Configuration changed after the query. The next iteration rechecks it.
			continue
		}
		if err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

func linkWebhookPayload(linkID, version uint64) (json.RawMessage, error) {
	if linkID == 0 || version == 0 {
		return nil, ErrInvalid
	}
	return json.Marshal(struct {
		LinkID  uint64 `json:"link_id"`
		Version uint64 `json:"version"`
	}{LinkID: linkID, Version: version})
}
