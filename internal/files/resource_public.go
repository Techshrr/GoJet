package files

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *ResourceStore) GetBySlug(ctx context.Context, slug string) (Resource, string, error) {
	if s == nil || s.db == nil || strings.TrimSpace(slug) == "" {
		return Resource{}, "", ErrInvalidInput
	}
	var resource Resource
	var passwordHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT `+resourceColumns+`,password_hash FROM files WHERE public_slug=?`, strings.TrimSpace(slug)).Scan(resourceScanArgs(&resource, &passwordHash)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Resource{}, "", ErrNotFound
	}
	if err != nil {
		return Resource{}, "", err
	}
	if resource.DeletedAt != nil {
		return Resource{}, "", ErrDeleted
	}
	return resource, passwordHash.String, nil
}

func (s *ResourceStore) ReservePublicDownload(ctx context.Context, slug string, now time.Time, authorizedPasswordHash string) (Resource, error) {
	if s == nil || s.db == nil || strings.TrimSpace(slug) == "" || now.IsZero() {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()

	var resource Resource
	var passwordHash sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT `+resourceColumns+`,password_hash FROM files WHERE public_slug=? FOR UPDATE`, strings.TrimSpace(slug)).
		Scan(resourceScanArgs(&resource, &passwordHash)...)
	if errors.Is(err, sql.ErrNoRows) {
		return Resource{}, ErrNotFound
	}
	if err != nil {
		return Resource{}, err
	}
	if resource.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}
	if !resource.Published || resource.ScanState != ScanSafe {
		return Resource{}, ErrNotSafe
	}
	if resource.ExpiresAt != nil && !now.UTC().Before(resource.ExpiresAt.UTC()) {
		return Resource{}, ErrExpired
	}
	if resource.DownloadLimit != nil && resource.DownloadCount >= *resource.DownloadLimit {
		return Resource{}, ErrDownloadLimit
	}
	if passwordHash.Valid && passwordHash.String != "" {
		if len(passwordHash.String) != len(authorizedPasswordHash) ||
			subtle.ConstantTimeCompare([]byte(passwordHash.String), []byte(authorizedPasswordHash)) != 1 {
			return Resource{}, ErrPasswordRequired
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE files SET download_count=download_count+1,updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND published=1 AND scan_state='safe' AND deleted_at IS NULL`, resource.ID)
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
	resource.DownloadCount++
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return resource, nil
}
