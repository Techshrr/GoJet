package links

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ListOptions struct {
	WorkspaceID string
	Query       string
	Hostname    string
	Status      string
	CampaignID  string
	TagID       uint64
	FolderID    uint64
	UpdatedFrom *time.Time
	UpdatedTo   *time.Time
	Limit       int
	Offset      int
}

type ListResult struct {
	Items []Link `json:"items"`
	Total int64  `json:"total"`
}

type AccessClaimState string

const (
	AccessClaimAllowed   AccessClaimState = "allowed"
	AccessClaimPaused    AccessClaimState = "paused"
	AccessClaimExpired   AccessClaimState = "expired"
	AccessClaimExhausted AccessClaimState = "exhausted"
	AccessClaimDeleted   AccessClaimState = "deleted"
	AccessClaimConflict  AccessClaimState = "conflict"
)

func (s *MySQLStore) List(ctx context.Context, options ListOptions) (ListResult, error) {
	workspaceID := strings.TrimSpace(options.WorkspaceID)
	if workspaceID == "" {
		return ListResult{}, ErrInvalidInput
	}
	limit := options.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if options.Offset < 0 {
		return ListResult{}, ErrInvalidInput
	}

	where := []string{"links.workspace_id = ?"}
	args := []any{workspaceID}
	if query := strings.TrimSpace(options.Query); query != "" {
		where = append(where, "(links.code LIKE ? ESCAPE '\\\\' OR links.title LIKE ? ESCAPE '\\\\' OR links.primary_destination LIKE ? ESCAPE '\\\\')")
		pattern := "%" + escapeLike(query) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if hostname := strings.TrimSpace(options.Hostname); hostname != "" {
		normalized, err := normalizeHostname(hostname)
		if err != nil {
			return ListResult{}, err
		}
		where = append(where, "links.hostname = ?")
		args = append(args, normalized)
	}
	if status := strings.TrimSpace(options.Status); status != "" {
		if status != "deleted" {
			if err := validateLifecycleStatus(status); err != nil {
				return ListResult{}, err
			}
		}
		where = append(where, "links.status = ?")
		args = append(args, status)
	} else {
		where = append(where, "links.status <> 'deleted'")
	}
	if campaignID := strings.TrimSpace(options.CampaignID); campaignID != "" {
		if len(campaignID) > 64 {
			return ListResult{}, ErrInvalidInput
		}
		where = append(where, `EXISTS (
			SELECT 1 FROM workspace_link_organization wlo
			WHERE wlo.workspace_id=links.workspace_id AND wlo.link_id=links.id AND wlo.campaign_id=?
		)`)
		args = append(args, campaignID)
	}
	if options.TagID != 0 {
		where = append(where, `EXISTS (
			SELECT 1 FROM workspace_link_tags wlt
			WHERE wlt.workspace_id=links.workspace_id AND wlt.link_id=links.id AND wlt.tag_id=?
		)`)
		args = append(args, options.TagID)
	}
	if options.FolderID != 0 {
		where = append(where, `EXISTS (
			SELECT 1 FROM workspace_link_organization wlo
			WHERE wlo.workspace_id=links.workspace_id AND wlo.link_id=links.id AND wlo.folder_id=?
		)`)
		args = append(args, options.FolderID)
	}
	if options.UpdatedFrom != nil {
		where = append(where, "links.updated_at >= ?")
		args = append(args, options.UpdatedFrom.UTC())
	}
	if options.UpdatedTo != nil {
		where = append(where, "links.updated_at <= ?")
		args = append(args, options.UpdatedTo.UTC())
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM links WHERE "+whereSQL, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}

	listArgs := append(append([]any(nil), args...), limit, options.Offset)
	rows, err := s.db.QueryContext(ctx, linkSelect+" WHERE "+whereSQL+" ORDER BY links.updated_at DESC, links.id DESC LIMIT ? OFFSET ?", listArgs...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	items := make([]Link, 0, min(limit, int(total)))
	for rows.Next() {
		item, err := scanLink(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total}, nil
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}

// ClaimRedirectAccess executes only after destination-risk allow. It performs
// the expiry/click-limit/one-time checks atomically and increments click_count
// only for an accepted request. Both the version and fingerprint must still be
// identical to the state that received risk allow, preventing TOCTOU reuse.
func (s *MySQLStore) ClaimRedirectAccess(ctx context.Context, id, expectedVersion uint64, expectedFingerprint string, now time.Time) (Link, AccessClaimState, error) {
	if id == 0 || expectedVersion == 0 || !validateFingerprint(expectedFingerprint) {
		return Link{}, AccessClaimConflict, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, linkSelect+` WHERE id = ? FOR UPDATE`, id)
	current, err := scanLink(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Link{}, AccessClaimDeleted, nil
		}
		return Link{}, AccessClaimConflict, err
	}
	if current.Version != expectedVersion || current.RiskFingerprint != expectedFingerprint {
		return current, AccessClaimConflict, nil
	}
	if current.Status == "deleted" || current.DeletedAt != nil {
		return current, AccessClaimDeleted, nil
	}
	if current.Status != "active" {
		return current, AccessClaimPaused, nil
	}
	now = now.UTC()
	if current.ExpiresAt != nil && !current.ExpiresAt.After(now) {
		return current, AccessClaimExpired, nil
	}
	if current.ClickLimit != nil && current.ClickCount >= *current.ClickLimit {
		return current, AccessClaimExhausted, nil
	}
	if current.OneTime && current.ClickCount >= 1 {
		return current, AccessClaimExhausted, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET click_count = click_count + 1
		WHERE id = ? AND version = ? AND risk_fingerprint = ?`,
		current.ID, current.Version, expectedFingerprint,
	)
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Link{}, AccessClaimConflict, err
	}
	if affected != 1 {
		return current, AccessClaimConflict, nil
	}
	current.ClickCount++
	if err := tx.Commit(); err != nil {
		return Link{}, AccessClaimConflict, err
	}
	return current, AccessClaimAllowed, nil
}

func (s *MySQLStore) SetStatus(ctx context.Context, workspaceID string, id, expectedVersion uint64, status, actorID, correlationID, reason string) (Link, error) {
	if status != "active" && status != "paused" {
		return Link{}, ErrInvalidInput
	}
	current, err := s.GetByID(ctx, workspaceID, id)
	if err != nil {
		return Link{}, err
	}
	return s.Update(ctx, id, UpdateInput{
		WorkspaceID:        workspaceID,
		ActorID:            actorID,
		CorrelationID:      correlationID,
		ChangeReason:       reason,
		ExpectedVersion:    expectedVersion,
		Hostname:           current.Hostname,
		DomainKind:         current.DomainKind,
		Code:               current.Code,
		Title:              current.Title,
		PrimaryDestination: current.PrimaryDestination,
		RedirectStatus:     current.RedirectStatus,
		Status:             status,
		Routing:            current.Routing,
		AB:                 current.AB,
		UTM:                current.UTM,
		Access:             current.Access,
		ExpiresAt:          current.ExpiresAt,
		ClickLimit:         current.ClickLimit,
		OneTime:            current.OneTime,
	})
}

func (s *MySQLStore) ExportCSVRows(ctx context.Context, workspaceID string) ([][]string, error) {
	result, err := s.List(ctx, ListOptions{WorkspaceID: workspaceID, Status: "", Limit: 200, Offset: 0})
	if err != nil {
		return nil, err
	}
	if result.Total > int64(len(result.Items)) {
		return nil, fmt.Errorf("export requires pagination: %w", ErrInvalidInput)
	}
	rows := make([][]string, 0, len(result.Items)+1)
	rows = append(rows, []string{"id", "hostname", "code", "title", "destination", "status", "version", "risk_fingerprint"})
	for _, link := range result.Items {
		rows = append(rows, []string{
			fmt.Sprintf("%d", link.ID), link.Hostname, link.Code, link.Title,
			link.PrimaryDestination, link.Status, fmt.Sprintf("%d", link.Version), link.RiskFingerprint,
		})
	}
	return rows, nil
}
