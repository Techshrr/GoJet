package trust

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AdminAbuseHoldRecord struct {
	ID             uint64    `json:"id"`
	ActionState    string    `json:"state"`
	ReasonCategory string    `json:"reason_category"`
	CreatedAt      time.Time `json:"created_at"`
}

type AdminAbuseReportRecord struct {
	ID                     uint64                 `json:"id"`
	PublicID               string                 `json:"public_id"`
	WorkspaceID            string                 `json:"workspace_id"`
	ResourceType           AbuseResourceType      `json:"resource_type"`
	ResourceID             string                 `json:"resource_id"`
	HostnameASCII          string                 `json:"hostname"`
	SafeCode               string                 `json:"safe_code,omitempty"`
	DestinationFingerprint string                 `json:"destination_fingerprint,omitempty"`
	Category               AbuseCategory          `json:"category"`
	DetailsRedacted        string                 `json:"details"`
	Status                 AbuseStatus            `json:"status"`
	Version                uint64                 `json:"version"`
	ActiveHold             *AdminAbuseHoldRecord  `json:"active_hold,omitempty"`
	CreatedAt              time.Time              `json:"created_at"`
	UpdatedAt              time.Time              `json:"updated_at"`
}

const adminAbuseSelect = `
SELECT id,public_id,workspace_id,resource_type,resource_id,hostname_ascii,COALESCE(safe_code,''),COALESCE(destination_fingerprint,''),
       category,details_redacted,status,version,created_at,updated_at
FROM abuse_reports`

type adminAbuseScanner interface {
	Scan(...any) error
}

func (s *Store) ListAdminAbuseReports(ctx context.Context, limit int) ([]AdminAbuseReportRecord, error) {
	if s == nil || s.db == nil || limit < 1 || limit > 500 {
		return nil, ErrInvalid
	}
	rows, err := s.db.QueryContext(ctx, adminAbuseSelect+` ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AdminAbuseReportRecord, 0)
	for rows.Next() {
		item, err := scanAdminAbuse(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) GetAdminAbuseReport(ctx context.Context, reportID uint64) (AdminAbuseReportRecord, error) {
	if s == nil || s.db == nil || reportID == 0 {
		return AdminAbuseReportRecord{}, ErrInvalid
	}
	item, err := scanAdminAbuse(s.db.QueryRowContext(ctx, adminAbuseSelect+` WHERE id=?`, reportID))
	if err != nil {
		return AdminAbuseReportRecord{}, err
	}
	hold, err := s.ActiveAbuseHold(ctx, item.WorkspaceID, item.ResourceType, item.ResourceID)
	if err == nil {
		item.ActiveHold = &AdminAbuseHoldRecord{
			ID:             hold.ID,
			ActionState:    strings.TrimSpace(hold.State),
			ReasonCategory: strings.TrimSpace(hold.ReasonCategory),
			CreatedAt:      hold.CreatedAt.UTC(),
		}
	} else if !errors.Is(err, ErrNotFound) {
		return AdminAbuseReportRecord{}, err
	}
	return item, nil
}

func scanAdminAbuse(scanner adminAbuseScanner) (AdminAbuseReportRecord, error) {
	var item AdminAbuseReportRecord
	var resourceType, category, status string
	if err := scanner.Scan(
		&item.ID,
		&item.PublicID,
		&item.WorkspaceID,
		&resourceType,
		&item.ResourceID,
		&item.HostnameASCII,
		&item.SafeCode,
		&item.DestinationFingerprint,
		&category,
		&item.DetailsRedacted,
		&status,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AdminAbuseReportRecord{}, ErrNotFound
		}
		return AdminAbuseReportRecord{}, err
	}
	item.ResourceType = AbuseResourceType(resourceType)
	item.Category = AbuseCategory(category)
	item.Status = AbuseStatus(status)
	return item, nil
}
