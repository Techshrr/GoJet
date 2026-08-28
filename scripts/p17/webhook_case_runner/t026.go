package main

import (
	"bytes"
	"context"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT026(ctx context.Context, r *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 14, 20, 0, 0, time.UTC)
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t026", map[string]string{"owner-t026": "owner"}, now); err != nil {
		return nil, nil, err
	}
	receiver := newWebhookReceiver(204, 204)
	defer receiver.close()
	resolver := newFixtureResolver()
	resolver.set("hooks.example.com", []string{"1.1.1.1"})
	dialer := &localFixtureDialer{address: receiver.address()}
	authority, err := newWebhookAuthority(r, resolver, dialer)
	if err != nil {
		return nil, nil, err
	}
	created, err := authority.Create(ctx, "ws-t026", "owner-t026", adminaccess.WorkspaceWebhookInput{
		Name: "Signed hook", EndpointURL: receiver.endpoint("hooks.example.com"), Events: []string{"link.created", "link.updated"},
	}, "t026-create", now)
	if err != nil {
		return nil, nil, err
	}
	first, err := authority.QueueDelivery(ctx, "ws-t026", created.Webhook.ID, "evt-t026-1", "link.created", mustJSONRaw(`{"resource":"alpha"}`), "t026-event-1", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	worked, err := authority.RunDeliveryOnce(ctx, now.Add(2*time.Second))
	if err != nil || !worked {
		return nil, nil, err
	}
	firstRequests := receiver.snapshot()
	if len(firstRequests) != 1 {
		return nil, nil, nil
	}
	firstTS, err := parseWebhookTimestamp(firstRequests[0].Timestamp)
	if err != nil {
		return nil, nil, err
	}
	firstVerified := adminaccess.VerifyWorkspaceWebhookDeliverySignature(created.Secret, first.ID, firstTS, firstRequests[0].Body, firstRequests[0].Signature)

	rotated, err := authority.RotateSecret(ctx, "ws-t026", "owner-t026", created.Webhook.ID, "t026-rotate", now.Add(3*time.Second))
	if err != nil {
		return nil, nil, err
	}
	second, err := authority.QueueDelivery(ctx, "ws-t026", created.Webhook.ID, "evt-t026-2", "link.updated", mustJSONRaw(`{"resource":"beta"}`), "t026-event-2", now.Add(4*time.Second))
	if err != nil {
		return nil, nil, err
	}
	worked, err = authority.RunDeliveryOnce(ctx, now.Add(5*time.Second))
	if err != nil || !worked {
		return nil, nil, err
	}
	requests := receiver.snapshot()
	if len(requests) != 2 {
		return nil, nil, nil
	}
	secondTS, err := parseWebhookTimestamp(requests[1].Timestamp)
	if err != nil {
		return nil, nil, err
	}
	secondNewVerified := adminaccess.VerifyWorkspaceWebhookDeliverySignature(rotated.Secret, second.ID, secondTS, requests[1].Body, requests[1].Signature)
	secondOldVerified := adminaccess.VerifyWorkspaceWebhookDeliverySignature(created.Secret, second.ID, secondTS, requests[1].Body, requests[1].Signature)
	var storedCipher []byte
	var storedPrefix, keyID string
	if err := r.DB.QueryRowContext(ctx, `SELECT secret_ciphertext,secret_prefix,secret_key_id FROM workspace_webhooks WHERE workspace_id=? AND id=?`, "ws-t026", created.Webhook.ID).Scan(&storedCipher, &storedPrefix, &keyID); err != nil {
		return nil, nil, err
	}
	deliveredRows, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_webhook_deliveries WHERE workspace_id=? AND webhook_id=? AND status='delivered'`, "ws-t026", created.Webhook.ID)
	if err != nil {
		return nil, nil, err
	}
	rotationAudit, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action='webhook.rotate_secret' AND result='success'`, "ws-t026")
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"canonical_signature_verified":     firstVerified,
		"delivery_identity_signed":         firstRequests[0].DeliveryID == first.ID && firstRequests[0].IdempotencyID == first.ID,
		"secret_rotation_atomic":           rotated.Secret != "" && rotated.Secret != created.Secret && rotated.Webhook.SecretPrefix == storedPrefix,
		"new_secret_signs_new_delivery":    secondNewVerified,
		"old_secret_authority_invalidated": !secondOldVerified,
		"secret_encrypted_at_rest":         len(storedCipher) > 0 && !bytes.Contains(storedCipher, []byte(created.Secret)) && !bytes.Contains(storedCipher, []byte(rotated.Secret)),
		"secret_key_bound":                 keyID == "p17-webhook-fixture-v1",
		"both_deliveries_durable":          deliveredRows == 2,
		"rotation_audited_without_secret":  rotationAudit == 1,
	}
	counts := map[string]int{"delivered": deliveredRows, "rotation_audit": rotationAudit, "receiver_requests": len(requests)}
	return checks, counts, nil
}
