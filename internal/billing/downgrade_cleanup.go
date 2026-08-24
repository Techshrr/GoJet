package billing

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
)

// cancelScheduledDowngradeTx removes only future downgrade authority. It is
// used when an authoritative paid activation supersedes a pending downgrade or
// when the currently controlling paid subscription is refunded during grace.
func cancelScheduledDowngradeTx(ctx context.Context, tx *sql.Tx, workspaceID string, now time.Time, correlationID, reason string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	correlationID = strings.TrimSpace(correlationID)
	reason = strings.TrimSpace(reason)
	if tx == nil || workspaceID == "" || now.IsZero() || correlationID == "" || reason == "" {
		return ErrInvalidInput
	}
	now = now.UTC()

	rows, err := tx.QueryContext(ctx, `
SELECT id FROM workspace_subscriptions
WHERE workspace_id=? AND status='pending'
ORDER BY starts_at,id
FOR UPDATE`, workspaceID)
	if err != nil {
		return err
	}
	var pendingIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		pendingIDs = append(pendingIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, id := range pendingIDs {
		if _, err := tx.ExecContext(ctx, `
UPDATE entitlement_grants
SET revoked_at=COALESCE(revoked_at,?),updated_at=?
WHERE workspace_id=? AND source_type='billing' AND source_id=?`, now, now, workspaceID, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
UPDATE workspace_subscriptions
SET status='canceled',cancel_at=COALESCE(cancel_at,?),version=version+1,updated_at=?
WHERE id=? AND workspace_id=? AND status='pending'`, now, now, id, workspaceID); err != nil {
			return err
		}
		if err := appendAuditTx(ctx, tx, workspaceID, "system:billing-lifecycle", "billing.downgrade.cancel", "subscription", id, reason, correlationID, "success", map[string]any{
			"canceled_pending_subscription": id,
		}); err != nil {
			return err
		}
	}

	// A future P06 target source is independent durable authority and must be
	// removed even if a stale pending subscription row is already absent.
	if _, err := domains.ExpirePlanSourceTx(ctx, tx, workspaceID, p13DomainTargetPlanSourceKey, reason, correlationID); err != nil {
		return err
	}
	return nil
}
