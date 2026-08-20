package links

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

type RestoreInput struct {
	WorkspaceID     string
	ActorID         string
	CorrelationID   string
	ChangeReason    string
	ExpectedVersion uint64
	RestoreVersion  uint64
}

func (s *MySQLStore) Restore(ctx context.Context, id uint64, input RestoreInput) (Link, error) {
	if id == 0 || input.ExpectedVersion == 0 || input.RestoreVersion == 0 ||
		strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.ActorID) == "" ||
		strings.TrimSpace(input.CorrelationID) == "" || strings.TrimSpace(input.ChangeReason) == "" {
		return Link{}, ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback()

	current, err := s.getByIDTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), id, true)
	if err != nil {
		return Link{}, err
	}
	if current.Version != input.ExpectedVersion {
		return Link{}, ErrConflict
	}

	var snapshotJSON []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT snapshot_json FROM link_versions
		WHERE workspace_id = ? AND link_id = ? AND version = ?`,
		strings.TrimSpace(input.WorkspaceID), id, input.RestoreVersion,
	).Scan(&snapshotJSON); err != nil {
		if err == sql.ErrNoRows {
			return Link{}, ErrNotFound
		}
		return Link{}, err
	}

	var snapshot Link
	if err := json.Unmarshal(snapshotJSON, &snapshot); err != nil {
		return Link{}, err
	}
	primary, routing, variants, fingerprint, err := normalizeBehavior(snapshot.PrimaryDestination, snapshot.Routing, snapshot.AB)
	if err != nil {
		return Link{}, err
	}
	routingJSON, abJSON, utmJSON, accessJSON, err := marshalBehavior(routing, variants, snapshot.UTM, snapshot.Access)
	if err != nil {
		return Link{}, err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET
			hostname = ?, domain_kind = ?, code = ?, title = ?, primary_destination = ?,
			redirect_status = ?, status = ?, version = version + 1, risk_fingerprint = ?,
			routing_json = ?, ab_json = ?, utm_json = ?, access_json = ?, expires_at = ?,
			click_limit = ?, one_time = ?, deleted_at = NULL
		WHERE workspace_id = ? AND id = ? AND version = ?`,
		snapshot.Hostname, snapshot.DomainKind, snapshot.Code, snapshot.Title, primary,
		snapshot.RedirectStatus, normalizeRestoredStatus(snapshot.Status), fingerprint,
		routingJSON, abJSON, utmJSON, accessJSON, snapshot.ExpiresAt,
		snapshot.ClickLimit, snapshot.OneTime,
		strings.TrimSpace(input.WorkspaceID), id, input.ExpectedVersion,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return Link{}, ErrConflict
		}
		return Link{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Link{}, err
	}
	if affected != 1 {
		return Link{}, ErrConflict
	}

	restored, err := s.getByIDTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), id, false)
	if err != nil {
		return Link{}, err
	}
	if err := appendVersionTx(ctx, tx, restored, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.ChangeReason)); err != nil {
		return Link{}, err
	}
	if err := appendAuditTx(ctx, tx, restored.WorkspaceID, restored.ID, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.CorrelationID), "link.restore", strings.TrimSpace(input.ChangeReason), "success", map[string]any{
		"from_version":        current.Version,
		"restored_from":       input.RestoreVersion,
		"to_version":          restored.Version,
		"risk_fingerprint":    restored.RiskFingerprint,
		"risk_invalidated":    current.RiskFingerprint != restored.RiskFingerprint,
		"click_count_preserved": restored.ClickCount,
	}); err != nil {
		return Link{}, err
	}
	if err := tx.Commit(); err != nil {
		return Link{}, err
	}
	return restored, nil
}

func normalizeRestoredStatus(status string) string {
	if status == "paused" {
		return "paused"
	}
	return "active"
}
