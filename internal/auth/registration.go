package auth

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/securetoken"
	"github.com/Techshrr/GoJet/internal/support"
)

const EmailVerificationTTL = 15 * time.Minute

type RegistrationInput struct {
	Email         string
	DisplayName   string
	CorrelationID string
}

type VerificationGrant struct {
	ID              string
	UserID          string
	EmailNormalized string
	Purpose         string
	TokenKeyID      string
	ExpiresAt       time.Time
	CorrelationID   string
	CreatedAt       time.Time
}

type RegistrationResult struct {
	User             User
	Grant            VerificationGrant
	VerificationCode string
}

type RegistrationService struct {
	db       *sql.DB
	grantKey securetoken.Key
}

func NewRegistrationService(db *sql.DB, grantKey securetoken.Key) (*RegistrationService, error) {
	if db == nil || strings.TrimSpace(grantKey.ID()) == "" {
		return nil, ErrInvalid
	}
	return &RegistrationService{db: db, grantKey: grantKey}, nil
}

func (s *RegistrationService) Register(ctx context.Context, input RegistrationInput) (RegistrationResult, error) {
	if s == nil || s.db == nil {
		return RegistrationResult{}, ErrInvalid
	}
	email := strings.TrimSpace(input.Email)
	normalized, err := NormalizeEmail(email)
	if err != nil {
		return RegistrationResult{}, ErrInvalid
	}
	displayName := strings.TrimSpace(input.DisplayName)
	correlationID := strings.TrimSpace(input.CorrelationID)
	if len(displayName) > 255 || !validCorrelationID(correlationID) {
		return RegistrationResult{}, ErrInvalid
	}

	userID, err := newOpaqueID("usr_", 18)
	if err != nil {
		return RegistrationResult{}, err
	}
	grantID, err := newOpaqueID("grt_", 18)
	if err != nil {
		return RegistrationResult{}, err
	}
	verificationCode, err := s.grantKey.Derive("gvc_", "email_verification", grantID)
	if err != nil {
		return RegistrationResult{}, err
	}
	tokenHash := securetoken.Hash(verificationCode)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiresAt := now.Add(EmailVerificationTTL)

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return RegistrationResult{}, err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
INSERT INTO auth_users
(id,email,email_normalized,display_name,status,version,created_at,updated_at)
VALUES (?,?,?,?,'pending_verification',1,?,?)`,
		userID, email, normalized, displayName, now, now)
	if err != nil {
		if mysqlDuplicate(err) {
			return RegistrationResult{}, ErrConflict
		}
		return RegistrationResult{}, err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO auth_one_time_grants
(id,purpose,user_id,email_normalized,token_hash,token_key_id,attempt_count,max_attempts,expires_at,consumed_at,invalidated_at,correlation_id,created_at)
VALUES (?,'email_verification',?,?,?,?,0,8,?,NULL,NULL,?,?)`,
		grantID, userID, normalized, tokenHash[:], s.grantKey.ID(), expiresAt, correlationID, now)
	if err != nil {
		return RegistrationResult{}, err
	}

	if err := support.EnqueueMailTx(ctx, tx, support.MailEnqueueInput{
		TemplateKey:    "auth-email-verification",
		Locale:         "en",
		RecipientKind:  "auth_user",
		RecipientValue: email,
		ResourceType:   "auth_one_time_grant",
		ResourceID:     grantID,
	}, now); err != nil {
		return RegistrationResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO auth_audit_events
(actor_kind,actor_id,user_id,action,resource_type,resource_id,result,request_correlation_id,metadata_json,created_at)
VALUES ('public','',?,'auth.registration.created','auth_one_time_grant',?,'success',?,JSON_OBJECT('purpose','email_verification'),?)`,
		userID, grantID, correlationID, now); err != nil {
		return RegistrationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return RegistrationResult{}, err
	}

	return RegistrationResult{
		User: User{
			ID:              userID,
			Email:           email,
			EmailNormalized: normalized,
			DisplayName:     displayName,
			Status:          UserStatusPendingVerification,
			Version:         1,
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		Grant: VerificationGrant{
			ID:              grantID,
			UserID:          userID,
			EmailNormalized: normalized,
			Purpose:         "email_verification",
			TokenKeyID:      s.grantKey.ID(),
			ExpiresAt:       expiresAt,
			CorrelationID:   correlationID,
			CreatedAt:       now,
		},
		VerificationCode: verificationCode,
	}, nil
}

func validCorrelationID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}
