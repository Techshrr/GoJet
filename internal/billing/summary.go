package billing

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

type WorkspaceBillingState string

const (
	WorkspaceBillingActive          WorkspaceBillingState = "active"
	WorkspaceBillingPaymentPending  WorkspaceBillingState = "payment-pending"
	WorkspaceBillingPaymentFailed   WorkspaceBillingState = "payment-failed"
	WorkspaceBillingOverdue         WorkspaceBillingState = "overdue"
	WorkspaceBillingCanceled        WorkspaceBillingState = "canceled"
	WorkspaceBillingProviderPartial WorkspaceBillingState = "provider-partial"
)

type WorkspaceBillingSummary struct {
	State             WorkspaceBillingState `json:"state"`
	Plan              *Plan                 `json:"plan,omitempty"`
	Subscription      *Subscription         `json:"subscription,omitempty"`
	ScheduledTarget   *Subscription         `json:"scheduled_target,omitempty"`
	LatestOrderStatus OrderStatus           `json:"latest_order_status,omitempty"`
}

type WorkspaceBillingSummaryStore interface {
	GetWorkspaceBillingSummary(context.Context, string, time.Time) (WorkspaceBillingSummary, error)
}

func (s *Store) GetWorkspaceBillingSummary(ctx context.Context, workspaceID string, now time.Time) (WorkspaceBillingSummary, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if s == nil || s.db == nil || workspaceID == "" || now.IsZero() {
		return WorkspaceBillingSummary{}, ErrInvalidInput
	}
	now = now.UTC()

	var summary WorkspaceBillingSummary
	var currentID string
	err := s.db.QueryRowContext(ctx, `
SELECT id
FROM workspace_subscriptions
WHERE workspace_id=? AND status IN ('active','grace','overdue','canceled','expired')
ORDER BY updated_at DESC,id DESC
LIMIT 1`, workspaceID).Scan(&currentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBillingSummary{}, err
	}
	if err == nil {
		current, loadErr := loadSubscription(ctx, s.db, currentID)
		if loadErr != nil {
			return WorkspaceBillingSummary{}, loadErr
		}
		summary.Subscription = &current
	}

	var targetID string
	err = s.db.QueryRowContext(ctx, `
SELECT id
FROM workspace_subscriptions
WHERE workspace_id=? AND status='pending' AND starts_at>=?
ORDER BY starts_at ASC,id ASC
LIMIT 1`, workspaceID, now).Scan(&targetID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBillingSummary{}, err
	}
	if err == nil {
		target, loadErr := loadSubscription(ctx, s.db, targetID)
		if loadErr != nil {
			return WorkspaceBillingSummary{}, loadErr
		}
		summary.ScheduledTarget = &target
	}

	var latest OrderStatus
	err = s.db.QueryRowContext(ctx, `
SELECT status
FROM billing_orders
WHERE workspace_id=?
ORDER BY created_at DESC,id DESC
LIMIT 1`, workspaceID).Scan(&latest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return WorkspaceBillingSummary{}, err
	}
	if err == nil {
		summary.LatestOrderStatus = latest
	}

	planID := uint64(0)
	if summary.Subscription != nil {
		planID = summary.Subscription.PlanID
	} else if summary.ScheduledTarget != nil {
		planID = summary.ScheduledTarget.PlanID
	}
	if planID != 0 {
		plan, loadErr := s.GetAdminPlan(ctx, planID)
		if loadErr != nil {
			return WorkspaceBillingSummary{}, loadErr
		}
		summary.Plan = &plan
	}
	summary.State = resolveWorkspaceBillingState(summary.Subscription, summary.LatestOrderStatus)
	return summary, nil
}

func resolveWorkspaceBillingState(subscription *Subscription, latest OrderStatus) WorkspaceBillingState {
	switch latest {
	case OrderPending:
		return WorkspaceBillingPaymentPending
	case OrderProcessing:
		return WorkspaceBillingProviderPartial
	case OrderFailed:
		return WorkspaceBillingPaymentFailed
	}
	if subscription != nil {
		switch subscription.Status {
		case SubscriptionOverdue:
			return WorkspaceBillingOverdue
		case SubscriptionCanceled, SubscriptionExpired:
			return WorkspaceBillingCanceled
		case SubscriptionActive, SubscriptionGrace:
			return WorkspaceBillingActive
		}
	}
	return WorkspaceBillingCanceled
}

func NewWorkspaceBillingSummaryHandler(store WorkspaceBillingSummaryStore, principals PrincipalResolver, memberships WorkspaceRoleResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		if r.Method != http.MethodGet {
			writeBillingError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if store == nil || principals == nil || memberships == nil {
			writeBillingError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			return
		}
		principal, err := principals.ResolvePrincipal(r)
		if err != nil || strings.TrimSpace(principal.UserID) == "" {
			if errors.Is(err, ErrAuthenticationUnavailable) {
				writeBillingError(w, http.StatusServiceUnavailable, "auth_dependency_unavailable", "Authentication dependency is unavailable.")
			} else {
				writeBillingError(w, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
			}
			return
		}
		workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
		if workspaceID == "" {
			writeBillingError(w, http.StatusBadRequest, "invalid_workspace", "Invalid Workspace.")
			return
		}
		role, err := memberships.ResolveWorkspaceRole(r.Context(), workspaceID, principal.UserID)
		if err != nil {
			writeBillingError(w, http.StatusForbidden, "forbidden", "Workspace access denied.")
			return
		}
		if role != "owner" && role != "admin" {
			writeBillingError(w, http.StatusForbidden, "forbidden", "Billing summary requires a Workspace owner or admin.")
			return
		}
		summary, err := store.GetWorkspaceBillingSummary(r.Context(), workspaceID, time.Now().UTC())
		if err != nil {
			writeBillingStoreError(w, err)
			return
		}
		writeBillingJSON(w, http.StatusOK, map[string]any{"summary": summary})
	})
}
