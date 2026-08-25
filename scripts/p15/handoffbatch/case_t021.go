package handoffbatch

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

func runT021(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fx, cleanup, err := newOAuthFixture(ctx, db, auth.ProviderFacebook, "p15-t021", now)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	key, err := runnerutil.GrantKey()
	if err != nil {
		return nil, nil, err
	}
	social, err := auth.NewSocialRegistrationService(db, key, time.Hour)
	if err != nil {
		return nil, nil, err
	}
	firstCode, err := socialCode(ctx, fx, auth.ProviderFacebook, "p15-t021-subject", "p15-t021-owner@example.test", true, "p15-t021-owner", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	ownerSession, err := social.Complete(ctx, auth.CompleteSocialRegistrationInput{SocialCode: firstCode, CorrelationID: "p15-t021-owner-complete"}, now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	ownerID := ownerSession.Session.UserID

	loginCB, err := callback(ctx, fx, auth.ProviderFacebook, auth.OAuthIntentLogin, "p15-t021-subject", "p15-t021-owner@example.test", true, "p15-t021-login", now.Add(3*time.Second))
	if err != nil {
		return nil, nil, err
	}
	loginHandoff, err := fx.service.CreateBrowserHandoff(ctx, loginCB, "p15-t021-login-handoff", now.Add(4*time.Second))
	if err != nil {
		return nil, nil, err
	}
	tampered := loginHandoff.Code[:len(loginHandoff.Code)-1] + "A"
	if tampered == loginHandoff.Code {
		tampered = loginHandoff.Code[:len(loginHandoff.Code)-1] + "B"
	}
	_, tamperedErr := fx.service.ExchangeBrowserHandoff(ctx, tampered, "p15-t021-tampered", now.Add(5*time.Second))
	loginExchange, loginErr := fx.service.ExchangeBrowserHandoff(ctx, loginHandoff.Code, "p15-t021-login-exchange", now.Add(6*time.Second))
	_, replayErr := fx.service.ExchangeBrowserHandoff(ctx, loginHandoff.Code, "p15-t021-login-replay", now.Add(7*time.Second))

	registerCB, err := callback(ctx, fx, auth.ProviderFacebook, auth.OAuthIntentRegister, "p15-t021-subject", "attacker@example.test", true, "p15-t021-register-takeover", now.Add(8*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, registerConflict := fx.service.CreateBrowserHandoff(ctx, registerCB, "p15-t021-register-conflict", now.Add(9*time.Second))
	var identityRows, ownerSessions int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE provider=? AND provider_subject_hash=?`, auth.ProviderFacebook, hashProviderSubject(auth.ProviderFacebook, "p15-t021-subject")).Scan(&identityRows)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=?`, ownerID).Scan(&ownerSessions)
	checks := map[string]bool{
		"forged_browser_code_cannot_authenticate":              errors.Is(tamperedErr, auth.ErrUnauthorized),
		"bound_identity_login_resolves_only_to_original_owner": loginErr == nil && loginExchange.Session != nil && loginExchange.Session.Session.UserID == ownerID,
		"bound_identity_register_takeover_is_conflict":         errors.Is(registerConflict, auth.ErrConflict),
		"valid_handoff_is_one_time_after_owner_login":          errors.Is(replayErr, auth.ErrReplay),
		"provider_subject_has_single_owner":                    identityRows == 1,
	}
	return checks, map[string]int{"provider_identity_rows": identityRows, "owner_session_rows": ownerSessions}, nil
}
