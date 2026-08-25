package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/securetoken"
	_ "github.com/go-sql-driver/mysql"
)

type result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func main() {
	db, key, ctx := setup()
	defer db.Close()
	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}
	registration, _ := auth.NewRegistrationService(db, key)
	verification, _ := auth.NewVerificationService(db)
	passwords, _ := auth.NewPasswordService(db)
	login, _ := auth.NewPasswordLoginService(db, time.Hour)
	recovery, _ := auth.NewPasswordRecoveryService(db, key)
	store := auth.NewStore(db)

	stamp := time.Now().UTC().UnixNano()
	oldPassword := "P15-T007 Old Password!"
	newPassword := "P15-T007 New Password!"
	account, err := registration.Register(ctx, auth.RegistrationInput{Email: fmt.Sprintf("p15-t007-%d@example.test", stamp), DisplayName: "P15 T007", CorrelationID: fmt.Sprintf("p15-t007-register-%d", stamp)})
	if err != nil {
		fail(err)
	}
	if err := passwords.SetInitialPassword(ctx, account.User.ID, oldPassword, fmt.Sprintf("p15-t007-initial-%d", stamp)); err != nil {
		fail(err)
	}
	if _, err := verification.VerifyEmail(ctx, auth.EmailVerificationInput{Code: account.VerificationCode, CorrelationID: fmt.Sprintf("p15-t007-verify-%d", stamp)}); err != nil {
		fail(err)
	}
	oldSession, err := login.LoginPassword(ctx, auth.PasswordLoginInput{Email: account.User.Email, Password: oldPassword, CorrelationID: fmt.Sprintf("p15-t007-old-login-%d", stamp)})
	if err != nil {
		fail(err)
	}

	if err := recovery.RequestReset(ctx, account.User.Email, fmt.Sprintf("p15-t007-request-%d", stamp)); err != nil {
		fail(err)
	}
	grantID := latestGrant(ctx, db, account.User.ID, "")
	resetToken, err := key.Derive("grp_", "password_reset", grantID)
	if err != nil {
		fail(err)
	}
	if err := recovery.ResetPassword(ctx, resetToken, newPassword, fmt.Sprintf("p15-t007-reset-%d", stamp)); err != nil {
		fail(err)
	}

	var consumedAt sql.NullTime
	if err := db.QueryRowContext(ctx, `SELECT consumed_at FROM auth_one_time_grants WHERE id=?`, grantID).Scan(&consumedAt); err != nil {
		fail(err)
	}
	oldSessionErr := func() error { _, err := store.GetSessionByToken(ctx, oldSession.Token, time.Now().UTC()); return err }()
	replayErr := recovery.ResetPassword(ctx, resetToken, "P15-T007 Replay Password!", fmt.Sprintf("p15-t007-replay-%d", stamp))
	oldPasswordErr := func() error {
		_, err := login.LoginPassword(ctx, auth.PasswordLoginInput{Email: account.User.Email, Password: oldPassword, CorrelationID: fmt.Sprintf("p15-t007-old-password-%d", stamp)})
		return err
	}()
	newSession, newPasswordErr := login.LoginPassword(ctx, auth.PasswordLoginInput{Email: account.User.Email, Password: newPassword, CorrelationID: fmt.Sprintf("p15-t007-new-password-%d", stamp)})

	var credentialBeforeExpired string
	if err := db.QueryRowContext(ctx, `SELECT password_hash FROM auth_credentials WHERE user_id=?`, account.User.ID).Scan(&credentialBeforeExpired); err != nil {
		fail(err)
	}
	if err := recovery.RequestReset(ctx, account.User.Email, fmt.Sprintf("p15-t007-expired-request-%d", stamp)); err != nil {
		fail(err)
	}
	expiredGrantID := latestGrant(ctx, db, account.User.ID, grantID)
	expiredToken, err := key.Derive("grp_", "password_reset", expiredGrantID)
	if err != nil {
		fail(err)
	}
	createdPast := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	expiresPast := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `UPDATE auth_one_time_grants SET created_at=?,expires_at=? WHERE id=?`, createdPast, expiresPast, expiredGrantID); err != nil {
		fail(err)
	}
	expiredErr := recovery.ResetPassword(ctx, expiredToken, "P15-T007 Must Not Apply!", fmt.Sprintf("p15-t007-expired-consume-%d", stamp))
	var credentialAfterExpired string
	if err := db.QueryRowContext(ctx, `SELECT password_hash FROM auth_credentials WHERE user_id=?`, account.User.ID).Scan(&credentialAfterExpired); err != nil {
		fail(err)
	}
	invalidatedExpired := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE id=? AND invalidated_at IS NOT NULL AND consumed_at IS NULL`, expiredGrantID)
	oldRevoked := count(ctx, db, `SELECT COUNT(*) FROM auth_sessions WHERE id=? AND status='revoked' AND revoked_at IS NOT NULL`, oldSession.Session.ID)
	activeSessions := count(ctx, db, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=? AND status='active'`, account.User.ID)
	resetAudit := count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action='auth.password.reset' AND resource_id=? AND result='success'`, account.User.ID, grantID)

	checks := map[string]bool{
		"valid_reset_consumes_grant_once":             consumedAt.Valid,
		"reset_revokes_existing_sessions_server_side": errors.Is(oldSessionErr, auth.ErrRevoked) && oldRevoked == 1,
		"reset_token_reuse_fails_closed":              errors.Is(replayErr, auth.ErrReplay),
		"old_password_is_invalid_after_reset":         errors.Is(oldPasswordErr, auth.ErrUnauthorized),
		"new_password_establishes_fresh_session":      newPasswordErr == nil && newSession.Session.UserID == account.User.ID && activeSessions == 1,
		"expired_reset_fails_closed":                  errors.Is(expiredErr, auth.ErrExpired) && invalidatedExpired == 1,
		"expired_reset_does_not_change_password":      credentialBeforeExpired == credentialAfterExpired,
		"successful_reset_is_audited":                 resetAudit == 1,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T007", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"revoked_pre_reset_sessions": oldRevoked, "active_post_reset_sessions": activeSessions, "successful_reset_audits": resetAudit}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func setup() (*sql.DB, securetoken.Key, context.Context) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	if dsn == "" || keyHex == "" || keyID == "" {
		fail(fmt.Errorf("required P15-T007 integration configuration missing"))
	}
	secret, err := hex.DecodeString(keyHex)
	if err != nil {
		fail(err)
	}
	key, err := securetoken.NewKey(keyID, secret)
	if err != nil {
		fail(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail(err)
	}
	ctx, _ := context.WithTimeout(context.Background(), 120*time.Second)
	if err := db.PingContext(ctx); err != nil {
		fail(err)
	}
	return db, key, ctx
}
func latestGrant(ctx context.Context, db *sql.DB, userID, excludeID string) string {
	query := `SELECT id FROM auth_one_time_grants WHERE user_id=? AND purpose='password_reset'`
	args := []any{userID}
	if excludeID != "" {
		query += ` AND id<>?`
		args = append(args, excludeID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT 1`
	var id string
	if err := db.QueryRowContext(ctx, query, args...).Scan(&id); err != nil {
		fail(err)
	}
	return id
}
func count(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		fail(err)
	}
	return n
}
