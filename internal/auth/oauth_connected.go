package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

func (s *OAuthService) BindConnectedAccount(ctx context.Context, current Session, authority *UnsafeMutationAuthority, callback OAuthCallbackResult, correlationID string, now time.Time) (OAuthIdentity, error) {
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !authority.consumeFor(current) || callback.Intent != OAuthIntentBind || !ValidProvider(callback.Provider) || strings.TrimSpace(callback.ProviderSubject) == "" || !validCorrelationID(correlationID) {
		return OAuthIdentity{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OAuthIdentity{}, err
	}
	defer tx.Rollback()
	if err := requireCurrentSessionTx(ctx, tx, current, now); err != nil {
		return OAuthIdentity{}, err
	}
	if callback.InitiatingUserID != current.UserID || callback.InitiatingSessionID != current.ID {
		return OAuthIdentity{}, ErrForbidden
	}
	var consumedAt sql.NullTime
	var stateUserID, stateSessionID sql.NullString
	var stateProvider, stateIntent string
	if err := tx.QueryRowContext(ctx, `
SELECT provider,intent,initiating_user_id,initiating_session_id,consumed_at
FROM oauth_states WHERE id=? FOR UPDATE`, callback.StateID).Scan(&stateProvider, &stateIntent, &stateUserID, &stateSessionID, &consumedAt); errors.Is(err, sql.ErrNoRows) {
		return OAuthIdentity{}, ErrForbidden
	} else if err != nil {
		return OAuthIdentity{}, err
	}
	if !consumedAt.Valid || stateProvider != callback.Provider || stateIntent != OAuthIntentBind || !stateUserID.Valid || stateUserID.String != current.UserID || !stateSessionID.Valid || stateSessionID.String != current.ID {
		return OAuthIdentity{}, ErrForbidden
	}
	identityID, err := newOpaqueID("oid_", 18)
	if err != nil {
		return OAuthIdentity{}, err
	}
	subjectHash := HashOpaque(callback.Provider + "\x00" + callback.ProviderSubject)
	var emailValue any
	if callback.ProviderEmail != "" {
		normalized, err := NormalizeEmail(callback.ProviderEmail)
		if err != nil {
			return OAuthIdentity{}, ErrInvalid
		}
		emailValue = normalized
	}
	when := now.UTC().Truncate(time.Microsecond)
	_, err = tx.ExecContext(ctx, `
INSERT INTO oauth_identities
(id,user_id,provider,provider_subject_hash,provider_email_normalized,provider_email_verified,display_name,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`, identityID, current.UserID, callback.Provider, subjectHash[:], emailValue, callback.ProviderEmailVerified, strings.TrimSpace(callback.DisplayName), when, when)
	if err != nil {
		if mysqlDuplicate(err) {
			return OAuthIdentity{}, ErrConflict
		}
		return OAuthIdentity{}, err
	}
	if err := recordAccountAuditTx(ctx, tx, current.UserID, "auth.oauth.connected", "oauth_identity", identityID, correlationID, "success", when); err != nil {
		return OAuthIdentity{}, err
	}
	if err := tx.Commit(); err != nil {
		return OAuthIdentity{}, err
	}
	identity := OAuthIdentity{ID: identityID, UserID: current.UserID, Provider: callback.Provider, ProviderEmailVerified: callback.ProviderEmailVerified, DisplayName: strings.TrimSpace(callback.DisplayName), CreatedAt: when, UpdatedAt: when}
	if value, ok := emailValue.(string); ok {
		identity.ProviderEmail = value
	}
	return identity, nil
}

func (s *OAuthService) ListConnectedAccounts(ctx context.Context, current Session, now time.Time) ([]OAuthIdentity, error) {
	if s == nil || s.db == nil {
		return nil, ErrInvalid
	}
	if err := requireCurrentSessionDB(ctx, s.db, current, now); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id,user_id,provider,provider_email_normalized,provider_email_verified,display_name,created_at,updated_at
FROM oauth_identities WHERE user_id=? ORDER BY provider,id`, current.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OAuthIdentity{}
	for rows.Next() {
		var item OAuthIdentity
		var email sql.NullString
		if err := rows.Scan(&item.ID, &item.UserID, &item.Provider, &email, &item.ProviderEmailVerified, &item.DisplayName, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if email.Valid {
			item.ProviderEmail = email.String
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *OAuthService) UnbindConnectedAccount(ctx context.Context, current Session, authority *UnsafeMutationAuthority, provider, correlationID string, now time.Time) error {
	provider = strings.TrimSpace(provider)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || !authority.consumeFor(current) || !ValidProvider(provider) || !validCorrelationID(correlationID) {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := requireCurrentSessionTx(ctx, tx, current, now); err != nil {
		return err
	}
	var identityID string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM oauth_identities WHERE user_id=? AND provider=? FOR UPDATE`, current.UserID, provider).Scan(&identityID); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM oauth_identities WHERE id=? AND user_id=?`, identityID, current.UserID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrNotFound
	}
	when := now.UTC().Truncate(time.Microsecond)
	if err := recordAccountAuditTx(ctx, tx, current.UserID, "auth.oauth.disconnected", "oauth_identity", identityID, correlationID, "success", when); err != nil {
		return err
	}
	return tx.Commit()
}
