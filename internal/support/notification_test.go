package support

import (
	"context"
	"testing"
	"time"

	"github.com/Techshrr/GoJet/internal/workspace"
)

type fakeSupportNotificationProducer struct {
	input workspace.NotificationInput
	calls int
}

func (f *fakeSupportNotificationProducer) ProduceNotification(_ context.Context, input workspace.NotificationInput) (workspace.Notification, bool, error) {
	f.calls++
	f.input = input
	return workspace.Notification{WorkspaceID: input.WorkspaceID, RecipientUserID: input.RecipientUserID, Category: input.Category, EventKey: input.EventKey, DedupeKey: input.DedupeKey, Title: input.Title, Summary: input.Summary, DeepLink: input.DeepLink, ResourceType: input.ResourceType, ResourceID: input.ResourceID}, true, nil
}

func TestProduceSupportNotificationUsesP12SupportCategoryAndSafeDeepLink(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-notify-1", "ws-1", "user-1", "general", "Private subject should not enter notification summary", "corr-notify-1", now)
	if err != nil {
		t.Fatal(err)
	}
	producer := &fakeSupportNotificationProducer{}
	_, created, err := ProduceSupportNotification(context.Background(), producer, ticket, "ticket_reply_received", "msg-1")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if producer.calls != 1 || producer.input.Category != "support" || producer.input.RecipientUserID != ticket.RequesterUserID {
		t.Fatalf("producer input=%+v", producer.input)
	}
	if producer.input.DeepLink != "/app/support/"+ticket.ID || producer.input.ResourceType != "support_ticket" || producer.input.ResourceID != ticket.ID {
		t.Fatalf("deep-link/resource mismatch: %+v", producer.input)
	}
	if producer.input.Summary == ticket.Subject || producer.input.Title == ticket.Subject {
		t.Fatal("ticket subject leaked into notification")
	}
}

func TestProduceSupportNotificationSkipsPublicContactForP12WorkspaceCore(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	ticket := Ticket{ID: "tkt-public-1", PublicContactID: "pc-1", Category: "general", Subject: "Help", Status: TicketOpen, CreatedAt: now, UpdatedAt: now, Version: 1, CorrelationID: "corr-public-1"}
	if err := ticket.Validate(); err != nil {
		t.Fatal(err)
	}
	producer := &fakeSupportNotificationProducer{}
	_, created, err := ProduceSupportNotification(context.Background(), producer, ticket, "ticket_created", "1")
	if err != nil || created || producer.calls != 0 {
		t.Fatalf("created=%v calls=%d err=%v", created, producer.calls, err)
	}
}
