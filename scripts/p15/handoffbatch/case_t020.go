package handoffbatch

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

func runT020(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	fx, cleanup, err := newOAuthFixture(ctx, db, auth.ProviderGoogle, "p15-t020", now)
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
	code, err := socialCode(ctx, fx, auth.ProviderGoogle, "p15-t020-subject", "", false, "p15-t020-good", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	state, err := social.GetState(ctx, code, now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	grant, err := social.RequestEmailVerification(ctx, code, "p15-t020-user@example.test", "p15-t020-verify-issue", now.Add(3*time.Second))
	if err != nil {
		return nil, nil, err
	}
	verificationCode, err := key.Derive("gsv_", "social_email_verification", grant.ID)
	if err != nil {
		return nil, nil, err
	}
	session, completeErr := social.Complete(ctx, auth.CompleteSocialRegistrationInput{SocialCode: code, VerificationCode: verificationCode, CorrelationID: "p15-t020-complete"}, now.Add(4*time.Second))
	_, replayErr := social.Complete(ctx, auth.CompleteSocialRegistrationInput{SocialCode: code, VerificationCode: verificationCode, CorrelationID: "p15-t020-replay"}, now.Add(5*time.Second))

	_, err = runnerutil.ActivateUser(ctx, db, "p15-t020-conflict@example.test", "P15 T020 Conflict", now)
	if err != nil {
		return nil, nil, err
	}
	conflictCode, err := socialCode(ctx, fx, auth.ProviderGoogle, "p15-t020-conflict-subject", "", false, "p15-t020-conflict", now.Add(6*time.Second))
	if err != nil {
		return nil, nil, err
	}
	_, conflictErr := social.RequestEmailVerification(ctx, conflictCode, "p15-t020-conflict@example.test", "p15-t020-conflict-email", now.Add(7*time.Second))

	expiredCode, err := socialCode(ctx, fx, auth.ProviderGoogle, "p15-t020-expired-subject", "", false, "p15-t020-expired", now.Add(8*time.Second))
	if err != nil {
		return nil, nil, err
	}
	expiredHash := auth.HashOpaque(expiredCode)
	if _, err := db.ExecContext(ctx, `UPDATE oauth_handoffs SET created_at=?,expires_at=? WHERE handoff_kind='social_registration' AND code_hash=?`, now.Add(-2*time.Minute), now.Add(-time.Minute), expiredHash[:]); err != nil {
		return nil, nil, err
	}
	_, expiredErr := social.GetState(ctx, expiredCode, now.Add(9*time.Second))

	var identityRows, consumedGrantRows int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_identities WHERE user_id=? AND provider=?`, session.Session.UserID, auth.ProviderGoogle).Scan(&identityRows)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_one_time_grants WHERE id=? AND consumed_at IS NOT NULL`, grant.ID).Scan(&consumedGrantRows)
	checks := map[string]bool{
		"missing_provider_email_requires_verification":          state.RequiresEmailVerification && state.Email == "",
		"social_email_grant_is_one_time_and_account_is_created": completeErr == nil && session.Session.UserID != "" && identityRows == 1 && consumedGrantRows == 1,
		"completed_social_handoff_replay_is_denied":             errors.Is(replayErr, auth.ErrReplay),
		"existing_account_email_conflict_is_denied":             errors.Is(conflictErr, auth.ErrConflict),
		"expired_social_handoff_fails_closed":                   errors.Is(expiredErr, auth.ErrExpired),
	}
	return checks, map[string]int{"oauth_identities": identityRows, "consumed_social_email_grants": consumedGrantRows}, nil
}
