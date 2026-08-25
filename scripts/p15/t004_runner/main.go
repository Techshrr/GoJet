package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/securetoken"
	_ "github.com/go-sql-driver/mysql"
)

type t004Result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func t004Fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	if dsn == "" || keyID == "" || keyHex == "" {
		t004Fail(fmt.Errorf("required P15-T004 integration configuration missing"))
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t004Fail(fmt.Errorf("grant key is not valid hex: %w", err))
	}
	grantKey, err := securetoken.NewKey(keyID, keyBytes)
	if err != nil {
		t004Fail(err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t004Fail(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t004Fail(err)
	}

	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		t004Fail(err)
	}
	registrationService, err := auth.NewRegistrationService(db, grantKey)
	if err != nil {
		t004Fail(err)
	}
	verificationService, err := auth.NewVerificationService(db)
	if err != nil {
		t004Fail(err)
	}
	passwordService, err := auth.NewPasswordService(db)
	if err != nil {
		t004Fail(err)
	}
	loginService, err := auth.NewPasswordLoginService(db, time.Hour)
	if err != nil {
		t004Fail(err)
	}
	store := auth.NewStore(db)

	var sessionsBeforeInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_sessions").Scan(&sessionsBeforeInvalid); err != nil {
		t004Fail(err)
	}
	_, invalidAccountErr := loginService.LoginPassword(ctx, auth.PasswordLoginInput{
		Email:         "missing-p15-t004@example.test",
		Password:      "P15-T004 Invalid Account Password!",
		CorrelationID: "p15-t004-invalid-account",
	})
	var sessionsAfterInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_sessions").Scan(&sessionsAfterInvalid); err != nil {
		t004Fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	pendingPassword := "P15-T004 Pending Password!"
	pending, err := registrationService.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t004-pending-%d@example.test", stamp),
		DisplayName:   "P15 T004 Pending",
		CorrelationID: fmt.Sprintf("p15-t004-pending-register-%d", stamp),
	})
	if err != nil {
		t004Fail(err)
	}
	if err := passwordService.SetInitialPassword(ctx, pending.User.ID, pendingPassword, fmt.Sprintf("p15-t004-pending-password-%d", stamp)); err != nil {
		t004Fail(err)
	}
	_, pendingErr := loginService.LoginPassword(ctx, auth.PasswordLoginInput{
		Email:         pending.User.Email,
		Password:      pendingPassword,
		CorrelationID: fmt.Sprintf("p15-t004-pending-login-%d", stamp),
	})
	pendingSessions := countSessions(ctx, db, pending.User.ID)

	lockedPassword := "P15-T004 Locked Password!"
	locked, err := registrationService.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t004-locked-%d@example.test", stamp),
		DisplayName:   "P15 T004 Locked",
		CorrelationID: fmt.Sprintf("p15-t004-locked-register-%d", stamp),
	})
	if err != nil {
		t004Fail(err)
	}
	if err := passwordService.SetInitialPassword(ctx, locked.User.ID, lockedPassword, fmt.Sprintf("p15-t004-locked-password-%d", stamp)); err != nil {
		t004Fail(err)
	}
	if _, err := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          locked.VerificationCode,
		CorrelationID: fmt.Sprintf("p15-t004-locked-verify-%d", stamp),
	}); err != nil {
		t004Fail(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_users SET status='locked',updated_at=? WHERE id=?`, time.Now().UTC().Truncate(time.Microsecond), locked.User.ID); err != nil {
		t004Fail(err)
	}
	_, lockedErr := loginService.LoginPassword(ctx, auth.PasswordLoginInput{
		Email:         locked.User.Email,
		Password:      lockedPassword,
		CorrelationID: fmt.Sprintf("p15-t004-locked-login-%d", stamp),
	})
	lockedSessions := countSessions(ctx, db, locked.User.ID)

	activePassword := "P15-T004 Active Password!"
	active, err := registrationService.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t004-active-%d@example.test", stamp),
		DisplayName:   "P15 T004 Active",
		CorrelationID: fmt.Sprintf("p15-t004-active-register-%d", stamp),
	})
	if err != nil {
		t004Fail(err)
	}
	if err := passwordService.SetInitialPassword(ctx, active.User.ID, activePassword, fmt.Sprintf("p15-t004-active-password-%d", stamp)); err != nil {
		t004Fail(err)
	}
	if _, err := verificationService.VerifyEmail(ctx, auth.EmailVerificationInput{
		Code:          active.VerificationCode,
		CorrelationID: fmt.Sprintf("p15-t004-active-verify-%d", stamp),
	}); err != nil {
		t004Fail(err)
	}

	_, wrongPasswordErr := loginService.LoginPassword(ctx, auth.PasswordLoginInput{
		Email:         active.User.Email,
		Password:      "P15-T004 Wrong Password!",
		CorrelationID: fmt.Sprintf("p15-t004-active-wrong-%d", stamp),
	})
	failedAfterWrong := credentialFailedAttempts(ctx, db, active.User.ID)
	sessionsAfterWrong := countSessions(ctx, db, active.User.ID)

	successCorrelation := fmt.Sprintf("p15-t004-active-success-%d", stamp)
	login, err := loginService.LoginPassword(ctx, auth.PasswordLoginInput{
		Email:         strings.ToUpper(active.User.Email),
		Password:      activePassword,
		CorrelationID: successCorrelation,
	})
	if err != nil {
		t004Fail(err)
	}
	failedAfterSuccess := credentialFailedAttempts(ctx, db, active.User.ID)
	activeSessions := countSessions(ctx, db, active.User.ID)
	resolvedSession, err := store.GetSessionByToken(ctx, login.Token, time.Now().UTC())
	if err != nil {
		t004Fail(err)
	}
	_, forgedSessionErr := store.GetSessionByToken(ctx, "gst_not-a-real-session-authority", time.Now().UTC())

	var storedTokenHash, storedCSRFHash []byte
	var storedSessionUser, storedSessionStatus, storedCorrelation string
	if err := db.QueryRowContext(ctx, `
SELECT user_id,status,token_hash,csrf_secret_hash,correlation_id
FROM auth_sessions WHERE id=?`, login.Session.ID).
		Scan(&storedSessionUser, &storedSessionStatus, &storedTokenHash, &storedCSRFHash, &storedCorrelation); err != nil {
		t004Fail(err)
	}
	tokenHash := auth.HashOpaque(login.Token)
	csrfHash := auth.HashOpaque(login.CSRFToken)
	var storedToken [32]byte
	var storedCSRF [32]byte
	copy(storedToken[:], storedTokenHash)
	copy(storedCSRF[:], storedCSRFHash)

	var passwordHash, passwordAlgorithm string
	var passwordVersion uint64
	if err := db.QueryRowContext(ctx, `
SELECT password_hash,password_algorithm,password_version FROM auth_credentials WHERE user_id=?`, active.User.ID).
		Scan(&passwordHash, &passwordAlgorithm, &passwordVersion); err != nil {
		t004Fail(err)
	}
	var successAuditCount int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.login.password' AND resource_type='auth_user'
  AND resource_id=? AND request_correlation_id=? AND result='success'`, active.User.ID, active.User.ID, successCorrelation).
		Scan(&successAuditCount); err != nil {
		t004Fail(err)
	}

	inputType := reflect.TypeOf(auth.PasswordLoginInput{})
	_, hasUserID := inputType.FieldByName("UserID")
	_, hasActorID := inputType.FieldByName("ActorID")

	counts := map[string]int{
		"invalid_account_sessions":      sessionsAfterInvalid - sessionsBeforeInvalid,
		"pending_account_sessions":      pendingSessions,
		"locked_account_sessions":       lockedSessions,
		"active_sessions_after_wrong":   sessionsAfterWrong,
		"active_sessions_after_success": activeSessions,
		"success_login_audits":          successAuditCount,
	}
	checks := map[string]bool{
		"unknown_account_is_generic_unauthorized":     errors.Is(invalidAccountErr, auth.ErrUnauthorized) && sessionsAfterInvalid == sessionsBeforeInvalid,
		"pending_account_requires_verification":       errors.Is(pendingErr, auth.ErrVerificationRequired) && pendingSessions == 0,
		"locked_account_fails_without_session":        errors.Is(lockedErr, auth.ErrLocked) && lockedSessions == 0,
		"wrong_password_fails_without_session":        errors.Is(wrongPasswordErr, auth.ErrUnauthorized) && sessionsAfterWrong == 0 && failedAfterWrong == 1,
		"successful_login_resets_failure_state":       failedAfterSuccess == 0,
		"successful_login_establishes_server_session": activeSessions == 1 && login.Session.UserID == active.User.ID && resolvedSession.ID == login.Session.ID && resolvedSession.UserID == active.User.ID,
		"client_identity_is_not_login_input":          !hasUserID && !hasActorID && inputType.NumField() == 3,
		"session_identity_is_server_resolved":         storedSessionUser == active.User.ID && storedSessionUser == login.Session.UserID && storedSessionStatus == auth.SessionStatusActive && storedCorrelation == successCorrelation,
		"session_secrets_are_hash_only_at_rest":       len(storedTokenHash) == 32 && len(storedCSRFHash) == 32 && auth.EqualOpaqueHash(tokenHash, storedToken) && auth.EqualOpaqueHash(csrfHash, storedCSRF),
		"forged_session_token_is_rejected":            errors.Is(forgedSessionErr, auth.ErrUnauthorized),
		"password_is_kdf_hash_only_at_rest":           passwordAlgorithm == "pbkdf2-sha256" && passwordVersion == 1 && strings.HasPrefix(passwordHash, "pbkdf2-sha256$") && !strings.Contains(passwordHash, activePassword),
		"successful_login_is_audited":                 successAuditCount == 1,
	}

	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := t004Result{Case: "P15-T004", Status: status, MySQLVersion: mysqlVersion, RecordCounts: counts, Checks: checks}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		t004Fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func countSessions(ctx context.Context, db *sql.DB, userID string) int {
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=?`, userID).Scan(&count); err != nil {
		t004Fail(err)
	}
	return count
}

func credentialFailedAttempts(ctx context.Context, db *sql.DB, userID string) uint32 {
	var count uint32
	if err := db.QueryRowContext(ctx, `SELECT failed_attempts FROM auth_credentials WHERE user_id=?`, userID).Scan(&count); err != nil {
		t004Fail(err)
	}
	return count
}
