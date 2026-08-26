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

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func main() {
	db, key, ctx := setup()
	defer db.Close()
	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}
	registration, _ := auth.NewRegistrationService(db, key)
	verification, _ := auth.NewVerificationService(db)
	emailCode, _ := auth.NewEmailCodeService(db, key, time.Hour)

	stamp := time.Now().UTC().UnixNano()
	account, err := registration.Register(ctx, auth.RegistrationInput{
		Email:         fmt.Sprintf("p15-t005-%d@example.test", stamp),
		DisplayName:   "P15 T005",
		CorrelationID: fmt.Sprintf("p15-t005-register-%d", stamp),
	})
	if err != nil {
		fail(err)
	}
	if _, err := verification.VerifyEmail(ctx, auth.EmailVerificationInput{Code: account.VerificationCode, CorrelationID: fmt.Sprintf("p15-t005-verify-%d", stamp)}); err != nil {
		fail(err)
	}

	firstCorrelation := fmt.Sprintf("p15-t005-issue-%d", stamp)
	if err := emailCode.RequestLoginCode(ctx, account.User.Email, firstCorrelation); err != nil {
		fail(err)
	}
	firstID, firstHash, firstKeyID, firstCreated, firstExpires := grantRow(ctx, db, account.User.ID, "")
	firstCode, err := key.Derive("glc_", "login_email_code", firstID)
	if err != nil {
		fail(err)
	}
	firstExpectedHash := securetoken.Hash(firstCode)
	var firstStoredHash [32]byte
	copy(firstStoredHash[:], firstHash)

	rateErr := emailCode.RequestLoginCode(ctx, account.User.Email, fmt.Sprintf("p15-t005-rate-%d", stamp))
	grantsAfterRate := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE user_id=? AND purpose='login_email_code'`, account.User.ID)
	mailForFirst := count(ctx, db, `SELECT COUNT(*) FROM mail_jobs WHERE resource_type='auth_one_time_grant' AND resource_id=? AND template_key='auth-login-email-code'`, firstID)
	templateCount := count(ctx, db, `SELECT COUNT(*) FROM mail_templates WHERE template_key='auth-login-email-code' AND locale='en' AND version=1 AND enabled=1`)

	session, err := emailCode.ConsumeLoginCode(ctx, firstCode, fmt.Sprintf("p15-t005-consume-%d", stamp))
	if err != nil {
		fail(err)
	}
	replayErr := func() error {
		_, err := emailCode.ConsumeLoginCode(ctx, firstCode, fmt.Sprintf("p15-t005-replay-%d", stamp))
		return err
	}()

	if err := emailCode.RequestLoginCode(ctx, account.User.Email, fmt.Sprintf("p15-t005-expired-issue-%d", stamp)); err != nil {
		fail(err)
	}
	secondID, _, _, _, _ := grantRow(ctx, db, account.User.ID, firstID)
	secondCode, err := key.Derive("glc_", "login_email_code", secondID)
	if err != nil {
		fail(err)
	}
	createdPast := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Microsecond)
	expiresPast := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `UPDATE auth_one_time_grants SET created_at=?,expires_at=? WHERE id=?`, createdPast, expiresPast, secondID); err != nil {
		fail(err)
	}
	expiredErr := func() error {
		_, err := emailCode.ConsumeLoginCode(ctx, secondCode, fmt.Sprintf("p15-t005-expired-consume-%d", stamp))
		return err
	}()
	invalidatedSecond := count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE id=? AND invalidated_at IS NOT NULL AND consumed_at IS NULL`, secondID)
	sessions := count(ctx, db, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=?`, account.User.ID)
	audits := count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action IN ('auth.login.email_code.issued','auth.login.email_code')`, account.User.ID)

	checks := map[string]bool{
		"login_email_code_is_expiry_bound":      firstExpires.Sub(firstCreated) >= auth.LoginEmailCodeTTL-time.Second && firstExpires.Sub(firstCreated) <= auth.LoginEmailCodeTTL+time.Second,
		"login_email_code_is_hash_only_at_rest": len(firstHash) == 32 && firstExpectedHash == firstStoredHash && firstKeyID == key.ID(),
		"p14_mail_authority_is_reused":          mailForFirst == 1 && templateCount == 1,
		"immediate_reissue_is_rate_limited":     errors.Is(rateErr, auth.ErrRateLimited) && grantsAfterRate == 1,
		"valid_code_establishes_one_session":    session.Session.UserID == account.User.ID && sessions == 1,
		"reused_code_fails_closed":              errors.Is(replayErr, auth.ErrReplay),
		"expired_code_fails_closed":             errors.Is(expiredErr, auth.ErrExpired) && invalidatedSecond == 1,
		"login_email_code_lifecycle_is_audited": audits >= 3,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T005", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{
		"login_email_code_grants":   count(ctx, db, `SELECT COUNT(*) FROM auth_one_time_grants WHERE user_id=? AND purpose='login_email_code'`, account.User.ID),
		"login_email_code_sessions": sessions,
		"login_email_code_audits":   audits,
	}, Checks: checks}
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
		fail(fmt.Errorf("required P15-T005 integration configuration missing"))
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

func grantRow(ctx context.Context, db *sql.DB, userID, excludeID string) (string, []byte, string, time.Time, time.Time) {
	query := `SELECT id,token_hash,token_key_id,created_at,expires_at FROM auth_one_time_grants WHERE user_id=? AND purpose='login_email_code'`
	args := []any{userID}
	if excludeID != "" {
		query += ` AND id<>?`
		args = append(args, excludeID)
	}
	query += ` ORDER BY created_at DESC,id DESC LIMIT 1`
	var id, keyID string
	var hash []byte
	var createdAt, expiresAt time.Time
	if err := db.QueryRowContext(ctx, query, args...).Scan(&id, &hash, &keyID, &createdAt, &expiresAt); err != nil {
		fail(err)
	}
	return id, hash, keyID, createdAt, expiresAt
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		fail(err)
	}
	return n
}
