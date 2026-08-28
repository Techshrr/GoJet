package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	workspaceAPIKeySecretPrefix = "gak_"
	workspaceAPIKeyRatePrefix   = "workspace-api-key:rate:"
)

type WorkspaceAPIKeyAuthority struct {
	db    *sql.DB
	redis *redis.Client
}

type WorkspaceAPIKeyInput struct {
	Name               string     `json:"name"`
	Scopes             []string   `json:"scopes"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RateLimitPerMinute int        `json:"rate_limit_per_minute"`
}

type WorkspaceAPIKey struct {
	ID                 string     `json:"id"`
	WorkspaceID        string     `json:"workspace_id"`
	Name               string     `json:"name"`
	Prefix             string     `json:"prefix"`
	Scopes             []string   `json:"scopes"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RateLimitPerMinute int        `json:"rate_limit_per_minute"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	RotatedAt          *time.Time `json:"rotated_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

type WorkspaceAPIKeySecret struct {
	Key    WorkspaceAPIKey `json:"key"`
	Secret string          `json:"secret"`
}

func NewWorkspaceAPIKeyAuthority(db *sql.DB, redisClient *redis.Client) (*WorkspaceAPIKeyAuthority, error) {
	if db == nil || redisClient == nil {
		return nil, ErrInvalid
	}
	return &WorkspaceAPIKeyAuthority{db: db, redis: redisClient}, nil
}

func normalizeWorkspaceAPIKeyScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 || len(scopes) > 32 {
		return nil, ErrInvalid
	}
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, raw := range scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" || len(scope) > 96 || strings.Contains(scope, "*") || strings.ContainsAny(scope, "\r\n\t ") {
			return nil, ErrInvalid
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	if len(out) == 0 {
		return nil, ErrInvalid
	}
	sort.Strings(out)
	return out, nil
}

func validateWorkspaceAPIKeyInput(input WorkspaceAPIKeyInput, now time.Time) (WorkspaceAPIKeyInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 128 || input.RateLimitPerMinute < 1 || input.RateLimitPerMinute > 10000 {
		return WorkspaceAPIKeyInput{}, ErrInvalid
	}
	scopes, err := normalizeWorkspaceAPIKeyScopes(input.Scopes)
	if err != nil {
		return WorkspaceAPIKeyInput{}, err
	}
	input.Scopes = scopes
	if input.ExpiresAt != nil {
		value := input.ExpiresAt.UTC()
		if !value.After(now.UTC()) {
			return WorkspaceAPIKeyInput{}, ErrInvalid
		}
		input.ExpiresAt = &value
	}
	return input, nil
}

func (a *WorkspaceAPIKeyAuthority) requireWorkspaceManager(ctx context.Context, workspaceID, actorID string) error {
	workspaceID, actorID = strings.TrimSpace(workspaceID), strings.TrimSpace(actorID)
	if workspaceID == "" || actorID == "" {
		return ErrInvalid
	}
	var role string
	err := a.db.QueryRowContext(ctx, `SELECT m.role FROM workspace_memberships m JOIN workspaces w ON w.id=m.workspace_id WHERE m.workspace_id=? AND m.user_id=? AND w.status='active'`, workspaceID, actorID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if role != "owner" && role != "admin" {
		return ErrForbidden
	}
	return nil
}

func apiKeyPrefix(secret string) string {
	if len(secret) <= 12 {
		return secret
	}
	return secret[:12]
}

func (a *WorkspaceAPIKeyAuthority) Create(ctx context.Context, workspaceID, actorID string, input WorkspaceAPIKeyInput, correlationID string, now time.Time) (WorkspaceAPIKeySecret, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "api_key.create", "", correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceAPIKeySecret{}, err
	}
	normalized, err := validateWorkspaceAPIKeyInput(input, now)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	id, err := newOpaque("wak_", 18)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	secret, err := newOpaque(workspaceAPIKeySecretPrefix, 32)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	digest := hashOpaque(secret)
	scopesJSON, _ := json.Marshal(normalized.Scopes)
	_, err = a.db.ExecContext(ctx, `INSERT INTO workspace_api_keys(id,workspace_id,name,secret_hash,secret_prefix,scopes_json,status,expires_at,rate_limit_per_minute,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?,'active',?,?,?, ?,?)`, id, workspaceID, normalized.Name, digest[:], apiKeyPrefix(secret), scopesJSON, normalized.ExpiresAt, normalized.RateLimitPerMinute, actorID, now.UTC(), now.UTC())
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	key, err := a.get(ctx, workspaceID, id)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "api_key.create", id, correlationID, "success", map[string]any{"prefix": key.Prefix, "scopes": key.Scopes}, now)
	return WorkspaceAPIKeySecret{Key: key, Secret: secret}, nil
}

func (a *WorkspaceAPIKeyAuthority) List(ctx context.Context, workspaceID, actorID string) ([]WorkspaceAPIKey, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		return nil, err
	}
	rows, err := a.db.QueryContext(ctx, `SELECT id,workspace_id,name,secret_prefix,scopes_json,status,expires_at,rate_limit_per_minute,created_by,created_at,updated_at,rotated_at,revoked_at FROM workspace_api_keys WHERE workspace_id=? ORDER BY created_at,id`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceAPIKey{}
	for rows.Next() {
		key, err := scanWorkspaceAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

func (a *WorkspaceAPIKeyAuthority) Rotate(ctx context.Context, workspaceID, actorID, keyID, correlationID string, now time.Time) (WorkspaceAPIKeySecret, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "api_key.rotate", keyID, correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceAPIKeySecret{}, err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM workspace_api_keys WHERE workspace_id=? AND id=? FOR UPDATE`, workspaceID, keyID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WorkspaceAPIKeySecret{}, ErrNotFound
		}
		return WorkspaceAPIKeySecret{}, err
	}
	if status != "active" {
		return WorkspaceAPIKeySecret{}, ErrConflict
	}
	secret, err := newOpaque(workspaceAPIKeySecretPrefix, 32)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	digest := hashOpaque(secret)
	if _, err := tx.ExecContext(ctx, `UPDATE workspace_api_keys SET secret_hash=?,secret_prefix=?,rotated_at=?,updated_at=? WHERE workspace_id=? AND id=? AND status='active'`, digest[:], apiKeyPrefix(secret), now.UTC(), now.UTC(), workspaceID, keyID); err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	if err := tx.Commit(); err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	key, err := a.get(ctx, workspaceID, keyID)
	if err != nil {
		return WorkspaceAPIKeySecret{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "api_key.rotate", keyID, correlationID, "success", map[string]any{"prefix": key.Prefix}, now)
	return WorkspaceAPIKeySecret{Key: key, Secret: secret}, nil
}

func (a *WorkspaceAPIKeyAuthority) Revoke(ctx context.Context, workspaceID, actorID, keyID, correlationID string, now time.Time) (WorkspaceAPIKey, error) {
	if err := a.requireWorkspaceManager(ctx, workspaceID, actorID); err != nil {
		a.auditBestEffort(ctx, workspaceID, actorID, "api_key.revoke", keyID, correlationID, "denied", map[string]any{"reason": "workspace_role"}, now)
		return WorkspaceAPIKey{}, err
	}
	result, err := a.db.ExecContext(ctx, `UPDATE workspace_api_keys SET status='revoked',revoked_by=?,revoked_at=?,updated_at=? WHERE workspace_id=? AND id=? AND status='active'`, actorID, now.UTC(), now.UTC(), workspaceID, keyID)
	if err != nil {
		return WorkspaceAPIKey{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var exists int
		if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id=? AND id=?`, workspaceID, keyID).Scan(&exists); err != nil {
			return WorkspaceAPIKey{}, err
		}
		if exists == 0 {
			return WorkspaceAPIKey{}, ErrNotFound
		}
		return WorkspaceAPIKey{}, ErrConflict
	}
	key, err := a.get(ctx, workspaceID, keyID)
	if err != nil {
		return WorkspaceAPIKey{}, err
	}
	a.auditBestEffort(ctx, workspaceID, actorID, "api_key.revoke", keyID, correlationID, "success", map[string]any{"prefix": key.Prefix}, now)
	return key, nil
}

func (a *WorkspaceAPIKeyAuthority) Authenticate(ctx context.Context, secret, requiredScope string, now time.Time) (WorkspaceAPIKey, error) {
	secret, requiredScope = strings.TrimSpace(secret), strings.TrimSpace(requiredScope)
	if !strings.HasPrefix(secret, workspaceAPIKeySecretPrefix) || requiredScope == "" || strings.Contains(requiredScope, "*") {
		return WorkspaceAPIKey{}, ErrUnauthorized
	}
	digest := hashOpaque(secret)
	row := a.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,secret_prefix,scopes_json,status,expires_at,rate_limit_per_minute,created_by,created_at,updated_at,rotated_at,revoked_at FROM workspace_api_keys WHERE secret_hash=?`, digest[:])
	key, err := scanWorkspaceAPIKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceAPIKey{}, ErrUnauthorized
	}
	if err != nil {
		return WorkspaceAPIKey{}, err
	}
	if key.Status != "active" || (key.ExpiresAt != nil && !key.ExpiresAt.After(now.UTC())) {
		return WorkspaceAPIKey{}, ErrUnauthorized
	}
	allowed := false
	for _, scope := range key.Scopes {
		if scope == requiredScope {
			allowed = true
			break
		}
	}
	if !allowed {
		return WorkspaceAPIKey{}, ErrForbidden
	}
	window := now.UTC().Unix() / 60
	redisKey := fmt.Sprintf("%s%s:%d", workspaceAPIKeyRatePrefix, key.ID, window)
	count, err := a.redis.Incr(ctx, redisKey).Result()
	if err != nil {
		return WorkspaceAPIKey{}, err
	}
	if count == 1 {
		if err := a.redis.Expire(ctx, redisKey, 2*time.Minute).Err(); err != nil {
			return WorkspaceAPIKey{}, err
		}
	}
	if count > int64(key.RateLimitPerMinute) {
		return WorkspaceAPIKey{}, ErrRateLimited
	}
	return key, nil
}

func (a *WorkspaceAPIKeyAuthority) get(ctx context.Context, workspaceID, keyID string) (WorkspaceAPIKey, error) {
	key, err := scanWorkspaceAPIKey(a.db.QueryRowContext(ctx, `SELECT id,workspace_id,name,secret_prefix,scopes_json,status,expires_at,rate_limit_per_minute,created_by,created_at,updated_at,rotated_at,revoked_at FROM workspace_api_keys WHERE workspace_id=? AND id=?`, workspaceID, keyID))
	if errors.Is(err, sql.ErrNoRows) {
		return WorkspaceAPIKey{}, ErrNotFound
	}
	return key, err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanWorkspaceAPIKey(row rowScanner) (WorkspaceAPIKey, error) {
	var key WorkspaceAPIKey
	var scopes []byte
	var expires, rotated, revoked sql.NullTime
	if err := row.Scan(&key.ID, &key.WorkspaceID, &key.Name, &key.Prefix, &scopes, &key.Status, &expires, &key.RateLimitPerMinute, &key.CreatedBy, &key.CreatedAt, &key.UpdatedAt, &rotated, &revoked); err != nil {
		return WorkspaceAPIKey{}, err
	}
	if err := json.Unmarshal(scopes, &key.Scopes); err != nil {
		return WorkspaceAPIKey{}, err
	}
	if expires.Valid {
		value := expires.Time.UTC()
		key.ExpiresAt = &value
	}
	if rotated.Valid {
		value := rotated.Time.UTC()
		key.RotatedAt = &value
	}
	if revoked.Valid {
		value := revoked.Time.UTC()
		key.RevokedAt = &value
	}
	return key, nil
}

func (a *WorkspaceAPIKeyAuthority) auditBestEffort(ctx context.Context, workspaceID, actorID, action, resourceID, correlationID, result string, metadata map[string]any, now time.Time) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(actorID) == "" {
		return
	}
	if strings.TrimSpace(correlationID) == "" {
		correlationID = fmt.Sprintf("p17-api-key-%d", now.UTC().UnixNano())
	}
	raw, _ := json.Marshal(metadata)
	_, _ = a.db.ExecContext(ctx, `INSERT INTO workspace_audit_events(workspace_id,actor_id,action,resource_type,resource_id,reason,request_correlation_id,result,metadata_json,created_at) SELECT ?,?,?,?,?,NULL,?,?,?,? FROM workspaces WHERE id=?`, workspaceID, actorID, action, "api_key", resourceID, correlationID, result, raw, now.UTC(), workspaceID)
}
