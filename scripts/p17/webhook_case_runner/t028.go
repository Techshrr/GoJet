package main

import (
	"context"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT028(ctx context.Context, r *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 14, 40, 0, 0, time.UTC)
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t028", map[string]string{"owner-t028": "owner"}, now); err != nil {
		return nil, nil, err
	}
	receiver := newWebhookReceiver(500, 500, 204)
	defer receiver.close()
	resolver := newFixtureResolver()
	resolver.set("retry.example.com", []string{"1.1.1.1"})
	dialer := &localFixtureDialer{address: receiver.address()}
	authority, err := newWebhookAuthority(r, resolver, dialer)
	if err != nil {
		return nil, nil, err
	}
	hook, err := authority.Create(ctx, "ws-t028", "owner-t028", adminaccess.WorkspaceWebhookInput{
		Name: "Durable retry", EndpointURL: receiver.endpoint("retry.example.com"), Events: []string{"link.updated"},
	}, "t028-create", now)
	if err != nil {
		return nil, nil, err
	}
	queueAt := now.Add(time.Second)
	first, err := authority.QueueDelivery(ctx, "ws-t028", hook.Webhook.ID, "evt-t028-once", "link.updated", mustJSONRaw(`{"marker":"durable-retry"}`), "t028-event", queueAt)
	if err != nil {
		return nil, nil, err
	}
	duplicate, err := authority.QueueDelivery(ctx, "ws-t028", hook.Webhook.ID, "evt-t028-once", "link.updated", mustJSONRaw(`{"marker":"durable-retry"}`), "t028-event-duplicate", queueAt)
	if err != nil {
		return nil, nil, err
	}
	rowCount, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_webhook_deliveries WHERE workspace_id=? AND webhook_id=? AND event_id=?`, "ws-t028", hook.Webhook.ID, "evt-t028-once")
	if err != nil {
		return nil, nil, err
	}

	worked1, err1 := authority.RunDeliveryOnce(ctx, queueAt)
	var attempts1 int
	var status1 string
	var next1 time.Time
	if err := r.DB.QueryRowContext(ctx, `SELECT attempts,status,next_attempt_at FROM workspace_webhook_deliveries WHERE id=?`, first.ID).Scan(&attempts1, &status1, &next1); err != nil {
		return nil, nil, err
	}
	tooEarlyWorked, tooEarlyErr := authority.RunDeliveryOnce(ctx, queueAt.Add(30*time.Second))
	worked2, err2 := authority.RunDeliveryOnce(ctx, next1.Add(time.Millisecond))
	var attempts2 int
	var status2 string
	var next2 time.Time
	if err := r.DB.QueryRowContext(ctx, `SELECT attempts,status,next_attempt_at FROM workspace_webhook_deliveries WHERE id=?`, first.ID).Scan(&attempts2, &status2, &next2); err != nil {
		return nil, nil, err
	}
	if _, err := authority.Disable(ctx, "ws-t028", "owner-t028", hook.Webhook.ID, "t028-disable", next2.Add(-time.Second)); err != nil {
		return nil, nil, err
	}
	disabledWorked, disabledErr := authority.RunDeliveryOnce(ctx, next2.Add(time.Millisecond))

	restarted, err := newWebhookAuthority(r, resolver, dialer)
	if err != nil {
		return nil, nil, err
	}
	restartDisabledWorked, restartDisabledErr := restarted.RunDeliveryOnce(ctx, next2.Add(time.Second))
	if _, err := restarted.Enable(ctx, "ws-t028", "owner-t028", hook.Webhook.ID, "t028-enable", next2.Add(2*time.Second)); err != nil {
		return nil, nil, err
	}
	worked3, err3 := restarted.RunDeliveryOnce(ctx, next2.Add(3*time.Second))
	var attempts3 int
	var status3 string
	if err := r.DB.QueryRowContext(ctx, `SELECT attempts,status FROM workspace_webhook_deliveries WHERE id=?`, first.ID).Scan(&attempts3, &status3); err != nil {
		return nil, nil, err
	}
	requests := receiver.snapshot()
	stableIdentity := len(requests) == 3
	if stableIdentity {
		for _, request := range requests {
			stableIdentity = stableIdentity && request.DeliveryID == first.ID && request.IdempotencyID == first.ID
		}
	}
	leaseRows, err := r.Redis.Exists(ctx, "workspace-webhook:lease:"+first.ID).Result()
	if err != nil {
		return nil, nil, err
	}
	disableAudit, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action IN ('webhook.disable','webhook.enable') AND result='success'`, "ws-t028")
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"event_idempotency_deduped": first.ID == duplicate.ID && rowCount == 1,
		"bounded_retry_first_backoff": worked1 && err1 != nil && attempts1 == 1 && status1 == "retrying" && next1.Equal(queueAt.Add(time.Minute)),
		"not_due_not_retried": !tooEarlyWorked && tooEarlyErr == nil && len(receiver.snapshot()) == 3,
		"bounded_retry_second_backoff": worked2 && err2 != nil && attempts2 == 2 && status2 == "retrying" && next2.Equal(next1.Add(time.Millisecond).Add(5*time.Minute)),
		"disable_stops_delivery": !disabledWorked && disabledErr == nil,
		"restart_preserves_disabled_state": !restartDisabledWorked && restartDisabledErr == nil,
		"restart_recovery_delivers_same_authority": worked3 && err3 == nil && attempts3 == 3 && status3 == "delivered",
		"stable_delivery_idempotency_key": stableIdentity,
		"lease_released": leaseRows == 0,
		"enable_disable_audited": disableAudit == 2,
	}
	counts := map[string]int{"delivery_rows": rowCount, "receiver_requests": len(requests), "final_attempts": attempts3, "enable_disable_audit": disableAudit}
	return checks, counts, nil
}
