package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	passwordAlgorithm        = "pbkdf2-sha256"
	passwordAlgorithmVersion = uint64(1)
	passwordPBKDF2Iterations = 600000
	passwordSaltBytes        = 16
	passwordDerivedBytes     = 32
	passwordMaxBytes         = 1024
)

type PasswordService struct {
	db *sql.DB
}

func NewPasswordService(db *sql.DB) (*PasswordService, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &PasswordService{db: db}, nil
}

func (s *PasswordService) SetInitialPassword(ctx context.Context, userID, password, correlationID string) error {
	userID = strings.TrimSpace(userID)
	correlationID = strings.TrimSpace(correlationID)
	if s == nil || s.db == nil || userID == "" || !validPassword(password) || !validCorrelationID(correlationID) {
		return ErrInvalid
	}
	encoded, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentVersion uint64
	if err := tx.QueryRowContext(ctx, `SELECT version FROM auth_users WHERE id=? FOR UPDATE`, userID).Scan(&currentVersion); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO auth_credentials
(user_id,password_hash,password_algorithm,password_version,failed_attempts,locked_until,created_at,updated_at)
VALUES (?,?,?,?,0,NULL,?,?)`, userID, encoded, passwordAlgorithm, passwordAlgorithmVersion, now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return ErrConflict
		}
		return err
	}
	result, err := tx.ExecContext(ctx, `
UPDATE auth_users
SET password_changed_at=?,version=version+1,updated_at=?
WHERE id=? AND version=?`, now, now, userID, currentVersion)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.password.initialized','auth_user',?,'success',?,JSON_OBJECT('algorithm',?,'password_version',1),?)`,
		userID, userID, correlationID, passwordAlgorithm, now); err != nil {
		return err
	}
	return tx.Commit()
}

func validPassword(password string) bool {
	return password != "" && len(password) <= passwordMaxBytes && utf8.ValidString(password)
}

func hashPassword(password string) (string, error) {
	if !validPassword(password) {
		return "", ErrInvalid
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := pbkdf2SHA256([]byte(password), salt, passwordPBKDF2Iterations, passwordDerivedBytes)
	return strings.Join([]string{
		passwordAlgorithm,
		strconv.Itoa(passwordPBKDF2Iterations),
		base64.RawURLEncoding.EncodeToString(salt),
		base64.RawURLEncoding.EncodeToString(derived),
	}, "$"), nil
}

func verifyPassword(encoded, password string) bool {
	if !validPassword(password) {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordAlgorithm {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations < 100000 || iterations > 1000000 {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) < 16 || len(salt) > 64 {
		return false
	}
	expected, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(expected) != passwordDerivedBytes {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	if iterations <= 0 || keyLength <= 0 {
		return nil
	}
	hashLength := sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	derived := make([]byte, 0, blocks*hashLength)
	var blockIndex [4]byte
	for block := 1; block <= blocks; block++ {
		binary.BigEndian.PutUint32(blockIndex[:], uint32(block))
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write(blockIndex[:])
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for iteration := 1; iteration < iterations; iteration++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for i := range t {
				t[i] ^= u[i]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLength]
}
