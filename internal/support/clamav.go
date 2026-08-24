package support

import (
	"context"
	"io"
	"time"

	filecore "github.com/Techshrr/GoJet/internal/files"
)

type P09AttachmentScanner interface {
	Scan(ctx context.Context, src io.Reader) (filecore.ScanResult, error)
}

// ApplyP09ScanOutcome maps the inherited P09 ClamAV authority into P14's
// attachment state machine. A scanner/provider error always wins over any
// optimistic verdict and therefore cannot release an attachment.
func ApplyP09ScanOutcome(attachment Attachment, result filecore.ScanResult, scanErr error, now time.Time) (Attachment, error) {
	target := AttachmentScanError
	if scanErr == nil {
		switch result.Verdict {
		case filecore.VerdictClean:
			target = AttachmentClean
		case filecore.VerdictInfected:
			target = AttachmentInfected
		case filecore.VerdictError:
			target = AttachmentScanError
		default:
			target = AttachmentScanError
		}
	}
	return CompleteAttachmentScan(attachment, target, now)
}

// ScanTicketAttachment sends quarantine bytes only through the inherited P09
// scanner interface. Unknown, unavailable, stale-signature and indeterminate
// outcomes remain fail-closed in scan-error and are never downloadable.
func ScanTicketAttachment(ctx context.Context, scanner P09AttachmentScanner, attachment Attachment, src io.Reader, startedAt, completedAt time.Time) (Attachment, filecore.ScanResult, error) {
	if scanner == nil || src == nil {
		return Attachment{}, filecore.ScanResult{}, ErrInvalidInput
	}
	scanning, err := BeginAttachmentScan(attachment, startedAt)
	if err != nil {
		return Attachment{}, filecore.ScanResult{}, err
	}
	result, scanErr := scanner.Scan(ctx, src)
	completed, stateErr := ApplyP09ScanOutcome(scanning, result, scanErr, completedAt)
	if stateErr != nil {
		return Attachment{}, result, stateErr
	}
	if scanErr != nil {
		return completed, result, scanErr
	}
	if result.Verdict != filecore.VerdictClean && result.Verdict != filecore.VerdictInfected {
		return completed, result, filecore.ErrScanIndeterminate
	}
	return completed, result, nil
}
