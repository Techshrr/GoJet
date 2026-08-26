package support

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// MailEnqueueInput is the shared P14 durable queue boundary consumed by later
// nodes. It creates a mail job only; SMTP delivery remains exclusively owned by
// the inherited P14 mailworker.
type MailEnqueueInput struct {
	TemplateKey    string
	Locale         string
	RecipientKind  string
	RecipientValue string
	ResourceType   string
	ResourceID     string
}

func EnqueueMailTx(ctx context.Context, tx *sql.Tx, input MailEnqueueInput, now time.Time) error {
	input.TemplateKey = strings.TrimSpace(input.TemplateKey)
	input.Locale = strings.TrimSpace(input.Locale)
	input.RecipientKind = strings.TrimSpace(input.RecipientKind)
	input.RecipientValue = strings.TrimSpace(input.RecipientValue)
	input.ResourceType = strings.TrimSpace(input.ResourceType)
	input.ResourceID = strings.TrimSpace(input.ResourceID)
	if tx == nil || now.IsZero() || input.TemplateKey == "" || input.Locale == "" || input.RecipientKind == "" || input.RecipientValue == "" || input.ResourceType == "" || input.ResourceID == "" {
		return ErrInvalidInput
	}
	return enqueueMailTx(ctx, tx, input.TemplateKey, input.Locale, input.RecipientKind, input.RecipientValue, input.ResourceType, input.ResourceID, now.UTC())
}
