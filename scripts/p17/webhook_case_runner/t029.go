package main

import (
	"context"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT029(ctx context.Context, r *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 14, 50, 0, 0, time.UTC)
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t029-a", map[string]string{"owner-t029": "owner", "member-t029": "member"}, now); err != nil {
		return nil, nil, err
	}
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t029-b", map[string]string{"owner-t029-b": "owner"}, now); err != nil {
		return nil, nil, err
	}
	resolver := newFixtureResolver()
	resolver.set("audit.example.com", []string{"1.1.1.1"})
	authority, err := newWebhookAuthority(r, resolver, nil)
	if err != nil {
		return nil, nil, err
	}
	hook, err := authority.Create(ctx, "ws-t029-a", "owner-t029", adminaccess.WorkspaceWebhookInput{
		Name: "Audit hook", EndpointURL: "https://audit.example.com/events", Events: []string{"link.updated"},
	}, "t029-create", now)
	if err != nil {
		return nil, nil, err
	}
	delivery, err := authority.QueueDelivery(ctx, "ws-t029-a", hook.Webhook.ID, "evt-t029", "link.updated", mustJSONRaw(`{"private_marker":"T029-PAYLOAD-MUST-NOT-LEAK"}`), "t029-event", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	if _, err := r.DB.ExecContext(ctx, `UPDATE workspace_webhook_deliveries SET status='failed',attempts=5,last_attempt_at=?,last_error_code='fixture_exhausted',updated_at=? WHERE id=?`, now.Add(2*time.Second), now.Add(2*time.Second), delivery.ID); err != nil {
		return nil, nil, err
	}
	handler, err := requireWebhookAPI(authority)
	if err != nil {
		return nil, nil, err
	}
	memberInspect, _, _, _, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/deliveries", "member-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	crossInspect, _, _, _, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID, "owner-t029-b", nil)
	if err != nil {
		return nil, nil, err
	}
	memberRetry, _, _, _, err := webhookAPIRequest(handler, "POST", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/deliveries/"+delivery.ID+"/retry", "member-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	memberDisable, _, _, _, err := webhookAPIRequest(handler, "POST", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/disable", "member-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	ownerRetry, retryHeaders, _, retryRaw, err := webhookAPIRequest(handler, "POST", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/deliveries/"+delivery.ID+"/retry", "owner-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	var retryStatus string
	var retryAttempts int
	if err := r.DB.QueryRowContext(ctx, `SELECT status,attempts FROM workspace_webhook_deliveries WHERE id=?`, delivery.ID).Scan(&retryStatus, &retryAttempts); err != nil {
		return nil, nil, err
	}
	ownerList, listHeaders, _, listRaw, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/deliveries", "owner-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	ownerDisable, _, _, disableRaw, err := webhookAPIRequest(handler, "POST", "/api/workspaces/ws-t029-a/webhooks/"+hook.Webhook.ID+"/disable", "owner-t029", nil)
	if err != nil {
		return nil, nil, err
	}
	lifecycleAudit, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND resource_type='webhook' AND action IN ('webhook.create','webhook.delivery.retry','webhook.disable')`, "ws-t029-a")
	if err != nil {
		return nil, nil, err
	}
	deniedAudit, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND resource_type='webhook' AND result='denied'`, "ws-t029-a")
	if err != nil {
		return nil, nil, err
	}
	secretLeak, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND (CAST(metadata_json AS CHAR) LIKE ? OR COALESCE(reason,'') LIKE ?)`, "ws-t029-a", "%"+hook.Secret+"%", "%"+hook.Secret+"%")
	if err != nil {
		return nil, nil, err
	}
	payloadLeak, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND CAST(metadata_json AS CHAR) LIKE '%T029-PAYLOAD-MUST-NOT-LEAK%'`, "ws-t029-a")
	if err != nil {
		return nil, nil, err
	}
	responsesRedacted := !strings.Contains(retryRaw, hook.Secret) && !strings.Contains(listRaw, hook.Secret) && !strings.Contains(disableRaw, hook.Secret) && !strings.Contains(listRaw, "T029-PAYLOAD-MUST-NOT-LEAK")
	checks := map[string]bool{
		"member_inspection_denied": memberInspect == 403,
		"cross_workspace_inspection_denied": crossInspect == 403,
		"member_retry_denied": memberRetry == 403,
		"member_disable_denied": memberDisable == 403,
		"owner_retry_authorized": ownerRetry == 200 && retryStatus == "retrying" && retryAttempts == 0,
		"owner_disable_authorized": ownerDisable == 200,
		"owner_delivery_inspection_authorized": ownerList == 200,
		"workspace_surfaces_no_store_noindex": webhookNoStoreNoIndex(retryHeaders) && webhookNoStoreNoIndex(listHeaders),
		"lifecycle_and_denials_audited": lifecycleAudit >= 3 && deniedAudit >= 2,
		"audit_secret_redacted": secretLeak == 0,
		"audit_payload_redacted": payloadLeak == 0,
		"api_evidence_redacted": responsesRedacted,
	}
	counts := map[string]int{"webhook_audit": lifecycleAudit, "denied_audit": deniedAudit, "secret_leaks": secretLeak, "payload_leaks": payloadLeak}
	return checks, counts, nil
}
