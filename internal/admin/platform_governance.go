package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var officialHostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

type PlatformSetting struct {
	Key       string            `json:"key"`
	Value     map[string]string `json:"value"`
	Version   uint64            `json:"version"`
	UpdatedAt time.Time         `json:"updated_at"`
}

type UpdatePlatformSettingInput struct {
	Value           map[string]string `json:"value"`
	ExpectedVersion uint64            `json:"expected_version"`
}

type TurnstileConfig struct {
	SiteKey             string    `json:"site_key"`
	Enabled             bool      `json:"enabled"`
	ProviderState       string    `json:"provider_state"`
	SecretConfigured    bool      `json:"secret_configured"`
	ProtectionAvailable bool      `json:"protection_available"`
	FailClosed          bool      `json:"fail_closed"`
	Version             uint64    `json:"version"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type UpdateTurnstileInput struct {
	SiteKey         string `json:"site_key"`
	Secret          string `json:"secret"`
	Enabled         bool   `json:"enabled"`
	ProviderState   string `json:"provider_state"`
	ExpectedVersion uint64 `json:"expected_version"`
}

type OfficialDomain struct {
	ID         string    `json:"id"`
	Hostname   string    `json:"hostname"`
	Enabled    bool      `json:"enabled"`
	IsDefault  bool      `json:"is_default"`
	HTTPSState string    `json:"https_state"`
	Version    uint64    `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreateOfficialDomainInput struct {
	Hostname string `json:"hostname"`
}

type OfficialDomainActionInput struct {
	Action          string `json:"action"`
	ExpectedVersion uint64 `json:"expected_version"`
}

func VerifyPlatformGovernanceSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return ErrInvalid
	}
	for _, table := range []string{"admin_platform_settings", "admin_turnstile_config", "admin_official_domains", "admin_announcements", "admin_content_cache_state"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name=?`, table).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrInvalid
		}
	}
	return nil
}

func normalizeSettingValue(key string, value map[string]string) (map[string]string, error) {
	key = strings.TrimSpace(key)
	clean := map[string]string{}
	switch key {
	case "general":
		allowed := map[string]bool{"site_name": true, "public_base_url": true, "support_url": true}
		for k, v := range value {
			if !allowed[k] {
				return nil, ErrInvalid
			}
			clean[k] = strings.TrimSpace(v)
		}
		if clean["site_name"] == "" || len(clean["site_name"]) > 120 {
			return nil, ErrInvalid
		}
		for _, k := range []string{"public_base_url", "support_url"} {
			u, err := url.Parse(clean[k])
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
				return nil, ErrInvalid
			}
		}
	case "brand":
		allowed := map[string]bool{"logo_path": true, "favicon_path": true}
		for k, v := range value {
			if !allowed[k] {
				return nil, ErrInvalid
			}
			v = strings.TrimSpace(v)
			if !safeAssetPath(v) {
				return nil, ErrInvalid
			}
			clean[k] = v
		}
		if clean["logo_path"] == "" || clean["favicon_path"] == "" {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	return clean, nil
}

func safeAssetPath(value string) bool {
	if value == "" || len(value) > 255 || !strings.HasPrefix(value, "/assets/") || strings.Contains(value, "..") || strings.ContainsAny(value, "?#\\\r\n\t") {
		return false
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{"secret", "token", "password", "credential"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func (s *Service) GetPlatformSetting(ctx context.Context, p Principal, key string) (PlatformSetting, error) {
	if err := s.Require(p, PermissionSettingsManage); err != nil {
		return PlatformSetting{}, err
	}
	key = strings.TrimSpace(key)
	if key != "general" && key != "brand" {
		return PlatformSetting{}, ErrInvalid
	}
	var item PlatformSetting
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT setting_key,value_json,version,updated_at FROM admin_platform_settings WHERE setting_key=?`, key).Scan(&item.Key, &raw, &item.Version, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PlatformSetting{}, ErrNotFound
	}
	if err != nil {
		return PlatformSetting{}, err
	}
	if err := json.Unmarshal(raw, &item.Value); err != nil {
		return PlatformSetting{}, err
	}
	return item, nil
}

func (s *Service) UpdatePlatformSetting(ctx context.Context, p Principal, key string, input UpdatePlatformSettingInput, authority MutationAuthority, now time.Time) (PlatformSetting, bool, error) {
	if err := s.RequireHighRisk(p, PermissionSettingsManage, authority, now); err != nil {
		return PlatformSetting{}, false, err
	}
	key = strings.TrimSpace(key)
	clean, err := normalizeSettingValue(key, input.Value)
	if err != nil {
		return PlatformSetting{}, false, err
	}
	fingerprint, err := requestFingerprint(struct {
		Key             string
		Value           map[string]string
		ExpectedVersion uint64
	}{key, clean, input.ExpectedVersion})
	if err != nil {
		return PlatformSetting{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return PlatformSetting{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.platform.setting.update"
	if replay, ok, err := loadIdempotency[PlatformSetting](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return PlatformSetting{}, false, err
	} else if ok {
		return replay, true, nil
	}
	var current uint64
	err = tx.QueryRowContext(ctx, `SELECT version FROM admin_platform_settings WHERE setting_key=? FOR UPDATE`, key).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PlatformSetting{}, false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		if input.ExpectedVersion != 0 {
			return PlatformSetting{}, false, ErrConflict
		}
		current = 0
	} else if input.ExpectedVersion != current {
		return PlatformSetting{}, false, ErrConflict
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return PlatformSetting{}, false, err
	}
	version := current + 1
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO admin_platform_settings(setting_key,value_json,version,updated_by,updated_at) VALUES (?,CAST(? AS JSON),?,?,?)`, key, string(raw), version, p.Administrator.ID, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE admin_platform_settings SET value_json=CAST(? AS JSON),version=?,updated_by=?,updated_at=? WHERE setting_key=? AND version=?`, string(raw), version, p.Administrator.ID, now, key, current)
	}
	if err != nil {
		return PlatformSetting{}, false, mapDuplicate(err)
	}
	item := PlatformSetting{Key: key, Value: clean, Version: version, UpdatedAt: now}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "platform_setting", ResourceID: key, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"version": current}, After: map[string]any{"version": version}, Metadata: map[string]any{"resource_kind": key}, CreatedAt: now})
	if err != nil {
		return PlatformSetting{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return PlatformSetting{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return PlatformSetting{}, false, err
	}
	return item, false, nil
}

func validProviderState(value string) bool {
	return value == "healthy" || value == "incomplete" || value == "provider_error"
}

func turnstileProjection(siteKey string, enabled bool, providerState string, secretConfigured bool, version uint64, updatedAt time.Time) TurnstileConfig {
	available := enabled && providerState == "healthy" && strings.TrimSpace(siteKey) != "" && secretConfigured
	return TurnstileConfig{SiteKey: siteKey, Enabled: enabled, ProviderState: providerState, SecretConfigured: secretConfigured, ProtectionAvailable: available, FailClosed: enabled && !available, Version: version, UpdatedAt: updatedAt}
}

func (s *Service) GetTurnstileConfig(ctx context.Context, p Principal) (TurnstileConfig, error) {
	if err := s.Require(p, PermissionSettingsManage); err != nil {
		return TurnstileConfig{}, err
	}
	var siteKey, providerState string
	var ciphertext []byte
	var keyID sql.NullString
	var enabled bool
	var version uint64
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT site_key,secret_ciphertext,secret_key_id,enabled,provider_state,version,updated_at FROM admin_turnstile_config WHERE id=1`).Scan(&siteKey, &ciphertext, &keyID, &enabled, &providerState, &version, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return turnstileProjection("", false, "incomplete", false, 0, time.Time{}), nil
	}
	if err != nil {
		return TurnstileConfig{}, err
	}
	return turnstileProjection(siteKey, enabled, providerState, len(ciphertext) > 0 && keyID.Valid, version, updatedAt), nil
}

func (s *Service) UpdateTurnstileConfig(ctx context.Context, p Principal, input UpdateTurnstileInput, authority MutationAuthority, now time.Time) (TurnstileConfig, bool, error) {
	if err := s.RequireHighRisk(p, PermissionSettingsManage, authority, now); err != nil {
		return TurnstileConfig{}, false, err
	}
	input.SiteKey = strings.TrimSpace(input.SiteKey)
	input.Secret = strings.TrimSpace(input.Secret)
	input.ProviderState = strings.TrimSpace(input.ProviderState)
	if !validProviderState(input.ProviderState) || len(input.SiteKey) > 255 || len(input.Secret) > 1024 {
		return TurnstileConfig{}, false, ErrInvalid
	}
	if input.Enabled && (input.SiteKey == "" || input.Secret == "") {
		return TurnstileConfig{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		SiteKey         string
		SecretPresent   bool
		Enabled         bool
		ProviderState   string
		ExpectedVersion uint64
	}{input.SiteKey, input.Secret != "", input.Enabled, input.ProviderState, input.ExpectedVersion})
	if err != nil {
		return TurnstileConfig{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return TurnstileConfig{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.platform.turnstile.update"
	if replay, ok, err := loadIdempotency[TurnstileConfig](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return TurnstileConfig{}, false, err
	} else if ok {
		return replay, true, nil
	}
	var current uint64
	var oldCipher []byte
	var oldKey sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT version,secret_ciphertext,secret_key_id FROM admin_turnstile_config WHERE id=1 FOR UPDATE`).Scan(&current, &oldCipher, &oldKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TurnstileConfig{}, false, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		if input.ExpectedVersion != 0 {
			return TurnstileConfig{}, false, ErrConflict
		}
		current = 0
	} else if input.ExpectedVersion != current {
		return TurnstileConfig{}, false, ErrConflict
	}
	ciphertext := oldCipher
	keyID := ""
	if oldKey.Valid {
		keyID = oldKey.String
	}
	if input.Secret != "" {
		ciphertext, err = s.cipher.Encrypt([]byte(input.Secret))
		if err != nil {
			return TurnstileConfig{}, false, err
		}
		keyID = s.cipher.KeyID()
	}
	if input.Enabled && (len(ciphertext) == 0 || keyID == "") {
		return TurnstileConfig{}, false, ErrInvalid
	}
	version := current + 1
	if current == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO admin_turnstile_config(id,site_key,secret_ciphertext,secret_key_id,enabled,provider_state,version,updated_by,updated_at) VALUES (1,?,?,?,?,?,?,?,?)`, input.SiteKey, nullBytes(ciphertext), nullString(keyID), input.Enabled, input.ProviderState, version, p.Administrator.ID, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE admin_turnstile_config SET site_key=?,secret_ciphertext=?,secret_key_id=?,enabled=?,provider_state=?,version=?,updated_by=?,updated_at=? WHERE id=1 AND version=?`, input.SiteKey, nullBytes(ciphertext), nullString(keyID), input.Enabled, input.ProviderState, version, p.Administrator.ID, now, current)
	}
	if err != nil {
		return TurnstileConfig{}, false, mapDuplicate(err)
	}
	item := turnstileProjection(input.SiteKey, input.Enabled, input.ProviderState, len(ciphertext) > 0 && keyID != "", version, now)
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "turnstile_config", ResourceID: "singleton", Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"version": current}, After: map[string]any{"version": version, "status": input.ProviderState}, Metadata: map[string]any{"configured": item.SecretConfigured}, CreatedAt: now})
	if err != nil {
		return TurnstileConfig{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return TurnstileConfig{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return TurnstileConfig{}, false, err
	}
	return item, false, nil
}

func nullBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func normalizeOfficialHostname(value string) (string, error) {
	value = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
	if len(value) > 253 || !officialHostnamePattern.MatchString(value) {
		return "", ErrInvalid
	}
	return value, nil
}

func (s *Service) ListOfficialDomains(ctx context.Context, p Principal) ([]OfficialDomain, error) {
	if err := s.Require(p, PermissionDomainsManage); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,hostname_ascii,enabled,is_default,https_state,version,created_at,updated_at FROM admin_official_domains ORDER BY hostname_ascii,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OfficialDomain{}
	for rows.Next() {
		var item OfficialDomain
		if err := rows.Scan(&item.ID, &item.Hostname, &item.Enabled, &item.IsDefault, &item.HTTPSState, &item.Version, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) CreateOfficialDomain(ctx context.Context, p Principal, input CreateOfficialDomainInput, authority MutationAuthority, now time.Time) (OfficialDomain, bool, error) {
	if err := s.RequireHighRisk(p, PermissionDomainsManage, authority, now); err != nil {
		return OfficialDomain{}, false, err
	}
	hostname, err := normalizeOfficialHostname(input.Hostname)
	if err != nil {
		return OfficialDomain{}, false, err
	}
	fingerprint, err := requestFingerprint(hostname)
	if err != nil {
		return OfficialDomain{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OfficialDomain{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.platform.official_domain.create"
	if replay, ok, err := loadIdempotency[OfficialDomain](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return OfficialDomain{}, false, err
	} else if ok {
		return replay, true, nil
	}
	id, err := newOpaque("ofd_", 18)
	if err != nil {
		return OfficialDomain{}, false, err
	}
	item := OfficialDomain{ID: id, Hostname: hostname, Enabled: true, IsDefault: false, HTTPSState: "pending", Version: 1, CreatedAt: now, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_official_domains(id,hostname_ascii,enabled,is_default,https_state,version,created_by,updated_by,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, item.ID, item.Hostname, item.Enabled, item.IsDefault, item.HTTPSState, item.Version, p.Administrator.ID, p.Administrator.ID, now, now)
	if err != nil {
		return OfficialDomain{}, false, mapDuplicate(err)
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "official_domain", ResourceID: item.ID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, After: map[string]any{"status": item.HTTPSState, "version": item.Version}, Metadata: map[string]any{"hostname": item.Hostname}, CreatedAt: now})
	if err != nil {
		return OfficialDomain{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return OfficialDomain{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return OfficialDomain{}, false, err
	}
	return item, false, nil
}

func (s *Service) MutateOfficialDomain(ctx context.Context, p Principal, domainID string, input OfficialDomainActionInput, authority MutationAuthority, now time.Time) (OfficialDomain, bool, error) {
	if err := s.RequireHighRisk(p, PermissionDomainsManage, authority, now); err != nil {
		return OfficialDomain{}, false, err
	}
	domainID = strings.TrimSpace(domainID)
	input.Action = strings.TrimSpace(input.Action)
	if !validID(domainID, 64) {
		return OfficialDomain{}, false, ErrInvalid
	}
	validAction := map[string]bool{"enable": true, "disable": true, "set_default": true, "https_active": true, "https_failed": true}
	if !validAction[input.Action] {
		return OfficialDomain{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		ID              string
		Action          string
		ExpectedVersion uint64
	}{domainID, input.Action, input.ExpectedVersion})
	if err != nil {
		return OfficialDomain{}, false, err
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OfficialDomain{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.platform.official_domain.mutate"
	if replay, ok, err := loadIdempotency[OfficialDomain](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return OfficialDomain{}, false, err
	} else if ok {
		return replay, true, nil
	}
	var item OfficialDomain
	if err := tx.QueryRowContext(ctx, `SELECT id,hostname_ascii,enabled,is_default,https_state,version,created_at,updated_at FROM admin_official_domains WHERE id=? FOR UPDATE`, domainID).Scan(&item.ID, &item.Hostname, &item.Enabled, &item.IsDefault, &item.HTTPSState, &item.Version, &item.CreatedAt, &item.UpdatedAt); errors.Is(err, sql.ErrNoRows) {
		return OfficialDomain{}, false, ErrNotFound
	} else if err != nil {
		return OfficialDomain{}, false, err
	}
	if item.Version != input.ExpectedVersion {
		return OfficialDomain{}, false, ErrConflict
	}
	before := item
	switch input.Action {
	case "enable":
		item.Enabled = true
	case "disable":
		if item.IsDefault {
			return OfficialDomain{}, false, ErrConflict
		}
		item.Enabled = false
	case "set_default":
		if !item.Enabled || item.HTTPSState != "active" {
			return OfficialDomain{}, false, ErrConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE admin_official_domains SET is_default=FALSE,version=version+1,updated_by=?,updated_at=? WHERE is_default=TRUE AND id<>?`, p.Administrator.ID, now, item.ID); err != nil {
			return OfficialDomain{}, false, err
		}
		item.IsDefault = true
	case "https_active":
		item.HTTPSState = "active"
	case "https_failed":
		item.HTTPSState = "failed"
		if item.IsDefault {
			return OfficialDomain{}, false, ErrConflict
		}
	}
	item.Version++
	item.UpdatedAt = now
	_, err = tx.ExecContext(ctx, `UPDATE admin_official_domains SET enabled=?,is_default=?,https_state=?,version=?,updated_by=?,updated_at=? WHERE id=? AND version=?`, item.Enabled, item.IsDefault, item.HTTPSState, item.Version, p.Administrator.ID, now, item.ID, before.Version)
	if err != nil {
		return OfficialDomain{}, false, err
	}
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "official_domain", ResourceID: item.ID, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"status": before.HTTPSState, "version": before.Version, "enabled": before.Enabled, "default": before.IsDefault}, After: map[string]any{"status": item.HTTPSState, "version": item.Version, "enabled": item.Enabled, "default": item.IsDefault}, Metadata: map[string]any{"hostname": item.Hostname, "action": input.Action}, CreatedAt: now})
	if err != nil {
		return OfficialDomain{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return OfficialDomain{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return OfficialDomain{}, false, err
	}
	return item, false, nil
}
