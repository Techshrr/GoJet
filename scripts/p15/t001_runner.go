package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	_ "github.com/go-sql-driver/mysql"
)

type result struct {
	Case         string         `json:"case"`
	Status       string         `json:"status"`
	MySQLVersion string         `json:"mysql_version"`
	RecordCounts map[string]int `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func randomSuffix() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fail(fmt.Errorf("GOJET_MYSQL_DSN is required"))
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	store := auth.NewStore(db)
	if err := store.Ping(ctx); err != nil {
		fail(err)
	}

	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}

	suffix, err := randomSuffix()
	if err != nil {
		fail(err)
	}
	correlationID := "p15-t001-" + suffix
	email := "p15-t001-" + suffix + "@example.test"

	user, err := store.CreateUser(ctx, auth.CreateUserInput{
		Email:       email,
		DisplayName: "P15 T001 Integration",
	})
	if err != nil {
		fail(err)
	}

	// T001 validates durable credential schema and foreign-key authority only. Password
	// hashing/login behavior is owned by later frozen cases; this non-login fixture is a
	// one-way digest and is never emitted into evidence.
	credentialFixture := sha256.Sum256([]byte("p15-t001-schema-fixture:" + suffix))
	credentialHash := "$p15-t001-schema$" + hex.EncodeToString(credentialFixture[:])
	if _, err := db.ExecContext(ctx, `
INSERT INTO auth_credentials
(user_id,password_hash,password_algorithm,password_version,failed_attempts,created_at,updated_at)
VALUES (?,?,'p15-t001-schema-fixture',1,0,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`,
		user.ID, credentialHash); err != nil {
		fail(err)
	}

	sessionSecret, err := store.CreateSession(ctx, user.ID, 30*time.Minute, correlationID)
	if err != nil {
		fail(err)
	}
	loadedSession, err := store.GetSessionByToken(ctx, sessionSecret.Token, time.Now().UTC())
	if err != nil {
		fail(err)
	}

	providerSubject := "p15-t001-provider-subject-" + suffix
	identity, err := store.BindOAuthIdentity(ctx, auth.BindOAuthIdentityInput{
		UserID:                user.ID,
		Provider:              auth.ProviderGitHub,
		ProviderSubject:       providerSubject,
		ProviderEmail:         email,
		ProviderEmailVerified: true,
		DisplayName:           "P15 T001 Provider",
	})
	if err != nil {
		fail(err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('user',?,?, 'auth.t001.schema_probe','auth_session',?,'success',?,JSON_OBJECT('fixture', true),UTC_TIMESTAMP(6))`,
		user.ID, user.ID, sessionSecret.Session.ID, correlationID); err != nil {
		fail(err)
	}

	var storedTokenHash, storedCSRFHash, storedSubjectHash []byte
	var storedCorrelation string
	if err := db.QueryRowContext(ctx, `
SELECT token_hash,csrf_secret_hash,correlation_id
FROM auth_sessions WHERE id=? AND user_id=?`, sessionSecret.Session.ID, user.ID).
		Scan(&storedTokenHash, &storedCSRFHash, &storedCorrelation); err != nil {
		fail(err)
	}
	if err := db.QueryRowContext(ctx, `
SELECT provider_subject_hash FROM oauth_identities WHERE id=? AND user_id=? AND provider='github'`, identity.ID, user.ID).
		Scan(&storedSubjectHash); err != nil {
		fail(err)
	}

	var credentialLength int
	var credentialAlgorithm string
	if err := db.QueryRowContext(ctx, `
SELECT CHAR_LENGTH(password_hash),password_algorithm FROM auth_credentials WHERE user_id=?`, user.ID).
		Scan(&credentialLength, &credentialAlgorithm); err != nil {
		fail(err)
	}

	var auditCorrelationCount int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM auth_audit_events
WHERE user_id=? AND request_correlation_id=? AND resource_id=?`,
		user.ID, correlationID, sessionSecret.Session.ID).Scan(&auditCorrelationCount); err != nil {
		fail(err)
	}

	var unsafePlainColumns int
	if err := db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA=DATABASE()
  AND (
    (TABLE_NAME='auth_credentials' AND COLUMN_NAME='password')
    OR (TABLE_NAME='auth_sessions' AND COLUMN_NAME IN ('token','csrf_secret'))
    OR (TABLE_NAME='oauth_identities' AND COLUMN_NAME='provider_subject')
  )`).Scan(&unsafePlainColumns); err != nil {
		fail(err)
	}

	var tokenHashType, csrfHashType, subjectHashType string
	var tokenHashLength, csrfHashLength, subjectHashLength sql.NullInt64
	for _, probe := range []struct {
		table  string
		column string
		typeOut *string
		lenOut  *sql.NullInt64
	}{
		{"auth_sessions", "token_hash", &tokenHashType, &tokenHashLength},
		{"auth_sessions", "csrf_secret_hash", &csrfHashType, &csrfHashLength},
		{"oauth_identities", "provider_subject_hash", &subjectHashType, &subjectHashLength},
	} {
		if err := db.QueryRowContext(ctx, `
SELECT DATA_TYPE,CHARACTER_MAXIMUM_LENGTH
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, probe.table, probe.column).
			Scan(probe.typeOut, probe.lenOut); err != nil {
			fail(err)
		}
	}

	tokenExpected := auth.HashOpaque(sessionSecret.Token)
	csrfExpected := auth.HashOpaque(sessionSecret.CSRFToken)
	subjectExpected := auth.HashOpaque(auth.ProviderGitHub + "\x00" + providerSubject)

	counts := map[string]int{}
	for _, table := range []string{"auth_users", "auth_credentials", "auth_sessions", "oauth_identities", "auth_audit_events"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table
		if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			fail(err)
		}
		counts[table] = count
	}

	checks := map[string]bool{
		"user_id_is_opaque_server_identifier":             strings.HasPrefix(user.ID, "usr_") && len(user.ID) >= 20,
		"session_id_is_opaque_server_identifier":          strings.HasPrefix(sessionSecret.Session.ID, "ses_") && len(sessionSecret.Session.ID) >= 20,
		"oauth_identity_id_is_opaque_server_identifier":   strings.HasPrefix(identity.ID, "oid_") && len(identity.ID) >= 20,
		"credential_record_is_durable_and_non_plaintext":  credentialLength >= 32 && credentialAlgorithm == "p15-t001-schema-fixture",
		"session_token_is_hash_only_at_rest":              len(storedTokenHash) == 32 && auth.EqualOpaqueHash(tokenExpected, bytesToHash(storedTokenHash)),
		"session_csrf_is_hash_only_at_rest":               len(storedCSRFHash) == 32 && auth.EqualOpaqueHash(csrfExpected, bytesToHash(storedCSRFHash)),
		"oauth_subject_is_hash_only_at_rest":              len(storedSubjectHash) == 32 && auth.EqualOpaqueHash(subjectExpected, bytesToHash(storedSubjectHash)),
		"session_lookup_is_server_authoritative":          loadedSession.ID == sessionSecret.Session.ID && loadedSession.UserID == user.ID && loadedSession.Status == auth.SessionStatusActive,
		"correlation_survives_session_round_trip":         storedCorrelation == correlationID && loadedSession.CorrelationID == correlationID,
		"audit_correlation_matches_session_authority":     auditCorrelationCount == 1,
		"unsafe_plain_secret_columns_are_absent":          unsafePlainColumns == 0,
		"session_token_hash_schema_is_binary32":           strings.EqualFold(tokenHashType, "binary") && tokenHashLength.Valid && tokenHashLength.Int64 == 32,
		"session_csrf_hash_schema_is_binary32":            strings.EqualFold(csrfHashType, "binary") && csrfHashLength.Valid && csrfHashLength.Int64 == 32,
		"oauth_subject_hash_schema_is_binary32":           strings.EqualFold(subjectHashType, "binary") && subjectHashLength.Valid && subjectHashLength.Int64 == 32,
		"required_records_exist":                          counts["auth_users"] >= 1 && counts["auth_credentials"] >= 1 && counts["auth_sessions"] >= 1 && counts["oauth_identities"] >= 1 && counts["auth_audit_events"] >= 1,
	}

	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}

	out := result{
		Case:         "P15-T001",
		Status:       status,
		MySQLVersion: mysqlVersion,
		RecordCounts: counts,
		Checks:       checks,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func bytesToHash(raw []byte) [32]byte {
	var out [32]byte
	copy(out[:], raw)
	return out
}
