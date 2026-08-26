package trust

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Techshrr/GoJet/internal/support"
)

type AbuseResourceType string
type AbuseCategory string
type AbuseStatus string

const (
	AbuseShortLinkRisk   AbuseResourceType = "short-link-risk"
	AbuseCustomDomainRisk AbuseResourceType = "custom-domain-risk"

	AbusePhishing      AbuseCategory = "phishing"
	AbuseMalware       AbuseCategory = "malware"
	AbuseSpam          AbuseCategory = "spam"
	AbuseScam          AbuseCategory = "scam"
	AbuseImpersonation AbuseCategory = "impersonation"
	AbuseOther         AbuseCategory = "other"

	AbuseOpen          AbuseStatus = "open"
	AbuseInvestigating AbuseStatus = "investigating"
	AbuseResolved      AbuseStatus = "resolved"
	AbuseDismissed     AbuseStatus = "dismissed"
)

type AbuseReport struct {
	ID                     uint64
	PublicID               string
	WorkspaceID            string
	ResourceType           AbuseResourceType
	ResourceID             string
	HostnameASCII          string
	SafeCode               string
	DestinationFingerprint string
	Category               AbuseCategory
	DetailsRedacted        string
	RequestFingerprint     string
	IdempotencyKeyHash     string
	Status                 AbuseStatus
	Version                uint64
	CorrelationID          string
	EvidenceRef            string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type SubmitAbuseInput struct {
	ResourceType    AbuseResourceType
	Hostname        string
	Code            string
	Category        AbuseCategory
	Details         string
	TurnstileToken  string
	IdempotencyKey  string
	CorrelationID   string
	RemoteAddr      string
}

type SubmitAbuseResult struct {
	Report  AbuseReport
	Created bool
}

type AbuseService struct {
	Store    *Store
	Verifier support.TurnstileVerifier
	Guard    *AbuseSubmissionGuard
}

var (
	abuseHostnamePattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)
	abuseURLPattern      = regexp.MustCompile(`(?i)\bhttps?://[^\s<>]+`)
	abuseEmailPattern    = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+`)
	abuseBearerPattern   = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/-]{8,}`)
	abuseJWTLikePattern  = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9_-]{8,}\b`)
	abuseSecretPattern   = regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key|authorization)\s*[:=]\s*[^\s,;]+`)
)

func NewAbuseService(store *Store, verifier support.TurnstileVerifier, guard *AbuseSubmissionGuard) (*AbuseService, error) {
	if store == nil || store.db == nil || verifier == nil || guard == nil {
		return nil, ErrInvalid
	}
	return &AbuseService{Store: store, Verifier: verifier, Guard: guard}, nil
}

func (s *AbuseService) Submit(ctx context.Context, input SubmitAbuseInput) (SubmitAbuseResult, error) {
	if s == nil || s.Store == nil || s.Store.db == nil || s.Verifier == nil || s.Guard == nil {
		return SubmitAbuseResult{}, ErrInvalid
	}
	input, requestFingerprint, idempotencyHash, err := normalizeAbuseInput(input)
	if err != nil {
		return SubmitAbuseResult{}, err
	}

	// Idempotent network retries return the already-persisted safe receipt before
	// consuming another Turnstile token. A different body under the same key is
	// always a conflict.
	if existing, err := loadAbuseByIdempotency(ctx, s.Store.db, idempotencyHash); err == nil {
		if existing.RequestFingerprint != requestFingerprint {
			return SubmitAbuseResult{}, ErrConflict
		}
		return SubmitAbuseResult{Report: existing, Created: false}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return SubmitAbuseResult{}, err
	}

	allowed, err := s.Guard.AllowSubmission(ctx, input.RemoteAddr)
	if err != nil || !allowed {
		return SubmitAbuseResult{}, ErrRateLimited
	}
	if err := support.VerifyProtectedSubmission(ctx, input.TurnstileToken, s.Verifier, s.Guard); err != nil {
		return SubmitAbuseResult{}, ErrVerification
	}

	tx, err := s.Store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	defer tx.Rollback()

	if existing, err := loadAbuseByIdempotency(ctx, tx, idempotencyHash); err == nil {
		if existing.RequestFingerprint != requestFingerprint {
			return SubmitAbuseResult{}, ErrConflict
		}
		if err := tx.Commit(); err != nil {
			return SubmitAbuseResult{}, err
		}
		return SubmitAbuseResult{Report: existing, Created: false}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return SubmitAbuseResult{}, err
	}

	resolved, err := resolveAbuseResource(ctx, tx, input.ResourceType, input.Hostname, input.Code)
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	publicID, err := newAbusePublicID()
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	evidenceRef := "abuse-report:" + publicID
	result, err := tx.ExecContext(ctx, `
INSERT INTO abuse_reports
(public_id,workspace_id,resource_type,resource_id,hostname_ascii,safe_code,destination_fingerprint,category,details_redacted,
 request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref,created_at,updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,'open',1,?,?,?,?)`,
		publicID, resolved.workspaceID, string(input.ResourceType), resolved.resourceID, resolved.hostname, nullString(resolved.code), nullString(resolved.fingerprint),
		string(input.Category), input.Details, requestFingerprint, idempotencyHash, input.CorrelationID, evidenceRef, time.Now().UTC(), time.Now().UTC())
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	idRaw, err := result.LastInsertId()
	if err != nil || idRaw <= 0 {
		if err == nil {
			err = ErrConflict
		}
		return SubmitAbuseResult{}, err
	}
	id := uint64(idRaw)
	metadata, err := json.Marshal(map[string]any{
		"resource_type":   input.ResourceType,
		"details_present": input.Details != "",
	})
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO abuse_report_events
(report_id,workspace_id,actor_id,action,from_status,to_status,result,reason_category,correlation_id,idempotency_key_hash,request_fingerprint,metadata_json)
VALUES (?,?,?,'abuse.public-intake',NULL,'open','success','report-received',?,?,?,?)`,
		id, resolved.workspaceID, "public-reporter", input.CorrelationID, idempotencyHash, requestFingerprint, string(metadata)); err != nil {
		return SubmitAbuseResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return SubmitAbuseResult{}, err
	}
	report, err := loadAbuseByID(ctx, s.Store.db, id)
	if err != nil {
		return SubmitAbuseResult{}, err
	}
	return SubmitAbuseResult{Report: report, Created: true}, nil
}

type resolvedAbuseResource struct {
	workspaceID string
	resourceID  string
	hostname    string
	code        string
	fingerprint string
}

func resolveAbuseResource(ctx context.Context, tx *sql.Tx, resourceType AbuseResourceType, hostname, code string) (resolvedAbuseResource, error) {
	switch resourceType {
	case AbuseShortLinkRisk:
		var id uint64
		var workspaceID, storedHostname, storedCode, fingerprint string
		err := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,hostname,code,risk_fingerprint
FROM links
WHERE hostname=? AND code=? AND deleted_at IS NULL AND status<>'deleted'`, hostname, code).
			Scan(&id, &workspaceID, &storedHostname, &storedCode, &fingerprint)
		if errors.Is(err, sql.ErrNoRows) {
			return resolvedAbuseResource{}, ErrNotFound
		}
		if err != nil {
			return resolvedAbuseResource{}, err
		}
		return resolvedAbuseResource{workspaceID: workspaceID, resourceID: strconv.FormatUint(id, 10), hostname: storedHostname, code: storedCode, fingerprint: fingerprint}, nil
	case AbuseCustomDomainRisk:
		var id uint64
		var workspaceID, storedHostname string
		err := tx.QueryRowContext(ctx, `
SELECT id,workspace_id,hostname_ascii
FROM custom_domains
WHERE hostname_ascii=? AND removed_at IS NULL`, hostname).
			Scan(&id, &workspaceID, &storedHostname)
		if errors.Is(err, sql.ErrNoRows) {
			return resolvedAbuseResource{}, ErrNotFound
		}
		if err != nil {
			return resolvedAbuseResource{}, err
		}
		return resolvedAbuseResource{workspaceID: workspaceID, resourceID: strconv.FormatUint(id, 10), hostname: storedHostname}, nil
	default:
		return resolvedAbuseResource{}, ErrInvalid
	}
}

type abuseQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const abuseSelect = `
SELECT id,public_id,workspace_id,resource_type,resource_id,hostname_ascii,COALESCE(safe_code,''),COALESCE(destination_fingerprint,''),category,
       details_redacted,request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref,created_at,updated_at
FROM abuse_reports`

func loadAbuseByIdempotency(ctx context.Context, q abuseQueryer, idempotencyHash string) (AbuseReport, error) {
	return scanAbuse(q.QueryRowContext(ctx, abuseSelect+` WHERE idempotency_key_hash=?`, idempotencyHash))
}

func loadAbuseByID(ctx context.Context, q abuseQueryer, id uint64) (AbuseReport, error) {
	return scanAbuse(q.QueryRowContext(ctx, abuseSelect+` WHERE id=?`, id))
}

func scanAbuse(row *sql.Row) (AbuseReport, error) {
	var report AbuseReport
	var resourceType, category, status string
	if err := row.Scan(&report.ID, &report.PublicID, &report.WorkspaceID, &resourceType, &report.ResourceID, &report.HostnameASCII, &report.SafeCode,
		&report.DestinationFingerprint, &category, &report.DetailsRedacted, &report.RequestFingerprint, &report.IdempotencyKeyHash, &status, &report.Version,
		&report.CorrelationID, &report.EvidenceRef, &report.CreatedAt, &report.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AbuseReport{}, ErrNotFound
		}
		return AbuseReport{}, err
	}
	report.ResourceType = AbuseResourceType(resourceType)
	report.Category = AbuseCategory(category)
	report.Status = AbuseStatus(status)
	return report, nil
}

func normalizeAbuseInput(input SubmitAbuseInput) (SubmitAbuseInput, string, string, error) {
	input.Hostname = strings.ToLower(strings.TrimSpace(input.Hostname))
	input.Code = strings.TrimSpace(input.Code)
	input.Details = SanitizeAbuseDetails(input.Details)
	input.TurnstileToken = strings.TrimSpace(input.TurnstileToken)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.RemoteAddr = strings.TrimSpace(input.RemoteAddr)
	if !validAbuseResourceType(input.ResourceType) || !validAbuseCategory(input.Category) || !validAbuseHostname(input.Hostname) ||
		input.TurnstileToken == "" || input.IdempotencyKey == "" || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 200 ||
		input.CorrelationID == "" || len(input.CorrelationID) > 128 || input.RemoteAddr == "" || len(input.Details) > 1000 {
		return SubmitAbuseInput{}, "", "", ErrInvalid
	}
	if input.ResourceType == AbuseShortLinkRisk {
		if !validAbuseCode(input.Code) {
			return SubmitAbuseInput{}, "", "", ErrInvalid
		}
	} else if input.Code != "" {
		return SubmitAbuseInput{}, "", "", ErrInvalid
	}
	requestFingerprint := abuseRequestFingerprint(input)
	idempotencySum := sha256.Sum256([]byte(input.IdempotencyKey))
	return input, requestFingerprint, hex.EncodeToString(idempotencySum[:]), nil
}

func validAbuseResourceType(value AbuseResourceType) bool {
	return value == AbuseShortLinkRisk || value == AbuseCustomDomainRisk
}

func validAbuseCategory(value AbuseCategory) bool {
	switch value {
	case AbusePhishing, AbuseMalware, AbuseSpam, AbuseScam, AbuseImpersonation, AbuseOther:
		return true
	default:
		return false
	}
}

func validAbuseHostname(value string) bool {
	return value != "" && len(value) <= 253 && !strings.Contains(value, "..") && !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".") && abuseHostnamePattern.MatchString(value)
}

func validAbuseCode(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "/\\?#") {
		return false
	}
	for _, r := range value {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func abuseRequestFingerprint(input SubmitAbuseInput) string {
	canonical := strings.Join([]string{string(input.ResourceType), input.Hostname, input.Code, string(input.Category), input.Details}, "\n")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func newAbusePublicID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "abr_" + hex.EncodeToString(buf), nil
}

func SanitizeAbuseDetails(value string) string {
	value = strings.TrimSpace(value)
	value = abuseURLPattern.ReplaceAllString(value, "[redacted-url]")
	value = abuseEmailPattern.ReplaceAllString(value, "[redacted-email]")
	value = abuseBearerPattern.ReplaceAllString(value, "[redacted-credential]")
	value = abuseJWTLikePattern.ReplaceAllString(value, "[redacted-credential]")
	value = abuseSecretPattern.ReplaceAllString(value, "$1=[redacted]")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, value)
	if utf8.RuneCountInString(value) > 1000 {
		runes := []rune(value)
		value = string(runes[:1000])
	}
	return strings.TrimSpace(value)
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
