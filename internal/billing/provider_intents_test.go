package billing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestProviderIntentCreationProviderBoundary(t *testing.T) {
	for _, provider := range []Provider{ProviderStripe, ProviderWeChat, ProviderPayPal} {
		if !providerIntentCreationEnabled(provider) {
			t.Fatalf("provider %q should be enabled for frozen payment-side intent creation", provider)
		}
	}
	for _, provider := range []Provider{ProviderAlipay, ProviderEpay, ProviderCrypto, Provider("unknown")} {
		if providerIntentCreationEnabled(provider) {
			t.Fatalf("provider %q unexpectedly enabled", provider)
		}
	}
}

func TestProviderReferenceValidation(t *testing.T) {
	for _, value := range []string{"pi_123", "5O190127TN364715T", "wx-prepay_123", "evt:123.abc"} {
		if !validProviderReference(value) {
			t.Fatalf("valid reference rejected: %q", value)
		}
	}
	for _, value := range []string{"", " has-space", "has space", "x/y", "秘密"} {
		if validProviderReference(value) {
			t.Fatalf("invalid reference accepted: %q", value)
		}
	}
}

func TestProviderIntentUsable(t *testing.T) {
	now := time.Date(2026, 9, 8, 15, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)
	if !providerIntentUsable(ProviderIntent{Status: ProviderIntentActive}, now) {
		t.Fatal("active non-expiring intent rejected")
	}
	if !providerIntentUsable(ProviderIntent{Status: ProviderIntentActive, ExpiresAt: &future}, now) {
		t.Fatal("active future intent rejected")
	}
	if providerIntentUsable(ProviderIntent{Status: ProviderIntentActive, ExpiresAt: &past}, now) {
		t.Fatal("expired intent accepted")
	}
	if providerIntentUsable(ProviderIntent{Status: ProviderIntentCanceled}, now) {
		t.Fatal("canceled intent accepted")
	}
}

func TestProviderIntentMySQLLifecycle(t *testing.T) {
	dsn := os.Getenv("GOJET_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("GOJET_TEST_MYSQL_DSN not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	workspaceID := "ws_pint_" + suffix
	planCode := "pint_" + suffix
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (id,name,status,version,created_by,created_at,updated_at)
VALUES (?,?,'active',1,'p20-d016-test',?,?)`, workspaceID, "Provider Intent Test", now, now); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM billing_provider_intents WHERE workspace_id=?`, workspaceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM billing_invoices WHERE workspace_id=?`, workspaceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM billing_orders WHERE workspace_id=?`, workspaceID)
		_, _ = db.ExecContext(ctx, `DELETE FROM billing_plans WHERE code=?`, planCode)
		_, _ = db.ExecContext(ctx, `DELETE FROM workspaces WHERE id=?`, workspaceID)
	}()

	result, err := db.ExecContext(ctx, `
INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version,created_at,updated_at)
VALUES (?,?,'active','USD',2100,'one_time',1,?,?)`, planCode, "Provider Intent Plan", now, now)
	if err != nil {
		t.Fatal(err)
	}
	planID64, err := result.LastInsertId()
	if err != nil || planID64 <= 0 {
		t.Fatalf("plan id=%d err=%v", planID64, err)
	}

	store := NewStore(db)
	order, created, err := store.CreateOrder(ctx, CreateOrderInput{
		WorkspaceID:    workspaceID,
		PlanID:         uint64(planID64),
		Kind:           OrderNew,
		IdempotencyKey: "provider-intent-" + suffix,
		Now:            now,
	})
	if err != nil || !created {
		t.Fatalf("create order created=%v err=%v", created, err)
	}

	expires := now.Add(30 * time.Minute)
	intent, created, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID,
		OrderID:     order.ID,
		Provider:    ProviderStripe,
		ExpiresAt:   &expires,
		Now:         now,
	})
	if err != nil || !created {
		t.Fatalf("create intent created=%v err=%v", created, err)
	}
	if intent.Provider != ProviderStripe || intent.OrderID != order.ID || intent.WorkspaceID != workspaceID ||
		intent.SettlementAssetKind != SettlementAssetFiat || intent.SettlementAsset != "USD" || intent.SettlementAmountUnits != 2100 || intent.SettlementScale != nil ||
		intent.ProviderReference != "" || intent.Status != ProviderIntentActive || !validProviderReference(intent.MerchantReference) {
		t.Fatalf("unexpected intent: %+v", intent)
	}

	replay, created, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID, OrderID: order.ID, Provider: ProviderStripe, ExpiresAt: &expires, Now: now.Add(time.Second),
	})
	if err != nil || created || replay.ID != intent.ID {
		t.Fatalf("intent replay created=%v id=%q want=%q err=%v", created, replay.ID, intent.ID, err)
	}
	if _, _, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID, OrderID: order.ID, Provider: ProviderPayPal, Now: now.Add(2 * time.Second),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("cross-provider live intent should conflict, err=%v", err)
	}

	bound, changed, err := store.BindProviderReference(ctx, BindProviderReferenceInput{
		IntentID: intent.ID, Provider: ProviderStripe, ProviderReference: "pi_" + suffix, Now: now.Add(3 * time.Second),
	})
	if err != nil || !changed || bound.ProviderReference == "" {
		t.Fatalf("bind changed=%v intent=%+v err=%v", changed, bound, err)
	}
	boundAgain, changed, err := store.BindProviderReference(ctx, BindProviderReferenceInput{
		IntentID: intent.ID, Provider: ProviderStripe, ProviderReference: bound.ProviderReference, Now: now.Add(4 * time.Second),
	})
	if err != nil || changed || boundAgain.ProviderReference != bound.ProviderReference {
		t.Fatalf("idempotent bind changed=%v intent=%+v err=%v", changed, boundAgain, err)
	}
	if _, _, err := store.BindProviderReference(ctx, BindProviderReferenceInput{
		IntentID: intent.ID, Provider: ProviderStripe, ProviderReference: "pi_other_" + suffix, Now: now.Add(5 * time.Second),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("provider reference rebind should conflict, err=%v", err)
	}

	byProvider, err := store.ResolveProviderIntentByProviderReference(ctx, ProviderStripe, bound.ProviderReference, now.Add(6*time.Second))
	if err != nil || byProvider.ID != intent.ID {
		t.Fatalf("provider resolution intent=%+v err=%v", byProvider, err)
	}
	byMerchant, err := store.ResolveProviderIntentByMerchantReference(ctx, ProviderStripe, intent.MerchantReference, now.Add(6*time.Second))
	if err != nil || byMerchant.ID != intent.ID {
		t.Fatalf("merchant resolution intent=%+v err=%v", byMerchant, err)
	}

	canceled, changed, err := store.CancelProviderIntent(ctx, workspaceID, intent.ID, now.Add(7*time.Second))
	if err != nil || !changed || canceled.Status != ProviderIntentCanceled || canceled.CanceledAt == nil {
		t.Fatalf("cancel changed=%v intent=%+v err=%v", changed, canceled, err)
	}
	if _, err := store.ResolveProviderIntentByProviderReference(ctx, ProviderStripe, bound.ProviderReference, now.Add(8*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("canceled intent should fail closed, err=%v", err)
	}

	paypalExpiry := now.Add(10 * time.Minute)
	paypalIntent, created, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID, OrderID: order.ID, Provider: ProviderPayPal, ExpiresAt: &paypalExpiry, Now: now.Add(9 * time.Second),
	})
	if err != nil || !created || paypalIntent.Provider != ProviderPayPal {
		t.Fatalf("paypal intent created=%v intent=%+v err=%v", created, paypalIntent, err)
	}
	if _, err := store.ResolveProviderIntentByMerchantReference(ctx, ProviderPayPal, paypalIntent.MerchantReference, paypalExpiry.Add(time.Microsecond)); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired intent should fail closed, err=%v", err)
	}

	if _, _, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID, OrderID: order.ID, Provider: ProviderCrypto, Now: now,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("crypto token intent must remain disabled before crypto authority is frozen, err=%v", err)
	}
}
