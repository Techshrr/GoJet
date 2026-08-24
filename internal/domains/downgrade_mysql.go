package domains

import (
	"context"
	"database/sql"
	"time"
)

type NormalPlanDowngradeInput struct {
	WorkspaceID    string
	SourceKey      string
	DegradedAt     time.Time
	DecisionReason string
	CorrelationID  string
}

// ApplyNormalPlanDowngrade records the one authoritative normal-downgrade
// instant. Replays at the same instant are idempotent; attempts to move the
// instant later are rejected so a caller cannot extend the seven-day grace.
func (s *MySQLStore) ApplyNormalPlanDowngrade(ctx context.Context, input NormalPlanDowngradeInput) (EntitlementSource, error) {
	if s == nil || s.db == nil {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return EntitlementSource{}, err
	}
	defer tx.Rollback()
	updated, err := ApplyNormalPlanDowngradeTx(ctx, tx, input)
	if err != nil {
		return EntitlementSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return EntitlementSource{}, err
	}
	return updated, nil
}

func loadEntitlementSourceByKeyForUpdate(ctx context.Context, tx *sql.Tx, workspaceID string, kind EntitlementSourceKind, key string) (EntitlementSource, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
		       degraded_at, grace_until, granted_by, support_ticket_id, decision_reason, security_category
		FROM custom_domain_entitlement_sources
		WHERE workspace_id = ? AND source = ? AND source_key = ?
		FOR UPDATE`, workspaceID, kind, key)
	return scanEntitlementSource(row)
}
