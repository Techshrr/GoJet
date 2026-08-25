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

type t003Result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func t003Fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	if dsn == "" || keyID == "" || keyHex == "" {
		t003Fail(fmt.Errorf("required P15-T003 integration configuration missing"))
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t003Fail(fmt.Errorf("grant key is not valid hex: %w", err))
	}
	grantKey, err := securetoken.NewKey(keyID, keyBytes)
	if err != nil {
		t003Fail(err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t003Fail(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t003Fail(err)
	}

	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		t003Fail(err)
	}
	registrationService, err := auth.NewRegistrationService(db, grantKey)
	if err != nil {
		t003Fail(err)
	}
	verificationService, err := auth.NewVerificationService(db)
	if err != nil {
		t003Fail(err)
	}

	var usersBeforeInvalid, grantsBeforeInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&usersBeforeInvalid); err != nil {
		t003Fail(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_one_time_grants").Scan(&grantsBeforeInvalid); err != nil {
		t003Fail(err)
	}
	_, invalidErr := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          "gvc_" + strings.Repeat("A", 43),
		CorrelationID: "p15-t003-invalid",
	})
	var usersAfterInvalid, grantsAfterInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&usersAfterInvalid); err != nil {
		t003Fail(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_one_time_grants").Scan(&grantsAfterInvalid); err != nil {
		t003Fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	expiredRegistration, err := registrationService.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t003-expired-%d@example.test", stamp),
		DisplayName:   "P15 T003 Expired",
		CorrelationID: fmt.Sprintf("p15-t003-expired-register-%d", stamp),
	})
	if err != nil {
		t003Fail(err)
	}
	fixtureNow := time.Now().UTC().Truncate(time.Microsecond)
	fixtureCreated := fixtureNow.Add(-30 * time.Minute)
	fixtureExpires := fixtureNow.Add(-15 * time.Minute)
	if _, err := db.ExecContext(ctx, `
UPDATE auth_one_time_grants SET created_at=?,expires_at=? WHERE id=?`,
		fixtureCreated, fixtureExpires, expiredRegistration.Grant.ID); err != nil {
		t003Fail(err)
	}
	_, expiredErr := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          expiredRegistration.VerificationCode,
		CorrelationID: fmt.Sprintf("p15-t003-expired-verify-%d", stamp),
	})
	var expiredConsumed, expiredInvalidated sql.NullTime
	var expiredAttemptCount uint32
	if err := db.QueryRowContext(ctx, `
SELECT consumed_at,invalidated_at,attempt_count FROM auth_one_time_grants WHERE id=?`, expiredRegistration.Grant.ID).
		Scan(&expiredConsumed, &expiredInvalidated, &expiredAttemptCount); err != nil {
		t003Fail(err)
	}
	expiredUser, err := auth.NewStore(db).GetUserByID(ctx, expiredRegistration.User.ID)
	if err != nil {
		t003Fail(err)
	}
	var expiredDeniedAudit int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.email_verification.denied' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND result='denied'`, expiredRegistration.User.ID, expiredRegistration.Grant.ID).
		Scan(&expiredDeniedAudit); err != nil {
		t003Fail(err)
	}

	validStamp := time.Now().UTC().UnixNano()
	validCorrelation := fmt.Sprintf("p15-t003-valid-verify-%d", validStamp)
	validRegistration, err := registrationService.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t003-valid-%d@example.test", validStamp),
		DisplayName:   "P15 T003 Valid",
		CorrelationID: fmt.Sprintf("p15-t003-valid-register-%d", validStamp),
	})
	if err != nil {
		t003Fail(err)
	}
	verified, err := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          validRegistration.VerificationCode,
		CorrelationID: validCorrelation,
	})
	if err != nil {
		t003Fail(err)
	}
	var validConsumed, validInvalidated sql.NullTime
	var validAttemptCount uint32
	if err := db.QueryRowContext(ctx, `
SELECT consumed_at,invalidated_at,attempt_count FROM auth_one_time_grants WHERE id=?`, validRegistration.Grant.ID).
		Scan(&validConsumed, &validInvalidated, &validAttemptCount); err != nil {
		t003Fail(err)
	}
	validUserAfter, err := auth.NewStore(db).GetUserByID(ctx, validRegistration.User.ID)
	if err != nil {
		t003Fail(err)
	}
	var successAuditCount int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.email_verification.completed' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND request_correlation_id=? AND result='success'`,
		validRegistration.User.ID, validRegistration.Grant.ID, validCorrelation).Scan(&successAuditCount); err != nil {
		t003Fail(err)
	}

	_, replayErr := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          validRegistration.VerificationCode,
		CorrelationID: fmt.Sprintf("p15-t003-replay-%d", validStamp),
	})
	var replayConsumed, replayInvalidated sql.NullTime
	var replayAttemptCount uint32
	if err := db.QueryRowContext(ctx, `
SELECT consumed_at,invalidated_at,attempt_count FROM auth_one_time_grants WHERE id=?`, validRegistration.Grant.ID).
		Scan(&replayConsumed, &replayInvalidated, &replayAttemptCount); err != nil {
		t003Fail(err)
	}
	validUserAfterReplay, err := auth.NewStore(db).GetUserByID(ctx, validRegistration.User.ID)
	if err != nil {
		t003Fail(err)
	}
	var successAuditAfterReplay int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.email_verification.completed' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND result='success'`, validRegistration.User.ID, validRegistration.Grant.ID).
		Scan(&successAuditAfterReplay); err != nil {
		t003Fail(err)
	}

	counts := map[string]int{
		"expired_denied_audits":      expiredDeniedAudit,
		"valid_success_audits":       successAuditCount,
		"valid_success_after_replay": successAuditAfterReplay,
	}
	checks := map[string]bool{
		"invalid_code_fails_closed_without_mutation": errors.Is(invalidErr, auth.ErrInvalid) && usersBeforeInvalid == usersAfterInvalid && grantsBeforeInvalid == grantsAfterInvalid,
		"expired_grant_is_rejected_and_invalidated":  errors.Is(expiredErr, auth.ErrExpired) && !expiredConsumed.Valid && expiredInvalidated.Valid && expiredAttemptCount == 1 && expiredDeniedAudit == 1,
		"expired_grant_does_not_activate_user":       expiredUser.Status == auth.UserStatusPendingVerification && expiredUser.EmailVerifiedAt == nil && expiredUser.Version == 1,
		"valid_verification_activates_once":          verified.User.ID == validRegistration.User.ID && validUserAfter.Status == auth.UserStatusActive && validUserAfter.EmailVerifiedAt != nil && validUserAfter.Version == 2,
		"valid_grant_is_consumed_once":               validConsumed.Valid && !validInvalidated.Valid && validAttemptCount == 1 && verified.GrantID == validRegistration.Grant.ID,
		"verification_success_is_audited":            successAuditCount == 1,
		"reused_grant_returns_replay":                errors.Is(replayErr, auth.ErrReplay),
		"replay_preserves_terminal_state":            replayConsumed.Valid && validConsumed.Valid && replayConsumed.Time.Equal(validConsumed.Time) && !replayInvalidated.Valid && replayAttemptCount == validAttemptCount && validUserAfterReplay.Status == auth.UserStatusActive && validUserAfterReplay.Version == validUserAfter.Version && successAuditAfterReplay == 1,
	}

	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := t003Result{Case: "P15-T003", Status: status, MySQLVersion: mysqlVersion, RecordCounts: counts, Checks: checks}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		t003Fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}
