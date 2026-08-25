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

type t002Result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func t002Fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	keyID := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_ID"))
	keyHex := strings.TrimSpace(os.Getenv("GOJET_AUTH_GRANT_KEY_HEX"))
	if dsn == "" || keyID == "" || keyHex == "" {
		t002Fail(fmt.Errorf("required P15-T002 integration configuration missing"))
	}
	keyBytes, err := hex.DecodeString(keyHex)
	if err != nil {
		t002Fail(fmt.Errorf("grant key is not valid hex: %w", err))
	}
	grantKey, err := securetoken.NewKey(keyID, keyBytes)
	if err != nil {
		t002Fail(err)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t002Fail(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t002Fail(err)
	}

	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		t002Fail(err)
	}
	service, err := auth.NewRegistrationService(db, grantKey)
	if err != nil {
		t002Fail(err)
	}

	var usersBeforeInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&usersBeforeInvalid); err != nil {
		t002Fail(err)
	}
	_, invalidErr := service.Register(ctx, auth.RegistrationInput{
		Email:         "not-an-email",
		DisplayName:   "Invalid",
		CorrelationID: "p15-t002-invalid",
	})
	var usersAfterInvalid int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM auth_users").Scan(&usersAfterInvalid); err != nil {
		t002Fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	email := fmt.Sprintf("p15-t002-%d@example.test", stamp)
	correlationID := fmt.Sprintf("p15-t002-%d", stamp)
	registeredAt := time.Now().UTC()
	registration, err := service.Register(ctx, auth.RegistrationInput{
		Email:         email,
		DisplayName:   "P15 T002 Registration",
		CorrelationID: correlationID,
	})
	if err != nil {
		t002Fail(err)
	}
	_, duplicateErr := service.Register(ctx, auth.RegistrationInput{
		Email:         strings.ToUpper(email),
		DisplayName:   "Duplicate Must Not Persist",
		CorrelationID: correlationID + "-duplicate",
	})

	var userCount int
	var userStatus string
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*),MAX(status) FROM auth_users WHERE email_normalized=?`, strings.ToLower(email)).
		Scan(&userCount, &userStatus); err != nil {
		t002Fail(err)
	}

	var (
		grantCount       int
		grantPurpose     string
		grantTokenHash   []byte
		grantTokenKeyID  sql.NullString
		grantExpiresAt   time.Time
		grantConsumedAt  sql.NullTime
		grantInvalidated sql.NullTime
		grantCorrelation string
		grantCreatedAt   time.Time
	)
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*),MAX(purpose),MAX(token_hash),MAX(token_key_id),MAX(expires_at),MAX(consumed_at),MAX(invalidated_at),MAX(correlation_id),MAX(created_at)
FROM auth_one_time_grants WHERE user_id=? AND purpose='email_verification'`, registration.User.ID).
		Scan(&grantCount, &grantPurpose, &grantTokenHash, &grantTokenKeyID, &grantExpiresAt, &grantConsumedAt, &grantInvalidated, &grantCorrelation, &grantCreatedAt); err != nil {
		t002Fail(err)
	}

	var (
		mailCount         int
		mailTemplate      string
		mailRecipientKind string
		mailResourceType  string
		mailStatus        string
		mailAttemptCount  int
		mailHash          []byte
	)
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*),MAX(template_key),MAX(recipient_kind),MAX(resource_type),MAX(status),MAX(attempt_count),MAX(idempotency_key_hash)
FROM mail_jobs WHERE resource_type='auth_one_time_grant' AND resource_id=?`, registration.Grant.ID).
		Scan(&mailCount, &mailTemplate, &mailRecipientKind, &mailResourceType, &mailStatus, &mailAttemptCount, &mailHash); err != nil {
		t002Fail(err)
	}

	var templateCount int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM mail_templates
WHERE template_key='auth-email-verification' AND locale='en' AND version=1 AND enabled=1
  AND JSON_CONTAINS(variable_allowlist_json, JSON_QUOTE('verification_code'))`).Scan(&templateCount); err != nil {
		t002Fail(err)
	}

	var auditCount int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND action='auth.registration.created' AND resource_type='auth_one_time_grant'
  AND resource_id=? AND request_correlation_id=? AND result='success'`,
		registration.User.ID, registration.Grant.ID, correlationID).Scan(&auditCount); err != nil {
		t002Fail(err)
	}

	expectedHash := securetoken.Hash(registration.VerificationCode)
	var storedHash [32]byte
	copy(storedHash[:], grantTokenHash)
	derivedAgain, err := grantKey.Derive("gvc_", "email_verification", registration.Grant.ID)
	if err != nil {
		t002Fail(err)
	}
	expiryDelta := grantExpiresAt.Sub(grantCreatedAt)

	counts := map[string]int{
		"normalized_user_records": userCount,
		"verification_grants":     grantCount,
		"verification_mail_jobs":  mailCount,
		"verification_templates":  templateCount,
		"registration_audit_rows": auditCount,
	}
	checks := map[string]bool{
		"invalid_registration_rejected_without_mutation":      errors.Is(invalidErr, auth.ErrInvalid) && usersBeforeInvalid == usersAfterInvalid,
		"normalized_account_is_unique":                        userCount == 1 && userStatus == auth.UserStatusPendingVerification,
		"duplicate_registration_fails_without_second_account": errors.Is(duplicateErr, auth.ErrConflict) && userCount == 1,
		"verification_grant_is_single_and_pending":            grantCount == 1 && grantPurpose == "email_verification" && !grantConsumedAt.Valid && !grantInvalidated.Valid,
		"verification_grant_is_expiry_bound":                  expiryDelta >= auth.EmailVerificationTTL-time.Second && expiryDelta <= auth.EmailVerificationTTL+time.Second && grantExpiresAt.After(registeredAt),
		"verification_grant_is_hash_only_at_rest":             len(grantTokenHash) == 32 && expectedHash == storedHash,
		"verification_code_is_runtime_opaque":                 strings.HasPrefix(registration.VerificationCode, "gvc_") && len(registration.VerificationCode) >= 40 && registration.VerificationCode == derivedAgain,
		"verification_key_identity_is_durable":                grantTokenKeyID.Valid && grantTokenKeyID.String == grantKey.ID() && registration.Grant.TokenKeyID == grantKey.ID(),
		"verification_correlation_is_preserved":               grantCorrelation == correlationID && registration.Grant.CorrelationID == correlationID,
		"p14_mail_job_is_queued_once":                         mailCount == 1 && mailTemplate == "auth-email-verification" && mailRecipientKind == "auth_user" && mailResourceType == "auth_one_time_grant" && mailStatus == "queued" && mailAttemptCount == 0 && len(mailHash) == 32,
		"p14_versioned_template_is_present":                   templateCount == 1,
		"registration_is_audited_by_correlation":              auditCount == 1,
	}

	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := t002Result{Case: "P15-T002", Status: status, MySQLVersion: mysqlVersion, RecordCounts: counts, Checks: checks}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(out); err != nil {
		t002Fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}
