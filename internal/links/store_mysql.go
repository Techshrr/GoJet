package links

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type MySQLStore struct {
	db *sql.DB
}

type CreateInput struct {
	WorkspaceID        string
	ActorID            string
	CorrelationID      string
	ChangeReason       string
	Hostname           string
	DomainKind         string
	Code               string
	Title              string
	PrimaryDestination string
	RedirectStatus     int
	Routing            []RoutingRule
	AB                 []ABVariant
	UTM                UTMConfig
	Access             AccessConfig
	ExpiresAt          *time.Time
	ClickLimit         *uint64
	OneTime            bool
}

type UpdateInput struct {
	WorkspaceID        string
	ActorID            string
	CorrelationID      string
	ChangeReason       string
	ExpectedVersion    uint64
	Hostname           string
	DomainKind         string
	Code               string
	Title              string
	PrimaryDestination string
	RedirectStatus     int
	Status             string
	Routing            []RoutingRule
	AB                 []ABVariant
	UTM                UTMConfig
	Access             AccessConfig
	ExpiresAt          *time.Time
	ClickLimit         *uint64
	OneTime            bool
}

type LinkVersion struct {
	Version         uint64          `json:"version"`
	ActorID         string          `json:"actor_id"`
	ChangeReason    string          `json:"change_reason"`
	Snapshot        json.RawMessage `json:"snapshot"`
	RiskFingerprint string          `json:"risk_fingerprint"`
	CreatedAt       time.Time       `json:"created_at"`
}

func OpenMySQL(dsn string) (*sql.DB, error) {
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL DSN: %w", err)
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	return db, nil
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db}
}

func (s *MySQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func normalizeHostname(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(raw), "."))
	if host == "" || len(host) > 253 || strings.ContainsAny(host, "/?#@ ") {
		return "", ErrInvalidInput
	}
	return host, nil
}

func normalizeCode(raw string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if len(code) < 1 || len(code) > 64 {
		return "", ErrInvalidInput
	}
	for _, r := range code {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return "", ErrInvalidInput
	}
	return code, nil
}

func validateRedirectStatus(status int) error {
	switch status {
	case 301, 302, 307, 308:
		return nil
	default:
		return ErrInvalidInput
	}
}

func validateLifecycleStatus(status string) error {
	switch status {
	case "active", "paused":
		return nil
	default:
		return ErrInvalidInput
	}
}

func normalizeBehavior(primary string, routing []RoutingRule, variants []ABVariant) (string, []RoutingRule, []ABVariant, string, error) {
	normalizedPrimary, err := NormalizeDestination(primary)
	if err != nil {
		return "", nil, nil, "", err
	}
	if routing == nil {
		routing = []RoutingRule{}
	}
	if variants == nil {
		variants = []ABVariant{}
	}

	normalizedRouting := append([]RoutingRule(nil), routing...)
	for i := range normalizedRouting {
		if !normalizedRouting[i].Enabled {
			continue
		}
		normalized, normalizeErr := NormalizeDestination(normalizedRouting[i].Destination)
		if normalizeErr != nil {
			return "", nil, nil, "", fmt.Errorf("routing rule %q: %w", normalizedRouting[i].ID, normalizeErr)
		}
		normalizedRouting[i].Destination = normalized
	}

	normalizedAB := append([]ABVariant(nil), variants...)
	if err := ValidateABWeights(normalizedAB); err != nil {
		return "", nil, nil, "", err
	}
	for i := range normalizedAB {
		if !normalizedAB[i].Enabled {
			continue
		}
		normalized, normalizeErr := NormalizeDestination(normalizedAB[i].Destination)
		if normalizeErr != nil {
			return "", nil, nil, "", fmt.Errorf("A/B variant %q: %w", normalizedAB[i].ID, normalizeErr)
		}
		normalizedAB[i].Destination = normalized
	}

	fingerprint, _, err := RiskFingerprint(normalizedPrimary, normalizedRouting, normalizedAB)
	if err != nil {
		return "", nil, nil, "", err
	}
	return normalizedPrimary, normalizedRouting, normalizedAB, fingerprint, nil
}

func validateCreateInput(in CreateInput) (CreateInput, string, error) {
	if strings.TrimSpace(in.WorkspaceID) == "" || strings.TrimSpace(in.ActorID) == "" || strings.TrimSpace(in.CorrelationID) == "" || strings.TrimSpace(in.ChangeReason) == "" {
		return CreateInput{}, "", ErrInvalidInput
	}
	hostname, err := normalizeHostname(in.Hostname)
	if err != nil {
		return CreateInput{}, "", err
	}
	code, err := normalizeCode(in.Code)
	if err != nil {
		return CreateInput{}, "", err
	}
	if in.DomainKind != "official" && in.DomainKind != "custom" {
		return CreateInput{}, "", ErrInvalidInput
	}
	if err := validateRedirectStatus(in.RedirectStatus); err != nil {
		return CreateInput{}, "", err
	}
	if in.ClickLimit != nil && *in.ClickLimit == 0 {
		return CreateInput{}, "", ErrInvalidInput
	}
	primary, routing, variants, fingerprint, err := normalizeBehavior(in.PrimaryDestination, in.Routing, in.AB)
	if err != nil {
		return CreateInput{}, "", err
	}
	in.Hostname = hostname
	in.Code = code
	in.PrimaryDestination = primary
	in.Routing = routing
	in.AB = variants
	in.WorkspaceID = strings.TrimSpace(in.WorkspaceID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.CorrelationID = strings.TrimSpace(in.CorrelationID)
	in.ChangeReason = strings.TrimSpace(in.ChangeReason)
	return in, fingerprint, nil
}

func marshalBehavior(routing []RoutingRule, variants []ABVariant, utm UTMConfig, access AccessConfig) (string, string, string, string, error) {
	values := []any{routing, variants, utm, access}
	out := make([]string, 4)
	for i, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return "", "", "", "", err
		}
		out[i] = string(raw)
	}
	return out[0], out[1], out[2], out[3], nil
}

func (s *MySQLStore) Create(ctx context.Context, input CreateInput) (Link, error) {
	in, fingerprint, err := validateCreateInput(input)
	if err != nil {
		return Link{}, err
	}
	routingJSON, abJSON, utmJSON, accessJSON, err := marshalBehavior(in.Routing, in.AB, in.UTM, in.Access)
	if err != nil {
		return Link{}, err
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return Link{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO links (
			workspace_id, hostname, domain_kind, code, title, primary_destination,
			redirect_status, status, version, risk_fingerprint, routing_json, ab_json,
			utm_json, access_json, expires_at, click_limit, one_time
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.WorkspaceID, in.Hostname, in.DomainKind, in.Code, in.Title, in.PrimaryDestination,
		in.RedirectStatus, fingerprint, routingJSON, abJSON, utmJSON, accessJSON,
		in.ExpiresAt, in.ClickLimit, in.OneTime,
	)
	if err != nil {
		if isDuplicateKey(err) {
			return Link{}, ErrConflict
		}
		return Link{}, err
	}
	insertID, err := result.LastInsertId()
	if err != nil {
		return Link{}, err
	}

	created, err := s.getByIDTx(ctx, tx, in.WorkspaceID, uint64(insertID), false)
	if err != nil {
		return Link{}, err
	}
	if err := appendVersionTx(ctx, tx, created, in.ActorID, in.ChangeReason); err != nil {
		return Link{}, err
	}
	if err := appendAuditTx(ctx, tx, created.WorkspaceID, created.ID, in.ActorID, in.CorrelationID, "link.create", in.ChangeReason, "success", map[string]any{
		"version":          created.Version,
		"risk_fingerprint": created.RiskFingerprint,
	}); err != nil {
		return Link{}, err
	}
	if err := tx.Commit(); err != nil {
		return Link{}, err
	}
	return created, nil
}

func (s *MySQLStore) GetByID(ctx context.Context, workspaceID string, id uint64) (Link, error) {
	row := s.db.QueryRowContext(ctx, linkSelect+` WHERE workspace_id = ? AND id = ?`, strings.TrimSpace(workspaceID), id)
	return scanLink(row)
}

func (s *MySQLStore) GetByHostCode(ctx context.Context, hostname, code string) (Link, error) {
	host, err := normalizeHostname(hostname)
	if err != nil {
		return Link{}, err
	}
	normalizedCode, err := normalizeCode(code)
	if err != nil {
		return Link{}, err
	}
	row := s.db.QueryRowContext(ctx, linkSelect+` WHERE hostname = ? AND code = ?`, host, normalizedCode)
	return scanLink(row)
}

func (s *MySQLStore) Update(ctx context.Context, id uint64, input UpdateInput) (Link, error) {
	if input.ExpectedVersion == 0 || strings.TrimSpace(input.WorkspaceID) == "" || strings.TrimSpace(input.ActorID) == "" || strings.TrimSpace(input.CorrelationID) == "" || strings.TrimSpace(input.ChangeReason) == "" {
		return Link{}, ErrInvalidInput
	}
	if err := validateLifecycleStatus(input.Status); err != nil {
		return Link{}, err
	}
	hostname, err := normalizeHostname(input.Hostname)
	if err != nil {
		return Link{}, err
	}
	code, err := normalizeCode(input.Code)
	if err != nil {
		return Link{}, err
	}
	if input.DomainKind != "official" && input.DomainKind != "custom" {
		return Link{}, ErrInvalidInput
	}
	if err := validateRedirectStatus(input.RedirectStatus); err != nil {
		return Link{}, err
	}
	if input.ClickLimit != nil && *input.ClickLimit == 0 {
		return Link{}, ErrInvalidInput
	}
	primary, routing, variants, fingerprint, err := normalizeBehavior(input.PrimaryDestination, input.Routing, input.AB)
	if err != nil {
		return Link{}, err
	}
	routingJSON, abJSON, utmJSON, accessJSON, err := marshalBehavior(routing, variants, input.UTM, input.Access)
	if err != nil {
		return Link{}, err
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

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET
			hostname = ?, domain_kind = ?, code = ?, title = ?, primary_destination = ?,
			redirect_status = ?, status = ?, version = version + 1, risk_fingerprint = ?,
			routing_json = ?, ab_json = ?, utm_json = ?, access_json = ?, expires_at = ?,
			click_limit = ?, one_time = ?
		WHERE workspace_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`,
		hostname, input.DomainKind, code, input.Title, primary,
		input.RedirectStatus, input.Status, fingerprint,
		routingJSON, abJSON, utmJSON, accessJSON, input.ExpiresAt,
		input.ClickLimit, input.OneTime,
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

	updated, err := s.getByIDTx(ctx, tx, strings.TrimSpace(input.WorkspaceID), id, false)
	if err != nil {
		return Link{}, err
	}
	if err := appendVersionTx(ctx, tx, updated, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.ChangeReason)); err != nil {
		return Link{}, err
	}
	if err := appendAuditTx(ctx, tx, updated.WorkspaceID, updated.ID, strings.TrimSpace(input.ActorID), strings.TrimSpace(input.CorrelationID), "link.update", strings.TrimSpace(input.ChangeReason), "success", map[string]any{
		"from_version":      current.Version,
		"to_version":        updated.Version,
		"risk_fingerprint":  updated.RiskFingerprint,
		"risk_invalidated":  current.RiskFingerprint != updated.RiskFingerprint,
	}); err != nil {
		return Link{}, err
	}
	if err := tx.Commit(); err != nil {
		return Link{}, err
	}
	return updated, nil
}

func (s *MySQLStore) Delete(ctx context.Context, workspaceID string, id, expectedVersion uint64, actorID, correlationID, reason string) error {
	if expectedVersion == 0 || strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" || strings.TrimSpace(correlationID) == "" || strings.TrimSpace(reason) == "" {
		return ErrInvalidInput
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := s.getByIDTx(ctx, tx, strings.TrimSpace(workspaceID), id, true)
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return ErrConflict
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE links SET status = 'deleted', version = version + 1, deleted_at = CURRENT_TIMESTAMP(6)
		WHERE workspace_id = ? AND id = ? AND version = ? AND deleted_at IS NULL`,
		strings.TrimSpace(workspaceID), id, expectedVersion,
	)
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
	deleted, err := s.getByIDTx(ctx, tx, strings.TrimSpace(workspaceID), id, false)
	if err != nil {
		return err
	}
	if err := appendVersionTx(ctx, tx, deleted, strings.TrimSpace(actorID), strings.TrimSpace(reason)); err != nil {
		return err
	}
	if err := appendAuditTx(ctx, tx, deleted.WorkspaceID, deleted.ID, strings.TrimSpace(actorID), strings.TrimSpace(correlationID), "link.delete", strings.TrimSpace(reason), "success", map[string]any{"version": deleted.Version}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLStore) History(ctx context.Context, workspaceID string, id uint64) ([]LinkVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT version, actor_id, change_reason, snapshot_json, risk_fingerprint, created_at
		FROM link_versions WHERE workspace_id = ? AND link_id = ? ORDER BY version DESC`,
		strings.TrimSpace(workspaceID), id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []LinkVersion{}
	for rows.Next() {
		var version LinkVersion
		if err := rows.Scan(&version.Version, &version.ActorID, &version.ChangeReason, &version.Snapshot, &version.RiskFingerprint, &version.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, nil
}

const linkSelect = `
	SELECT id, workspace_id, hostname, domain_kind, code, title, primary_destination,
	       redirect_status, status, version, risk_fingerprint, routing_json, ab_json,
	       utm_json, access_json, expires_at, click_limit, click_count, one_time,
	       created_at, updated_at, deleted_at
	FROM links`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanLink(row rowScanner) (Link, error) {
	var link Link
	var routingJSON, abJSON, utmJSON, accessJSON []byte
	var expiresAt, deletedAt sql.NullTime
	var clickLimit sql.NullInt64
	if err := row.Scan(
		&link.ID, &link.WorkspaceID, &link.Hostname, &link.DomainKind, &link.Code, &link.Title,
		&link.PrimaryDestination, &link.RedirectStatus, &link.Status, &link.Version, &link.RiskFingerprint,
		&routingJSON, &abJSON, &utmJSON, &accessJSON, &expiresAt, &clickLimit, &link.ClickCount,
		&link.OneTime, &link.CreatedAt, &link.UpdatedAt, &deletedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Link{}, ErrNotFound
		}
		return Link{}, err
	}
	if err := json.Unmarshal(routingJSON, &link.Routing); err != nil {
		return Link{}, fmt.Errorf("decode routing JSON: %w", err)
	}
	if err := json.Unmarshal(abJSON, &link.AB); err != nil {
		return Link{}, fmt.Errorf("decode A/B JSON: %w", err)
	}
	if err := json.Unmarshal(utmJSON, &link.UTM); err != nil {
		return Link{}, fmt.Errorf("decode UTM JSON: %w", err)
	}
	if err := json.Unmarshal(accessJSON, &link.Access); err != nil {
		return Link{}, fmt.Errorf("decode access JSON: %w", err)
	}
	if expiresAt.Valid {
		value := expiresAt.Time.UTC()
		link.ExpiresAt = &value
	}
	if clickLimit.Valid {
		value := uint64(clickLimit.Int64)
		link.ClickLimit = &value
	}
	if deletedAt.Valid {
		value := deletedAt.Time.UTC()
		link.DeletedAt = &value
	}
	link.CreatedAt = link.CreatedAt.UTC()
	link.UpdatedAt = link.UpdatedAt.UTC()
	return link, nil
}

func (s *MySQLStore) getByIDTx(ctx context.Context, tx *sql.Tx, workspaceID string, id uint64, forUpdate bool) (Link, error) {
	query := linkSelect + ` WHERE workspace_id = ? AND id = ?`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanLink(tx.QueryRowContext(ctx, query, workspaceID, id))
}

func appendVersionTx(ctx context.Context, tx *sql.Tx, link Link, actorID, reason string) error {
	snapshot, err := json.Marshal(link)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO link_versions (link_id, workspace_id, version, actor_id, change_reason, snapshot_json, risk_fingerprint)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		link.ID, link.WorkspaceID, link.Version, actorID, reason, string(snapshot), link.RiskFingerprint,
	)
	return err
}

func appendAuditTx(ctx context.Context, tx *sql.Tx, workspaceID string, linkID uint64, actorID, correlationID, action, reason, result string, metadata map[string]any) error {
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO link_audit_events (workspace_id, link_id, actor_id, action, request_correlation_id, reason, result, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		workspaceID, linkID, actorID, action, correlationID, reason, result, string(metadataJSON),
	)
	return err
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
