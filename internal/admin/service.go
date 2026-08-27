package admin

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const (
	defaultSessionTTL   = 8 * time.Hour
	defaultLockDuration = 15 * time.Minute
	maxFailedAttempts   = 5
	mfaFreshness        = 10 * time.Minute
	dummyPasswordHash   = "pbkdf2-sha256$600000$R29KZXRQMTVEdW1teVNhbHQ$FE1V29Z-H9_2LCpHXlnJsBnhGH6b_q8BanyeJsSMHBI"
)

type LoginLimiter interface {
	Allow(context.Context, string) (bool, error)
	Reset(context.Context, string) error
}

type Service struct {
	db             *sql.DB
	limiter        LoginLimiter
	cipher         *SecretCipher
	sessionTTL     time.Duration
	allowedOrigins map[string]struct{}
}

func NewService(db *sql.DB, limiter LoginLimiter, cipher *SecretCipher, sessionTTL time.Duration, allowedOrigins []string) (*Service, error) {
	if db == nil || limiter == nil || cipher == nil {
		return nil, ErrInvalid
	}
	if sessionTTL == 0 {
		sessionTTL = defaultSessionTTL
	}
	if sessionTTL < 15*time.Minute || sessionTTL > 24*time.Hour {
		return nil, ErrInvalid
	}
	origins := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	if len(origins) == 0 {
		return nil, ErrInvalid
	}
	return &Service{db: db, limiter: limiter, cipher: cipher, sessionTTL: sessionTTL, allowedOrigins: origins}, nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 320 || strings.Count(value, "@") != 1 || strings.ContainsAny(value, " \t\r\n") {
		return "", ErrInvalid
	}
	parts := strings.Split(value, "@")
	if parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[1], ".") || strings.HasSuffix(parts[1], ".") {
		return "", ErrInvalid
	}
	return strings.ToLower(value), nil
}
func normalizeName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return "", ErrInvalid
	}
	return strings.ToLower(value), nil
}
func validCorrelation(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\r\n\t")
}
func validID(value string, max int) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= max && !strings.ContainsAny(value, " \t\r\n")
}

func rateKey(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "admin-login:" + stringHex(sum[:])
}
func stringHex(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = h[v>>4]
		out[i*2+1] = h[v&15]
	}
	return string(out)
}

func (s *Service) Login(ctx context.Context, email, password, totp, correlationID string, now time.Time) (SessionSecret, error) {
	if s == nil || s.db == nil || !validPassword(password) || !validCorrelation(correlationID) {
		return SessionSecret{}, ErrInvalid
	}
	normalized, err := normalizeEmail(email)
	if err != nil {
		return SessionSecret{}, ErrUnauthorized
	}
	allowed, err := s.limiter.Allow(ctx, rateKey(normalized))
	if err != nil {
		return SessionSecret{}, err
	}
	if !allowed {
		return SessionSecret{}, ErrRateLimited
	}
	now = now.UTC().Truncate(time.Microsecond)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SessionSecret{}, err
	}
	defer tx.Rollback()
	var admin Administrator
	var passwordHash string
	var failed uint32
	var lockedUntil sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT a.id,a.email,a.display_name,a.status,a.version,a.created_at,a.updated_at,c.password_hash,c.failed_attempts,c.locked_until
FROM admin_administrators a JOIN admin_credentials c ON c.administrator_id=a.id WHERE a.email_normalized=? FOR UPDATE`, normalized).Scan(&admin.ID, &admin.Email, &admin.DisplayName, &admin.Status, &admin.Version, &admin.CreatedAt, &admin.UpdatedAt, &passwordHash, &failed, &lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_ = verifyPassword(dummyPasswordHash, password)
		_, _ = recordAuditTx(ctx, tx, auditInput{ActorKind: "anonymous", Action: "admin.auth.login", ResourceType: "administrator", Result: "denied", CorrelationID: correlationID, CreatedAt: now})
		if commitErr := tx.Commit(); commitErr != nil {
			return SessionSecret{}, commitErr
		}
		return SessionSecret{}, ErrUnauthorized
	}
	if err != nil {
		return SessionSecret{}, err
	}
	if admin.Status != "active" || (lockedUntil.Valid && lockedUntil.Time.After(now)) {
		_, _ = recordAuditTx(ctx, tx, auditInput{ActorKind: "anonymous", Action: "admin.auth.login", ResourceType: "administrator", ResourceID: admin.ID, Result: "denied", CorrelationID: correlationID, Metadata: map[string]any{"locked": true}, CreatedAt: now})
		if err := tx.Commit(); err != nil {
			return SessionSecret{}, err
		}
		return SessionSecret{}, ErrLocked
	}
	if !verifyPassword(passwordHash, password) {
		return SessionSecret{}, s.failLogin(ctx, tx, admin.ID, failed, correlationID, now, ErrUnauthorized)
	}
	var totpCipher []byte
	var totpKeyID, state string
	err = tx.QueryRowContext(ctx, `SELECT secret_ciphertext,secret_key_id,state FROM admin_totp_credentials WHERE administrator_id=?`, admin.ID).Scan(&totpCipher, &totpKeyID, &state)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SessionSecret{}, err
	}
	var mfaAt *time.Time
	if err == nil && state == "active" {
		if strings.TrimSpace(totp) == "" {
			_, _ = recordAuditTx(ctx, tx, auditInput{ActorKind: "anonymous", Action: "admin.auth.login", ResourceType: "administrator", ResourceID: admin.ID, Result: "denied", CorrelationID: correlationID, Metadata: map[string]any{"mfa_enabled": true}, CreatedAt: now})
			if err := tx.Commit(); err != nil {
				return SessionSecret{}, err
			}
			return SessionSecret{}, ErrMFARequired
		}
		secret, decErr := s.cipher.Decrypt(totpCipher, totpKeyID, "admin-totp:"+admin.ID)
		if decErr != nil {
			return SessionSecret{}, decErr
		}
		if !VerifyTOTP(secret, totp, now) {
			return SessionSecret{}, s.failLogin(ctx, tx, admin.ID, failed, correlationID, now, ErrMFAInvalid)
		}
		t := now
		mfaAt = &t
		admin.MFAEnabled = true
	}
	token, err := newOpaque("gas_", 32)
	if err != nil {
		return SessionSecret{}, err
	}
	csrf, err := newOpaque("gac_", 32)
	if err != nil {
		return SessionSecret{}, err
	}
	sid, err := newOpaque("ads_", 18)
	if err != nil {
		return SessionSecret{}, err
	}
	tokenHash := hashOpaque(token)
	csrfHash := hashOpaque(csrf)
	expires := now.Add(s.sessionTTL)
	_, err = tx.ExecContext(ctx, `INSERT INTO admin_sessions(id,administrator_id,token_hash,csrf_hash,status,expires_at,mfa_verified_at,last_seen_at,correlation_id,created_at,updated_at) VALUES (?,?,?,?,'active',?,?,?,?,?,?)`, sid, admin.ID, tokenHash[:], csrfHash[:], expires, mfaAt, now, correlationID, now, now)
	if err != nil {
		return SessionSecret{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE admin_credentials SET failed_attempts=0,locked_until=NULL,updated_at=? WHERE administrator_id=?`, now, admin.ID); err != nil {
		return SessionSecret{}, err
	}
	_, err = recordAuditTx(ctx, tx, auditInput{ActorKind: "administrator", ActorID: admin.ID, Action: "admin.auth.login", ResourceType: "admin_session", ResourceID: sid, Result: "success", CorrelationID: correlationID, After: map[string]any{"session_id": sid, "mfa_enabled": admin.MFAEnabled}, CreatedAt: now})
	if err != nil {
		return SessionSecret{}, err
	}
	if err = tx.Commit(); err != nil {
		return SessionSecret{}, err
	}
	_ = s.limiter.Reset(ctx, rateKey(normalized))
	return SessionSecret{Session: Session{ID: sid, AdministratorID: admin.ID, Status: "active", ExpiresAt: expires, MFAVerifiedAt: mfaAt, LastSeenAt: now, CreatedAt: now}, Token: token, CSRFToken: csrf}, nil
}

func (s *Service) failLogin(ctx context.Context, tx *sql.Tx, adminID string, failed uint32, correlationID string, now time.Time, returned error) error {
	failed++
	var lock any
	if failed >= maxFailedAttempts {
		lock = now.Add(defaultLockDuration)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE admin_credentials SET failed_attempts=?,locked_until=?,updated_at=? WHERE administrator_id=?`, failed, lock, now, adminID); err != nil {
		return err
	}
	_, err := recordAuditTx(ctx, tx, auditInput{ActorKind: "anonymous", Action: "admin.auth.login", ResourceType: "administrator", ResourceID: adminID, Result: "denied", CorrelationID: correlationID, Metadata: map[string]any{"attempts": int(failed), "locked": lock != nil}, CreatedAt: now})
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return returned
}

func (s *Service) Authenticate(ctx context.Context, token string, now time.Time) (Principal, error) {
	if s == nil || s.db == nil || strings.TrimSpace(token) == "" {
		return Principal{}, ErrUnauthorized
	}
	h := hashOpaque(token)
	now = now.UTC().Truncate(time.Microsecond)
	var p Principal
	var mfa, revoked sql.NullTime
	var csrf []byte
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.administrator_id,s.status,s.expires_at,s.mfa_verified_at,s.last_seen_at,s.created_at,s.revoked_at,s.csrf_hash,
a.id,a.email,a.display_name,a.status,a.version,a.created_at,a.updated_at,
EXISTS(SELECT 1 FROM admin_totp_credentials t WHERE t.administrator_id=a.id AND t.state='active')
FROM admin_sessions s JOIN admin_administrators a ON a.id=s.administrator_id WHERE s.token_hash=?`, h[:]).Scan(&p.Session.ID, &p.Session.AdministratorID, &p.Session.Status, &p.Session.ExpiresAt, &mfa, &p.Session.LastSeenAt, &p.Session.CreatedAt, &revoked, &csrf, &p.Administrator.ID, &p.Administrator.Email, &p.Administrator.DisplayName, &p.Administrator.Status, &p.Administrator.Version, &p.Administrator.CreatedAt, &p.Administrator.UpdatedAt, &p.Administrator.MFAEnabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthorized
	}
	if err != nil {
		return Principal{}, err
	}
	if p.Session.Status != "active" || p.Administrator.Status != "active" || !p.Session.ExpiresAt.After(now) {
		return Principal{}, ErrUnauthorized
	}
	if len(csrf) != 32 {
		return Principal{}, ErrUnauthorized
	}
	copy(p.CSRFHash[:], csrf)
	if mfa.Valid {
		t := mfa.Time.UTC()
		p.Session.MFAVerifiedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		p.Session.RevokedAt = &t
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT rp.permission FROM admin_role_assignments ra JOIN admin_role_permissions rp ON rp.role_id=ra.role_id JOIN admin_permissions ap ON ap.permission=rp.permission WHERE ra.administrator_id=? ORDER BY rp.permission`, p.Administrator.ID)
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()
	p.Permissions = map[string]struct{}{}
	for rows.Next() {
		var perm string
		if err := rows.Scan(&perm); err != nil {
			return Principal{}, err
		}
		if !ValidPermission(perm) {
			return Principal{}, ErrForbidden
		}
		p.Permissions[perm] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return Principal{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE admin_sessions SET last_seen_at=?,updated_at=? WHERE id=? AND status='active'`, now, now, p.Session.ID)
	return p, nil
}

func (s *Service) ValidateOrigin(origin string) bool {
	_, ok := s.allowedOrigins[strings.TrimSpace(origin)]
	return ok
}
func (s *Service) ValidateCSRF(p Principal, token string) bool {
	h := hashOpaque(strings.TrimSpace(token))
	return subtle.ConstantTimeCompare(h[:], p.CSRFHash[:]) == 1
}
func (s *Service) Require(p Principal, permission string) error {
	if !ValidPermission(permission) || !p.Has(permission) {
		return ErrForbidden
	}
	return nil
}
func (s *Service) RequireHighRisk(p Principal, permission string, authority MutationAuthority, now time.Time) error {
	if err := s.Require(p, permission); err != nil {
		return err
	}
	if strings.TrimSpace(authority.Reason) == "" {
		return ErrReasonRequired
	}
	if len(strings.TrimSpace(authority.Reason)) > 500 || !validCorrelation(authority.CorrelationID) || !validID(authority.IdempotencyKey, 200) {
		return ErrInvalid
	}
	if p.Session.MFAVerifiedAt == nil || now.UTC().Sub(p.Session.MFAVerifiedAt.UTC()) > mfaFreshness {
		return ErrMFARequired
	}
	return nil
}

func sortedPermissions(input []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, p := range input {
		p = strings.TrimSpace(p)
		if !ValidPermission(p) {
			return nil, ErrInvalid
		}
		set[p] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func requestFingerprint(value any) ([32]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(raw), nil
}
