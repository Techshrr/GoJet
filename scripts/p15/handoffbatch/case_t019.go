package handoffbatch

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
)

func runT019(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fx, cleanup, err := newOAuthFixture(ctx, db, auth.ProviderGitHub, "p15-t019", now)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	cb, err := callback(ctx, fx, auth.ProviderGitHub, auth.OAuthIntentRegister, "p15-t019-subject", "", false, "p15-t019-a", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	handoff, err := fx.service.CreateBrowserHandoff(ctx, cb, "p15-t019-handoff", now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	hash := auth.HashOpaque(handoff.Code)
	var hashRows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_handoffs WHERE handoff_kind='callback' AND code_hash=?`, hash[:]).Scan(&hashRows); err != nil {
		return nil, nil, err
	}
	exchanged, err := fx.service.ExchangeBrowserHandoff(ctx, handoff.Code, "p15-t019-exchange", now.Add(3*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, replayErr := fx.service.ExchangeBrowserHandoff(ctx, handoff.Code, "p15-t019-replay", now.Add(4*time.Second))

	cb2, err := callback(ctx, fx, auth.ProviderGitHub, auth.OAuthIntentRegister, "p15-t019-expired-subject", "", false, "p15-t019-b", now.Add(5*time.Second))
	if err != nil {
		return nil, nil, err
	}
	expiredHandoff, err := fx.service.CreateBrowserHandoff(ctx, cb2, "p15-t019-expired-handoff", now.Add(6*time.Second))
	if err != nil {
		return nil, nil, err
	}
	expiredHash := auth.HashOpaque(expiredHandoff.Code)
	if _, err := db.ExecContext(ctx, `UPDATE oauth_handoffs SET created_at=?,expires_at=? WHERE handoff_kind='callback' AND code_hash=?`, now.Add(-2*time.Minute), now.Add(-time.Minute), expiredHash[:]); err != nil {
		return nil, nil, err
	}
	_, expiredErr := fx.service.ExchangeBrowserHandoff(ctx, expiredHandoff.Code, "p15-t019-expired", now.Add(7*time.Second))

	var auditText string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(GROUP_CONCAT(CAST(metadata_json AS CHAR) SEPARATOR '|'),'') FROM auth_audit_events WHERE request_correlation_id LIKE 'p15-t019-%'`).Scan(&auditText); err != nil {
		return nil, nil, err
	}
	leakFree := true
	for _, fragment := range []string{handoff.Code, exchanged.SocialRegistrationCode, "p15-t019-subject", fx.secret} {
		if fragment != "" && strings.Contains(auditText, fragment) {
			leakFree = false
		}
	}
	var callbackRows, socialRows int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_handoffs WHERE handoff_kind='callback' AND correlation_id LIKE 'p15-t019-%'`).Scan(&callbackRows)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_handoffs WHERE handoff_kind='social_registration' AND correlation_id LIKE 'p15-t019-%'`).Scan(&socialRows)
	checks := map[string]bool{
		"browser_code_is_hash_only_durable_authority":                 hashRows == 1 && strings.HasPrefix(handoff.Code, "goh_"),
		"unbound_callback_exchanges_only_to_social_continuation":      exchanged.Session == nil && strings.HasPrefix(exchanged.SocialRegistrationCode, "gsr_"),
		"browser_handoff_is_one_time":                                 errors.Is(replayErr, auth.ErrReplay),
		"expired_browser_handoff_fails_closed":                        errors.Is(expiredErr, auth.ErrExpired),
		"audit_metadata_excludes_handoff_provider_secret_and_subject": leakFree,
	}
	return checks, map[string]int{"callback_handoffs": callbackRows, "social_handoffs": socialRows}, nil
}
