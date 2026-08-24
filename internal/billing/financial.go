package billing

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type PaymentRecord struct {
	ID                    uint64            `json:"id"`
	WorkspaceID           string            `json:"workspace_id"`
	OrderID               string            `json:"order_id"`
	Provider              Provider          `json:"provider"`
	ProviderTransactionID string            `json:"provider_transaction_id"`
	Money                 Money             `json:"money"`
	Status                TransactionStatus `json:"status"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

func (s *Store) ListWorkspaceInvoices(ctx context.Context, workspaceID string, limit int) ([]Invoice, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" {
		return nil, ErrInvalidInput
	}
	limit = normalizeFinancialLimit(limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,order_id,currency,amount_minor,status,issued_at,paid_at,refunded_at,created_at
FROM billing_invoices WHERE workspace_id=? ORDER BY issued_at DESC,id DESC LIMIT ?`, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Invoice{}
	for rows.Next() {
		item, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWorkspacePayments(ctx context.Context, workspaceID string, limit int) ([]PaymentRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" {
		return nil, ErrInvalidInput
	}
	return s.listPayments(ctx, `
SELECT id,workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at
FROM billing_transactions WHERE workspace_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, workspaceID, normalizeFinancialLimit(limit))
}

func (s *Store) ListAdminPayments(ctx context.Context, limit int) ([]PaymentRecord, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	return s.listPayments(ctx, `
SELECT id,workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at
FROM billing_transactions ORDER BY created_at DESC,id DESC LIMIT ?`, normalizeFinancialLimit(limit))
}

func (s *Store) GetAdminPayment(ctx context.Context, paymentID uint64) (PaymentRecord, error) {
	if s == nil || s.db == nil || paymentID == 0 {
		return PaymentRecord{}, ErrInvalidInput
	}
	var item PaymentRecord
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at
FROM billing_transactions WHERE id=?`, paymentID).Scan(
		&item.ID, &item.WorkspaceID, &item.OrderID, &item.Provider, &item.ProviderTransactionID,
		&item.Money.Currency, &item.Money.AmountMinor, &item.Status, &item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PaymentRecord{}, ErrNotFound
	}
	if err != nil {
		return PaymentRecord{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return item, nil
}

func (s *Store) ListAdminInvoices(ctx context.Context, limit int) ([]Invoice, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalidInput
	}
	limit = normalizeFinancialLimit(limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,order_id,currency,amount_minor,status,issued_at,paid_at,refunded_at,created_at
FROM billing_invoices ORDER BY issued_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Invoice{}
	for rows.Next() {
		item, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listPayments(ctx context.Context, query string, args ...any) ([]PaymentRecord, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PaymentRecord{}
	for rows.Next() {
		var item PaymentRecord
		if err := rows.Scan(
			&item.ID, &item.WorkspaceID, &item.OrderID, &item.Provider, &item.ProviderTransactionID,
			&item.Money.Currency, &item.Money.AmountMinor, &item.Status, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.UpdatedAt = item.UpdatedAt.UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanInvoice(scanner rowScannerBilling) (Invoice, error) {
	var item Invoice
	var paidAt, refundedAt sql.NullTime
	if err := scanner.Scan(
		&item.ID, &item.WorkspaceID, &item.OrderID, &item.Money.Currency, &item.Money.AmountMinor,
		&item.Status, &item.IssuedAt, &paidAt, &refundedAt, &item.CreatedAt,
	); err != nil {
		return Invoice{}, err
	}
	item.IssuedAt = item.IssuedAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	if paidAt.Valid {
		value := paidAt.Time.UTC()
		item.PaidAt = &value
	}
	if refundedAt.Valid {
		value := refundedAt.Time.UTC()
		item.RefundedAt = &value
	}
	return item, nil
}

type rowScannerBilling interface {
	Scan(...any) error
}

func normalizeFinancialLimit(limit int) int {
	if limit <= 0 || limit > 100 {
		return 50
	}
	return limit
}
