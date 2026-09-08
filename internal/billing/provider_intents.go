package billing

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
)

type SettlementAssetKind string

const (
	SettlementAssetFiat  SettlementAssetKind = "fiat"
	SettlementAssetToken SettlementAssetKind = "token"
)

type ProviderIntentStatus string

const (
	ProviderIntentActive   ProviderIntentStatus = "active"
	ProviderIntentCanceled ProviderIntentStatus = "canceled"
)

type ProviderIntent struct {
	ID                    string               `json:"id"`
	WorkspaceID           string               `json:"workspace_id"`
	OrderID               string               `json:"order_id"`
	Provider              Provider             `json:"provider"`
	MerchantReference     string               `json:"merchant_reference"`
	ProviderReference     string               `json:"provider_reference,omitempty"`
	SettlementAssetKind   SettlementAssetKind  `json:"settlement_asset_kind"`
	SettlementAsset       string               `json:"settlement_asset"`
	SettlementAmountUnits int64                `json:"settlement_amount_units"`
	SettlementScale       *uint8               `json:"settlement_scale,omitempty"`
	Status                ProviderIntentStatus `json:"status"`
	ExpiresAt             *time.Time           `json:"expires_at,omitempty"`
	CanceledAt            *time.Time           `json:"canceled_at,omitempty"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

type CreateProviderIntentInput struct {
	WorkspaceID string
	OrderID     string
	Provider    Provider
	ExpiresAt   *time.Time
	Now         time.Time
}

type BindProviderReferenceInput struct {
	IntentID          string
	Provider          Provider
	ProviderReference string
	Now               time.Time
}

var providerReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,190}$`)

// CreateProviderIntent creates the server-authoritative pre-callback binding for
// the production provider families whose payment-side authority is frozen in
// P20-DR001. The initial implementation intentionally inherits the GoJet order's
// ISO-3 Money exactly. Cross-currency and token settlement intents require their
// own separately frozen quote authority and cannot be caller-supplied here.
func (s *Store) CreateProviderIntent(ctx context.Context, input CreateProviderIntentInput) (ProviderIntent, bool, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.OrderID = strings.TrimSpace(input.OrderID)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.OrderID == "" || !providerIntentCreationEnabled(input.Provider) || input.Now.IsZero() {
		return ProviderIntent{}, false, ErrInvalidInput
	}
	now := input.Now.UTC()
	var expiresAt *time.Time
	if input.ExpiresAt != nil {
		v := input.ExpiresAt.UTC()
		if !v.After(now) {
			return ProviderIntent{}, false, ErrInvalidInput
		}
		expiresAt = &v
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ProviderIntent{}, false, err
	}
	defer tx.Rollback()

	order, err := loadOrderForUpdate(ctx, tx, input.OrderID)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if order.WorkspaceID != input.WorkspaceID || (order.Status != OrderPending && order.Status != OrderProcessing) || order.Money.Validate(false) != nil {
		return ProviderIntent{}, false, ErrConflict
	}

	// The locked order row serializes provider-intent creation for this order.
	// One live payment attempt is authoritative at a time, even across providers.
	if existing, err := loadLiveProviderIntentForOrder(ctx, tx, input.OrderID, now); err == nil {
		if existing.Provider != input.Provider {
			return ProviderIntent{}, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return ProviderIntent{}, false, err
		}
		return existing, false, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ProviderIntent{}, false, err
	}

	intentID, err := newOpaqueID("pint_")
	if err != nil {
		return ProviderIntent{}, false, err
	}
	merchantReference, err := newOpaqueID("gj_")
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO billing_provider_intents
(id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at,canceled_at,created_at,updated_at)
VALUES (?,?,?,?,?,NULL,'fiat',?,?,NULL,'active',?,NULL,?,?)`,
		intentID, input.WorkspaceID, input.OrderID, input.Provider, merchantReference,
		order.Money.Currency, order.Money.AmountMinor, expiresAt, now, now); err != nil {
		return ProviderIntent{}, false, wrapConflict(err)
	}
	intent, err := loadProviderIntent(ctx, tx, intentID, false)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderIntent{}, false, err
	}
	return intent, true, nil
}

// BindProviderReference attaches the server-created provider checkout/payment
// object identity (for example a Stripe PaymentIntent or PayPal Order) exactly
// once. Rebinding an intent to a different provider object is forbidden.
func (s *Store) BindProviderReference(ctx context.Context, input BindProviderReferenceInput) (ProviderIntent, bool, error) {
	input.IntentID = strings.TrimSpace(input.IntentID)
	if s == nil || s.db == nil || input.IntentID == "" || !providerIntentCreationEnabled(input.Provider) || !validProviderReference(input.ProviderReference) || input.Now.IsZero() {
		return ProviderIntent{}, false, ErrInvalidInput
	}
	now := input.Now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ProviderIntent{}, false, err
	}
	defer tx.Rollback()
	intent, err := loadProviderIntent(ctx, tx, input.IntentID, true)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if intent.Provider != input.Provider || !providerIntentUsable(intent, now) {
		return ProviderIntent{}, false, ErrConflict
	}
	if intent.ProviderReference != "" {
		if intent.ProviderReference != input.ProviderReference {
			return ProviderIntent{}, false, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return ProviderIntent{}, false, err
		}
		return intent, false, nil
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE billing_provider_intents
SET provider_reference=?,updated_at=?
WHERE id=? AND status='active' AND provider_reference IS NULL`, input.ProviderReference, now, input.IntentID); err != nil {
		return ProviderIntent{}, false, wrapConflict(err)
	}
	intent, err = loadProviderIntent(ctx, tx, input.IntentID, false)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderIntent{}, false, err
	}
	return intent, true, nil
}

func (s *Store) ResolveProviderIntentByProviderReference(ctx context.Context, provider Provider, providerReference string, now time.Time) (ProviderIntent, error) {
	if s == nil || s.db == nil || !providerIntentCreationEnabled(provider) || !validProviderReference(providerReference) || now.IsZero() {
		return ProviderIntent{}, ErrInvalidInput
	}
	intent, err := loadProviderIntentByReference(ctx, s.db, provider, "provider_reference", providerReference)
	if err != nil {
		return ProviderIntent{}, err
	}
	if !providerIntentUsable(intent, now.UTC()) {
		return ProviderIntent{}, ErrConflict
	}
	return intent, nil
}

func (s *Store) ResolveProviderIntentByMerchantReference(ctx context.Context, provider Provider, merchantReference string, now time.Time) (ProviderIntent, error) {
	if s == nil || s.db == nil || !providerIntentCreationEnabled(provider) || !validProviderReference(merchantReference) || now.IsZero() {
		return ProviderIntent{}, ErrInvalidInput
	}
	intent, err := loadProviderIntentByReference(ctx, s.db, provider, "merchant_reference", merchantReference)
	if err != nil {
		return ProviderIntent{}, err
	}
	if !providerIntentUsable(intent, now.UTC()) {
		return ProviderIntent{}, ErrConflict
	}
	return intent, nil
}

func (s *Store) CancelProviderIntent(ctx context.Context, workspaceID, intentID string, now time.Time) (ProviderIntent, bool, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	intentID = strings.TrimSpace(intentID)
	if s == nil || s.db == nil || workspaceID == "" || intentID == "" || now.IsZero() {
		return ProviderIntent{}, false, ErrInvalidInput
	}
	now = now.UTC()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ProviderIntent{}, false, err
	}
	defer tx.Rollback()
	intent, err := loadProviderIntent(ctx, tx, intentID, true)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if intent.WorkspaceID != workspaceID {
		return ProviderIntent{}, false, ErrNotFound
	}
	if intent.Status == ProviderIntentCanceled {
		if err := tx.Commit(); err != nil {
			return ProviderIntent{}, false, err
		}
		return intent, false, nil
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE billing_provider_intents
SET status='canceled',canceled_at=?,updated_at=?
WHERE id=? AND status='active'`, now, now, intentID); err != nil {
		return ProviderIntent{}, false, err
	}
	intent, err = loadProviderIntent(ctx, tx, intentID, false)
	if err != nil {
		return ProviderIntent{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ProviderIntent{}, false, err
	}
	return intent, true, nil
}

func providerIntentCreationEnabled(provider Provider) bool {
	switch provider {
	case ProviderStripe, ProviderWeChat, ProviderPayPal:
		return true
	default:
		return false
	}
}

func validProviderReference(value string) bool {
	return providerReferencePattern.MatchString(value)
}

func providerIntentUsable(intent ProviderIntent, now time.Time) bool {
	if intent.Status != ProviderIntentActive {
		return false
	}
	return intent.ExpiresAt == nil || intent.ExpiresAt.After(now)
}

func loadLiveProviderIntentForOrder(ctx context.Context, tx *sql.Tx, orderID string, now time.Time) (ProviderIntent, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at,canceled_at,created_at,updated_at
FROM billing_provider_intents
WHERE order_id=? AND status='active' AND (expires_at IS NULL OR expires_at>?)
ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE`, orderID, now)
	return scanProviderIntent(row)
}

func loadProviderIntent(ctx context.Context, q rowQueryer, intentID string, forUpdate bool) (ProviderIntent, error) {
	query := `
SELECT id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at,canceled_at,created_at,updated_at
FROM billing_provider_intents WHERE id=?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	intent, err := scanProviderIntent(q.QueryRowContext(ctx, query, intentID))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderIntent{}, ErrNotFound
	}
	return intent, err
}

func loadProviderIntentByReference(ctx context.Context, q rowQueryer, provider Provider, column, value string) (ProviderIntent, error) {
	if column != "provider_reference" && column != "merchant_reference" {
		return ProviderIntent{}, ErrInvalidInput
	}
	query := `
SELECT id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at,canceled_at,created_at,updated_at
FROM billing_provider_intents WHERE provider=? AND ` + column + `=?`
	intent, err := scanProviderIntent(q.QueryRowContext(ctx, query, provider, value))
	if errors.Is(err, sql.ErrNoRows) {
		return ProviderIntent{}, ErrNotFound
	}
	return intent, err
}

type providerIntentScanner interface {
	Scan(...any) error
}

func scanProviderIntent(row providerIntentScanner) (ProviderIntent, error) {
	var intent ProviderIntent
	var provider, assetKind, status string
	var providerReference sql.NullString
	var scale sql.NullInt64
	var expiresAt, canceledAt sql.NullTime
	if err := row.Scan(
		&intent.ID, &intent.WorkspaceID, &intent.OrderID, &provider, &intent.MerchantReference, &providerReference,
		&assetKind, &intent.SettlementAsset, &intent.SettlementAmountUnits, &scale, &status,
		&expiresAt, &canceledAt, &intent.CreatedAt, &intent.UpdatedAt,
	); err != nil {
		return ProviderIntent{}, err
	}
	intent.Provider = Provider(provider)
	intent.SettlementAssetKind = SettlementAssetKind(assetKind)
	intent.Status = ProviderIntentStatus(status)
	if providerReference.Valid {
		intent.ProviderReference = providerReference.String
	}
	if scale.Valid {
		if scale.Int64 < 0 || scale.Int64 > 255 {
			return ProviderIntent{}, ErrInvalidInput
		}
		v := uint8(scale.Int64)
		intent.SettlementScale = &v
	}
	if expiresAt.Valid {
		v := expiresAt.Time.UTC()
		intent.ExpiresAt = &v
	}
	if canceledAt.Valid {
		v := canceledAt.Time.UTC()
		intent.CanceledAt = &v
	}
	intent.CreatedAt = intent.CreatedAt.UTC()
	intent.UpdatedAt = intent.UpdatedAt.UTC()
	return intent, nil
}
