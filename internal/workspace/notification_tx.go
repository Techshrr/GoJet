package workspace

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ProduceNotificationTx is the transaction-aware internal producer boundary
// used by upstream capabilities that must atomically commit their domain state
// and the corresponding P12 notification. The caller owns commit/rollback.
func ProduceNotificationTx(ctx context.Context, tx *sql.Tx, input NotificationInput) (Notification, bool, error) {
	if tx == nil {
		return Notification{}, false, ErrInvalid
	}
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.RecipientUserID = strings.TrimSpace(input.RecipientUserID)
	input.Category = strings.TrimSpace(input.Category)
	input.EventKey = strings.TrimSpace(input.EventKey)
	input.DedupeKey = strings.TrimSpace(input.DedupeKey)
	input.Title = redactNotificationText(strings.TrimSpace(input.Title))
	input.Summary = redactNotificationText(strings.TrimSpace(input.Summary))
	input.DeepLink = normalizeDeepLink(input.DeepLink)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	if input.WorkspaceID == "" || input.RecipientUserID == "" || !validNotificationCategory(input.Category) ||
		input.EventKey == "" || input.DedupeKey == "" || input.Title == "" || len(input.Title) > 200 ||
		len(input.Summary) > 500 || len(input.EventKey) > 96 || len(input.DedupeKey) > 160 ||
		len(input.ResourceType) > 64 || len(input.ResourceID) > 128 ||
		notificationContainsSensitiveData(input.EventKey) || notificationContainsSensitiveData(input.DedupeKey) ||
		notificationContainsSensitiveData(input.ResourceType) || notificationContainsSensitiveData(input.ResourceID) {
		return Notification{}, false, ErrInvalid
	}

	var role string
	err := tx.QueryRowContext(ctx, `
SELECT role FROM workspace_memberships
WHERE workspace_id=? AND user_id=?`, input.WorkspaceID, input.RecipientUserID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return Notification{}, false, ErrForbidden
	}
	if err != nil {
		return Notification{}, false, err
	}
	switch role {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
	default:
		return Notification{}, false, ErrForbidden
	}

	res, err := tx.ExecContext(ctx, `
INSERT IGNORE INTO workspace_notifications
(workspace_id,recipient_user_id,category,event_key,dedupe_key,title,summary,deep_link,resource_type,resource_id)
VALUES (?,?,?,?,?,?,?,?,?,?)`,
		input.WorkspaceID, input.RecipientUserID, input.Category, input.EventKey, input.DedupeKey,
		input.Title, input.Summary, nullIfEmpty(input.DeepLink), input.ResourceType, input.ResourceID)
	if err != nil {
		return Notification{}, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Notification{}, false, err
	}

	var item Notification
	err = tx.QueryRowContext(ctx, `
SELECT id,workspace_id,recipient_user_id,category,event_key,dedupe_key,title,summary,
       COALESCE(deep_link,''),resource_type,resource_id,read_at,created_at
FROM workspace_notifications
WHERE workspace_id=? AND recipient_user_id=? AND dedupe_key=?`,
		input.WorkspaceID, input.RecipientUserID, input.DedupeKey).
		Scan(&item.ID, &item.WorkspaceID, &item.RecipientUserID, &item.Category, &item.EventKey,
			&item.DedupeKey, &item.Title, &item.Summary, &item.DeepLink, &item.ResourceType,
			&item.ResourceID, &item.ReadAt, &item.CreatedAt)
	return item, n == 1, err
}

// ProduceOwnerNotificationsTx resolves the current P12 owner membership set
// inside the caller's transaction, then applies the ordinary P12 producer
// validation/dedupe boundary for each recipient.
func ProduceOwnerNotificationsTx(ctx context.Context, tx *sql.Tx, input NotificationInput) ([]Notification, error) {
	if tx == nil || strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.RecipientUserID) != "" {
		return nil, ErrInvalid
	}
	rows, err := tx.QueryContext(ctx, `
SELECT user_id FROM workspace_memberships
WHERE workspace_id=? AND role='owner'
ORDER BY id`, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	owners := []string{}
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		owners = append(owners, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		return nil, ErrForbidden
	}

	items := make([]Notification, 0, len(owners))
	for _, userID := range owners {
		recipient := input
		recipient.RecipientUserID = userID
		item, _, err := ProduceNotificationTx(ctx, tx, recipient)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
