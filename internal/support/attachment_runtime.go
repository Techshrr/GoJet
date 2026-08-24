package support

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"time"

	filecore "github.com/Techshrr/GoJet/internal/files"
)

type AttachmentPersistence interface {
	CreateAttachment(ctx context.Context, attachment Attachment) error
	GetAttachment(ctx context.Context, attachmentID string) (Attachment, error)
	TransitionAttachment(ctx context.Context, attachmentID string, expected AttachmentStatus, next Attachment) error
}

type AttachmentStorage interface {
	WriteQuarantine(key string, src io.Reader, maxBytes int64) (uint64, string, error)
	OpenQuarantine(key string) (*os.File, error)
	OpenPublished(key string) (*os.File, error)
	Publish(key string) error
	ReturnToQuarantine(key string) error
	Remove(key string) error
}

type AttachmentTypePolicy interface {
	Validate(originalName, declaredMIME string, prefix []byte) (string, string, string, error)
}

type AttachmentRuntime struct {
	store    AttachmentPersistence
	storage  AttachmentStorage
	policy   AttachmentTypePolicy
	scanner  P09AttachmentScanner
	maxBytes int64
}

func NewAttachmentRuntime(store AttachmentPersistence, storage AttachmentStorage, policy AttachmentTypePolicy, scanner P09AttachmentScanner, maxBytes int64) (*AttachmentRuntime, error) {
	if store == nil || storage == nil || policy == nil || scanner == nil || maxBytes <= 0 {
		return nil, ErrInvalidInput
	}
	return &AttachmentRuntime{store: store, storage: storage, policy: policy, scanner: scanner, maxBytes: maxBytes}, nil
}

func (r *AttachmentRuntime) Intake(ctx context.Context, ticketID, messageID, originalName, declaredMIME string, src io.Reader, now time.Time) (Attachment, error) {
	if r == nil || r.store == nil || r.storage == nil || r.policy == nil || src == nil || now.IsZero() {
		return Attachment{}, ErrInvalidInput
	}
	prefix := make([]byte, 512)
	n, readErr := io.ReadFull(src, prefix)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return Attachment{}, ErrInvalidInput
	}
	prefix = prefix[:n]
	if len(prefix) == 0 {
		return Attachment{}, ErrInvalidInput
	}
	name, normalizedMIME, _, err := r.policy.Validate(originalName, declaredMIME, prefix)
	if err != nil {
		return Attachment{}, ErrInvalidInput
	}
	storageKey, err := filecore.NewStorageKey()
	if err != nil {
		return Attachment{}, err
	}
	size, digest, err := r.storage.WriteQuarantine(storageKey, io.MultiReader(bytes.NewReader(prefix), src), r.maxBytes)
	if err != nil {
		return Attachment{}, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = r.storage.Remove(storageKey)
		}
	}()
	attachmentID, err := newOpaqueID("att")
	if err != nil {
		return Attachment{}, err
	}
	now = now.UTC()
	attachment := Attachment{
		ID: attachmentID, TicketID: ticketID, MessageID: messageID, StorageKey: storageKey,
		OriginalNameSafe: name, MIMEType: normalizedMIME, SizeBytes: size, SHA256: digest,
		ScanStatus: AttachmentQuarantined, ScanUpdatedAt: now, CreatedAt: now,
	}
	if err := attachment.Validate(); err != nil {
		return Attachment{}, err
	}
	if err := r.store.CreateAttachment(ctx, attachment); err != nil {
		return Attachment{}, err
	}
	cleanup = false
	return attachment, nil
}

func (r *AttachmentRuntime) Scan(ctx context.Context, attachmentID string, startedAt, completedAt time.Time) (Attachment, filecore.ScanResult, error) {
	if r == nil || r.store == nil || r.storage == nil || r.scanner == nil || startedAt.IsZero() || completedAt.IsZero() {
		return Attachment{}, filecore.ScanResult{}, ErrInvalidInput
	}
	current, err := r.store.GetAttachment(ctx, attachmentID)
	if err != nil {
		return Attachment{}, filecore.ScanResult{}, err
	}
	scanning, err := BeginAttachmentScan(current, startedAt.UTC())
	if err != nil {
		return Attachment{}, filecore.ScanResult{}, err
	}
	if err := r.store.TransitionAttachment(ctx, current.ID, current.ScanStatus, scanning); err != nil {
		return Attachment{}, filecore.ScanResult{}, err
	}
	file, err := r.storage.OpenQuarantine(current.StorageKey)
	if err != nil {
		final, stateErr := CompleteAttachmentScan(scanning, AttachmentScanError, completedAt.UTC())
		if stateErr == nil {
			stateErr = r.store.TransitionAttachment(ctx, current.ID, AttachmentScanning, final)
		}
		if stateErr != nil {
			return Attachment{}, filecore.ScanResult{}, stateErr
		}
		return final, filecore.ScanResult{Verdict: filecore.VerdictError, ErrorCode: "quarantine_unavailable"}, err
	}
	result, scanErr := r.scanner.Scan(ctx, file)
	_ = file.Close()
	final, stateErr := ApplyP09ScanOutcome(scanning, result, scanErr, completedAt.UTC())
	if stateErr != nil {
		return Attachment{}, result, stateErr
	}
	if final.ScanStatus == AttachmentClean {
		if err := r.storage.Publish(current.StorageKey); err != nil {
			failed, failStateErr := CompleteAttachmentScan(scanning, AttachmentScanError, completedAt.UTC())
			if failStateErr == nil {
				failStateErr = r.store.TransitionAttachment(ctx, current.ID, AttachmentScanning, failed)
			}
			if failStateErr != nil {
				return Attachment{}, result, failStateErr
			}
			return failed, result, err
		}
		if err := r.store.TransitionAttachment(ctx, current.ID, AttachmentScanning, final); err != nil {
			_ = r.storage.ReturnToQuarantine(current.StorageKey)
			return Attachment{}, result, err
		}
	} else if err := r.store.TransitionAttachment(ctx, current.ID, AttachmentScanning, final); err != nil {
		return Attachment{}, result, err
	}
	if scanErr != nil {
		return final, result, scanErr
	}
	if result.Verdict != filecore.VerdictClean && result.Verdict != filecore.VerdictInfected {
		return final, result, filecore.ErrScanIndeterminate
	}
	return final, result, nil
}

func (r *AttachmentRuntime) OpenDownload(ctx context.Context, attachmentID string) (Attachment, *os.File, error) {
	if r == nil || r.store == nil || r.storage == nil {
		return Attachment{}, nil, ErrInvalidInput
	}
	attachment, err := r.store.GetAttachment(ctx, attachmentID)
	if err != nil {
		return Attachment{}, nil, err
	}
	if err := RequireAttachmentDownloadable(attachment); err != nil {
		return Attachment{}, nil, err
	}
	file, err := r.storage.OpenPublished(attachment.StorageKey)
	if err != nil {
		return Attachment{}, nil, ErrAttachmentBlocked
	}
	return attachment, file, nil
}
