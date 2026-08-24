package billing

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type ExpiringNotificationSweepResult struct {
	Candidates int `json:"candidates"`
}

type expiringSubscriptionCandidate struct {
	ID          string
	WorkspaceID string
	EffectiveAt time.Time
}

// ProduceExpiringEntitlementNotifications emits the P13-owned
// entitlement_expiring producer event through the inherited P12 notification
// core. It is intentionally an internal Store operation: P13 does not expose a
// public emit endpoint and does not claim the later operations-monitor
// lifecycle. A later fixed-boundary scheduler may invoke this operation.
func (s *Store) ProduceExpiringEntitlementNotifications(ctx context.Context, now time.Time, horizon time.Duration) (ExpiringNotificationSweepResult, error) {
	if s == nil || s.db == nil || now.IsZero() || horizon <= 0 || horizon > 30*24*time.Hour {
		return ExpiringNotificationSweepResult{}, ErrInvalidInput
	}
	now = now.UTC()
	end := now.Add(horizon)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ExpiringNotificationSweepResult{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT id,workspace_id,grace_ends_at
FROM workspace_subscriptions
WHERE status='grace'
  AND grace_ends_at IS NOT NULL
  AND grace_ends_at > ?
  AND grace_ends_at <= ?
ORDER BY grace_ends_at,id
FOR SHARE`, now, end)
	if err != nil {
		return ExpiringNotificationSweepResult{}, err
	}

	candidates := []expiringSubscriptionCandidate{}
	for rows.Next() {
		var item expiringSubscriptionCandidate
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.EffectiveAt); err != nil {
			rows.Close()
			return ExpiringNotificationSweepResult{}, err
		}
		item.ID = strings.TrimSpace(item.ID)
		item.WorkspaceID = strings.TrimSpace(item.WorkspaceID)
		item.EffectiveAt = item.EffectiveAt.UTC()
		if item.ID == "" || item.WorkspaceID == "" {
			rows.Close()
			return ExpiringNotificationSweepResult{}, ErrInvalidInput
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return ExpiringNotificationSweepResult{}, err
	}
	if err := rows.Close(); err != nil {
		return ExpiringNotificationSweepResult{}, err
	}

	for _, item := range candidates {
		_, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, workspace.NotificationInput{
			WorkspaceID:  item.WorkspaceID,
			Category:     "billing",
			EventKey:     "entitlement_expiring",
			DedupeKey:    expiringNotificationDedupeKey(item.ID, item.EffectiveAt),
			Title:        "Billing entitlement expiring",
			Summary:      "Your current billing entitlement will change when the scheduled grace period ends.",
			DeepLink:     "/app/billing",
			ResourceType: "billing_subscription",
			ResourceID:   item.ID,
		})
		if err != nil {
			return ExpiringNotificationSweepResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return ExpiringNotificationSweepResult{}, err
	}
	return ExpiringNotificationSweepResult{Candidates: len(candidates)}, nil
}

func expiringNotificationDedupeKey(subscriptionID string, effectiveAt time.Time) string {
	return fmt.Sprintf("billing:entitlement_expiring:%s:%d", strings.TrimSpace(subscriptionID), effectiveAt.UTC().UnixMicro())
}
