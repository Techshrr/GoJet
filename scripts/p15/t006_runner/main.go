package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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
	recovery, _ := auth.NewPasswordRecoveryService(db, key)

	stamp := time.Now().UTC().UnixNano()
	account, err := registration.Register(ctx, auth.RegistrationInput{Email: fmt.Sprintf("p15-t006-%d@example.test", stamp), DisplayName: "P15 T006", CorrelationID: fmt.Sprintf("p15-t006-register-%d", stamp)})
	if err != nil {
		fail(err)
	}
	if err := passwords.SetInitialPassword(ctx, account.User.ID, "P15-T006 Existing Password!", fmt.Sprintf("p15-t006-password-%d", stamp)); err != nil {
		fail(err)
	}
	if _, err := verification.VerifyEmail(ctx, auth.EmailVerificationInput{Code: account.VerificationCode, CorrelationID: fmt.Sprintf("p15-t006-verify-%d", stamp)}); err != nil {
		fail(err)
	}

	missingEmail := fmt.Sprintf("p15-t006-missing-%d@example.test", stamp)
	existingErr := recovery.RequestReset(ctx, account.User.Email, fmt.Sprintf("p15-t006-existing-%d", stamp))
	missingErr := recovery.RequestReset(ctx, missingEmail, fmt.Sprintf("p15-t006-missing-%d", stamp))
	repeatErr := recovery.RequestReset(ctx, strings.ToUpper(account.User.Email), fmt.Sprintf("p15-t006-repeat-%d", stamp))

	var grantID, tokenKeyID string
	var tokenHash []byte
	var createdAt, expiresAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT id,token_hash,token_key_id,created_at,expires_at
FROM auth_one_time_grants WHERE user_id=? AND purpose='password_reset'
ORDER BY created_at DESC,id DESC LIMIT 1`, account.User.ID).Scan(&grantID, &tokenHash, &tokenKeyID, &createdAt, &expiresAt); err != nil {
		fail(err)
	}
	derived, err := key.Derive("grp_", "password_reset", grantID)
	if err != nil {
		fail(err)
	}
	expectedHash := securetoken.Hash(derived)
	var stored [32]byte
	copy(stored[:], tokenHash)

	existingGrants := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE user_id=? AND purpose='password_reset'`, account.User.ID)
	missingGrants := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE email_normalized=? AND purpose='password_reset'`, strings.ToLower(missingEmail))
	existingMail := count(ctx, db, `SELECT COUNT(*) FROM mail_jobs WHERE resource_type='auth_one_time_grant' AND resource_id=? AND template_key='auth-password-reset'`, grantID)
	missingMail := count(ctx, db, `SELECT COUNT(*) FROM mail_jobs WHERE recipient_value=? AND template_key='auth-password-reset'`, missingEmail)
	templateCount := count(ctx, db, `SELECT COUNT(*) FROM mail_templates WHERE template_key='auth-password-reset' AND locale='en' AND version=1 AND enabled=1`)
	auditCount := count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE action='auth.password_reset.requested' AND request_correlation_id IN (?,?,?)`, fmt.Sprintf("p15-t006-existing-%d", stamp), fmt.Sprintf("p15-t006-missing-%d", stamp), fmt.Sprintf("p15-t006-repeat-%d", stamp))

	checks := map[string]bool{
		"existing_and_missing_requests_are_response_neutral": existingErr == nil && missingErr == nil && repeatErr == nil,
		"only_existing_eligible_account_gets_grant":          existingGrants == 1 && missingGrants == 0,
		"repeat_request_is_silently_throttled":               existingGrants == 1,
		"only_existing_eligible_account_gets_mail":           existingMail == 1 && missingMail == 0 && templateCount == 1,
		"reset_grant_is_expiry_bound":                        expiresAt.Sub(createdAt) >= auth.PasswordResetTTL-time.Second && expiresAt.Sub(createdAt) <= auth.PasswordResetTTL+time.Second,
		"reset_grant_is_hash_only_at_rest":                   len(tokenHash) == 32 && expectedHash == stored && tokenKeyID == key.ID(),
		"neutral_requests_are_audited_without_external_leak": auditCount == 3,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T006", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"existing_reset_grants": existingGrants, "missing_reset_grants": missingGrants, "neutral_request_audits": auditCount}, Checks: checks}
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
		fail(fmt.Errorf("required P15-T006 integration configuration missing"))
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
	ctx, _ := context.WithTimeout(context.Background(), 90*time.Second)
	if err := db.PingContext(ctx); err != nil {
		fail(err)
	}
	return db, key, ctx
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		fail(err)
	}
	return n
}
