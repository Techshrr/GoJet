package workspace

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"regexp"
	"strings"
	"time"
)

type NotificationInput struct {
	WorkspaceID     string
	RecipientUserID string
	Category        string
	EventKey        string
	DedupeKey       string
	Title           string
	Summary         string
	DeepLink        string
	ResourceType    string
	ResourceID      string
}

type NotificationPage struct {
	Items       []Notification    `json:"items"`
	UnreadCount uint64            `json:"unread_count"`
	State       NotificationState `json:"state"`
}

var (
	notificationEmailLike  = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)
	notificationBearerLike = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/-]{8,}`)
	notificationJWTLike    = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\b`)
)

func validNotificationCategory(value string) bool {
	switch value {
	case "security", "domains", "billing", "support", "resources":
		return true
	default:
		return false
	}
}

func (s *Store) ProduceNotification(ctx context.Context, input NotificationInput) (Notification, bool, error) {
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
	if _, err := s.GetMembership(ctx, input.WorkspaceID, input.RecipientUserID); err != nil {
		return Notification{}, false, ErrForbidden
	}
	res, err := s.db.ExecContext(ctx, `
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
	err = s.db.QueryRowContext(ctx, `
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

func (s *Store) ListNotifications(ctx context.Context, workspaceID, userID, category string, limit int) (NotificationPage, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	category = strings.TrimSpace(category)
	args := []any{workspaceID, userID}
	where := ""
	if category != "" && category != "all" {
		if !validNotificationCategory(category) {
			return NotificationPage{}, ErrInvalid
		}
		where = " AND category=?"
		args = append(args, category)
	}
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,recipient_user_id,category,event_key,dedupe_key,title,summary,
       COALESCE(deep_link,''),resource_type,resource_id,read_at,created_at
FROM workspace_notifications
WHERE workspace_id=? AND recipient_user_id=?`+where+`
ORDER BY (read_at IS NULL) DESC,created_at DESC,id DESC
LIMIT ?`, args...)
	if err != nil {
		return NotificationPage{}, err
	}
	defer rows.Close()
	page := NotificationPage{}
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.RecipientUserID, &item.Category, &item.EventKey,
			&item.DedupeKey, &item.Title, &item.Summary, &item.DeepLink, &item.ResourceType,
			&item.ResourceID, &item.ReadAt, &item.CreatedAt); err != nil {
			return NotificationPage{}, err
		}
		if item.DeepLink != "" {
			ok, err := s.AuthorizeDeepLink(ctx, workspaceID, userID, item.DeepLink)
			if err != nil || !ok {
				item.DeepLink = "/app/notifications"
			}
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return NotificationPage{}, err
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=? AND recipient_user_id=? AND read_at IS NULL`,
		workspaceID, userID).Scan(&page.UnreadCount); err != nil {
		return NotificationPage{}, err
	}
	state, err := s.GetNotificationState(ctx, workspaceID)
	if err != nil {
		return NotificationPage{}, err
	}
	page.State = state
	return page, nil
}

func (s *Store) SetNotificationRead(ctx context.Context, workspaceID, userID string, notificationID uint64, read bool) error {
	var res sql.Result
	var err error
	if read {
		res, err = s.db.ExecContext(ctx, `
UPDATE workspace_notifications SET read_at=COALESCE(read_at,CURRENT_TIMESTAMP(6))
WHERE id=? AND workspace_id=? AND recipient_user_id=?`, notificationID, workspaceID, userID)
	} else {
		res, err = s.db.ExecContext(ctx, `
UPDATE workspace_notifications SET read_at=NULL
WHERE id=? AND workspace_id=? AND recipient_user_id=?`, notificationID, workspaceID, userID)
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, workspaceID, userID string) (uint64, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_notifications SET read_at=CURRENT_TIMESTAMP(6)
WHERE workspace_id=? AND recipient_user_id=? AND read_at IS NULL`, workspaceID, userID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return uint64(n), err
}

func (s *Store) GetNotificationState(ctx context.Context, workspaceID string) (NotificationState, error) {
	var state NotificationState
	err := s.db.QueryRowContext(ctx, `
SELECT workspace_id,status,data_through_at,state_reason,updated_at
FROM workspace_notification_state WHERE workspace_id=?`, workspaceID).
		Scan(&state.WorkspaceID, &state.Status, &state.DataThroughAt, &state.StateReason, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return NotificationState{}, ErrNotFound
	}
	return state, err
}

func (s *Store) SetNotificationState(ctx context.Context, workspaceID, status, reason string, dataThrough *time.Time) error {
	if status != "complete" && status != "partial" && status != "stale" {
		return ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 160 || notificationContainsSensitiveData(reason) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE workspace_notification_state
SET status=?,data_through_at=?,state_reason=?
WHERE workspace_id=?`, status, dataThrough, reason, workspaceID)
	return err
}

func normalizeDeepLink(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/app") || strings.HasPrefix(value, "//") || strings.ContainsAny(value, "?#") || notificationContainsSensitiveData(value) {
		return ""
	}
	clean := path.Clean(value)
	if clean == "." || strings.Contains(clean, "\\") || !strings.HasPrefix(clean, "/app") {
		return ""
	}
	return clean
}

func (s *Store) AuthorizeDeepLink(ctx context.Context, workspaceID, userID, deepLink string) (bool, error) {
	if _, err := s.GetMembership(ctx, workspaceID, userID); err != nil {
		if errors.Is(err, ErrForbidden) {
			return false, nil
		}
		return false, err
	}
	deepLink = normalizeDeepLink(deepLink)
	if deepLink == "" {
		return false, nil
	}
	switch deepLink {
	case "/app", "/app/notifications", "/app/organization", "/app/campaigns", "/app/tags", "/app/members", "/app/settings/workspace", "/app/billing":
		return true, nil
	}
	prefixes := []struct {
		prefix string
		table  string
		idcol  string
	}{
		{"/app/links/", "links", "id"},
		{"/app/qr/", "qr_codes", "id"},
		{"/app/files/", "files", "id"},
		{"/app/text/", "text_shares", "id"},
		{"/app/bio/", "bio_pages", "id"},
		{"/app/domains/", "custom_domains", "id"},
	}
	for _, item := range prefixes {
		if strings.HasPrefix(deepLink, item.prefix) {
			id := strings.TrimPrefix(deepLink, item.prefix)
			if id == "" || strings.Contains(id, "/") {
				return false, nil
			}
			var count uint64
			query := "SELECT COUNT(*) FROM " + item.table + " WHERE workspace_id=? AND " + item.idcol + "=?"
			if err := s.db.QueryRowContext(ctx, query, workspaceID, id).Scan(&count); err != nil {
				return false, err
			}
			return count == 1, nil
		}
	}
	return false, nil
}

func notificationContainsSensitiveData(value string) bool {
	lower := strings.ToLower(value)
	deny := []string{"password=", "token=", "secret=", "authorization:", "cookie:", "oauth_", "webhook_secret", "payment_secret", "risk_evidence"}
	for _, marker := range deny {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return notificationEmailLike.MatchString(value) || notificationBearerLike.MatchString(value) || notificationJWTLike.MatchString(value)
}

func redactNotificationText(value string) string {
	if notificationContainsSensitiveData(value) {
		return "[redacted]"
	}
	return value
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
