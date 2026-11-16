package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
)

// NumericEmailCode is a domain-separated, deterministic six-digit display code.
// The database retains its existing high-entropy grant hashes. Codes are never
// looked up globally: ResolveEmailCode binds every attempt to email and purpose.
func NumericEmailCode(key securetoken.Key, purpose, grantID string) (string, error) {
	secret, err := key.Derive("otp_", "numeric-email:"+purpose, grantID)
	if err != nil {
		return "", err
	}
	for n := 0; ; n++ {
		h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", secret, n)))
		v := binary.BigEndian.Uint32(h[:4])
		if v < 4294000000 {
			return fmt.Sprintf("%06d", v%1000000), nil
		}
	}
}

func IsNumericEmailCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// ResolveEmailCode charges an attempt while holding the grant row lock. Failed
// attempts commit, so retries and concurrent requests cannot reset the budget.
// The existing purpose-specific consumer still atomically enforces single use.
func ResolveEmailCode(ctx context.Context, db *sql.DB, key securetoken.Key, email, purpose, code string) (string, error) {
	prefix := map[string]string{"email_verification": "gvc_", "login_email_code": "glc_", "password_reset": "grp_", "social_email_verification": "gsv_"}[purpose]
	normalized, err := NormalizeEmail(email)
	if err != nil || prefix == "" || !IsNumericEmailCode(code) {
		return "", ErrUnauthorized
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id, keyID string
	var hash []byte
	var expires time.Time
	var consumed, invalidated sql.NullTime
	var attempts, maximum int
	err = tx.QueryRowContext(ctx, `SELECT id,token_key_id,token_hash,expires_at,consumed_at,invalidated_at,attempt_count,max_attempts
FROM auth_one_time_grants WHERE email_normalized=? AND purpose=? ORDER BY created_at DESC,id DESC LIMIT 1 FOR UPDATE`, normalized, purpose).
		Scan(&id, &keyID, &hash, &expires, &consumed, &invalidated, &attempts, &maximum)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthorized
	}
	if err != nil {
		return "", err
	}
	if keyID != key.ID() || consumed.Valid || invalidated.Valid {
		return "", ErrUnauthorized
	}
	if !expires.After(time.Now().UTC()) {
		return "", ErrExpired
	}
	if attempts >= 5 || attempts >= maximum {
		return "", ErrRateLimited
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_one_time_grants SET attempt_count=attempt_count+1 WHERE id=?`, id); err != nil {
		return "", err
	}
	expected, err := NumericEmailCode(key, purpose, id)
	if err != nil {
		return "", err
	}
	canonical, err := key.Derive(prefix, purpose, id)
	if err != nil {
		return "", err
	}
	canonicalHash := securetoken.Hash(canonical)
	valid := subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 && subtle.ConstantTimeCompare(hash, canonicalHash[:]) == 1
	if err = tx.Commit(); err != nil {
		return "", err
	}
	if !valid {
		return "", ErrUnauthorized
	}
	return canonical, nil
}

// Legacy links remain usable until their original expiry. Numeric entry always
// requires the matching mailbox; it can never fall through to hash-only lookup.
func ResolveEmailInput(ctx context.Context, db *sql.DB, key securetoken.Key, email, purpose, code string) (string, error) {
	code = strings.TrimSpace(code)
	if IsNumericEmailCode(code) {
		return ResolveEmailCode(ctx, db, key, email, purpose, code)
	}
	return code, nil
}
