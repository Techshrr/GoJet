package support

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Store) CreateAttachment(ctx context.Context, attachment Attachment) error {
	if s == nil || s.db == nil {
		return ErrInvalidInput
	}
	if err := attachment.Validate(); err != nil || attachment.ScanStatus != AttachmentQuarantined {
		return ErrInvalidInput
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO support_ticket_attachments
(id,ticket_id,message_id,storage_key,original_name_safe,mime_type,size_bytes,sha256,scan_status,scan_updated_at,created_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		attachment.ID, attachment.TicketID, attachment.MessageID, attachment.StorageKey,
		attachment.OriginalNameSafe, attachment.MIMEType, attachment.SizeBytes, attachment.SHA256,
		string(attachment.ScanStatus), attachment.ScanUpdatedAt, attachment.CreatedAt)
	return err
}

func (s *Store) GetAttachment(ctx context.Context, attachmentID string) (Attachment, error) {
	if s == nil || s.db == nil || attachmentID == "" {
		return Attachment{}, ErrInvalidInput
	}
	var attachment Attachment
	var status string
	err := s.db.QueryRowContext(ctx, `
SELECT id,ticket_id,message_id,storage_key,original_name_safe,mime_type,size_bytes,sha256,scan_status,scan_updated_at,created_at
FROM support_ticket_attachments WHERE id=?`, attachmentID).
		Scan(&attachment.ID, &attachment.TicketID, &attachment.MessageID, &attachment.StorageKey,
			&attachment.OriginalNameSafe, &attachment.MIMEType, &attachment.SizeBytes, &attachment.SHA256,
			&status, &attachment.ScanUpdatedAt, &attachment.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Attachment{}, ErrSupportNotFound
	}
	if err != nil {
		return Attachment{}, err
	}
	attachment.ScanStatus = AttachmentStatus(status)
	if err := attachment.Validate(); err != nil {
		return Attachment{}, err
	}
	return attachment, nil
}

func (s *Store) TransitionAttachment(ctx context.Context, attachmentID string, expected AttachmentStatus, next Attachment) error {
	if s == nil || s.db == nil || attachmentID == "" || next.ID != attachmentID {
		return ErrInvalidInput
	}
	if err := next.Validate(); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE support_ticket_attachments
SET scan_status=?,scan_updated_at=?
WHERE id=? AND scan_status=?`, string(next.ScanStatus), next.ScanUpdatedAt, attachmentID, string(expected))
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrSupportConflict
	}
	return nil
}
