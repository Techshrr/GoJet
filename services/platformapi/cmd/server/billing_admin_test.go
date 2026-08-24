package main

import (
	"context"
	"testing"

	"github.com/Techshrr/GoJet/internal/billing"
)

func TestBillingTestAdminPermissionResolverUsesServerActorOnly(t *testing.T) {
	resolver := billingTestAdminPermissionResolver{actorID: "admin-fixture"}
	allowed, err := resolver.HasPermission(context.Background(), billing.RequestPrincipal{UserID: "admin-fixture"}, billing.BillingManagePermission)
	if err != nil || !allowed {
		t.Fatalf("expected configured test actor to be allowed: allowed=%v err=%v", allowed, err)
	}
	allowed, err = resolver.HasPermission(context.Background(), billing.RequestPrincipal{UserID: "other"}, billing.BillingManagePermission)
	if err != nil || allowed {
		t.Fatalf("unconfigured actor must be denied: allowed=%v err=%v", allowed, err)
	}
	allowed, err = resolver.HasPermission(context.Background(), billing.RequestPrincipal{UserID: "admin-fixture"}, "other.permission")
	if err != nil || allowed {
		t.Fatalf("wrong permission must be denied: allowed=%v err=%v", allowed, err)
	}
}
