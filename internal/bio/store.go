package bio

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Techshrr/GoJet/internal/links"
)

const (
	maxTitleRunes   = 160
	maxBioBytes     = 16 << 10
	maxLabelRunes   = 160
	maxChildLinks   = 100
	publicSlugBytes = 18
)

var (
	ErrInvalidInput   = errors.New("invalid Bio input")
	ErrNotFound       = errors.New("Bio page not found")
	ErrDeleted        = errors.New("Bio page deleted")
	ErrQuota          = errors.New("Bio page quota reached")
	ErrConflict       = errors.New("Bio page version conflict")
	ErrNotPublished   = errors.New("Bio page not published")
	ErrRiskUnresolved = errors.New("Bio child-link risk unresolved")
)

type ChildLink struct {
	ID                     uint64     `json:"id"`
	BioPageID              uint64     `json:"bio_page_id"`
	Position               uint       `json:"position"`
	Label                  string     `json:"label"`
	DestinationURL         string     `json:"destination_url"`
	DestinationFingerprint string     `json:"destination_fingerprint"`
	RiskStatus             string     `json:"risk_status"`
	RiskCheckedAt          *time.Time `json:"risk_checked_at,omitempty"`
}

type Page struct {
	ID          uint64      `json:"id"`
	WorkspaceID string      `json:"workspace_id"`
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Bio         string      `json:"bio"`
	Status      string      `json:"status"`
	Version     uint64      `json:"version"`
	PublishedAt *time.Time  `json:"published_at,omitempty"`
	CreatedBy   string      `json:"created_by"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	DeletedAt   *time.Time  `json:"deleted_at,omitempty"`
	Links       []ChildLink `json:"links"`
}

type ListResult struct {
	Items      []Page `json:"items"`
	Total      int64  `json:"total"`
	QuotaUsed  uint64 `json:"quota_used"`
	QuotaLimit uint64 `json:"quota_limit"`
}

type ChildInput struct {
	ID             uint64
	Position       uint
	Label          string
	DestinationURL string
}

type CreateInput struct {
	WorkspaceID   string
	Title         string
	Bio           string
	Links         []ChildInput
	ActorID       string
	CorrelationID string
	Reason        string
}

type UpdateInput struct {
	WorkspaceID     string
	PageID          uint64
	ExpectedVersion uint64
	Title           *string
	Bio             *string
	Links           *[]ChildInput
	ActorID         string
	CorrelationID   string
	Reason          string
}

type TransitionInput struct {
	WorkspaceID     string
	PageID          uint64
	ExpectedVersion uint64
	Status          string
	ActorID         string
	CorrelationID   string
	Reason          string
}

type DeleteInput struct {
	WorkspaceID     string
	PageID          uint64
	ExpectedVersion uint64
	ActorID         string
	CorrelationID   string
	Reason          string
}

type normalizedChild struct {
	ID                     uint64
	Position               uint
	Label                  string
	DestinationURL         string
	DestinationFingerprint string
}

type Store struct {
	db             *sql.DB
	workspaceQuota uint64
}

func NewStore(db *sql.DB, workspaceQuota uint64) *Store {
	if workspaceQuota == 0 {
		workspaceQuota = 25
	}
	return &Store{db: db, workspaceQuota: workspaceQuota}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("Bio store unavailable")
	}
	return s.db.PingContext(ctx)
}

func normalizeTitle(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxTitleRunes {
		return "", ErrInvalidInput
	}
	return value, nil
}

func normalizeBio(raw string) (string, error) {
	if !utf8.ValidString(raw) || len(raw) > maxBioBytes {
		return "", ErrInvalidInput
	}
	return strings.TrimSpace(raw), nil
}

func normalizeLabel(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxLabelRunes {
		return "", ErrInvalidInput
	}
	return value, nil
}

func normalizeChildren(input []ChildInput, creating bool) ([]normalizedChild, error) {
	if len(input) > maxChildLinks {
		return nil, ErrInvalidInput
	}
	seenID := make(map[uint64]struct{}, len(input))
	seenPosition := make(map[uint]struct{}, len(input))
	result := make([]normalizedChild, 0, len(input))
	for _, item := range input {
		if creating && item.ID != 0 {
			return nil, ErrInvalidInput
		}
		if item.ID != 0 {
			if _, exists := seenID[item.ID]; exists {
				return nil, ErrInvalidInput
			}
			seenID[item.ID] = struct{}{}
		}
		if _, exists := seenPosition[item.Position]; exists {
			return nil, ErrInvalidInput
		}
		seenPosition[item.Position] = struct{}{}
		label, err := normalizeLabel(item.Label)
		if err != nil {
			return nil, err
		}
		normalizedURL, err := links.NormalizeDestination(item.DestinationURL)
		if err != nil {
			return nil, ErrInvalidInput
		}
		fingerprint, _, err := links.RiskFingerprint(normalizedURL, nil, nil)
		if err != nil {
			return nil, ErrInvalidInput
		}
		result = append(result, normalizedChild{
			ID: item.ID, Position: item.Position, Label: label,
			DestinationURL: normalizedURL, DestinationFingerprint: fingerprint,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Position < result[j].Position })
	for i, item := range result {
		if item.Position != uint(i) {
			return nil, ErrInvalidInput
		}
	}
	return result, nil
}

func validateWriteIdentity(workspaceID, actorID, correlationID, reason string) error {
	if strings.TrimSpace(workspaceID) == "" ||
		strings.TrimSpace(actorID) == "" ||
		strings.TrimSpace(correlationID) == "" ||
		strings.TrimSpace(reason) == "" {
		return ErrInvalidInput
	}
	return nil
}

func generateSlug() (string, error) {
	raw := make([]byte, publicSlugBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Page, error) {
	if s == nil || s.db == nil || validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return Page{}, ErrInvalidInput
	}
	title, err := normalizeTitle(input.Title)
	if err != nil {
		return Page{}, err
	}
	bioText, err := normalizeBio(input.Bio)
	if err != nil {
		return Page{}, err
	}
	children, err := normalizeChildren(input.Links, true)
	if err != nil {
		return Page{}, err
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO bio_workspace_counters (workspace_id,active_count) VALUES (?,0) ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id)`, workspaceID); err != nil {
		return Page{}, err
	}
	var used uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count FROM bio_workspace_counters WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&used); err != nil {
		return Page{}, err
	}
	if used >= s.workspaceQuota {
		if err := appendAuditTx(ctx, tx, workspaceID, nil, actorID, correlationID, "bio.create", reason, "denied", map[string]any{"reason": "quota", "quota_limit": s.workspaceQuota}); err != nil {
			return Page{}, err
		}
		if err := tx.Commit(); err != nil {
			return Page{}, err
		}
		return Page{}, ErrQuota
	}

	slug, err := generateSlug()
	if err != nil {
		return Page{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO bio_pages (workspace_id,slug,title,bio,status,created_by) VALUES (?,?,?,?,'draft',?)`, workspaceID, slug, title, bioText, actorID)
	if err != nil {
		return Page{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return Page{}, err
	}
	pageID := uint64(insertID)
	for _, child := range children {
		if _, err := tx.ExecContext(ctx, `INSERT INTO bio_child_links (bio_page_id,position,label,destination_url,destination_fingerprint,risk_status) VALUES (?,?,?,?,?,'review')`,
			pageID, child.Position, child.Label, child.DestinationURL, child.DestinationFingerprint); err != nil {
			return Page{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bio_workspace_counters SET active_count=active_count+1 WHERE workspace_id=?`, workspaceID); err != nil {
		return Page{}, err
	}
	created, err := getPageTx(ctx, tx, workspaceID, pageID, false)
	if err != nil {
		return Page{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &pageID, actorID, correlationID, "bio.create", reason, "success", map[string]any{
		"status": created.Status, "links": len(created.Links), "quota_used": used + 1, "quota_limit": s.workspaceQuota,
	}); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(); err != nil {
		return Page{}, err
	}
	return created, nil
}

func (s *Store) List(ctx context.Context, workspaceID string, limit, offset int) (ListResult, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || offset < 0 {
		return ListResult{}, ErrInvalidInput
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bio_pages WHERE workspace_id=? AND deleted_at IS NULL`, workspaceID).Scan(&total); err != nil {
		return ListResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, pageSelect+` WHERE workspace_id=? AND deleted_at IS NULL ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, workspaceID, limit, offset)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items := make([]Page, 0, min(limit, int(total)))
	for rows.Next() {
		page, err := scanPage(rows)
		if err != nil {
			return ListResult{}, err
		}
		page.Links, err = loadChildren(ctx, s.db, page.ID)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, page)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	var used uint64
	err = s.db.QueryRowContext(ctx, `SELECT active_count FROM bio_workspace_counters WHERE workspace_id=?`, workspaceID).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		used = 0
	} else if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total, QuotaUsed: used, QuotaLimit: s.workspaceQuota}, nil
}

func (s *Store) Get(ctx context.Context, workspaceID string, pageID uint64) (Page, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || pageID == 0 {
		return Page{}, ErrInvalidInput
	}
	page, err := scanPage(s.db.QueryRowContext(ctx, pageSelect+` WHERE workspace_id=? AND id=?`, strings.TrimSpace(workspaceID), pageID))
	if err != nil {
		return Page{}, err
	}
	if page.DeletedAt != nil {
		return Page{}, ErrDeleted
	}
	page.Links, err = loadChildren(ctx, s.db, page.ID)
	return page, err
}

func (s *Store) GetPublic(ctx context.Context, slug string) (Page, error) {
	if s == nil || s.db == nil || strings.TrimSpace(slug) == "" {
		return Page{}, ErrInvalidInput
	}
	page, err := scanPage(s.db.QueryRowContext(ctx, pageSelect+` WHERE slug=?`, strings.TrimSpace(slug)))
	if err != nil {
		return Page{}, err
	}
	page.Links, err = loadChildren(ctx, s.db, page.ID)
	return page, err
}

func (s *Store) Update(ctx context.Context, input UpdateInput) (Page, error) {
	if s == nil || s.db == nil || input.PageID == 0 || input.ExpectedVersion == 0 ||
		validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return Page{}, ErrInvalidInput
	}
	if input.Title == nil && input.Bio == nil && input.Links == nil {
		return Page{}, ErrInvalidInput
	}
	var titleValue *string
	if input.Title != nil {
		value, err := normalizeTitle(*input.Title)
		if err != nil {
			return Page{}, err
		}
		titleValue = &value
	}
	var bioValue *string
	if input.Bio != nil {
		value, err := normalizeBio(*input.Bio)
		if err != nil {
			return Page{}, err
		}
		bioValue = &value
	}
	var childValues []normalizedChild
	var err error
	if input.Links != nil {
		childValues, err = normalizeChildren(*input.Links, false)
		if err != nil {
			return Page{}, err
		}
	}

	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	current, err := getPageTx(ctx, tx, workspaceID, input.PageID, true)
	if err != nil {
		return Page{}, err
	}
	if current.DeletedAt != nil {
		return Page{}, ErrDeleted
	}
	if current.Version != input.ExpectedVersion {
		_ = appendAuditTx(ctx, tx, workspaceID, &input.PageID, actorID, correlationID, "bio.update", reason, "conflict", map[string]any{"expected_version": input.ExpectedVersion, "current_version": current.Version})
		return Page{}, ErrConflict
	}

	title := current.Title
	bioText := current.Bio
	if titleValue != nil {
		title = *titleValue
	}
	if bioValue != nil {
		bioText = *bioValue
	}

	if input.Links != nil {
		currentByID := make(map[uint64]ChildLink, len(current.Links))
		for _, child := range current.Links {
			currentByID[child.ID] = child
		}
		for _, child := range childValues {
			if child.ID != 0 {
				if _, exists := currentByID[child.ID]; !exists {
					return Page{}, ErrInvalidInput
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bio_child_links SET position=position+1000000 WHERE bio_page_id=?`, input.PageID); err != nil {
			return Page{}, err
		}
		kept := make(map[uint64]struct{}, len(childValues))
		for _, child := range childValues {
			if child.ID == 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO bio_child_links (bio_page_id,position,label,destination_url,destination_fingerprint,risk_status,risk_checked_at) VALUES (?,?,?,?,?,'review',NULL)`,
					input.PageID, child.Position, child.Label, child.DestinationURL, child.DestinationFingerprint); err != nil {
					return Page{}, err
				}
				continue
			}
			previous := currentByID[child.ID]
			riskStatus := previous.RiskStatus
			riskCheckedAt := previous.RiskCheckedAt
			if previous.DestinationFingerprint != child.DestinationFingerprint {
				riskStatus = "review"
				riskCheckedAt = nil
			}
			if _, err := tx.ExecContext(ctx, `UPDATE bio_child_links SET position=?,label=?,destination_url=?,destination_fingerprint=?,risk_status=?,risk_checked_at=?,updated_at=CURRENT_TIMESTAMP(6) WHERE bio_page_id=? AND id=?`,
				child.Position, child.Label, child.DestinationURL, child.DestinationFingerprint, riskStatus, riskCheckedAt, input.PageID, child.ID); err != nil {
				return Page{}, err
			}
			kept[child.ID] = struct{}{}
		}
		for id := range currentByID {
			if _, exists := kept[id]; exists {
				continue
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM bio_child_links WHERE bio_page_id=? AND id=?`, input.PageID, id); err != nil {
				return Page{}, err
			}
		}
	}

	result, err := tx.ExecContext(ctx, `UPDATE bio_pages SET title=?,bio=?,version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id=? AND id=? AND deleted_at IS NULL AND version=?`,
		title, bioText, workspaceID, input.PageID, input.ExpectedVersion)
	if err != nil {
		return Page{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Page{}, err
	}
	if affected != 1 {
		return Page{}, ErrConflict
	}
	updated, err := getPageTx(ctx, tx, workspaceID, input.PageID, false)
	if err != nil {
		return Page{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &input.PageID, actorID, correlationID, "bio.update", reason, "success", map[string]any{
		"version": updated.Version, "status": updated.Status, "links": len(updated.Links),
	}); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(); err != nil {
		return Page{}, err
	}
	return updated, nil
}

func (s *Store) Transition(ctx context.Context, input TransitionInput) (Page, error) {
	if s == nil || s.db == nil || input.PageID == 0 || input.ExpectedVersion == 0 ||
		validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return Page{}, ErrInvalidInput
	}
	status := strings.ToLower(strings.TrimSpace(input.Status))
	if status != "published" && status != "paused" {
		return Page{}, ErrInvalidInput
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	current, err := getPageTx(ctx, tx, workspaceID, input.PageID, true)
	if err != nil {
		return Page{}, err
	}
	if current.DeletedAt != nil {
		return Page{}, ErrDeleted
	}
	if current.Version != input.ExpectedVersion {
		return Page{}, ErrConflict
	}
	if status == "published" {
		var unresolved uint64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM bio_child_links WHERE bio_page_id=? AND risk_status<>'allowed'`, input.PageID).Scan(&unresolved); err != nil {
			return Page{}, err
		}
		if unresolved != 0 {
			if err := appendAuditTx(ctx, tx, workspaceID, &input.PageID, actorID, correlationID, "bio.publish", reason, "denied", map[string]any{"reason": "child_link_risk", "unresolved_count": unresolved}); err != nil {
				return Page{}, err
			}
			if err := tx.Commit(); err != nil {
				return Page{}, err
			}
			return Page{}, ErrRiskUnresolved
		}
	}
	action := "bio.pause"
	publishedAtSQL := "published_at"
	if status == "published" {
		action = "bio.publish"
		publishedAtSQL = "CURRENT_TIMESTAMP(6)"
	}
	query := fmt.Sprintf(`UPDATE bio_pages SET status=?,published_at=%s,version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id=? AND id=? AND deleted_at IS NULL AND version=?`, publishedAtSQL)
	result, err := tx.ExecContext(ctx, query, status, workspaceID, input.PageID, input.ExpectedVersion)
	if err != nil {
		return Page{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Page{}, err
	}
	if affected != 1 {
		return Page{}, ErrConflict
	}
	updated, err := getPageTx(ctx, tx, workspaceID, input.PageID, false)
	if err != nil {
		return Page{}, err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &input.PageID, actorID, correlationID, action, reason, "success", map[string]any{"version": updated.Version, "status": updated.Status}); err != nil {
		return Page{}, err
	}
	if err := tx.Commit(); err != nil {
		return Page{}, err
	}
	return updated, nil
}

func (s *Store) Delete(ctx context.Context, input DeleteInput) error {
	if s == nil || s.db == nil || input.PageID == 0 || input.ExpectedVersion == 0 ||
		validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return ErrInvalidInput
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := getPageTx(ctx, tx, workspaceID, input.PageID, true)
	if err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return ErrDeleted
	}
	if current.Version != input.ExpectedVersion {
		return ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE bio_pages SET deleted_at=CURRENT_TIMESTAMP(6),version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id=? AND id=? AND deleted_at IS NULL AND version=?`, workspaceID, input.PageID, input.ExpectedVersion)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bio_workspace_counters SET active_count=IF(active_count>0,active_count-1,0) WHERE workspace_id=?`, workspaceID); err != nil {
		return err
	}
	if err := appendAuditTx(ctx, tx, workspaceID, &input.PageID, actorID, correlationID, "bio.delete", reason, "success", map[string]any{"previous_status": current.Status}); err != nil {
		return err
	}
	return tx.Commit()
}

// SyncChildRisk writes only a decision for the currently-bound fingerprint.
// A concurrent destination change makes the update a no-op and therefore
// cannot resurrect an approval for a stale destination.
func (s *Store) SyncChildRisk(ctx context.Context, childID uint64, fingerprint, status string, checkedAt *time.Time) error {
	if s == nil || s.db == nil || childID == 0 || !validFingerprint(fingerprint) {
		return ErrInvalidInput
	}
	if status != "review" && status != "allowed" && status != "blocked" {
		return ErrInvalidInput
	}
	result, err := s.db.ExecContext(ctx, `UPDATE bio_child_links SET risk_status=?,risk_checked_at=?,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND destination_fingerprint=?`, status, checkedAt, childID, fingerprint)
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

const pageSelect = `SELECT id,workspace_id,slug,title,bio,status,version,published_at,created_by,created_at,updated_at,deleted_at FROM bio_pages`

type scanner interface {
	Scan(dest ...any) error
}

func scanPage(row scanner) (Page, error) {
	var page Page
	var publishedAt, deletedAt sql.NullTime
	if err := row.Scan(&page.ID, &page.WorkspaceID, &page.Slug, &page.Title, &page.Bio, &page.Status, &page.Version, &publishedAt, &page.CreatedBy, &page.CreatedAt, &page.UpdatedAt, &deletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Page{}, ErrNotFound
		}
		return Page{}, err
	}
	if publishedAt.Valid {
		value := publishedAt.Time.UTC()
		page.PublishedAt = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time.UTC()
		page.DeletedAt = &value
	}
	return page, nil
}

type childQueryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadChildren(ctx context.Context, queryer childQueryer, pageID uint64) ([]ChildLink, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id,bio_page_id,position,label,destination_url,destination_fingerprint,risk_status,risk_checked_at FROM bio_child_links WHERE bio_page_id=? ORDER BY position ASC,id ASC`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ChildLink, 0)
	for rows.Next() {
		var child ChildLink
		var checkedAt sql.NullTime
		if err := rows.Scan(&child.ID, &child.BioPageID, &child.Position, &child.Label, &child.DestinationURL, &child.DestinationFingerprint, &child.RiskStatus, &checkedAt); err != nil {
			return nil, err
		}
		if checkedAt.Valid {
			value := checkedAt.Time.UTC()
			child.RiskCheckedAt = &value
		}
		result = append(result, child)
	}
	return result, rows.Err()
}

func getPageTx(ctx context.Context, tx *sql.Tx, workspaceID string, pageID uint64, lock bool) (Page, error) {
	query := pageSelect + ` WHERE workspace_id=? AND id=?`
	if lock {
		query += ` FOR UPDATE`
	}
	page, err := scanPage(tx.QueryRowContext(ctx, query, workspaceID, pageID))
	if err != nil {
		return Page{}, err
	}
	page.Links, err = loadChildren(ctx, tx, page.ID)
	return page, err
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, pageID *uint64, actorID, correlationID, action, reason, result string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO bio_audit_events (workspace_id,bio_page_id,actor_id,action,request_correlation_id,reason,result,metadata_json) VALUES (?,?,?,?,?,?,?,?)`,
		workspaceID, nullableUint64(pageID), actorID, action, correlationID, nullableString(reason), result, raw)
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableUint64(value *uint64) any {
	if value == nil {
		return nil
	}
	return *value
}
