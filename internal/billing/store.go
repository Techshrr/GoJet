package billing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid billing input")
	ErrNotFound     = errors.New("billing resource not found")
	ErrConflict     = errors.New("billing conflict")
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return ErrInvalidInput
	}
	return s.db.PingContext(ctx)
}

func (s *Store) ListPublicPlans(ctx context.Context) ([]Plan, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,code,name,status,currency,amount_minor,billing_period,version,created_at,updated_at
FROM billing_plans WHERE status='active' ORDER BY amount_minor,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	plans := []Plan{}
	for rows.Next() {
		var p Plan
		if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Status, &p.Money.Currency, &p.Money.AmountMinor, &p.BillingPeriod, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.CreatedAt = p.CreatedAt.UTC()
		p.UpdatedAt = p.UpdatedAt.UTC()
		plans = append(plans, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range plans {
		items, err := s.listPlanEntitlements(ctx, plans[i].ID)
		if err != nil {
			return nil, err
		}
		plans[i].Entitlements = items
	}
	return plans, nil
}

func (s *Store) listPlanEntitlements(ctx context.Context, planID uint64) ([]PlanEntitlement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT capability,limit_value,unit,source_version FROM billing_plan_entitlements WHERE plan_id=? ORDER BY capability`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PlanEntitlement{}
	for rows.Next() {
		var item PlanEntitlement
		if err := rows.Scan(&item.Capability, &item.LimitValue, &item.Unit, &item.SourceVersion); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type CreateOrderInput struct {
	WorkspaceID    string
	PlanID         uint64
	Kind           OrderKind
	IdempotencyKey string
	Now            time.Time
}

func (s *Store) CreateOrder(ctx context.Context, input CreateOrderInput) (Order, bool, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if s == nil || s.db == nil || input.WorkspaceID == "" || input.PlanID == 0 || input.Now.IsZero() || len(input.IdempotencyKey) < 16 || len(input.IdempotencyKey) > 256 || !validOrderKind(input.Kind) {
		return Order{}, false, ErrInvalidInput
	}
	hash := sha256.Sum256([]byte(input.IdempotencyKey))
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Order{}, false, err
	}
	defer tx.Rollback()
	if existing, err := loadOrderByIdempotency(ctx, tx, input.WorkspaceID, hash[:]); err == nil {
		return existing, false, tx.Commit()
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Order{}, false, err
	}

	var money Money
	var status PlanStatus
	if err := tx.QueryRowContext(ctx, `SELECT status,currency,amount_minor FROM billing_plans WHERE id=? FOR SHARE`, input.PlanID).Scan(&status, &money.Currency, &money.AmountMinor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Order{}, false, ErrNotFound
		}
		return Order{}, false, err
	}
	if status != PlanActive || money.Validate(false) != nil {
		return Order{}, false, ErrConflict
	}
	orderID, err := newOpaqueID("ord_")
	if err != nil {
		return Order{}, false, err
	}
	invoiceID, err := newOpaqueID("inv_")
	if err != nil {
		return Order{}, false, err
	}
	now := input.Now.UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO billing_orders (id,workspace_id,plan_id,kind,currency,amount_minor,status,idempotency_key_hash,created_at,updated_at) VALUES (?,?,?,?,?,?,'pending',?,?,?)`, orderID, input.WorkspaceID, input.PlanID, input.Kind, money.Currency, money.AmountMinor, hash[:], now, now); err != nil {
		return Order{}, false, wrapConflict(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO billing_invoices (id,workspace_id,order_id,currency,amount_minor,status,issued_at,created_at) VALUES (?,?,?,?,?,'open',?,?)`, invoiceID, input.WorkspaceID, orderID, money.Currency, money.AmountMinor, now, now); err != nil {
		return Order{}, false, err
	}
	order, err := loadOrder(ctx, tx, orderID)
	if err != nil {
		return Order{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Order{}, false, err
	}
	return order, true, nil
}

func (s *Store) GetOrder(ctx context.Context, workspaceID, orderID string) (Order, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	orderID = strings.TrimSpace(orderID)
	if workspaceID == "" || orderID == "" {
		return Order{}, ErrInvalidInput
	}
	var o Order
	err := s.db.QueryRowContext(ctx, `SELECT id,workspace_id,plan_id,kind,currency,amount_minor,status,created_at,updated_at FROM billing_orders WHERE id=? AND workspace_id=?`, orderID, workspaceID).Scan(&o.ID, &o.WorkspaceID, &o.PlanID, &o.Kind, &o.Money.Currency, &o.Money.AmountMinor, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return o, nil
}

func (s *Store) ResolveWorkspaceEntitlement(ctx context.Context, workspaceID, capability string, now time.Time) (ResolvedEntitlement, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	capability = strings.TrimSpace(capability)
	if workspaceID == "" || capability == "" || now.IsZero() {
		return ResolvedEntitlement{}, ErrInvalidInput
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,workspace_id,capability,source_type,source_id,limit_value,starts_at,ends_at,revoked_at FROM entitlement_grants WHERE workspace_id=? AND capability=? ORDER BY id`, workspaceID, capability)
	if err != nil {
		return ResolvedEntitlement{}, err
	}
	defer rows.Close()
	grants := []EntitlementGrant{}
	for rows.Next() {
		var g EntitlementGrant
		var ends, revoked sql.NullTime
		if err := rows.Scan(&g.ID, &g.WorkspaceID, &g.Capability, &g.SourceType, &g.SourceID, &g.LimitValue, &g.StartsAt, &ends, &revoked); err != nil {
			return ResolvedEntitlement{}, err
		}
		g.StartsAt = g.StartsAt.UTC()
		if ends.Valid {
			v := ends.Time.UTC()
			g.EndsAt = &v
		}
		if revoked.Valid {
			v := revoked.Time.UTC()
			g.RevokedAt = &v
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return ResolvedEntitlement{}, err
	}
	return ResolveEntitlement(now, workspaceID, capability, grants)
}

func loadOrder(ctx context.Context, q rowQueryer, orderID string) (Order, error) {
	var o Order
	err := q.QueryRowContext(ctx, `SELECT id,workspace_id,plan_id,kind,currency,amount_minor,status,created_at,updated_at FROM billing_orders WHERE id=?`, orderID).Scan(&o.ID, &o.WorkspaceID, &o.PlanID, &o.Kind, &o.Money.Currency, &o.Money.AmountMinor, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return Order{}, err
	}
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return o, nil
}
func loadOrderForUpdate(ctx context.Context, tx *sql.Tx, orderID string) (Order, error) {
	var o Order
	err := tx.QueryRowContext(ctx, `SELECT id,workspace_id,plan_id,kind,currency,amount_minor,status,created_at,updated_at FROM billing_orders WHERE id=? FOR UPDATE`, orderID).Scan(&o.ID, &o.WorkspaceID, &o.PlanID, &o.Kind, &o.Money.Currency, &o.Money.AmountMinor, &o.Status, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	if err != nil {
		return Order{}, err
	}
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return o, nil
}
func loadOrderByIdempotency(ctx context.Context, tx *sql.Tx, workspaceID string, hash []byte) (Order, error) {
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM billing_orders WHERE workspace_id=? AND idempotency_key_hash=? FOR UPDATE`, workspaceID, hash).Scan(&id); err != nil {
		return Order{}, err
	}
	return loadOrder(ctx, tx, id)
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func validOrderKind(kind OrderKind) bool {
	return kind == OrderNew || kind == OrderUpgrade || kind == OrderRenewal
}
func newOpaqueID(prefix string) (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}
func wrapConflict(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "duplicate") || strings.Contains(lower, "1062") {
		return fmt.Errorf("%w: %v", ErrConflict, err)
	}
	return err
}
