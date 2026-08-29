package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT027(ctx context.Context, r *adminfixture.Runtime) (map[string]bool, map[string]int, error) {
	now := time.Date(2026, 8, 28, 14, 30, 0, 0, time.UTC)
	if err := seedWebhookWorkspace(ctx, r.DB, "ws-t027", map[string]string{"owner-t027": "owner"}, now); err != nil {
		return nil, nil, err
	}
	resolver := newFixtureResolver()
	resolver.set("mixed.example.com", []string{"1.1.1.1", "127.0.0.1"})
	baseAuthority, err := newWebhookAuthority(r, resolver, nil)
	if err != nil {
		return nil, nil, err
	}
	unsafeURLs := []string{
		"http://127.0.0.1/hook",
		"http://10.0.0.1/hook",
		"http://169.254.169.254/latest/meta-data",
		"http://192.0.2.1/hook",
		"http://[::1]/hook",
		"http://mixed.example.com/hook",
	}
	unsafeRejected := true
	for index, endpoint := range unsafeURLs {
		_, createErr := baseAuthority.Create(ctx, "ws-t027", "owner-t027", adminaccess.WorkspaceWebhookInput{
			Name: "Unsafe endpoint", EndpointURL: endpoint, Events: []string{"link.updated"},
		}, "t027-unsafe", now.Add(time.Duration(index)*time.Millisecond))
		unsafeRejected = unsafeRejected && errors.Is(createErr, adminaccess.ErrInvalid)
	}

	rebindReceiver := newWebhookReceiver(204)
	defer rebindReceiver.close()
	rebindResolver := newFixtureResolver()
	rebindResolver.set("rebind.example.com",
		[]string{"1.1.1.1"},
		[]string{"1.1.1.1"},
		[]string{"127.0.0.1"},
	)
	rebindDialer := &localFixtureDialer{address: rebindReceiver.address()}
	rebindAuthority, err := newWebhookAuthority(r, rebindResolver, rebindDialer)
	if err != nil {
		return nil, nil, err
	}
	rebindHook, err := rebindAuthority.Create(ctx, "ws-t027", "owner-t027", adminaccess.WorkspaceWebhookInput{
		Name: "Rebind", EndpointURL: rebindReceiver.endpoint("rebind.example.com"), Events: []string{"link.updated"},
	}, "t027-rebind-create", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	rebindDelivery, err := rebindAuthority.QueueDelivery(ctx, "ws-t027", rebindHook.Webhook.ID, "evt-t027-rebind", "link.updated", mustJSONRaw(`{"marker":"rebind"}`), "t027-rebind-event", now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	rebindWorked, rebindErr := rebindAuthority.RunDeliveryOnce(ctx, now.Add(3*time.Second))
	var rebindStatus, rebindCode string
	if err := r.DB.QueryRowContext(ctx, `SELECT status,last_error_code FROM workspace_webhook_deliveries WHERE id=?`, rebindDelivery.ID).Scan(&rebindStatus, &rebindCode); err != nil {
		return nil, nil, err
	}

	redirectReceiver := newWebhookReceiver(302)
	redirectReceiver.redirects = []string{"http://169.254.169.254/latest/meta-data"}
	defer redirectReceiver.close()
	redirectResolver := newFixtureResolver()
	redirectResolver.set("redirect.example.com", []string{"1.1.1.1"})
	redirectDialer := &localFixtureDialer{address: redirectReceiver.address()}
	redirectAuthority, err := newWebhookAuthority(r, redirectResolver, redirectDialer)
	if err != nil {
		return nil, nil, err
	}
	redirectHook, err := redirectAuthority.Create(ctx, "ws-t027", "owner-t027", adminaccess.WorkspaceWebhookInput{
		Name: "Redirect", EndpointURL: redirectReceiver.endpoint("redirect.example.com"), Events: []string{"link.updated"},
	}, "t027-redirect-create", now.Add(4*time.Second))
	if err != nil {
		return nil, nil, err
	}
	redirectDelivery, err := redirectAuthority.QueueDelivery(ctx, "ws-t027", redirectHook.Webhook.ID, "evt-t027-redirect", "link.updated", mustJSONRaw(`{"marker":"redirect"}`), "t027-redirect-event", now.Add(5*time.Second))
	if err != nil {
		return nil, nil, err
	}
	redirectWorked, redirectErr := redirectAuthority.RunDeliveryOnce(ctx, now.Add(6*time.Second))
	var redirectStatus, redirectCode string
	if err := r.DB.QueryRowContext(ctx, `SELECT status,last_error_code FROM workspace_webhook_deliveries WHERE id=?`, redirectDelivery.ID).Scan(&redirectStatus, &redirectCode); err != nil {
		return nil, nil, err
	}

	failedRows, err := scalarWebhook(ctx, r.DB, `SELECT COUNT(*) FROM workspace_webhook_deliveries WHERE workspace_id=? AND status='failed' AND last_error_code='unsafe_destination'`, "ws-t027")
	if err != nil {
		return nil, nil, err
	}
	checks := map[string]bool{
		"private_linklocal_metadata_reserved_mixed_rejected": unsafeRejected,
		"dns_rebind_failed_closed":                           rebindWorked && rebindErr != nil && rebindStatus == "failed" && rebindCode == "unsafe_destination",
		"dns_rebind_blocked_before_socket":                   rebindDialer.count() == 0 && len(rebindReceiver.snapshot()) == 0 && rebindResolver.count("rebind.example.com") >= 3,
		"redirect_hop_revalidated":                           redirectWorked && redirectErr != nil && redirectStatus == "failed" && redirectCode == "unsafe_destination",
		"redirect_private_target_never_connected":            redirectDialer.count() == 1 && len(redirectReceiver.snapshot()) == 1,
		"unsafe_failures_durable":                            failedRows == 2,
	}
	counts := map[string]int{
		"unsafe_literals_and_mixed": len(unsafeURLs), "unsafe_delivery_failures": failedRows,
		"rebind_dns_calls": rebindResolver.count("rebind.example.com"), "rebind_socket_calls": rebindDialer.count(), "redirect_socket_calls": redirectDialer.count(),
	}
	return checks, counts, nil
}
