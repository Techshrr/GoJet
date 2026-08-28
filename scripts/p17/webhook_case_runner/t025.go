package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT025(ctx context.Context, r *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 14, 10, 0, 0, time.UTC)
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t025-a", map[string]string{"owner-t025": "owner", "member-t025": "member"}, now); err != nil {
		return nil, nil, err
	}
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t025-b", map[string]string{"owner-t025-b": "owner"}, now); err != nil {
		return nil, nil, err
	}
	resolver := newFixtureResolver()
	resolver.set("hooks.example.com", []string{"1.1.1.1"})
	authority, err := newWebhookAuthority(r, resolver, nil)
	if err != nil {
		return nil, nil, err
	}
	created, err := authority.Create(ctx, "ws-t025-a", "owner-t025", adminaccess.WorkspaceWebhookInput{
		Name: "Primary automation", EndpointURL: "https://hooks.example.com/v1/events", Events: []string{"link.updated", "link.created"},
	}, "t025-create", now)
	if err != nil {
		return nil, nil, err
	}
	var ciphertext []byte
	if err := r.DB.QueryRowContext(ctx, `SELECT secret_ciphertext FROM workspace_webhooks WHERE workspace_id=? AND id=?`, "ws-t025-a", created.Webhook.ID).Scan(&ciphertext); err != nil {
		return nil, nil, err
	}
	handler, err := requireWebhookAPI(authority)
	if err != nil {
		return nil, nil, err
	}
	ownerCode, ownerHeaders, _, ownerRaw, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t025-a/webhooks", "owner-t025", nil)
	if err != nil {
		return nil, nil, err
	}
	memberCode, _, _, _, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t025-a/webhooks", "member-t025", nil)
	if err != nil {
		return nil, nil, err
	}
	crossCode, _, _, _, err := webhookAPIRequest(handler, "GET", "/api/workspaces/ws-t025-a/webhooks/"+created.Webhook.ID, "owner-t025-b", nil)
	if err != nil {
		return nil, nil, err
	}
	_, unsafeErr := authority.Create(ctx, "ws-t025-a", "owner-t025", adminaccess.WorkspaceWebhookInput{
		Name: "Unsafe", EndpointURL: "http://127.0.0.1/internal", Events: []string{"link.created"},
	}, "t025-unsafe", now.Add(time.Second))
	paymentFKs, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='workspace_webhooks' AND REFERENCED_TABLE_NAME LIKE '%payment%'`)
	if err != nil {
		return nil, nil, err
	}
	webhookRows, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_webhooks WHERE workspace_id=?`, "ws-t025-a")
	if err != nil {
		return nil, nil, err
	}
	auditRows, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND action='webhook.create'`, "ws-t025-a")
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"workspace_owned":                        created.Webhook.WorkspaceID == "ws-t025-a" && webhookRows == 1,
		"configuration_validated":                created.Webhook.EndpointURL == "https://hooks.example.com/v1/events" && len(created.Webhook.Events) == 2,
		"unsafe_endpoint_rejected":               errors.Is(unsafeErr, adminaccess.ErrInvalid),
		"manager_authorized":                     ownerCode == 200,
		"member_denied":                          memberCode == 403,
		"cross_workspace_denied_without_leakage": crossCode == 403,
		"secret_once_not_listed":                 created.Secret != "" && !strings.Contains(ownerRaw, created.Secret),
		"secret_encrypted_at_rest":               len(ciphertext) > 0 && !bytes.Contains(ciphertext, []byte(created.Secret)),
		"no_store_noindex":                       webhookNoStoreNoIndex(ownerHeaders),
		"payment_callback_independent_schema":    paymentFKs == 0,
		"creation_audited":                       auditRows >= 1,
	}
	counts := map[string]int{"workspace_webhooks": webhookRows, "webhook_audit_events": auditRows, "payment_foreign_keys": paymentFKs}
	return checks, counts, nil
}
