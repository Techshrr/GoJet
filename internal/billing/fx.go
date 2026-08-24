package billing

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"regexp"
	"strings"
	"time"
)

type FXStatus string

const (
	FXCurrent       FXStatus = "current"
	FXStale         FXStatus = "stale"
	FXProviderError FXStatus = "provider-error"
	FXOverride      FXStatus = "override"
)

type FXRate struct {
	BaseCurrency   string    `json:"base_currency"`
	QuoteCurrency  string    `json:"quote_currency"`
	Rate           string    `json:"rate"`
	Source         string    `json:"source"`
	AsOf           time.Time `json:"as_of"`
	Status         FXStatus  `json:"status"`
	OverrideReason string    `json:"override_reason,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type UpsertFXRateInput struct {
	BaseCurrency   string
	QuoteCurrency  string
	Rate           string
	Source         string
	AsOf           time.Time
	Status         FXStatus
	OverrideReason string
	ActorID        string
	CorrelationID  string
}

type MarkFXProviderErrorInput struct {
	BaseCurrency  string
	QuoteCurrency string
	Source        string
	AsOf          time.Time
	ActorID       string
	CorrelationID string
}

var fxRatePattern = regexp.MustCompile(`^[0-9]{1,16}(?:\.[0-9]{1,12})?$`)
var fxSourcePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:-]{0,95}$`)

func (s *Store) ListFXRates(ctx context.Context) ([]FXRate, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT base_currency,quote_currency,CAST(rate AS CHAR),source,as_of,status,COALESCE(override_reason,''),updated_at
FROM billing_fx_rates ORDER BY base_currency,quote_currency`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []FXRate{}
	for rows.Next() {
		var item FXRate
		if err := rows.Scan(&item.BaseCurrency, &item.QuoteCurrency, &item.Rate, &item.Source, &item.AsOf, &item.Status, &item.OverrideReason, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.AsOf = item.AsOf.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetFXRate(ctx context.Context, base, quote string) (FXRate, error) {
	base = strings.TrimSpace(base)
	quote = strings.TrimSpace(quote)
	if s == nil || s.db == nil || !validFXPair(base, quote) {
		return FXRate{}, ErrInvalidInput
	}
	var item FXRate
	err := s.db.QueryRowContext(ctx, `
SELECT base_currency,quote_currency,CAST(rate AS CHAR),source,as_of,status,COALESCE(override_reason,''),updated_at
FROM billing_fx_rates WHERE base_currency=? AND quote_currency=?`, base, quote).Scan(
		&item.BaseCurrency, &item.QuoteCurrency, &item.Rate, &item.Source, &item.AsOf,
		&item.Status, &item.OverrideReason, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return FXRate{}, ErrNotFound
	}
	if err != nil {
		return FXRate{}, err
	}
	item.AsOf = item.AsOf.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func (s *Store) UpsertFXRate(ctx context.Context, input UpsertFXRateInput) (FXRate, error) {
	normalizeFXInput(&input)
	if s == nil || s.db == nil || !validFXPair(input.BaseCurrency, input.QuoteCurrency) ||
		!validPositiveFXRate(input.Rate) || !fxSourcePattern.MatchString(input.Source) || input.AsOf.IsZero() ||
		(input.Status != FXCurrent && input.Status != FXStale && input.Status != FXOverride) ||
		input.ActorID == "" || input.CorrelationID == "" {
		return FXRate{}, ErrInvalidInput
	}
	if input.Status == FXOverride {
		if input.OverrideReason == "" || len(input.OverrideReason) > 500 {
			return FXRate{}, ErrInvalidInput
		}
	} else if input.OverrideReason != "" {
		return FXRate{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return FXRate{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
INSERT INTO billing_fx_rates
(base_currency,quote_currency,rate,source,as_of,status,override_reason)
VALUES (?,?,?,?,?,?,NULLIF(?,''))
ON DUPLICATE KEY UPDATE
rate=VALUES(rate),source=VALUES(source),as_of=VALUES(as_of),status=VALUES(status),override_reason=VALUES(override_reason),updated_at=CURRENT_TIMESTAMP(6)`,
		input.BaseCurrency, input.QuoteCurrency, input.Rate, input.Source, input.AsOf.UTC(), input.Status, input.OverrideReason); err != nil {
		return FXRate{}, err
	}
	action := "billing.fx.update"
	if input.Status == FXOverride {
		action = "billing.fx.override"
	}
	if err := appendAuditTx(ctx, tx, "", input.ActorID, action, "billing_fx_rate", input.BaseCurrency+"/"+input.QuoteCurrency, input.OverrideReason, input.CorrelationID, "success", map[string]any{
		"base_currency": input.BaseCurrency, "quote_currency": input.QuoteCurrency, "source": input.Source,
		"as_of": input.AsOf.UTC().Format(time.RFC3339Nano), "status": input.Status,
	}); err != nil {
		return FXRate{}, err
	}
	if err := tx.Commit(); err != nil {
		return FXRate{}, err
	}
	return s.GetFXRate(ctx, input.BaseCurrency, input.QuoteCurrency)
}

func (s *Store) MarkFXProviderError(ctx context.Context, input MarkFXProviderErrorInput) (FXRate, error) {
	input.BaseCurrency = strings.TrimSpace(input.BaseCurrency)
	input.QuoteCurrency = strings.TrimSpace(input.QuoteCurrency)
	input.Source = strings.TrimSpace(input.Source)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if s == nil || s.db == nil || !validFXPair(input.BaseCurrency, input.QuoteCurrency) || !fxSourcePattern.MatchString(input.Source) ||
		input.AsOf.IsZero() || input.ActorID == "" || input.CorrelationID == "" {
		return FXRate{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return FXRate{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `
SELECT 1 FROM billing_fx_rates WHERE base_currency=? AND quote_currency=? FOR UPDATE`, input.BaseCurrency, input.QuoteCurrency).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return FXRate{}, ErrNotFound
		}
		return FXRate{}, err
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE billing_fx_rates
SET source=?,as_of=?,status='provider-error',override_reason=NULL,updated_at=CURRENT_TIMESTAMP(6)
WHERE base_currency=? AND quote_currency=?`, input.Source, input.AsOf.UTC(), input.BaseCurrency, input.QuoteCurrency); err != nil {
		return FXRate{}, err
	}
	if err := appendAuditTx(ctx, tx, "", input.ActorID, "billing.fx.provider_error", "billing_fx_rate", input.BaseCurrency+"/"+input.QuoteCurrency, "provider_error", input.CorrelationID, "success", map[string]any{
		"base_currency": input.BaseCurrency, "quote_currency": input.QuoteCurrency, "source": input.Source,
		"as_of": input.AsOf.UTC().Format(time.RFC3339Nano), "status": FXProviderError,
	}); err != nil {
		return FXRate{}, err
	}
	if err := tx.Commit(); err != nil {
		return FXRate{}, err
	}
	return s.GetFXRate(ctx, input.BaseCurrency, input.QuoteCurrency)
}

func normalizeFXInput(input *UpsertFXRateInput) {
	input.BaseCurrency = strings.TrimSpace(input.BaseCurrency)
	input.QuoteCurrency = strings.TrimSpace(input.QuoteCurrency)
	input.Rate = strings.TrimSpace(input.Rate)
	input.Source = strings.TrimSpace(input.Source)
	input.OverrideReason = strings.TrimSpace(input.OverrideReason)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
}

func validFXPair(base, quote string) bool {
	return isoCurrency.MatchString(base) && isoCurrency.MatchString(quote) && base != quote
}

func validPositiveFXRate(value string) bool {
	if !fxRatePattern.MatchString(value) {
		return false
	}
	rate, ok := new(big.Rat).SetString(value)
	return ok && rate.Sign() > 0
}
