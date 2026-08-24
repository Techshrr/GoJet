package support

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type MailStatus string

const (
	MailQueued   MailStatus = "queued"
	MailSending  MailStatus = "sending"
	MailSent     MailStatus = "sent"
	MailRetrying MailStatus = "retrying"
	MailFailed   MailStatus = "failed"
)

const (
	DefaultMailMaxAttempts = uint32(5)
	DefaultMailClaimTTL    = 2 * time.Minute
	mailBaseBackoff        = 30 * time.Second
	mailMaxBackoff         = 15 * time.Minute
)

type MailTemplate struct {
	Key                  string
	Version              uint64
	SubjectTemplate      string
	TextTemplate         string
	HTMLTemplate         string
	VariableAllowlist    []string
	InternalOnlyTemplate bool
}

type RenderedMail struct {
	Subject string
	Text    string
	HTML    string
}

type MailJob struct {
	ID                 string     `json:"id"`
	TemplateKey        string     `json:"template_key"`
	TemplateVersion    uint64     `json:"template_version"`
	RecipientKind      string     `json:"recipient_kind"`
	RecipientValue     string     `json:"recipient_value"`
	ResourceType       string     `json:"resource_type"`
	ResourceID         string     `json:"resource_id"`
	Status             MailStatus `json:"status"`
	AttemptCount       uint32     `json:"attempt_count"`
	NextAttemptAt      *time.Time `json:"next_attempt_at,omitempty"`
	IdempotencyKeyHash [32]byte   `json:"-"`
	ClaimTokenHash     [32]byte   `json:"-"`
	ClaimExpiresAt     *time.Time `json:"claim_expires_at,omitempty"`
	LastErrorCode      string     `json:"last_error_code,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type MailDeliveryResult struct {
	Success   bool
	Transient bool
	ErrorCode string
}

var templateVariablePattern = regexp.MustCompile(`\{\{\s*([A-Za-z][A-Za-z0-9_]*)\s*\}\}`)

func RenderMailTemplate(template MailTemplate, values map[string]string) (RenderedMail, error) {
	if strings.TrimSpace(template.Key) == "" || template.Version == 0 {
		return RenderedMail{}, ErrInvalidInput
	}
	allow := make(map[string]struct{}, len(template.VariableAllowlist))
	for _, name := range template.VariableAllowlist {
		name = strings.TrimSpace(name)
		if name == "" || isSensitiveTemplateVariable(name, template.InternalOnlyTemplate) {
			return RenderedMail{}, ErrSensitiveVariable
		}
		allow[name] = struct{}{}
	}
	for name := range values {
		if _, ok := allow[name]; !ok {
			return RenderedMail{}, fmt.Errorf("%w: %s", ErrTemplateVariable, name)
		}
		if isSensitiveTemplateVariable(name, template.InternalOnlyTemplate) {
			return RenderedMail{}, fmt.Errorf("%w: %s", ErrSensitiveVariable, name)
		}
	}

	subject, err := renderSimpleTemplate(template.SubjectTemplate, values, allow, false)
	if err != nil {
		return RenderedMail{}, err
	}
	if strings.ContainsAny(subject, "\r\n") {
		return RenderedMail{}, ErrTemplateVariable
	}
	textBody, err := renderSimpleTemplate(template.TextTemplate, values, allow, false)
	if err != nil {
		return RenderedMail{}, err
	}
	htmlBody, err := renderSimpleTemplate(template.HTMLTemplate, values, allow, true)
	if err != nil {
		return RenderedMail{}, err
	}
	return RenderedMail{Subject: subject, Text: textBody, HTML: htmlBody}, nil
}

func renderSimpleTemplate(source string, values map[string]string, allow map[string]struct{}, escapeHTML bool) (string, error) {
	matches := templateVariablePattern.FindAllStringSubmatchIndex(source, -1)
	var build strings.Builder
	last := 0
	for _, match := range matches {
		name := source[match[2]:match[3]]
		if _, ok := allow[name]; !ok {
			return "", fmt.Errorf("%w: %s", ErrTemplateVariable, name)
		}
		value, ok := values[name]
		if !ok {
			return "", fmt.Errorf("%w: missing %s", ErrTemplateVariable, name)
		}
		build.WriteString(source[last:match[0]])
		if escapeHTML {
			build.WriteString(html.EscapeString(value))
		} else {
			build.WriteString(value)
		}
		last = match[1]
	}
	build.WriteString(source[last:])
	rendered := build.String()
	if strings.Contains(rendered, "{{") || strings.Contains(rendered, "}}") {
		return "", ErrTemplateVariable
	}
	return rendered, nil
}

func isSensitiveTemplateVariable(name string, internalOnly bool) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, fragment := range []string{
		"password", "secret", "credential", "authorization", "turnstile", "smtp", "provider_response", "provider_evidence", "claim_token", "access_token", "refresh_token", "api_key", "private_key",
	} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	if strings.Contains(lower, "internal_note") && !internalOnly {
		return true
	}
	return false
}

func MailLogicalIdempotencyHash(templateKey string, templateVersion uint64, recipientPurpose, resourceType, resourceID string) ([32]byte, error) {
	parts := []string{strings.TrimSpace(templateKey), strings.TrimSpace(recipientPurpose), strings.TrimSpace(resourceType), strings.TrimSpace(resourceID)}
	if templateVersion == 0 {
		return [32]byte{}, ErrInvalidInput
	}
	for _, part := range parts {
		if part == "" {
			return [32]byte{}, ErrInvalidInput
		}
	}
	h := sha256.New()
	writeHashPart(h, parts[0])
	var version [8]byte
	binary.BigEndian.PutUint64(version[:], templateVersion)
	_, _ = h.Write(version[:])
	for _, part := range parts[1:] {
		writeHashPart(h, part)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func writeHashPart(h interface{ Write([]byte) (int, error) }, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(value))
}

func ClaimMailJob(job MailJob, rawClaimToken string, now time.Time) (MailJob, error) {
	if err := validateMailJob(job); err != nil {
		return MailJob{}, err
	}
	rawClaimToken = strings.TrimSpace(rawClaimToken)
	if rawClaimToken == "" {
		return MailJob{}, ErrMailClaim
	}
	now = now.UTC()
	if now.Before(job.UpdatedAt) {
		return MailJob{}, ErrInvalidInput
	}
	if job.Status != MailQueued && job.Status != MailRetrying {
		return MailJob{}, ErrMailState
	}
	if job.Status == MailRetrying && (job.NextAttemptAt == nil || now.Before(job.NextAttemptAt.UTC())) {
		return MailJob{}, ErrMailState
	}
	if job.AttemptCount >= DefaultMailMaxAttempts {
		return MailJob{}, ErrMailState
	}
	next := job
	next.Status = MailSending
	next.AttemptCount++
	next.NextAttemptAt = nil
	next.ClaimTokenHash = sha256.Sum256([]byte(rawClaimToken))
	claimExpiresAt := now.Add(DefaultMailClaimTTL)
	next.ClaimExpiresAt = &claimExpiresAt
	next.UpdatedAt = now
	return next, nil
}

func CompleteMailJob(job MailJob, rawClaimToken string, result MailDeliveryResult, now time.Time) (MailJob, error) {
	if err := validateMailJob(job); err != nil {
		return MailJob{}, err
	}
	if job.Status != MailSending || strings.TrimSpace(rawClaimToken) == "" {
		return MailJob{}, ErrMailState
	}
	provided := sha256.Sum256([]byte(strings.TrimSpace(rawClaimToken)))
	if subtle.ConstantTimeCompare(provided[:], job.ClaimTokenHash[:]) != 1 {
		return MailJob{}, ErrMailClaim
	}
	now = now.UTC()
	if now.Before(job.UpdatedAt) {
		return MailJob{}, ErrInvalidInput
	}

	next := job
	next.UpdatedAt = now
	next.ClaimTokenHash = [32]byte{}
	next.ClaimExpiresAt = nil
	if result.Success {
		next.Status = MailSent
		next.LastErrorCode = ""
		next.NextAttemptAt = nil
		return next, nil
	}
	code := strings.TrimSpace(result.ErrorCode)
	if code == "" {
		code = "delivery_failed"
	}
	next.LastErrorCode = code
	if result.Transient && next.AttemptCount < DefaultMailMaxAttempts {
		next.Status = MailRetrying
		due := now.Add(MailRetryBackoff(next.AttemptCount))
		next.NextAttemptAt = &due
		return next, nil
	}
	next.Status = MailFailed
	next.NextAttemptAt = nil
	return next, nil
}

func MailRetryBackoff(attempt uint32) time.Duration {
	if attempt == 0 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 16 {
		shift = 16
	}
	delay := mailBaseBackoff * time.Duration(uint64(1)<<shift)
	if delay > mailMaxBackoff {
		return mailMaxBackoff
	}
	return delay
}

func validateMailJob(job MailJob) error {
	if strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.TemplateKey) == "" || job.TemplateVersion == 0 || strings.TrimSpace(job.RecipientKind) == "" || strings.TrimSpace(job.RecipientValue) == "" || strings.TrimSpace(job.ResourceType) == "" || strings.TrimSpace(job.ResourceID) == "" || job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) {
		return ErrInvalidInput
	}
	switch job.Status {
	case MailQueued, MailSending, MailSent, MailRetrying, MailFailed:
	default:
		return ErrMailState
	}
	if job.Status == MailRetrying && job.NextAttemptAt == nil {
		return ErrMailState
	}
	if job.Status != MailRetrying && job.NextAttemptAt != nil {
		return ErrMailState
	}
	zeroClaimHash := [32]byte{}
	if job.Status == MailSending {
		if job.ClaimTokenHash == zeroClaimHash || job.ClaimExpiresAt == nil || !job.ClaimExpiresAt.After(job.UpdatedAt) {
			return ErrMailState
		}
	} else if job.ClaimTokenHash != zeroClaimHash || job.ClaimExpiresAt != nil {
		return ErrMailState
	}
	if job.AttemptCount > DefaultMailMaxAttempts {
		return ErrMailState
	}
	return nil
}

func MailHashHex(hash [32]byte) string {
	return fmt.Sprintf("%x", hash[:])
}

func MailAttemptLabel(attempt uint32) string {
	return "attempt-" + strconv.FormatUint(uint64(attempt), 10)
}
