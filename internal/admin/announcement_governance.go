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
	Version           uint64     `json:"version"`
	CacheGeneration   uint64     `json:"cache_generation"`
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
	ScheduledFor    *time.Time `json:"scheduled_for,omitempty"`
}

func validateAnnouncementInput(input CreateAnnouncementInput) (CreateAnnouncementInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Body = strings.TrimSpace(input.Body)
	input.Scope = strings.TrimSpace(input.Scope)
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.Title == "" || len(input.Title) > 200 || len(input.Summary) > 500 || input.Body == "" || len(input.Body) > 20000 {
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
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id,a.title,a.summary,a.body,a.scope,COALESCE(a.workspace_id,''),a.lifecycle_state,a.scheduled_for,a.published_at,a.archived_at,a.version,c.generation,a.created_at,a.updated_at FROM admin_announcements a JOIN admin_content_cache_state c ON c.cache_key='announcements' ORDER BY a.updated_at DESC,a.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Announcement
	for rows.Next() {
		item, err := scanAnnouncement(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type rowScanner func(dest ...any) error

func scanAnnouncement(scan rowScanner) (Announcement, error) {
	var item Announcement
	var scheduled, published, archived sql.NullTime
	if err := scan(&item.ID, &item.Title, &item.Summary, &item.Body, &item.Scope, &item.WorkspaceID, &item.State, &scheduled, &published, &archived, &item.Version, &item.CacheGeneration, &item.CreatedAt, &item.UpdatedAt); err != nil {
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
	clean, err := validateAnnouncementInput(input)
	if err != nil {
		return Announcement{}, false, err
	}
	fingerprint, err := requestFingerprint(clean)
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
	if clean.Scope == "workspace" {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE id=?`, clean.WorkspaceID).Scan(&exists); err != nil {
			return Announcement{}, false, err
		}
		if exists != 1 {
			return Announcement{}, false, ErrNotFound
		}
	}
	id, err := newOpaque("aan_", 18)
	if err != nil {
		return Announcement{}, false, err
	}
	var workspaceID any
	if clean.WorkspaceID != "" {
		workspaceID = clean.WorkspaceID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_announcements(id,title,summary,body,scope,workspace_id,lifecycle_state,version,created_by,updated_by,created_at,updated_at) VALUES (?,?,?,?,?,?,'draft',1,?,?,?,?)`, id, clean.Title, clean.Summary, clean.Body, clean.Scope, workspaceID, p.Administrator.ID, p.Administrator.ID, now, now)
	if err != nil {
		return Announcement{}, false, err
	}
	var generation uint64
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM admin_content_cache_state WHERE cache_key='announcements'`).Scan(&generation); err != nil {
		return Announcement{}, false, err
	}
	item := Announcement{ID: id, Title: clean.Title, Summary: clean.Summary, Body: clean.Body, Scope: clean.Scope, WorkspaceID: clean.WorkspaceID, State: "draft", Version: 1, CacheGeneration: generation, CreatedAt: now, UpdatedAt: now}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "announcement", ResourceID: id, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, After: map[string]any{"state": "draft", "version": uint64(1), "workspace_id": clean.WorkspaceID}, Metadata: map[string]any{"authority": "content.manage"}, CreatedAt: now})
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
	switch input.Action {
	case "schedule", "publish", "archive":
	default:
		return Announcement{}, false, ErrInvalid
	}
	if input.Action == "schedule" {
		if input.ScheduledFor == nil || !input.ScheduledFor.UTC().After(now.UTC()) {
			return Announcement{}, false, ErrInvalid
		}
	} else if input.ScheduledFor != nil {
		return Announcement{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		ID      string
		Input   AnnouncementActionInput
		Version uint64
	}{announcementID, input, input.ExpectedVersion})
	if err != nil {
		return Announcement{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
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
		return tx.QueryRowContext(ctx, `SELECT a.id,a.title,a.summary,a.body,a.scope,COALESCE(a.workspace_id,''),a.lifecycle_state,a.scheduled_for,a.published_at,a.archived_at,a.version,c.generation,a.created_at,a.updated_at FROM admin_announcements a JOIN admin_content_cache_state c ON c.cache_key='announcements' WHERE a.id=? FOR UPDATE`, announcementID).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Announcement{}, false, ErrNotFound
	}
	if err != nil {
		return Announcement{}, false, err
	}
	if input.ExpectedVersion != item.Version {
		return Announcement{}, false, ErrConflict
	}
	beforeState := item.State
	switch input.Action {
	case "schedule":
		if item.State != "draft" {
			return Announcement{}, false, ErrConflict
		}
		t := input.ScheduledFor.UTC().Truncate(time.Microsecond)
		item.ScheduledFor = &t
		item.State = "scheduled"
	case "publish":
		if item.State != "draft" && item.State != "scheduled" {
			return Announcement{}, false, ErrConflict
		}
		item.State = "published"
		item.ScheduledFor = nil
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
	item.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `UPDATE admin_content_cache_state SET generation=generation+1,updated_at=? WHERE cache_key='announcements'`, now)
	if err != nil {
		return Announcement{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM admin_content_cache_state WHERE cache_key='announcements'`).Scan(&item.CacheGeneration); err != nil {
		return Announcement{}, false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE admin_announcements SET lifecycle_state=?,scheduled_for=?,published_at=?,archived_at=?,version=?,updated_by=?,updated_at=? WHERE id=?`, item.State, item.ScheduledFor, item.PublishedAt, item.ArchivedAt, item.Version, p.Administrator.ID, now, item.ID)
	if err != nil {
		return Announcement{}, false, err
	}
	if input.Action == "publish" && item.Scope == "workspace" {
		notifications, err := workspace.ProduceOwnerNotificationsTx(ctx, tx, workspace.NotificationInput{
			WorkspaceID:  item.WorkspaceID,
			Category:     "resources",
			EventKey:     "announcement.published",
			DedupeKey:    "p17-announcement-" + item.ID,
			Title:        "Announcement published",
			Summary:      item.Title,
			DeepLink:     "/app/notifications",
			ResourceType: "announcement",
			ResourceID:   item.ID,
		})
		if err != nil {
			return Announcement{}, false, err
		}
		item.NotificationCount = len(notifications)
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "announcement", ResourceID: item.ID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"state": beforeState, "version": input.ExpectedVersion}, After: map[string]any{"state": item.State, "version": item.Version, "workspace_id": item.WorkspaceID}, Metadata: map[string]any{"authority": "content.manage"}, CreatedAt: now})
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
