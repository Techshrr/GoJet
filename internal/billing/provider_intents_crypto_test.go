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

func TestCryptoProviderReferenceBoundary(t *testing.T) {
	for _, value := range []string{
		"410000000000000000000000000000000000000000",
		"41abcdefabcdefabcdefabcdefabcdefabcdefabcd",
	} {
		if !canonicalCryptoProviderReference(value) {
			t.Fatalf("valid crypto provider reference rejected: %q", value)
		}
	}
	for _, value := range []string{
		"",
		"TJRabPrwbZy45sbavfcjinPJC18kjpRTv8",
		"41ABCDEFabcdefabcdefabcdefabcdefabcdefabcd",
		"40abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		"41abcdefabcdefabcdefabcdefabcdefabcdefabc",
		"41abcdefabcdefabcdefabcdefabcdefabcdefabcde",
	} {
		if canonicalCryptoProviderReference(value) {
			t.Fatalf("invalid crypto provider reference accepted: %q", value)
		}
	}
}

func TestCryptoProviderIntentResolutionKeepsCreationDisabled(t *testing.T) {
	if providerIntentCreationEnabled(ProviderCrypto) {
		t.Fatal("generic crypto provider-intent creation must remain disabled")
	}
}

func TestCryptoProviderIntentMySQLResolution(t *testing.T) {
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
	workspaceID := "ws_crypto_pint_" + suffix
	planCode := "crypto_pint_" + suffix
	intentID := "pint_crypto_" + suffix
	merchantReference := "gj_crypto_" + suffix
	providerReference := "41abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	now := time.Now().UTC().Truncate(time.Microsecond)

	if _, err := db.ExecContext(ctx, `
INSERT INTO workspaces (id,name,status,version,created_by,created_at,updated_at)
VALUES (?,?,'active',1,'p20-d016-crypto-test',?,?)`, workspaceID, "Crypto Provider Intent Test", now, now); err != nil {
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
VALUES (?,?,'active','USD',1234,'one_time',1,?,?)`, planCode, "Crypto Provider Intent Plan", now, now)
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
		IdempotencyKey: "crypto-provider-intent-" + suffix,
		Now:            now,
	})
	if err != nil || !created {
		t.Fatalf("create order created=%v err=%v", created, err)
	}

	expires := now.Add(30 * time.Minute)
	if _, err := db.ExecContext(ctx, `
INSERT INTO billing_provider_intents
(id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at,created_at,updated_at)
VALUES (?,?,?,'crypto',?,?,'token','USDTTRC20',12340000,6,'active',?,?,?)`,
		intentID, workspaceID, order.ID, merchantReference, providerReference, expires, now, now); err != nil {
		t.Fatal(err)
	}

	intent, err := store.ResolveCryptoProviderIntentByProviderReference(ctx, providerReference, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if intent.ID != intentID || intent.Provider != ProviderCrypto || intent.OrderID != order.ID || intent.WorkspaceID != workspaceID || intent.ProviderReference != providerReference || intent.SettlementAssetKind != SettlementAssetToken || intent.SettlementAsset != cryptoSettlementAsset || intent.SettlementScale == nil || *intent.SettlementScale != cryptoSettlementScale || intent.SettlementAmountUnits != 12340000 {
		t.Fatalf("unexpected crypto intent: %+v", intent)
	}

	if _, _, err := store.CreateProviderIntent(ctx, CreateProviderIntentInput{
		WorkspaceID: workspaceID, OrderID: order.ID, Provider: ProviderCrypto, Now: now,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("generic crypto creation unexpectedly enabled, err=%v", err)
	}
	if _, _, err := store.BindProviderReference(ctx, BindProviderReferenceInput{
		IntentID: intentID, Provider: ProviderCrypto, ProviderReference: providerReference, Now: now,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("generic crypto provider-reference binding unexpectedly enabled, err=%v", err)
	}
	if _, err := store.ResolveCryptoProviderIntentByProviderReference(ctx, providerReference, expires.Add(time.Microsecond)); !errors.Is(err, ErrConflict) {
		t.Fatalf("expired crypto intent should fail closed, err=%v", err)
	}
}
