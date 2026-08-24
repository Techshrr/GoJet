package support

import (
	"strings"
	"testing"
)

func TestBuildMailFailureNotificationUsesP12SupportContract(t *testing.T) {
	job := MailJob{ID: "mail_123", Status: MailFailed}
	input, err := buildMailFailureNotification(job, "tkt_123", "ws-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if input.WorkspaceID != "ws-1" || input.RecipientUserID != "user-1" || input.Category != "support" || input.EventKey != "mail_delivery_failed" {
		t.Fatalf("unexpected notification authority: %+v", input)
	}
	if input.DeepLink != "/app/support/tkt_123" || input.ResourceType != "support_ticket" || input.ResourceID != "tkt_123" {
		t.Fatalf("unexpected notification resource: %+v", input)
	}
	if input.DedupeKey != "support:mail_delivery_failed:tkt_123:mail_123" || len(input.DedupeKey) > 160 {
		t.Fatalf("unexpected dedupe key %q", input.DedupeKey)
	}
	combined := strings.ToLower(input.Title + " " + input.Summary)
	for _, forbidden := range []string{"@", "smtp", "password", "secret", "turnstile", "internal_note"} {
		if strings.Contains(combined, forbidden) {
			t.Fatalf("notification text contains forbidden marker %q: %q", forbidden, combined)
		}
	}
}

func TestBuildMailFailureNotificationRejectsNonTerminalState(t *testing.T) {
	_, err := buildMailFailureNotification(MailJob{ID: "mail_123", Status: MailRetrying}, "tkt_123", "ws-1", "user-1")
	if err == nil {
		t.Fatal("retrying job unexpectedly produced terminal failure notification")
	}
}

func TestBuildMailFailureNotificationRequiresScopedRecipient(t *testing.T) {
	job := MailJob{ID: "mail_123", Status: MailFailed}
	for _, tc := range []struct {
		ticket string
		ws     string
		user   string
	}{
		{"", "ws-1", "user-1"},
		{"tkt_123", "", "user-1"},
		{"tkt_123", "ws-1", ""},
	} {
		if _, err := buildMailFailureNotification(job, tc.ticket, tc.ws, tc.user); err == nil {
			t.Fatalf("unscoped notification accepted: %+v", tc)
		}
	}
}
