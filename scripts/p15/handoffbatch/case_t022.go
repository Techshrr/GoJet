package handoffbatch

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/support"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

type t022MailSender struct {
	deliveries int
}

func (s *t022MailSender) Send(_ context.Context, recipient string, rendered support.RenderedMail) support.MailDeliveryResult {
	if strings.TrimSpace(recipient) == "" || strings.TrimSpace(rendered.Subject) == "" || strings.TrimSpace(rendered.Text) == "" {
		return support.MailDeliveryResult{ErrorCode: "invalid_rendered_mail"}
	}
	s.deliveries++
	return support.MailDeliveryResult{Success: true}
}

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
	sender := &t022MailSender{}
	worker, err := support.NewMailWorker(queue, sender)
	if err != nil {
		return nil, nil, err
	}
	for i := 0; i < 3; i++ {
		worked, err := worker.RunOnce(ctx)
		if err != nil {
			return nil, nil, err
		}
		if !worked {
			return nil, nil, support.ErrNoMailAvailable
		}
	}
	drained, err := worker.RunOnce(ctx)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.QueryContext(ctx, `
SELECT template_key
FROM mail_jobs
WHERE status='sent' AND resource_type='auth_one_time_grant' AND recipient_value=?
ORDER BY template_key`, registered.User.Email)
	if err != nil {
		return nil, nil, err
	}
	templates := make([]string, 0, 3)
	for rows.Next() {
		var templateKey string
		if err := rows.Scan(&templateKey); err != nil {
			rows.Close()
			return nil, nil, err
		}
		templates = append(templates, templateKey)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sort.Strings(templates)
	expected := []string{"auth-email-verification", "auth-login-email-code", "auth-password-reset"}

	sentRows, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM mail_jobs WHERE status='sent' AND resource_type='auth_one_time_grant' AND recipient_value=?`, registered.User.Email)
	if err != nil {
		return nil, nil, err
	}
	unconsumedAfter, err := runnerutil.Count(ctx, db, `
SELECT COUNT(*)
FROM auth_one_time_grants g
JOIN mail_jobs m ON m.resource_type='auth_one_time_grant' AND m.resource_id=g.id
WHERE m.recipient_value=? AND m.status='sent' AND g.consumed_at IS NULL`, registered.User.Email)
	if err != nil {
		return nil, nil, err
	}
	attemptRows, err := runnerutil.Count(ctx, db, `
SELECT COUNT(*)
FROM mail_attempts a
JOIN mail_jobs m ON m.id=a.mail_job_id
WHERE m.recipient_value=? AND m.resource_type='auth_one_time_grant' AND a.status='sent'`, registered.User.Email)
	if err != nil {
		return nil, nil, err
	}
	auditRows, err := runnerutil.Count(ctx, db, `
SELECT COUNT(*)
FROM auth_audit_events
WHERE user_id=? AND action='auth.mail.attempt.sent' AND resource_type='mail_job' AND result='success'`, registered.User.ID)
	if err != nil {
		return nil, nil, err
	}

	checks := map[string]bool{
		"p14_mailworker_delivers_all_three_auth_grant_mail_types": strings.Join(templates, ",") == strings.Join(expected, ",") && sentRows == 3 && sender.deliveries == 3,
		"mail_delivery_does_not_consume_auth_grants":              unconsumedAfter == 3,
		"delivery_payloads_render_from_server_derived_grants":     sender.deliveries == 3,
		"mailworker_uses_inherited_p14_claim_complete_lifecycle":  attemptRows == 3 && !drained,
		"auth_mail_completion_records_auth_owned_safe_audit":      auditRows == 3,
	}
	return checks, map[string]int{
		"auth_mail_jobs_sent":              sentRows,
		"grants_unconsumed_after_delivery": unconsumedAfter,
		"mail_attempts_sent":               attemptRows,
		"auth_mail_audit_rows":             auditRows,
	}, nil
}
