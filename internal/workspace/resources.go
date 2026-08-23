package workspace

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) ListCampaigns(ctx context.Context, workspaceID string) ([]Campaign, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,name,status,version,created_by,created_at,updated_at
FROM workspace_campaigns WHERE workspace_id=?
ORDER BY updated_at DESC,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Campaign
	for rows.Next() {
		var item Campaign
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Status, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateCampaign(ctx context.Context, workspaceID, actorID, name string) (Campaign, error) {
	name = strings.TrimSpace(name)
	if workspaceID == "" || actorID == "" || name == "" || len(name) > 160 {
		return Campaign{}, ErrInvalid
	}
	id, err := newOpaqueID("cmp_", 18)
	if err != nil {
		return Campaign{}, err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO workspace_campaigns (id,workspace_id,name,status,version,created_by)
VALUES (?,?,?,'active',1,?)`, id, workspaceID, name, actorID)
	if err != nil {
		return Campaign{}, err
	}
	return s.campaignByID(ctx, workspaceID, id)
}

func (s *Store) UpdateCampaign(ctx context.Context, workspaceID, campaignID, name, status string, expectedVersion uint64) (Campaign, error) {
	name = strings.TrimSpace(name)
	status = strings.TrimSpace(status)
	if name == "" || len(name) > 160 || (status != "active" && status != "archived") || expectedVersion == 0 {
		return Campaign{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_campaigns SET name=?,status=?,version=version+1
WHERE workspace_id=? AND id=? AND version=?`, name, status, workspaceID, campaignID, expectedVersion)
	if err != nil {
		return Campaign{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Campaign{}, err
	}
	if n == 0 {
		if _, err := s.campaignByID(ctx, workspaceID, campaignID); err != nil {
			return Campaign{}, err
		}
		return Campaign{}, ErrConflict
	}
	return s.campaignByID(ctx, workspaceID, campaignID)
}

func (s *Store) DeleteCampaign(ctx context.Context, workspaceID, campaignID string) error {
	var count uint64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workspace_link_organization WHERE workspace_id=? AND campaign_id=?`,
		workspaceID, campaignID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM workspace_campaigns WHERE workspace_id=? AND id=?`, workspaceID, campaignID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) campaignByID(ctx context.Context, workspaceID, campaignID string) (Campaign, error) {
	var item Campaign
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,name,status,version,created_by,created_at,updated_at
FROM workspace_campaigns WHERE workspace_id=? AND id=?`, workspaceID, campaignID).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.Status, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Campaign{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListTags(ctx context.Context, workspaceID string) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,name,normalized_name,version,created_by,created_at,updated_at
FROM workspace_tags WHERE workspace_id=? ORDER BY normalized_name,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var item Tag
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.NormalizedName, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateTag(ctx context.Context, workspaceID, actorID, name string) (Tag, error) {
	name = strings.TrimSpace(name)
	normalized := normalizeName(name)
	if workspaceID == "" || actorID == "" || normalized == "" || len(name) > 96 || len(normalized) > 96 {
		return Tag{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO workspace_tags (workspace_id,name,normalized_name,version,created_by)
VALUES (?,?,?,1,?)`, workspaceID, name, normalized, actorID)
	if err != nil {
		return Tag{}, wrapConflict(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Tag{}, err
	}
	return s.tagByID(ctx, workspaceID, uint64(id))
}

func (s *Store) UpdateTag(ctx context.Context, workspaceID string, tagID uint64, name string, expectedVersion uint64) (Tag, error) {
	name = strings.TrimSpace(name)
	normalized := normalizeName(name)
	if normalized == "" || len(name) > 96 || len(normalized) > 96 || expectedVersion == 0 {
		return Tag{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_tags SET name=?,normalized_name=?,version=version+1
WHERE workspace_id=? AND id=? AND version=?`, name, normalized, workspaceID, tagID, expectedVersion)
	if err != nil {
		return Tag{}, wrapConflict(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Tag{}, err
	}
	if n == 0 {
		if _, err := s.tagByID(ctx, workspaceID, tagID); err != nil {
			return Tag{}, err
		}
		return Tag{}, ErrConflict
	}
	return s.tagByID(ctx, workspaceID, tagID)
}

func (s *Store) DeleteTag(ctx context.Context, workspaceID string, tagID uint64) error {
	var count uint64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workspace_link_tags WHERE workspace_id=? AND tag_id=?`,
		workspaceID, tagID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM workspace_tags WHERE workspace_id=? AND id=?`, workspaceID, tagID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) tagByID(ctx context.Context, workspaceID string, tagID uint64) (Tag, error) {
	var item Tag
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,name,normalized_name,version,created_by,created_at,updated_at
FROM workspace_tags WHERE workspace_id=? AND id=?`, workspaceID, tagID).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.NormalizedName, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Tag{}, ErrNotFound
	}
	return item, err
}

func (s *Store) ListFolders(ctx context.Context, workspaceID string) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id,workspace_id,name,normalized_name,version,created_by,created_at,updated_at
FROM workspace_folders WHERE workspace_id=? ORDER BY normalized_name,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var item Folder
		if err := rows.Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.NormalizedName, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateFolder(ctx context.Context, workspaceID, actorID, name string) (Folder, error) {
	name = strings.TrimSpace(name)
	normalized := normalizeName(name)
	if workspaceID == "" || actorID == "" || normalized == "" || len(name) > 96 || len(normalized) > 96 {
		return Folder{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
INSERT INTO workspace_folders (workspace_id,name,normalized_name,version,created_by)
VALUES (?,?,?,1,?)`, workspaceID, name, normalized, actorID)
	if err != nil {
		return Folder{}, wrapConflict(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Folder{}, err
	}
	return s.folderByID(ctx, workspaceID, uint64(id))
}

func (s *Store) UpdateFolder(ctx context.Context, workspaceID string, folderID uint64, name string, expectedVersion uint64) (Folder, error) {
	name = strings.TrimSpace(name)
	normalized := normalizeName(name)
	if normalized == "" || len(name) > 96 || len(normalized) > 96 || expectedVersion == 0 {
		return Folder{}, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE workspace_folders SET name=?,normalized_name=?,version=version+1
WHERE workspace_id=? AND id=? AND version=?`, name, normalized, workspaceID, folderID, expectedVersion)
	if err != nil {
		return Folder{}, wrapConflict(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Folder{}, err
	}
	if n == 0 {
		if _, err := s.folderByID(ctx, workspaceID, folderID); err != nil {
			return Folder{}, err
		}
		return Folder{}, ErrConflict
	}
	return s.folderByID(ctx, workspaceID, folderID)
}

func (s *Store) DeleteFolder(ctx context.Context, workspaceID string, folderID uint64) error {
	var count uint64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM workspace_link_organization WHERE workspace_id=? AND folder_id=?`,
		workspaceID, folderID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrInUse
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM workspace_folders WHERE workspace_id=? AND id=?`, workspaceID, folderID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) folderByID(ctx context.Context, workspaceID string, folderID uint64) (Folder, error) {
	var item Folder
	err := s.db.QueryRowContext(ctx, `
SELECT id,workspace_id,name,normalized_name,version,created_by,created_at,updated_at
FROM workspace_folders WHERE workspace_id=? AND id=?`, workspaceID, folderID).
		Scan(&item.ID, &item.WorkspaceID, &item.Name, &item.NormalizedName, &item.Version,
			&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Folder{}, ErrNotFound
	}
	return item, err
}

type LinkOrganizationUpdate struct {
	LinkIDs    []uint64
	CampaignID *string
	FolderID   *uint64
	TagIDs     []uint64
}

func (s *Store) UpdateLinkOrganization(ctx context.Context, workspaceID string, input LinkOrganizationUpdate) error {
	if workspaceID == "" || len(input.LinkIDs) == 0 {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if input.CampaignID != nil {
		var count uint64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_campaigns WHERE workspace_id=? AND id=?`,
			workspaceID, *input.CampaignID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrForbidden
		}
	}
	if input.FolderID != nil {
		var count uint64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_folders WHERE workspace_id=? AND id=?`,
			workspaceID, *input.FolderID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrForbidden
		}
	}
	for _, tagID := range input.TagIDs {
		var count uint64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_tags WHERE workspace_id=? AND id=?`,
			workspaceID, tagID).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrForbidden
		}
	}

	for _, linkID := range input.LinkIDs {
		var linkWorkspace string
		err := tx.QueryRowContext(ctx, `SELECT workspace_id FROM links WHERE id=? AND status<>'deleted'`, linkID).Scan(&linkWorkspace)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && linkWorkspace != workspaceID) {
			return ErrForbidden
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO workspace_link_organization (workspace_id,link_id,campaign_id,folder_id,version)
VALUES (?,?,?,?,1)
ON DUPLICATE KEY UPDATE campaign_id=VALUES(campaign_id),folder_id=VALUES(folder_id),version=version+1`,
			workspaceID, linkID, input.CampaignID, input.FolderID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM workspace_link_tags WHERE workspace_id=? AND link_id=?`, workspaceID, linkID); err != nil {
			return err
		}
		for _, tagID := range input.TagIDs {
			if _, err := tx.ExecContext(ctx, `
INSERT INTO workspace_link_tags (workspace_id,link_id,tag_id) VALUES (?,?,?)`, workspaceID, linkID, tagID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) LinkOrganization(ctx context.Context, workspaceID string, linkID uint64) (LinkOrganization, error) {
	var item LinkOrganization
	err := s.db.QueryRowContext(ctx, `
SELECT workspace_id,link_id,campaign_id,folder_id,version,updated_at
FROM workspace_link_organization WHERE workspace_id=? AND link_id=?`, workspaceID, linkID).
		Scan(&item.WorkspaceID, &item.LinkID, &item.CampaignID, &item.FolderID, &item.Version, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return LinkOrganization{WorkspaceID: workspaceID, LinkID: linkID, Version: 0, TagIDs: []uint64{}}, nil
	}
	if err != nil {
		return LinkOrganization{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT tag_id FROM workspace_link_tags WHERE workspace_id=? AND link_id=? ORDER BY tag_id`, workspaceID, linkID)
	if err != nil {
		return LinkOrganization{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uint64
		if err := rows.Scan(&id); err != nil {
			return LinkOrganization{}, err
		}
		item.TagIDs = append(item.TagIDs, id)
	}
	return item, rows.Err()
}

func parseUint(value string) (uint64, error) {
	var id uint64
	_, err := fmt.Sscan(value, &id)
	if err != nil || id == 0 {
		return 0, ErrInvalid
	}
	return id, nil
}
