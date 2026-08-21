package domains

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

var (
	ErrEntitlementConflict = errors.New("custom-domain entitlement conflict")
	ErrEntitlementNotFound = errors.New("custom-domain entitlement not found")
)

type MySQLStore struct {
	db *sql.DB
}

type PlanSourceInput struct {
	WorkspaceID     string
	SourceKey       string
	Status          EntitlementStatus
	DomainLimit     uint32
	StartsAt        time.Time
	ExpiresAt       *time.Time
	DegradedAt      *time.Time
	GraceUntil      *time.Time
	DecisionReason  string
	SecurityCategory string
}

type ManualApprovalInput struct {
	WorkspaceID      string
	SourceKey        string
	DomainLimit      uint32
	StartsAt         time.Time
	ExpiresAt        time.Time
	GrantedBy        string
	SupportTicketID  string
	DecisionReason   string
	CorrelationID    string
}

type AccessRequestInput struct {
	WorkspaceID          string
	SupportTicketID      string
	RequestedDomainLimit *uint32
	SubmittedAt          time.Time
	CorrelationID        string
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

func (s *MySQLStore) ResolveEntitlement(ctx context.Context, workspaceID string, now time.Time) (ResolvedEntitlement, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return ResolvedEntitlement{}, ErrInvalidEntitlementSource
	}

	sources, err := s.loadEntitlementSources(ctx, s.db, workspaceID)
	if err != nil {
		return ResolvedEntitlement{}, err
	}
	request, err := s.loadLatestRequest(ctx, s.db, workspaceID)
	if err != nil {
		return ResolvedEntitlement{}, err
	}
	return ResolveEntitlement(now, sources, request)
}

func (s *MySQLStore) UpsertPlanSource(ctx context.Context, input PlanSourceInput, correlationID string) (EntitlementSource, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.SourceKey = strings.TrimSpace(input.SourceKey)
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}

	source := EntitlementSource{
		WorkspaceID:      input.WorkspaceID,
		Source:           SourcePlan,
		SourceKey:        input.SourceKey,
		Status:           input.Status,
		DomainLimit:      input.DomainLimit,
		StartsAt:         input.StartsAt.UTC(),
		ExpiresAt:        utcPtr(input.ExpiresAt),
		DegradedAt:       utcPtr(input.DegradedAt),
		GraceUntil:       utcPtr(input.GraceUntil),
		DecisionReason:   strings.TrimSpace(input.DecisionReason),
		SecurityCategory: strings.TrimSpace(input.SecurityCategory),
	}
	if err := ValidateEntitlementSource(source); err != nil {
		return EntitlementSource{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return EntitlementSource{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources (
			workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
			degraded_at, grace_until, decision_reason, security_category
		) VALUES (?, 'plan', ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''))
		ON DUPLICATE KEY UPDATE
			status = VALUES(status),
			domain_limit = VALUES(domain_limit),
			starts_at = VALUES(starts_at),
			expires_at = VALUES(expires_at),
			degraded_at = VALUES(degraded_at),
			grace_until = VALUES(grace_until),
			decision_reason = VALUES(decision_reason),
			security_category = VALUES(security_category)`,
		source.WorkspaceID, source.SourceKey, source.Status, source.DomainLimit, source.StartsAt,
		source.ExpiresAt, source.DegradedAt, source.GraceUntil, source.DecisionReason, source.SecurityCategory,
	)
	if err != nil {
		return EntitlementSource{}, err
	}

	created, err := loadEntitlementSourceByKey(ctx, tx, source.WorkspaceID, SourcePlan, source.SourceKey)
	if err != nil {
		return EntitlementSource{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, created.WorkspaceID, nil, &created.ID, "system:plan-entitlement", "domain.entitlement.plan.sync", "success", created.DecisionReason, correlationID, map[string]any{
		"source":       created.Source,
		"status":       created.Status,
		"domain_limit": created.DomainLimit,
	}); err != nil {
		return EntitlementSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return EntitlementSource{}, err
	}
	return created, nil
}

func (s *MySQLStore) CreateManualApproval(ctx context.Context, input ManualApprovalInput) (EntitlementSource, error) {
	source := EntitlementSource{
		WorkspaceID:     strings.TrimSpace(input.WorkspaceID),
		Source:          SourceManualApproval,
		SourceKey:       strings.TrimSpace(input.SourceKey),
		Status:          EntitlementActive,
		DomainLimit:     input.DomainLimit,
		StartsAt:        input.StartsAt.UTC(),
		ExpiresAt:       timePtr(input.ExpiresAt.UTC()),
		GrantedBy:       strings.TrimSpace(input.GrantedBy),
		SupportTicketID: strings.TrimSpace(input.SupportTicketID),
		DecisionReason:  strings.TrimSpace(input.DecisionReason),
	}
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if input.CorrelationID == "" {
		return EntitlementSource{}, ErrInvalidEntitlementSource
	}
	if err := ValidateEntitlementSource(source); err != nil {
		return EntitlementSource{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return EntitlementSource{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_sources (
			workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
			granted_by, support_ticket_id, decision_reason
		) VALUES (?, 'manual_approval', ?, 'active', ?, ?, ?, ?, ?, ?)`,
		source.WorkspaceID, source.SourceKey, source.DomainLimit, source.StartsAt, source.ExpiresAt,
		source.GrantedBy, source.SupportTicketID, source.DecisionReason,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return EntitlementSource{}, ErrEntitlementConflict
		}
		return EntitlementSource{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return EntitlementSource{}, err
	}
	source.ID = uint64(id)
	if err := appendDomainAuditTx(ctx, tx, source.WorkspaceID, nil, &source.ID, source.GrantedBy, "domain.entitlement.manual_approval.create", "success", source.DecisionReason, input.CorrelationID, map[string]any{
		"support_ticket_id": source.SupportTicketID,
		"domain_limit":      source.DomainLimit,
		"starts_at":         source.StartsAt,
		"expires_at":        source.ExpiresAt,
	}); err != nil {
		return EntitlementSource{}, err
	}
	if err := tx.Commit(); err != nil {
		return EntitlementSource{}, err
	}
	return source, nil
}

func (s *MySQLStore) ProjectAccessRequest(ctx context.Context, input AccessRequestInput) (AccessRequest, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.SupportTicketID = strings.TrimSpace(input.SupportTicketID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	if input.WorkspaceID == "" || input.SupportTicketID == "" || input.CorrelationID == "" || input.SubmittedAt.IsZero() {
		return AccessRequest{}, ErrInvalidEntitlementSource
	}
	if input.RequestedDomainLimit != nil && *input.RequestedDomainLimit == 0 {
		return AccessRequest{}, ErrInvalidEntitlementSource
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return AccessRequest{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_entitlement_requests (
			workspace_id, support_ticket_id, requested_domain_limit, status, submitted_at
		) VALUES (?, ?, ?, 'requested', ?)`,
		input.WorkspaceID, input.SupportTicketID, input.RequestedDomainLimit, input.SubmittedAt.UTC(),
	)
	if err != nil {
		if isDuplicateKey(err) {
			return AccessRequest{}, ErrEntitlementConflict
		}
		return AccessRequest{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, nil, nil, "system:support-ticket-projection", "domain.entitlement.request.project", "success", "support request projected without authority", input.CorrelationID, map[string]any{
		"support_ticket_id": input.SupportTicketID,
	}); err != nil {
		return AccessRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccessRequest{}, err
	}
	return AccessRequest{WorkspaceID: input.WorkspaceID, SupportTicketID: input.SupportTicketID, SubmittedAt: input.SubmittedAt.UTC()}, nil
}

func (s *MySQLStore) loadEntitlementSources(ctx context.Context, q queryer, workspaceID string) ([]EntitlementSource, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
		       degraded_at, grace_until, granted_by, support_ticket_id, decision_reason, security_category
		FROM custom_domain_entitlement_sources
		WHERE workspace_id = ?
		ORDER BY id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := []EntitlementSource{}
	for rows.Next() {
		source, scanErr := scanEntitlementSource(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sources, nil
}

func (s *MySQLStore) loadLatestRequest(ctx context.Context, q queryer, workspaceID string) (*AccessRequest, error) {
	row := q.QueryRowContext(ctx, `
		SELECT workspace_id, support_ticket_id, submitted_at
		FROM custom_domain_entitlement_requests
		WHERE workspace_id = ? AND status = 'requested'
		ORDER BY submitted_at DESC, id DESC
		LIMIT 1`, workspaceID)
	var request AccessRequest
	if err := row.Scan(&request.WorkspaceID, &request.SupportTicketID, &request.SubmittedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	request.SubmittedAt = request.SubmittedAt.UTC()
	return &request, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadEntitlementSourceByKey(ctx context.Context, q queryer, workspaceID string, kind EntitlementSourceKind, key string) (EntitlementSource, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, workspace_id, source, source_key, status, domain_limit, starts_at, expires_at,
		       degraded_at, grace_until, granted_by, support_ticket_id, decision_reason, security_category
		FROM custom_domain_entitlement_sources
		WHERE workspace_id = ? AND source = ? AND source_key = ?`, workspaceID, kind, key)
	return scanEntitlementSource(row)
}

type rowScanner interface {
	Scan(...any) error
}

func scanEntitlementSource(row rowScanner) (EntitlementSource, error) {
	var source EntitlementSource
	var expiresAt, degradedAt, graceUntil sql.NullTime
	var grantedBy, supportTicketID, decisionReason, securityCategory sql.NullString
	if err := row.Scan(
		&source.ID, &source.WorkspaceID, &source.Source, &source.SourceKey, &source.Status, &source.DomainLimit,
		&source.StartsAt, &expiresAt, &degradedAt, &graceUntil, &grantedBy, &supportTicketID, &decisionReason, &securityCategory,
	); err != nil {
		return EntitlementSource{}, err
	}
	source.StartsAt = source.StartsAt.UTC()
	if expiresAt.Valid {
		source.ExpiresAt = timePtr(expiresAt.Time.UTC())
	}
	if degradedAt.Valid {
		source.DegradedAt = timePtr(degradedAt.Time.UTC())
	}
	if graceUntil.Valid {
		source.GraceUntil = timePtr(graceUntil.Time.UTC())
	}
	if grantedBy.Valid {
		source.GrantedBy = grantedBy.String
	}
	if supportTicketID.Valid {
		source.SupportTicketID = supportTicketID.String
	}
	if decisionReason.Valid {
		source.DecisionReason = decisionReason.String
	}
	if securityCategory.Valid {
		source.SecurityCategory = securityCategory.String
	}
	if err := ValidateEntitlementSource(source); err != nil {
		return EntitlementSource{}, fmt.Errorf("persisted entitlement source %d invalid: %w", source.ID, err)
	}
	return source, nil
}

func utcPtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}

func timePtr(value time.Time) *time.Time {
	return &value
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
