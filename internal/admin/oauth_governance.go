package admin

import (
	"context"
	"database/sql"
	"errors"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

func (s *Service) UpdateOAuthProvider(ctx context.Context, p Principal, oauth *authn.OAuthService, input authn.OAuthProviderUpdate, version uint64, authority MutationAuthority, now time.Time) (authn.OAuthProviderConfig, bool, error) {
	if err := s.RequireHighRisk(p, PermissionSettingsManage, authority, now); err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	if oauth == nil {
		return authn.OAuthProviderConfig{}, false, ErrInvalid
	}
	fingerprint, err := requestFingerprint(struct {
		Input   authn.OAuthProviderUpdate
		Version uint64
	}{input, version})
	if err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	defer tx.Rollback()
	const action = "admin.oauth.provider.update"
	if result, ok, err := loadIdempotency[authn.OAuthProviderConfig](ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint); err != nil {
		return authn.OAuthProviderConfig{}, false, err
	} else if ok {
		return result, true, nil
	}
	item, err := oauth.UpdateProviderConfigForAdministratorTx(ctx, tx, p.Administrator.ID, version, input, now)
	if errors.Is(err, authn.ErrConflict) {
		return authn.OAuthProviderConfig{}, false, ErrConflict
	}
	if errors.Is(err, authn.ErrInvalid) {
		return authn.OAuthProviderConfig{}, false, ErrInvalid
	}
	if err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	// Never include submitted credentials or endpoint query strings in audit data.
	auditID, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: p.Administrator.ID, Action: action, ResourceType: "oauth_provider_config", ResourceID: item.Provider, Result: "success", CorrelationID: authority.CorrelationID, Reason: authority.Reason, Before: map[string]any{"version": version}, After: map[string]any{"version": item.Version, "enabled": item.Enabled}, CreatedAt: now})
	if err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	if err := storeIdempotency(ctx, tx, p.Administrator.ID, action, authority.IdempotencyKey, fingerprint, item, auditID, now); err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return authn.OAuthProviderConfig{}, false, err
	}
	return item, false, nil
}
