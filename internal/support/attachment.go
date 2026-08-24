package support

import (
	"strings"
	"time"
)

type AttachmentStatus string

const (
	AttachmentQuarantined AttachmentStatus = "quarantined"
	AttachmentScanning    AttachmentStatus = "scanning"
	AttachmentClean       AttachmentStatus = "clean"
	AttachmentInfected    AttachmentStatus = "infected"
	AttachmentScanError   AttachmentStatus = "scan-error"
	AttachmentRejected    AttachmentStatus = "rejected"
)

type Attachment struct {
	ID               string           `json:"id"`
	TicketID         string           `json:"ticket_id"`
	MessageID        string           `json:"message_id"`
	StorageKey       string           `json:"-"`
	OriginalNameSafe string           `json:"original_name_safe"`
	MIMEType         string           `json:"mime_type"`
	SizeBytes        uint64           `json:"size_bytes"`
	SHA256           string           `json:"sha256"`
	ScanStatus       AttachmentStatus `json:"scan_status"`
	ScanUpdatedAt    time.Time        `json:"scan_updated_at"`
	CreatedAt        time.Time        `json:"created_at"`
}

func (a Attachment) Validate() error {
	if strings.TrimSpace(a.ID) == "" || strings.TrimSpace(a.TicketID) == "" || strings.TrimSpace(a.MessageID) == "" || strings.TrimSpace(a.StorageKey) == "" || strings.TrimSpace(a.OriginalNameSafe) == "" || strings.TrimSpace(a.MIMEType) == "" || a.SizeBytes == 0 || len(strings.TrimSpace(a.SHA256)) != 64 || a.CreatedAt.IsZero() || a.ScanUpdatedAt.IsZero() || a.ScanUpdatedAt.Before(a.CreatedAt) {
		return ErrInvalidInput
	}
	switch a.ScanStatus {
	case AttachmentQuarantined, AttachmentScanning, AttachmentClean, AttachmentInfected, AttachmentScanError, AttachmentRejected:
	default:
		return ErrInvalidInput
	}
	return nil
}

func BeginAttachmentScan(attachment Attachment, now time.Time) (Attachment, error) {
	if err := attachment.Validate(); err != nil {
		return Attachment{}, err
	}
	if attachment.ScanStatus != AttachmentQuarantined && attachment.ScanStatus != AttachmentScanError {
		return Attachment{}, ErrInvalidTransition
	}
	now = now.UTC()
	if now.Before(attachment.ScanUpdatedAt) {
		return Attachment{}, ErrInvalidInput
	}
	next := attachment
	next.ScanStatus = AttachmentScanning
	next.ScanUpdatedAt = now
	return next, nil
}

func CompleteAttachmentScan(attachment Attachment, target AttachmentStatus, now time.Time) (Attachment, error) {
	if err := attachment.Validate(); err != nil {
		return Attachment{}, err
	}
	if attachment.ScanStatus != AttachmentScanning {
		return Attachment{}, ErrInvalidTransition
	}
	switch target {
	case AttachmentClean, AttachmentInfected, AttachmentScanError:
	default:
		return Attachment{}, ErrInvalidTransition
	}
	now = now.UTC()
	if now.Before(attachment.ScanUpdatedAt) {
		return Attachment{}, ErrInvalidInput
	}
	next := attachment
	next.ScanStatus = target
	next.ScanUpdatedAt = now
	return next, nil
}

func RejectAttachment(attachment Attachment, now time.Time) (Attachment, error) {
	if err := attachment.Validate(); err != nil {
		return Attachment{}, err
	}
	if attachment.ScanStatus != AttachmentQuarantined {
		return Attachment{}, ErrInvalidTransition
	}
	now = now.UTC()
	if now.Before(attachment.ScanUpdatedAt) {
		return Attachment{}, ErrInvalidInput
	}
	next := attachment
	next.ScanStatus = AttachmentRejected
	next.ScanUpdatedAt = now
	return next, nil
}

func AttachmentDownloadAllowed(attachment Attachment) bool {
	return attachment.Validate() == nil && attachment.ScanStatus == AttachmentClean
}

func RequireAttachmentDownloadable(attachment Attachment) error {
	if !AttachmentDownloadAllowed(attachment) {
		return ErrAttachmentBlocked
	}
	return nil
}
