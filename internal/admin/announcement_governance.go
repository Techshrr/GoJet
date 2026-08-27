package admin

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type Announcement struct {
	ID                string     `json:"id"`
	Title             string     `json:"title"`
	Summary           string     `json:"summary"`
	Body              string     `json:"body"`
	Scope             string     `json:"scope"`
	WorkspaceID       string     `json:"workspace_id,omitempty"`
	State             string     `json:"state"`
	ScheduledFor      *time.Time `json:"scheduled_for,omitempty"`
	PublishedAt       *time.Time `json:"published_at,omitempty"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`
	CacheGeneration   uint64     `json:"cache_generation"`
	Version           uint64     `json:"version"`
	NotificationCount int        `json:"notification_count,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CreateAnnouncementInput struct {
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Body        string `json:"body"`
	Scope       string `json:"scope"`
	WorkspaceID string `json:"workspace_id"`
}

type AnnouncementActionInput struct {
	Action          string     `json:"action"`
	ExpectedVersion uint64     `json:"expected_version"`
	ScheduledFor    *time.Time `json:"scheduled_for"`
}

func validateAnnouncementInput(input CreateAnnouncementInput) (CreateAnnouncementInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = strings.TrimSpace(input.Body)
	input.Scope = strings.TrimSpace(input.Scope)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Title == "" || len(input.Title) > 200 || len(input.Summary) > 500 || input.Body == "" || len(input.Body) > 1_000_000 {
		return CreateAnnouncementInput{}, ErrInvalid
	}
	switch input.Scope {
	case "global":
		if input.WorkspaceID != "" {
			return CreateAnnouncementInput{}, ErrInvalid
		}
	case "workspace":
		if !validID(input.WorkspaceID, 64) {
			return CreateAnnouncementInput{}, ErrInvalid
		}
	default:
		return CreateAnnouncementInput{}, ErrInvalid
	}
	return input, nil
}

func (s *Service) ListAnnouncements(ctx context.Context, p Principal, limit int) ([]Announcement, error) {
	if err := s.Require(p, PermissionContentManage); err != nil {
		return nil, err
	}
	limit, err := normalizeAdminEnumerationLimit(limit)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,summary,body,scope,COALESCE(workspace_id,''),lifecycle_state,scheduled_for,published_at,archived_at,cache_generation,version,created_at,updated_at FROM admin_announcements ORDER BY created_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Announcement{}
	for rows.Next() {
		item, err := scanAnnouncement(rows.Scan)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type rowScanner func(dest ...any) error

func scanAnnouncement(scan rowScanner) (Announcement, error) {
	var item Announcement
	var scheduled, published, archived sql.NullTime
	if err := scan(&item.ID, &item.Title, &item.Summary, &item.Body, &item.Scope, &item.WorkspaceID, &item.State, &scheduled, &published, &archived, &item.CacheGeneration, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return Announcement{}, err
	}
	if scheduled.Valid {
		t := scheduled.Time.UTC()
		item.ScheduledFor = &t
	}
	if published.Valid {
		t := published.Time.UTC()
		item.PublishedAt = &t
	}
	if archived.Valid {
		t := archived.Time.UTC()
		item.ArchivedAt = &t
	}
	return item, nil
}

func (s *Service) CreateAnnouncement(ctx context.Context, p Principal, input CreateAnnouncementInput, authority MutationAuthority, now time.Time) (Announcement, bool, error) {
	if err := s.RequireHighRisk(p, PermissionContentManage, authority, now); err != nil {
		return Announcement{}, false, err
	}
	input, err := validateAnnouncementInput(input)
	if err != nil {
		return Announcement{}, false, err
	}
	fingerprint, err := requestFingerprint(input)
	if err != nil {
		return Announcement{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Announcement{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.announcement.create"
	if replay, ok, err := loadIdempotency[Announcement](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return Announcement{}, false, err
	} else if ok {
		return replay, true, nil
	}
	if input.Scope == "workspace" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=?`, input.WorkspaceID).Scan(&count); err != nil {
			return Announcement{}, false, err
		}
		if count != 1 {
			return Announcement{}, false, ErrNotFound
		}
	}
	id, err := newOpaque("ann_", 18)
	if err != nil {
		return Announcement{}, false, err
	}
	var workspaceID any
	if input.Scope == "workspace" {
		workspaceID = input.WorkspaceID
	}
	item := Announcement{ID: id, Title: input.Title, Summary: input.Summary, Body: input.Body, Scope: input.Scope, WorkspaceID: input.WorkspaceID, State: "draft", CacheGeneration: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_announcements(id,title,summary,body,scope,workspace_id,lifecycle_state,cache_generation,version,created_by,updated_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Title, item.Summary, item.Body, item.Scope, workspaceID, item.State, item.CacheGeneration, item.Version, p.Administrator.ID, p.Administrator.ID, now, now)
	if err != nil {
		return Announcement{}, false, mapDuplicate(err)
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "announcement", ResourceID: item.ID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, After: map[string]any{"state": item.State, "version": item.Version}, Metadata: map[string]any{"scope": item.Scope}, CreatedAt: now})
	if err != nil {
		return Announcement{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return Announcement{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Announcement{}, false, err
	}
	return item, false, nil
}

func (s *Service) MutateAnnouncement(ctx context.Context, p Principal, announcementID string, input AnnouncementActionInput, authority MutationAuthority, now time.Time) (Announcement, bool, error) {
	if err := s.RequireHighRisk(p, PermissionContentManage, authority, now); err != nil {
		return Announcement{}, false, err
	}
	announcementID = strings.TrimSpace(announcementID)
	input.Action = strings.TrimSpace(input.Action)
	if !validID(announcementID, 64) {
		return Announcement{}, false, ErrInvalid
	}
	validAction := map[string]bool{"schedule": true, "publish": true, "archive": true}
	if !validAction[input.Action] {
		return Announcement{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		ID              string
		Action          string
		ExpectedVersion uint64
		ScheduledFor    *time.Time
	}{announcementID, input.Action, input.ExpectedVersion, input.ScheduledFor})
	if err != nil {
		return Announcement{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Announcement{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.announcement.mutate"
	if replay, ok, err := loadIdempotency[Announcement](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return Announcement{}, false, err
	} else if ok {
		return replay, true, nil
	}
	item, err := scanAnnouncement(func(dest ...any) error {
		return tx.QueryRowContext(ctx, `SELECT id,title,summary,body,scope,COALESCE(workspace_id,''),lifecycle_state,scheduled_for,published_at,archived_at,cache_generation,version,created_at,updated_at FROM admin_announcements WHERE id=? FOR UPDATE`, announcementID).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Announcement{}, false, ErrNotFound
	}
	if err != nil {
		return Announcement{}, false, err
	}
	if item.Version != input.ExpectedVersion {
		return Announcement{}, false, ErrConflict
	}
	beforeState := item.State
	switch input.Action {
	case "schedule":
		if item.State != "draft" || input.ScheduledFor == nil {
			return Announcement{}, false, ErrConflict
		}
		t := input.ScheduledFor.UTC().Truncate(time.Microsecond)
		if !t.After(now) {
			return Announcement{}, false, ErrInvalid
		}
		item.State = "scheduled"
		item.ScheduledFor = &t
	case "publish":
		if item.State != "draft" && item.State != "scheduled" {
			return Announcement{}, false, ErrConflict
		}
		item.State = "published"
		t := now
		item.PublishedAt = &t
	case "archive":
		if item.State != "published" {
			return Announcement{}, false, ErrConflict
		}
		item.State = "archived"
		t := now
		item.ArchivedAt = &t
	}
	item.Version++
	item.CacheGeneration++
	item.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `UPDATE admin_announcements SET lifecycle_state=?,scheduled_for=?,published_at=?,archived_at=?,cache_generation=?,version=?,updated_by=?,updated_at=? WHERE id=? AND version=?`, item.State, item.ScheduledFor, item.PublishedAt, item.ArchivedAt, item.CacheGeneration, item.Version, p.Administrator.ID, now, item.ID, input.ExpectedVersion)
	if err != nil {
		return Announcement{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_content_cache_state SET generation=generation+1,updated_at=? WHERE cache_key='announcements'`, now); err != nil {
		return Announcement{}, false, err
	}
	if input.Action == "publish" && item.Scope == "workspace" {
		notifications, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, workspace.NotificationInput{WorkspaceID: item.WorkspaceID, EventKey: "announcement.published", ResourceType: "announcement", ResourceID: item.ID, Severity: "info", Title: item.Title, Body: item.Summary, DeepLink: "/app/notifications", DedupeKey: "p17-announcement-" + item.ID, OccurredAt: now})
		if err != nil {
			return Announcement{}, false, err
		}
		item.NotificationCount = len(notifications)
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "announcement", ResourceID: item.ID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"state": beforeState, "version": input.ExpectedVersion}, After: map[string]any{"state": item.State, "version": item.Version}, Metadata: map[string]any{"action": input.Action, "scope": item.Scope}, CreatedAt: now})
	if err != nil {
		return Announcement{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return Announcement{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Announcement{}, false, err
	}
	return item, false, nil
}
