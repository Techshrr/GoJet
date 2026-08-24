package support

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type SupportNotificationProducer interface {
	ProduceNotification(ctx context.Context, input workspace.NotificationInput) (workspace.Notification, bool, error)
}

func ProduceSupportNotification(ctx context.Context, producer SupportNotificationProducer, ticket Ticket, eventKey, dedupeIdentity string) (workspace.Notification, bool, error) {
	if producer == nil {
		return workspace.Notification{}, false, ErrInvalidInput
	}
	if err := ticket.Validate(); err != nil {
		return workspace.Notification{}, false, err
	}
	if ticket.WorkspaceID == "" || ticket.RequesterUserID == "" {
		return workspace.Notification{}, false, nil
	}
	eventKey = strings.TrimSpace(eventKey)
	dedupeIdentity = strings.TrimSpace(dedupeIdentity)
	if dedupeIdentity == "" {
		dedupeIdentity = strconv.FormatUint(ticket.Version, 10)
	}
	var title, summary string
	switch eventKey {
	case "ticket_created":
		title, summary = "Support ticket created", "Your support ticket is open."
	case "ticket_reply_received":
		title, summary = "Support reply received", "Your support ticket has a new reply."
	case "ticket_reply_sent":
		title, summary = "Support reply sent", "Your support reply was recorded."
	case "ticket_closed":
		title, summary = "Support ticket closed", "Your support ticket is closed."
	case "mail_delivery_failed":
		title, summary = "Support email delivery failed", "A support email could not be delivered. Check the support thread for current status."
	default:
		return workspace.Notification{}, false, ErrInvalidInput
	}
	dedupeKey := fmt.Sprintf("support:%s:%s:%s", eventKey, ticket.ID, dedupeIdentity)
	if len(dedupeKey) > 160 {
		return workspace.Notification{}, false, ErrInvalidInput
	}
	return producer.ProduceNotification(ctx, workspace.NotificationInput{
		WorkspaceID:     ticket.WorkspaceID,
		RecipientUserID: ticket.RequesterUserID,
		Category:        "support",
		EventKey:        eventKey,
		DedupeKey:       dedupeKey,
		Title:           title,
		Summary:         summary,
		DeepLink:        "/app/support/" + ticket.ID,
		ResourceType:    "support_ticket",
		ResourceID:      ticket.ID,
	})
}
