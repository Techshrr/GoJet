package textshares

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxTitleRunes   = 160
	maxContentBytes = 1 << 20
	publicSlugBytes = 18
)

var (
	ErrInvalidInput     = errors.New("invalid text input")
	ErrNotFound         = errors.New("text share not found")
	ErrDeleted          = errors.New("text share deleted")
	ErrQuota            = errors.New("text share quota reached")
	ErrConflict         = errors.New("text share version conflict")
	ErrPrivate          = errors.New("text share private")
	ErrExpired          = errors.New("text share expired")
	ErrConsumed         = errors.New("text share consumed")
	ErrPasswordRequired = errors.New("text share password required")
)

type Resource struct {
	ID               uint64     `json:"id"`
	WorkspaceID      string     `json:"workspace_id"`
	PublicSlug       string     `json:"public_slug"`
	Title            string     `json:"title"`
	Content          string     `json:"content"`
	Visibility       string     `json:"visibility"`
	PasswordRequired bool       `json:"password_required"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	OneTime          bool       `json:"one_time"`
	ConsumedAt       *time.Time `json:"consumed_at,omitempty"`
	Version          uint64     `json:"version"`
	CreatedBy        string     `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
}

type storedResource struct {
	Resource
	PasswordHash string
}

type ListResult struct {
	Items      []Resource `json:"items"`
	Total      int64      `json:"total"`
	QuotaUsed  uint64     `json:"quota_used"`
	QuotaLimit uint64     `json:"quota_limit"`
}

type CreateInput struct {
	WorkspaceID   string
	Title         string
	Content       string
	Visibility    string
	PasswordHash  string
	ExpiresAt     *time.Time
	OneTime       bool
	ActorID       string
	CorrelationID string
	Reason        string
}

type UpdateInput struct {
	WorkspaceID     string
	ShareID         uint64
	ExpectedVersion uint64
	Title           *string
	Content         *string
	Visibility      *string
	PasswordHash    *string
	ClearPassword   bool
	ExpiresAt       *time.Time
	ClearExpiresAt  bool
	OneTime         *bool
	ActorID         string
	CorrelationID   string
	Reason          string
}

type DeleteInput struct {
	WorkspaceID     string
	ShareID         uint64
	ExpectedVersion uint64
	ActorID         string
	CorrelationID   string
	Reason          string
}

type Store struct {
	db             *sql.DB
	workspaceQuota uint64
}

func NewStore(db *sql.DB, workspaceQuota uint64) *Store {
	if workspaceQuota == 0 {
		workspaceQuota = 100
	}
	return &Store{db: db, workspaceQuota: workspaceQuota}
}

func (s *Store) Ping(ctx context.Context) error {
	if s == nil || s.db == nil {
		return errors.New("text store unavailable")
	}
	return s.db.PingContext(ctx)
}

func normalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > maxTitleRunes {
		return "", ErrInvalidInput
	}
	return title, nil
}

func normalizeContent(raw string) (string, error) {
	if !utf8.ValidString(raw) || strings.TrimSpace(raw) == "" || len(raw) > maxContentBytes {
		return "", ErrInvalidInput
	}
	return raw, nil
}

func normalizeVisibility(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = "private"
	}
	if value != "private" && value != "public" {
		return "", ErrInvalidInput
	}
	return value, nil
}

func validateWriteIdentity(workspaceID, actorID, correlationID, reason string) error {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalidInput
	}
	return nil
}

func generatePublicSlug() (string, error) {
	raw := make([]byte, publicSlugBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Store) Create(ctx context.Context, input CreateInput) (Resource, error) {
	if s == nil || s.db == nil || validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return Resource{}, ErrInvalidInput
	}
	title, err := normalizeTitle(input.Title)
	if err != nil {
		return Resource{}, err
	}
	content, err := normalizeContent(input.Content)
	if err != nil {
		return Resource{}, err
	}
	visibility, err := normalizeVisibility(input.Visibility)
	if err != nil {
		return Resource{}, err
	}
	passwordHash := strings.TrimSpace(input.PasswordHash)
	if input.PasswordHash != "" && passwordHash == "" {
		return Resource{}, ErrInvalidInput
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)
	if input.ExpiresAt != nil {
		expires := input.ExpiresAt.UTC()
		input.ExpiresAt = &expires
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO text_workspace_counters (workspace_id, active_count) VALUES (?,0) ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id)`, workspaceID); err != nil {
		return Resource{}, err
	}
	var used uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count FROM text_workspace_counters WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&used); err != nil {
		return Resource{}, err
	}
	if used >= s.workspaceQuota {
		if err := appendAuditTx(ctx, tx, workspaceID, nil, actorID, correlationID, "text.create", reason, "denied", map[string]any{"reason": "quota", "quota_limit": s.workspaceQuota}); err != nil {
			return Resource{}, err
		}
		if err := tx.Commit(); err != nil {
			return Resource{}, err
		}
		return Resource{}, ErrQuota
	}
	slug, err := generatePublicSlug()
	if err != nil {
		return Resource{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO text_shares (workspace_id,public_slug,title,content,visibility,password_hash,expires_at,one_time,created_by) VALUES (?,?,?,?,?,?,?,?,?)`,
		workspaceID, slug, title, content, visibility, nullableString(passwordHash), input.ExpiresAt, input.OneTime, actorID)
	if err != nil {
		return Resource{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return Resource{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE text_workspace_counters SET active_count=active_count+1 WHERE workspace_id=?`, workspaceID); err != nil {
		return Resource{}, err
	}
	created, err := getTx(ctx, tx, workspaceID, uint64(insertID), false)
	if err != nil {
		return Resource{}, err
	}
	shareID := created.ID
	if err := appendAuditTx(ctx, tx, workspaceID, &shareID, actorID, correlationID, "text.create", reason, "success", map[string]any{
		"visibility": created.Visibility, "password_required": created.PasswordRequired, "one_time": created.OneTime,
		"expires_at_set": created.ExpiresAt != nil, "quota_used": used + 1, "quota_limit": s.workspaceQuota,
	}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return created.Resource, nil
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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM text_shares WHERE workspace_id=? AND deleted_at IS NULL`, workspaceID).Scan(&total); err != nil {
		return ListResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, resourceSelect+` WHERE workspace_id=? AND deleted_at IS NULL ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, workspaceID, limit, offset)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items := make([]Resource, 0, min(limit, int(total)))
	for rows.Next() {
		item, err := scanStored(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, item.Resource)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	var used uint64
	err = s.db.QueryRowContext(ctx, `SELECT active_count FROM text_workspace_counters WHERE workspace_id=?`, workspaceID).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		used = 0
	} else if err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total, QuotaUsed: used, QuotaLimit: s.workspaceQuota}, nil
}

func (s *Store) Get(ctx context.Context, workspaceID string, id uint64) (Resource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(workspaceID) == "" || id == 0 {
		return Resource{}, ErrInvalidInput
	}
	stored, err := scanStored(s.db.QueryRowContext(ctx, resourceSelect+` WHERE workspace_id=? AND id=?`, strings.TrimSpace(workspaceID), id))
	if err != nil {
		return Resource{}, err
	}
	if stored.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	return stored.Resource, nil
}

func (s *Store) Update(ctx context.Context, input UpdateInput) (Resource, error) {
	if s == nil || s.db == nil || input.ShareID == 0 || input.ExpectedVersion == 0 || validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
		return Resource{}, ErrInvalidInput
	}
	hasChange := input.Title != nil || input.Content != nil || input.Visibility != nil || input.PasswordHash != nil || input.ClearPassword || input.ExpiresAt != nil || input.ClearExpiresAt || input.OneTime != nil
	if !hasChange || (input.PasswordHash != nil && input.ClearPassword) || (input.ExpiresAt != nil && input.ClearExpiresAt) {
		return Resource{}, ErrInvalidInput
	}
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	actorID := strings.TrimSpace(input.ActorID)
	correlationID := strings.TrimSpace(input.CorrelationID)
	reason := strings.TrimSpace(input.Reason)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()
	current, err := getTx(ctx, tx, workspaceID, input.ShareID, true)
	if err != nil {
		return Resource{}, err
	}
	if current.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	if current.ConsumedAt != nil {
		return Resource{}, ErrConsumed
	}
	if current.Version != input.ExpectedVersion {
		return Resource{}, ErrConflict
	}
	title := current.Title
	content := current.Content
	visibility := current.Visibility
	passwordHash := current.PasswordHash
	expiresAt := current.ExpiresAt
	oneTime := current.OneTime
	if input.Title != nil {
		title, err = normalizeTitle(*input.Title)
		if err != nil {
			return Resource{}, err
		}
	}
	if input.Content != nil {
		content, err = normalizeContent(*input.Content)
		if err != nil {
			return Resource{}, err
		}
	}
	if input.Visibility != nil {
		visibility, err = normalizeVisibility(*input.Visibility)
		if err != nil {
			return Resource{}, err
		}
	}
	if input.ClearPassword {
		passwordHash = ""
	} else if input.PasswordHash != nil {
		passwordHash = strings.TrimSpace(*input.PasswordHash)
		if passwordHash == "" {
			return Resource{}, ErrInvalidInput
		}
	}
	if input.ClearExpiresAt {
		expiresAt = nil
	} else if input.ExpiresAt != nil {
		expires := input.ExpiresAt.UTC()
		expiresAt = &expires
	}
	if input.OneTime != nil {
		oneTime = *input.OneTime
	}
	result, err := tx.ExecContext(ctx, `UPDATE text_shares SET title=?,content=?,visibility=?,password_hash=?,expires_at=?,one_time=?,version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id=? AND id=? AND deleted_at IS NULL AND consumed_at IS NULL AND version=?`,
		title, content, visibility, nullableString(passwordHash), expiresAt, oneTime, workspaceID, input.ShareID, input.ExpectedVersion)
	if err != nil {
		return Resource{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Resource{}, err
	}
	if affected != 1 {
		return Resource{}, ErrConflict
	}
	updated, err := getTx(ctx, tx, workspaceID, input.ShareID, false)
	if err != nil {
		return Resource{}, err
	}
	shareID := updated.ID
	if err := appendAuditTx(ctx, tx, workspaceID, &shareID, actorID, correlationID, "text.update", reason, "success", map[string]any{
		"version": updated.Version, "visibility": updated.Visibility, "password_required": updated.PasswordRequired, "one_time": updated.OneTime, "expires_at_set": updated.ExpiresAt != nil,
	}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return updated.Resource, nil
}

func (s *Store) Delete(ctx context.Context, input DeleteInput) error {
	if s == nil || s.db == nil || input.ShareID == 0 || input.ExpectedVersion == 0 || validateWriteIdentity(input.WorkspaceID, input.ActorID, input.CorrelationID, input.Reason) != nil {
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
	current, err := getTx(ctx, tx, workspaceID, input.ShareID, true)
	if err != nil {
		return err
	}
	if current.DeletedAt != nil {
		return ErrDeleted
	}
	if current.Version != input.ExpectedVersion {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO text_workspace_counters (workspace_id,active_count) VALUES (?,0) ON DUPLICATE KEY UPDATE workspace_id=VALUES(workspace_id)`, workspaceID); err != nil {
		return err
	}
	var used uint64
	if err := tx.QueryRowContext(ctx, `SELECT active_count FROM text_workspace_counters WHERE workspace_id=? FOR UPDATE`, workspaceID).Scan(&used); err != nil {
		return err
	}
	if used == 0 {
		return errors.New("text counter invariant violated")
	}
	result, err := tx.ExecContext(ctx, `UPDATE text_shares SET deleted_at=CURRENT_TIMESTAMP(6),version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id=? AND id=? AND deleted_at IS NULL AND version=?`, workspaceID, input.ShareID, input.ExpectedVersion)
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
	if _, err := tx.ExecContext(ctx, `UPDATE text_workspace_counters SET active_count=active_count-1 WHERE workspace_id=? AND active_count>0`, workspaceID); err != nil {
		return err
	}
	shareID := current.ID
	if err := appendAuditTx(ctx, tx, workspaceID, &shareID, actorID, correlationID, "text.delete", reason, "success", map[string]any{"quota_used": used - 1, "quota_limit": s.workspaceQuota}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPublic(ctx context.Context, slug string) (storedResource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(slug) == "" || len(slug) > 64 {
		return storedResource{}, ErrNotFound
	}
	return scanStored(s.db.QueryRowContext(ctx, resourceSelect+` WHERE public_slug=?`, strings.TrimSpace(slug)))
}

// ConsumePublic returns the current public record and atomically marks a one-time
// resource consumed. The first authorized caller receives the content returned
// by this transaction; later callers observe ErrConsumed.
func (s *Store) ConsumePublic(ctx context.Context, slug string, now time.Time) (storedResource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(slug) == "" {
		return storedResource{}, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return storedResource{}, err
	}
	defer tx.Rollback()
	current, err := scanStored(tx.QueryRowContext(ctx, resourceSelect+` WHERE public_slug=? FOR UPDATE`, strings.TrimSpace(slug)))
	if err != nil {
		return storedResource{}, err
	}
	if err := validatePublicLifecycle(current, now); err != nil {
		return storedResource{}, err
	}
	if !current.OneTime {
		return current, tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `UPDATE text_shares SET consumed_at=?,version=version+1,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND consumed_at IS NULL AND deleted_at IS NULL`, now.UTC(), current.ID)
	if err != nil {
		return storedResource{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storedResource{}, err
	}
	if affected != 1 {
		return storedResource{}, ErrConsumed
	}
	shareID := current.ID
	if err := appendAuditTx(ctx, tx, current.WorkspaceID, &shareID, "public", "public-text-consume", "text.consume", "one-time public access", "success", map[string]any{"one_time": true}); err != nil {
		return storedResource{}, err
	}
	if err := tx.Commit(); err != nil {
		return storedResource{}, err
	}
	return current, nil
}

func validatePublicLifecycle(current storedResource, now time.Time) error {
	if current.DeletedAt != nil {
		return ErrDeleted
	}
	if current.Visibility != "public" {
		return ErrPrivate
	}
	if current.ExpiresAt != nil && !now.Before(current.ExpiresAt.UTC()) {
		return ErrExpired
	}
	if current.ConsumedAt != nil {
		return ErrConsumed
	}
	return nil
}

const resourceSelect = `SELECT id,workspace_id,public_slug,title,content,visibility,password_hash,expires_at,one_time,consumed_at,version,created_by,created_at,updated_at,deleted_at FROM text_shares`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStored(row rowScanner) (storedResource, error) {
	var item storedResource
	var passwordHash sql.NullString
	if err := row.Scan(&item.ID, &item.WorkspaceID, &item.PublicSlug, &item.Title, &item.Content, &item.Visibility, &passwordHash, &item.ExpiresAt, &item.OneTime, &item.ConsumedAt, &item.Version, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedResource{}, ErrNotFound
		}
		return storedResource{}, err
	}
	if passwordHash.Valid {
		item.PasswordHash = passwordHash.String
		item.PasswordRequired = passwordHash.String != ""
	}
	return item, nil
}

func getTx(ctx context.Context, tx *sql.Tx, workspaceID string, id uint64, forUpdate bool) (storedResource, error) {
	query := resourceSelect + ` WHERE workspace_id=? AND id=?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanStored(tx.QueryRowContext(ctx, query, workspaceID, id))
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, shareID *uint64, actorID, correlationID, action, reason, result string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO text_audit_events (workspace_id,text_share_id,actor_id,action,request_correlation_id,reason,result,metadata_json) VALUES (?,?,?,?,?,?,?,?)`,
		workspaceID, shareID, actorID, action, correlationID, reason, result, string(raw))
	return err
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
