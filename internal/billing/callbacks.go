package billing

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	"github.com/Techshrr/GoJet/internal/workspace"
)

const p13DomainPlanSourceKey = "p13:billing"

func (s *Store) ApplyAuthenticatedCallback(ctx context.Context, cmd CallbackCommand) (CallbackResult, error) {
	if s == nil || s.db == nil {
		return CallbackResult{}, ErrInvalidInput
	}
	if err := validateCallbackCommand(cmd); err != nil {
		return CallbackResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CallbackResult{}, err
	}
	defer tx.Rollback()
	var existingID uint64
	var existingProviderTransactionID, existingEventType string
	if err := tx.QueryRowContext(ctx, `SELECT id,provider_transaction_id,event_type FROM payment_callback_events WHERE provider=? AND provider_event_id=? FOR UPDATE`, cmd.Provider, cmd.ProviderEventID).Scan(&existingID, &existingProviderTransactionID, &existingEventType); err == nil {
		if existingProviderTransactionID != cmd.ProviderTransactionID || existingEventType != cmd.EventType {
			return CallbackResult{}, ErrConflict
		}
		transaction, loadErr := loadTransactionByProvider(ctx, tx, cmd.Provider, existingProviderTransactionID)
		if loadErr != nil {
			return CallbackResult{}, loadErr
		}
		if transaction.OrderID != cmd.OrderID || transaction.Money != cmd.Money {
			return CallbackResult{}, ErrConflict
		}
		order, loadErr := loadOrder(ctx, tx, transaction.OrderID)
		if loadErr != nil {
			return CallbackResult{}, loadErr
		}
		if auditErr := appendAuditTx(ctx, tx, order.WorkspaceID, "system:payment-callback", "payment.callback.duplicate", "callback_event", fmt.Sprint(existingID), "duplicate_provider_event", cmd.CorrelationID, "success", map[string]any{"provider": cmd.Provider, "provider_event_id": cmd.ProviderEventID}); auditErr != nil {
			return CallbackResult{}, auditErr
		}
		if err := tx.Commit(); err != nil {
			return CallbackResult{}, err
		}
		return CallbackResult{Duplicate: true, EventStatus: CallbackDuplicate, Order: order, Transaction: transaction}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CallbackResult{}, err
	}

	order, err := loadOrderForUpdate(ctx, tx, cmd.OrderID)
	if err != nil {
		return CallbackResult{}, err
	}
	if order.Money != cmd.Money {
		return CallbackResult{}, ErrConflict
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO payment_callback_events (provider,provider_event_id,provider_transaction_id,event_type,status,received_at,correlation_id) VALUES (?,?,?,?, 'accepted',?,?)`, cmd.Provider, cmd.ProviderEventID, cmd.ProviderTransactionID, cmd.EventType, cmd.ReceivedAt.UTC(), cmd.CorrelationID)
	if err != nil {
		return CallbackResult{}, wrapConflict(err)
	}
	eventID, err := result.LastInsertId()
	if err != nil {
		return CallbackResult{}, err
	}

	transaction, err := upsertTransactionTx(ctx, tx, order, cmd)
	if err != nil {
		return CallbackResult{}, err
	}
	var subscription *Subscription
	stateChanged := false
	switch cmd.Outcome {
	case TransactionPaid:
		if order.Status != OrderPending && order.Status != OrderProcessing && order.Status != OrderPaid {
			return CallbackResult{}, ErrConflict
		}
		if order.Status != OrderPaid {
			if _, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='paid',updated_at=? WHERE id=?`, cmd.ReceivedAt.UTC(), order.ID); err != nil {
				return CallbackResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE billing_invoices SET status='paid',paid_at=COALESCE(paid_at,?) WHERE order_id=?`, cmd.ReceivedAt.UTC(), order.ID); err != nil {
				return CallbackResult{}, err
			}
			sub, err := activateSubscriptionTx(ctx, tx, order, cmd.ReceivedAt.UTC(), cmd.CorrelationID)
			if err != nil {
				return CallbackResult{}, err
			}
			if err := projectP06DomainEntitlementTx(ctx, tx, order, sub, cmd.CorrelationID); err != nil {
				return CallbackResult{}, err
			}
			subscription = &sub
			stateChanged = true
		}
	case TransactionFailed:
		if order.Status != OrderPending && order.Status != OrderProcessing && order.Status != OrderFailed {
			return CallbackResult{}, ErrConflict
		}
		if order.Status != OrderFailed {
			if _, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='failed',updated_at=? WHERE id=?`, cmd.ReceivedAt.UTC(), order.ID); err != nil {
				return CallbackResult{}, err
			}
			stateChanged = true
		}
	case TransactionRefunded:
		if order.Status != OrderPaid && order.Status != OrderRefunded {
			return CallbackResult{}, ErrConflict
		}
		if order.Status != OrderRefunded {
			controlsCurrent, err := subscriptionControlsCurrentBillingTx(ctx, tx, order)
			if err != nil {
				return CallbackResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE billing_orders SET status='refunded',updated_at=? WHERE id=?`, cmd.ReceivedAt.UTC(), order.ID); err != nil {
				return CallbackResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE billing_invoices SET status='refunded',refunded_at=COALESCE(refunded_at,?) WHERE order_id=?`, cmd.ReceivedAt.UTC(), order.ID); err != nil {
				return CallbackResult{}, err
			}
			if controlsCurrent {
				if err := cancelScheduledDowngradeTx(ctx, tx, order.WorkspaceID, cmd.ReceivedAt.UTC(), cmd.CorrelationID, "billing_current_subscription_refunded"); err != nil {
					return CallbackResult{}, err
				}
			}
			if err := revokeSubscriptionTx(ctx, tx, order, cmd.ReceivedAt.UTC()); err != nil {
				return CallbackResult{}, err
			}
			if controlsCurrent {
				if _, err := domains.ExpirePlanSourceTx(ctx, tx, order.WorkspaceID, p13DomainPlanSourceKey, "billing_entitlement_refunded", cmd.CorrelationID); err != nil {
					return CallbackResult{}, err
				}
			}
			stateChanged = true
		}
	default:
		return CallbackResult{}, ErrInvalidInput
	}
	if stateChanged {
		if err := produceP12BillingNotificationsTx(ctx, tx, order, cmd.Outcome, eventID); err != nil {
			return CallbackResult{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payment_callback_events SET status='processed',processed_at=? WHERE id=?`, cmd.ReceivedAt.UTC(), eventID); err != nil {
		return CallbackResult{}, err
	}
	if err := appendAuditTx(ctx, tx, order.WorkspaceID, "system:payment-callback", "payment.callback.process", "callback_event", fmt.Sprint(eventID), string(cmd.Outcome), cmd.CorrelationID, "success", map[string]any{"provider": cmd.Provider, "provider_event_id": cmd.ProviderEventID, "order_id": order.ID, "outcome": cmd.Outcome}); err != nil {
		return CallbackResult{}, err
	}
	order, err = loadOrder(ctx, tx, order.ID)
	if err != nil {
		return CallbackResult{}, err
	}
	transaction, err = loadTransactionByProvider(ctx, tx, cmd.Provider, cmd.ProviderTransactionID)
	if err != nil {
		return CallbackResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return CallbackResult{}, err
	}
	return CallbackResult{EventStatus: CallbackProcessed, Order: order, Transaction: transaction, Subscription: subscription}, nil
}

func validateCallbackCommand(cmd CallbackCommand) error {
	if !IsFrozenProvider(cmd.Provider) || strings.TrimSpace(cmd.ProviderEventID) == "" || len(cmd.ProviderEventID) > 191 || strings.TrimSpace(cmd.ProviderTransactionID) == "" || len(cmd.ProviderTransactionID) > 191 || strings.TrimSpace(cmd.OrderID) == "" || strings.TrimSpace(cmd.EventType) == "" || len(cmd.EventType) > 96 || strings.TrimSpace(cmd.CorrelationID) == "" || len(cmd.CorrelationID) > 128 || cmd.ReceivedAt.IsZero() {
		return ErrInvalidInput
	}
	if cmd.Money.Validate(false) != nil {
		return ErrInvalidMoney
	}
	if cmd.Outcome != TransactionPaid && cmd.Outcome != TransactionFailed && cmd.Outcome != TransactionRefunded {
		return ErrInvalidInput
	}
	return nil
}

func upsertTransactionTx(ctx context.Context, tx *sql.Tx, order Order, cmd CallbackCommand) (Transaction, error) {
	var id uint64
	var existingOrder string
	var currency string
	var amount int64
	var status TransactionStatus
	err := tx.QueryRowContext(ctx, `SELECT id,order_id,currency,amount_minor,status FROM billing_transactions WHERE provider=? AND provider_transaction_id=? FOR UPDATE`, cmd.Provider, cmd.ProviderTransactionID).Scan(&id, &existingOrder, &currency, &amount, &status)
	if err == nil {
		if existingOrder != order.ID || currency != cmd.Money.Currency || amount != cmd.Money.AmountMinor {
			return Transaction{}, ErrConflict
		}
		if !transactionTransitionAllowed(status, cmd.Outcome) {
			return Transaction{}, ErrConflict
		}
		if status != cmd.Outcome {
			if _, err := tx.ExecContext(ctx, `UPDATE billing_transactions SET status=?,updated_at=? WHERE id=?`, cmd.Outcome, cmd.ReceivedAt.UTC(), id); err != nil {
				return Transaction{}, err
			}
		}
		return loadTransactionByProvider(ctx, tx, cmd.Provider, cmd.ProviderTransactionID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Transaction{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO billing_transactions (workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, order.WorkspaceID, order.ID, cmd.Provider, cmd.ProviderTransactionID, cmd.Money.Currency, cmd.Money.AmountMinor, cmd.Outcome, cmd.ReceivedAt.UTC(), cmd.ReceivedAt.UTC())
	if err != nil {
		return Transaction{}, wrapConflict(err)
	}
	id64, err := res.LastInsertId()
	if err != nil {
		return Transaction{}, err
	}
	_ = id64
	return loadTransactionByProvider(ctx, tx, cmd.Provider, cmd.ProviderTransactionID)
}

func transactionTransitionAllowed(current, next TransactionStatus) bool {
	if current == next {
		return true
	}
	switch current {
	case TransactionPending:
		return next == TransactionPaid || next == TransactionFailed
	case TransactionPaid:
		return next == TransactionRefunded
	default:
		return false
	}
}

func loadTransactionByProvider(ctx context.Context, q rowQueryer, provider Provider, providerTxn string) (Transaction, error) {
	var t Transaction
	err := q.QueryRowContext(ctx, `SELECT id,workspace_id,order_id,provider,provider_transaction_id,currency,amount_minor,status,created_at,updated_at FROM billing_transactions WHERE provider=? AND provider_transaction_id=?`, provider, providerTxn).Scan(&t.ID, &t.WorkspaceID, &t.OrderID, &t.Provider, &t.ProviderTransactionID, &t.Money.Currency, &t.Money.AmountMinor, &t.Status, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return Transaction{}, err
	}
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	return t, nil
}

func activateSubscriptionTx(ctx context.Context, tx *sql.Tx, order Order, now time.Time, correlationID string) (Subscription, error) {
	var period BillingPeriod
	if err := tx.QueryRowContext(ctx, `SELECT billing_period FROM billing_plans WHERE id=?`, order.PlanID).Scan(&period); err != nil {
		return Subscription{}, err
	}
	termEnd := termEndFor(period, now)
	subID := subscriptionIDForOrder(order.ID)
	if err := cancelScheduledDowngradeTx(ctx, tx, order.WorkspaceID, now, correlationID, "authoritative_paid_activation_supersedes_downgrade"); err != nil {
		return Subscription{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_subscriptions SET status='expired',current_term_ends_at=COALESCE(current_term_ends_at,?),version=version+1,updated_at=? WHERE workspace_id=? AND status IN ('active','grace','overdue') AND id<>?`, now, now, order.WorkspaceID, subID); err != nil {
		return Subscription{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_subscriptions (id,workspace_id,plan_id,status,starts_at,current_term_ends_at,version,created_at,updated_at) VALUES (?,?,?,'active',?,?,1,?,?) ON DUPLICATE KEY UPDATE status='active',plan_id=VALUES(plan_id),current_term_ends_at=VALUES(current_term_ends_at),grace_ends_at=NULL,cancel_at=NULL,version=version+1,updated_at=VALUES(updated_at)`, subID, order.WorkspaceID, order.PlanID, now, termEnd, now, now); err != nil {
		return Subscription{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE entitlement_grants SET revoked_at=COALESCE(revoked_at,?),updated_at=? WHERE workspace_id=? AND source_type='billing' AND source_id<>? AND revoked_at IS NULL`, now, now, order.WorkspaceID, subID); err != nil {
		return Subscription{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT capability,limit_value FROM billing_plan_entitlements WHERE plan_id=? ORDER BY capability`, order.PlanID)
	if err != nil {
		return Subscription{}, err
	}
	entitlements := []PlanEntitlement{}
	for rows.Next() {
		var item PlanEntitlement
		if err := rows.Scan(&item.Capability, &item.LimitValue); err != nil {
			rows.Close()
			return Subscription{}, err
		}
		entitlements = append(entitlements, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Subscription{}, err
	}
	if err := rows.Close(); err != nil {
		return Subscription{}, err
	}
	for _, item := range entitlements {
		prov, _ := json.Marshal(map[string]any{"plan_id": order.PlanID, "order_id": order.ID, "source": "billing"})
		if _, err := tx.ExecContext(ctx, `INSERT INTO entitlement_grants (workspace_id,capability,source_type,source_id,limit_value,starts_at,ends_at,revoked_at,provenance_json,created_at,updated_at) VALUES (?,?,'billing',?,?,?,?,NULL,CAST(? AS JSON),?,?) ON DUPLICATE KEY UPDATE limit_value=VALUES(limit_value),starts_at=VALUES(starts_at),ends_at=VALUES(ends_at),revoked_at=NULL,provenance_json=VALUES(provenance_json),updated_at=VALUES(updated_at)`, order.WorkspaceID, item.Capability, subID, item.LimitValue, now, termEnd, string(prov), now, now); err != nil {
			return Subscription{}, err
		}
	}
	return loadSubscription(ctx, tx, subID)
}

func revokeSubscriptionTx(ctx context.Context, tx *sql.Tx, order Order, now time.Time) error {
	subID := subscriptionIDForOrder(order.ID)
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_subscriptions SET status='canceled',cancel_at=COALESCE(cancel_at,?),version=version+1,updated_at=? WHERE id=? AND workspace_id=?`, now, now, subID, order.WorkspaceID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE entitlement_grants SET revoked_at=COALESCE(revoked_at,?),updated_at=? WHERE workspace_id=? AND source_type='billing' AND source_id=?`, now, now, order.WorkspaceID, subID)
	return err
}

func subscriptionControlsCurrentBillingTx(ctx context.Context, tx *sql.Tx, order Order) (bool, error) {
	var status SubscriptionStatus
	err := tx.QueryRowContext(ctx, `
SELECT status FROM workspace_subscriptions
WHERE id=? AND workspace_id=? FOR UPDATE`, subscriptionIDForOrder(order.ID), order.WorkspaceID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == SubscriptionActive || status == SubscriptionGrace || status == SubscriptionOverdue, nil
}

func projectP06DomainEntitlementTx(ctx context.Context, tx *sql.Tx, order Order, subscription Subscription, correlationID string) error {
	var limit uint64
	err := tx.QueryRowContext(ctx, `
SELECT limit_value FROM billing_plan_entitlements
WHERE plan_id=? AND capability=?`, order.PlanID, domains.CustomDomainsCapability).Scan(&limit)
	if errors.Is(err, sql.ErrNoRows) {
		_, expireErr := domains.ExpirePlanSourceTx(ctx, tx, order.WorkspaceID, p13DomainPlanSourceKey, "billing_plan_custom_domains_not_granted", correlationID)
		return expireErr
	}
	if err != nil {
		return err
	}
	const maxDomainLimit = uint64(^uint32(0))
	if limit == 0 || limit > maxDomainLimit {
		return ErrConflict
	}
	_, err = domains.UpsertPlanSourceTx(ctx, tx, domains.PlanSourceInput{
		WorkspaceID:    order.WorkspaceID,
		SourceKey:      p13DomainPlanSourceKey,
		Status:         domains.EntitlementActive,
		DomainLimit:    uint32(limit),
		StartsAt:       subscription.StartsAt,
		ExpiresAt:      subscription.CurrentTermEndsAt,
		DecisionReason: "billing_plan_entitlement_active",
	}, correlationID)
	return err
}

type billingNotificationSpec struct {
	EventKey string
	Title    string
	Summary  string
}

func billingNotificationSpecs(order Order, outcome TransactionStatus) []billingNotificationSpec {
	switch outcome {
	case TransactionPaid:
		specs := []billingNotificationSpec{{
			EventKey: "payment_succeeded",
			Title:    "Payment received",
			Summary:  "Your billing payment was confirmed.",
		}}
		if order.Kind == OrderUpgrade {
			specs = append(specs, billingNotificationSpec{
				EventKey: "plan_upgraded",
				Title:    "Plan upgraded",
				Summary:  "Your Workspace plan upgrade is active.",
			})
		}
		return specs
	case TransactionFailed:
		return []billingNotificationSpec{{
			EventKey: "payment_failed",
			Title:    "Payment failed",
			Summary:  "A billing payment could not be completed.",
		}}
	case TransactionRefunded:
		return []billingNotificationSpec{{
			EventKey: "refund_processed",
			Title:    "Refund processed",
			Summary:  "A billing refund was processed.",
		}}
	default:
		return nil
	}
}

func produceP12BillingNotificationsTx(ctx context.Context, tx *sql.Tx, order Order, outcome TransactionStatus, callbackEventID int64) error {
	specs := billingNotificationSpecs(order, outcome)
	for _, spec := range specs {
		if _, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, workspace.NotificationInput{
			WorkspaceID:  order.WorkspaceID,
			Category:     "billing",
			EventKey:     spec.EventKey,
			DedupeKey:    fmt.Sprintf("billing:%s:callback:%d", spec.EventKey, callbackEventID),
			Title:        spec.Title,
			Summary:      spec.Summary,
			DeepLink:     "/app/billing",
			ResourceType: "billing_order",
			ResourceID:   order.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func loadSubscription(ctx context.Context, q rowQueryer, id string) (Subscription, error) {
	var s Subscription
	var term, grace, cancel sql.NullTime
	err := q.QueryRowContext(ctx, `SELECT id,workspace_id,plan_id,status,starts_at,current_term_ends_at,grace_ends_at,cancel_at,version,created_at,updated_at FROM workspace_subscriptions WHERE id=?`, id).Scan(&s.ID, &s.WorkspaceID, &s.PlanID, &s.Status, &s.StartsAt, &term, &grace, &cancel, &s.Version, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return Subscription{}, err
	}
	s.StartsAt = s.StartsAt.UTC()
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if term.Valid {
		v := term.Time.UTC()
		s.CurrentTermEndsAt = &v
	}
	if grace.Valid {
		v := grace.Time.UTC()
		s.GraceEndsAt = &v
	}
	if cancel.Valid {
		v := cancel.Time.UTC()
		s.CancelAt = &v
	}
	return s, nil
}

func termEndFor(period BillingPeriod, start time.Time) *time.Time {
	var end time.Time
	switch period {
	case BillingMonthly:
		end = start.AddDate(0, 1, 0)
	case BillingYearly:
		end = start.AddDate(1, 0, 0)
	case BillingOneTime:
		return nil
	default:
		return nil
	}
	end = end.UTC()
	return &end
}

func subscriptionIDForOrder(orderID string) string {
	sum := sha256String(orderID)
	return "sub_" + sum[:24]
}

func sha256String(value string) string {
	sum := sha256Sum([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func sha256Sum(value []byte) [32]byte { return sha256.Sum256(value) }

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID, actor, action, resourceType, resourceID, reason, correlation, result string, metadata map[string]any) error {
	if strings.TrimSpace(actor) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(resourceType) == "" || strings.TrimSpace(correlation) == "" {
		return ErrInvalidInput
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO billing_audit_events (workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json) VALUES (NULLIF(?,''),?,?,?,?,?,?,?,CAST(? AS JSON))`, workspaceID, actor, action, resourceType, resourceID, nullIfBlank(reason), correlation, result, string(raw))
	return err
}

func nullIfBlank(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
