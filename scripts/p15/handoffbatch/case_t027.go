package handoffbatch

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

func runT027(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	key, err := runnerutil.GrantKey()
	if err != nil {
		return nil, nil, err
	}
	reg, err := auth.NewRegistrationService(db, key)
	if err != nil {
		return nil, nil, err
	}
	registered, err := reg.Register(ctx, auth.RegistrationInput{Email: "p15-t027-user@example.test", DisplayName: "P15 T027", CorrelationID: "p15-t027-register"})
	if err != nil {
		return nil, nil, err
	}
	fx, cleanup, err := newOAuthFixture(ctx, db, auth.ProviderQQ, "p15-t027", now)
	if err != nil {
		return nil, nil, err
	}
	defer cleanup()
	cb, err := callback(ctx, fx, auth.ProviderQQ, auth.OAuthIntentRegister, "p15-t027-subject", "", false, "p15-t027-oauth", now.Add(time.Second))
	if err != nil {
		return nil, nil, err
	}
	handoff, err := fx.service.CreateBrowserHandoff(ctx, cb, "p15-t027-handoff", now.Add(2*time.Second))
	if err != nil {
		return nil, nil, err
	}
	exchanged, err := fx.service.ExchangeBrowserHandoff(ctx, handoff.Code, "p15-t027-exchange", now.Add(3*time.Second))
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.QueryContext(ctx, `SELECT action,request_correlation_id,CAST(metadata_json AS CHAR),actor_id,resource_id FROM auth_audit_events WHERE request_correlation_id LIKE 'p15-t027-%' ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var auditCount, correlated int
	var combined strings.Builder
	for rows.Next() {
		var action, correlation, metadata, actorID, resourceID string
		if err := rows.Scan(&action, &correlation, &metadata, &actorID, &resourceID); err != nil {
			return nil, nil, err
		}
		auditCount++
		if correlation != "" && strings.HasPrefix(correlation, "p15-t027-") {
			correlated++
		}
		combined.WriteString(action)
		combined.WriteString("|")
		combined.WriteString(metadata)
		combined.WriteString("|")
		combined.WriteString(actorID)
		combined.WriteString("|")
		combined.WriteString(resourceID)
		combined.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	text := combined.String()
	leakFree := true
	for _, fragment := range []string{
		registered.VerificationCode,
		"p15-t027-user@example.test",
		cb.ProviderSubject,
		fx.secret,
		handoff.Code,
		exchanged.SocialRegistrationCode,
		"p15-t027-oauth-provider-code",
	} {
		if fragment != "" && strings.Contains(text, fragment) {
			leakFree = false
		}
	}
	actionsPresent := strings.Contains(text, "auth.registration.created") && strings.Contains(text, "auth.oauth.callback") && strings.Contains(text, "auth.oauth.handoff.created") && strings.Contains(text, "auth.oauth.handoff.social")
	checks := map[string]bool{
		"security_sensitive_mutations_are_correlated":                           auditCount >= 4 && correlated == auditCount,
		"expected_auth_oauth_actions_are_auditable":                             actionsPresent,
		"audit_rows_exclude_raw_codes_tokens_provider_secret_subject_and_email": leakFree,
		"audit_metadata_remains_structured_and_minimized":                       !strings.Contains(strings.ToLower(text), "password") && !strings.Contains(strings.ToLower(text), "client_secret") && !strings.Contains(strings.ToLower(text), "provider_subject"),
	}
	return checks, map[string]int{"correlated_audit_rows": correlated, "audit_rows": auditCount}, nil
}
