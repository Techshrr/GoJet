package files

import (
	"context"
	"database/sql"
	"errors"
)

const resourceColumns = `id,workspace_id,public_slug,original_name,storage_key,size_bytes,content_sha256,declared_mime,detected_mime,scan_state,scan_generation,published,published_at,(password_hash IS NOT NULL),expires_at,retention_until,download_limit,download_count,created_by,created_at,updated_at,deleted_at`
const resourceSelect = `SELECT ` + resourceColumns + ` FROM files`

type rowScanner interface{ Scan(...any) error }

func resourceScanArgs(resource *Resource, extra ...any) []any {
	args := []any{&resource.ID, &resource.WorkspaceID, &resource.PublicSlug, &resource.OriginalName, &resource.StorageKey, &resource.SizeBytes, &resource.ContentSHA256, &resource.DeclaredMIME, &resource.DetectedMIME, &resource.ScanState, &resource.ScanGeneration, &resource.Published, &resource.PublishedAt, &resource.PasswordRequired, &resource.ExpiresAt, &resource.RetentionUntil, &resource.DownloadLimit, &resource.DownloadCount, &resource.CreatedBy, &resource.CreatedAt, &resource.UpdatedAt, &resource.DeletedAt}
	return append(args, extra...)
}

func scanResource(row rowScanner) (Resource, error) {
	var resource Resource
	if err := row.Scan(resourceScanArgs(&resource)...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Resource{}, ErrNotFound
		}
		return Resource{}, err
	}
	return resource, nil
}

func getResourceTx(ctx context.Context, tx *sql.Tx, workspaceID string, fileID uint64, forUpdate bool) (Resource, error) {
	query := resourceSelect + ` WHERE workspace_id=? AND id=?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanResource(tx.QueryRowContext(ctx, query, workspaceID, fileID))
}
