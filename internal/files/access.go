package files

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

type AccessPolicyInput struct {
	WorkspaceID         string
	FileID              uint64
	ActorID             string
	CorrelationID       string
	Reason              string
	PasswordHash        *string
	ClearPassword       bool
	ExpiresAt           *time.Time
	ClearExpiresAt      bool
	RetentionUntil      *time.Time
	ClearRetentionUntil bool
	DownloadLimit       *uint64
	ClearDownloadLimit  bool
}

func (s *ResourceStore) UpdateAccessPolicy(ctx context.Context, input AccessPolicyInput) (Resource, error) {
	hasChange := input.PasswordHash != nil || input.ClearPassword ||
		input.ExpiresAt != nil || input.ClearExpiresAt ||
		input.RetentionUntil != nil || input.ClearRetentionUntil ||
		input.DownloadLimit != nil || input.ClearDownloadLimit
	if s == nil || s.db == nil || input.FileID == 0 || !hasChange ||
		strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.ActorID) == "" ||
		strings.TrimSpace(input.CorrelationID) == "" || strings.TrimSpace(input.Reason) == "" ||
		(input.PasswordHash != nil && input.ClearPassword) ||
		(input.ExpiresAt != nil && input.ClearExpiresAt) ||
		(input.RetentionUntil != nil && input.ClearRetentionUntil) ||
		(input.DownloadLimit != nil && input.ClearDownloadLimit) ||
		(input.DownloadLimit != nil && *input.DownloadLimit == 0) {
		return Resource{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Resource{}, err
	}
	defer tx.Rollback()

	current, err := getResourceTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), input.FileID, true)
	if err != nil {
		return Resource{}, err
	}
	if current.DeletedAt != nil {
		return Resource{}, ErrDeleted
	}

	passwordExpr, passwordArg := "password_hash", any(nil)
	if input.ClearPassword {
		passwordExpr = "NULL"
	} else if input.PasswordHash != nil {
		passwordExpr = "?"
		passwordArg = strings.TrimSpace(*input.PasswordHash)
		if passwordArg == "" {
			return Resource{}, ErrInvalidInput
		}
	}
	expiresExpr, expiresArg := "expires_at", any(nil)
	if input.ClearExpiresAt {
		expiresExpr = "NULL"
	} else if input.ExpiresAt != nil {
		expiresExpr = "?"
		expiresArg = input.ExpiresAt.UTC()
	}
	retentionExpr, retentionArg := "retention_until", any(nil)
	if input.ClearRetentionUntil {
		retentionExpr = "NULL"
	} else if input.RetentionUntil != nil {
		retentionExpr = "?"
		retentionArg = input.RetentionUntil.UTC()
	}
	limitExpr, limitArg := "download_limit", any(nil)
	if input.ClearDownloadLimit {
		limitExpr = "NULL"
	} else if input.DownloadLimit != nil {
		limitExpr = "?"
		limitArg = *input.DownloadLimit
	}

	query := "UPDATE files SET password_hash=" + passwordExpr +
		",expires_at=" + expiresExpr +
		",retention_until=" + retentionExpr +
		",download_limit=" + limitExpr +
		",updated_at=CURRENT_TIMESTAMP(6) WHERE id=? AND workspace_id=? AND deleted_at IS NULL"
	args := make([]any, 0, 6)
	if passwordExpr == "?" {
		args = append(args, passwordArg)
	}
	if expiresExpr == "?" {
		args = append(args, expiresArg)
	}
	if retentionExpr == "?" {
		args = append(args, retentionArg)
	}
	if limitExpr == "?" {
		args = append(args, limitArg)
	}
	args = append(args, input.FileID, strings.TrimSpace(input.WorkspaceID))
	result, err := tx.ExecContext(ctx, query, args...)
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
	updated, err := getResourceTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), input.FileID, false)
	if err != nil {
		return Resource{}, err
	}
	if err := appendAuditTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), &input.FileID, input.ActorID, input.CorrelationID,
		"file.access.update", input.Reason, "success", map[string]any{
			"password_required":   updated.PasswordRequired,
			"expires_at_set":      updated.ExpiresAt != nil,
			"retention_until_set": updated.RetentionUntil != nil,
			"download_limit_set":  updated.DownloadLimit != nil,
		}); err != nil {
		return Resource{}, err
	}
	if err := tx.Commit(); err != nil {
		return Resource{}, err
	}
	return updated, nil
}
