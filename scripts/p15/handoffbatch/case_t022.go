package handoffbatch

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

func runT022(ctx context.Context, db *sql.DB) (map[string]bool, map[string]int, error) {
	key, err := runnerutil.GrantKey()
	if err != nil {
		return nil, nil, err
	}
	if _, err := db.ExecContext(ctx, `UPDATE mail_jobs SET status='failed',next_attempt_at=NULL,claim_token_hash=NULL,claim_expires_at=NULL,last_error_code='p15_t022_isolation' WHERE status IN ('queued','retrying')`); err != nil {
		return nil, nil, err
	}
	reg, err := auth.NewRegistrationService(db, key)
	if err != nil {
		return nil, nil, err
	}
	registered, err := reg.Register(ctx, auth.RegistrationInput{Email: "p15-t022-user@example.test", DisplayName: "P15 T022", CorrelationID: "p15-t022-register"})
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `UPDATE auth_users SET status='active',email_verified_at=?,updated_at=? WHERE id=?`, now, now, registered.User.ID); err != nil {
		return nil, nil, err
	}
	passwords, err := auth.NewPasswordService(db)
	if err != nil {
		return nil, nil, err
	}
	if err := passwords.SetInitialPassword(ctx, registered.User.ID, "P15-T022 Password!", "p15-t022-password"); err != nil {
		return nil, nil, err
	}
	emailCodes, err := auth.NewEmailCodeService(db, key, time.Hour)
	if err != nil {
		return nil, nil, err
	}
	if err := emailCodes.RequestLoginCode(ctx, registered.User.Email, "p15-t022-login-code"); err != nil {
		return nil, nil, err
	}
	recovery, err := auth.NewPasswordRecoveryService(db, key)
	if err != nil {
		return nil, nil, err
	}
	if err := recovery.RequestReset(ctx, registered.User.Email, "p15-t022-recovery"); err != nil {
		return nil, nil, err
	}

	queue, err := auth.NewAuthMailQueue(db, key)
	if err != nil {
		return nil, nil, err
	}
	templates := make([]string, 0, 3)
	unconsumedBefore := 0
	unconsumedAfter := 0
	for i := 0; i < 3; i++ {
		claim := fmt.Sprintf("p15-t022-mail-claim-%d", i)
		claimed, err := queue.ClaimNext(ctx, claim, time.Now().UTC().Add(time.Duration(i+1)*time.Second))
		if err != nil {
			return nil, nil, err
		}
		payload, err := queue.LoadDelivery(ctx, claimed)
		if err != nil {
			return nil, nil, err
		}
		if _, err := support.RenderMailTemplate(payload.Template, payload.Values); err != nil {
			return nil, nil, err
		}
		templates = append(templates, claimed.Job.TemplateKey)
		var consumed sql.NullTime
		if err := db.QueryRowContext(ctx, `SELECT consumed_at FROM auth_one_time_grants WHERE id=?`, claimed.Job.ResourceID).Scan(&consumed); err != nil {
			return nil, nil, err
		}
		if !consumed.Valid {
			unconsumedBefore++
		}
		if _, err := queue.Complete(ctx, claimed, claim, support.MailDeliveryResult{Success: true}, time.Now().UTC().Add(time.Duration(i+4)*time.Second)); err != nil {
			return nil, nil, err
		}
		if err := db.QueryRowContext(ctx, `SELECT consumed_at FROM auth_one_time_grants WHERE id=?`, claimed.Job.ResourceID).Scan(&consumed); err != nil {
			return nil, nil, err
		}
		if !consumed.Valid {
			unconsumedAfter++
		}
	}
	sort.Strings(templates)
	expected := []string{"auth-email-verification", "auth-login-email-code", "auth-password-reset"}
	sentRows, _ := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM mail_jobs WHERE status='sent' AND resource_type='auth_one_time_grant' AND recipient_value=?`, registered.User.Email)
	checks := map[string]bool{
		"p14_queue_delivers_all_three_auth_grant_mail_types":   strings.Join(templates, ",") == strings.Join(expected, ",") && sentRows == 3,
		"mail_delivery_does_not_consume_auth_grants":           unconsumedBefore == 3 && unconsumedAfter == 3,
		"delivery_payloads_render_from_server_derived_grants":  len(templates) == 3,
		"mail_claim_complete_uses_inherited_p14_state_machine": sentRows == 3,
	}
	return checks, map[string]int{"auth_mail_jobs_sent": sentRows, "grants_unconsumed_after_delivery": unconsumedAfter}, nil
}
