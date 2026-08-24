package support

import "context"

func (s *AuditedSupportStore) ListTicketMessages(ctx context.Context, ticketID string, includeInternal bool) ([]TicketMessage, error) {
	return s.inner.ListTicketMessages(ctx, ticketID, includeInternal)
}
