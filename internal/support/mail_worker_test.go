package support

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeMailQueue struct {
	claimed       ClaimedMail
	claimErr      error
	payload       MailDeliveryPayload
	loadErr       error
	completed     bool
	completeInput MailDeliveryResult
	claimToken    string
}

func (f *fakeMailQueue) ClaimNext(_ context.Context, rawClaimToken string, _ time.Time) (ClaimedMail, error) {
	f.claimToken = rawClaimToken
	return f.claimed, f.claimErr
}

func (f *fakeMailQueue) LoadDelivery(_ context.Context, _ ClaimedMail) (MailDeliveryPayload, error) {
	return f.payload, f.loadErr
}

func (f *fakeMailQueue) Complete(_ context.Context, claimed ClaimedMail, rawClaimToken string, delivery MailDeliveryResult, now time.Time) (MailJob, error) {
	f.completed = true
	f.completeInput = delivery
	if rawClaimToken == "" || rawClaimToken != f.claimToken {
		return MailJob{}, ErrMailClaim
	}
	job := claimed.Job
	job.Status = MailSent
	job.UpdatedAt = now.UTC()
	return job, nil
}

type fakeMailSender struct {
	result    MailDeliveryResult
	calls     int
	recipient string
	rendered  RenderedMail
}

func (f *fakeMailSender) Send(_ context.Context, recipient string, rendered RenderedMail) MailDeliveryResult {
	f.calls++
	f.recipient = recipient
	f.rendered = rendered
	return f.result
}

func TestMailWorkerNoJobIsNotFailure(t *testing.T) {
	queue := &fakeMailQueue{claimErr: ErrNoMailAvailable}
	sender := &fakeMailSender{}
	worker, err := NewMailWorker(queue, sender)
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if sender.calls != 0 || queue.completed {
		t.Fatal("empty queue reached sender or completion")
	}
}

func TestMailWorkerRendersSendsAndCompletesOnce(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	job := MailJob{ID: "mail-1", TemplateKey: "ticket-reply", TemplateVersion: 1, RecipientKind: "requester", RecipientValue: "user@example.test", ResourceType: "ticket", ResourceID: "tkt-1", Status: MailSending, AttemptCount: 1, CreatedAt: now, UpdatedAt: now}
	queue := &fakeMailQueue{
		claimed: ClaimedMail{Job: job, Locale: "en"},
		payload: MailDeliveryPayload{
			Template: MailTemplate{Key: "ticket-reply", Version: 1, SubjectTemplate: "Ticket {{ticket_id}}", TextTemplate: "Hello {{display_name}}", HTMLTemplate: "<p>Hello {{display_name}}</p>", VariableAllowlist: []string{"ticket_id", "display_name"}},
			Values: map[string]string{"ticket_id": "tkt-1", "display_name": "Ethan"}, Recipient: "user@example.test",
		},
	}
	sender := &fakeMailSender{result: MailDeliveryResult{Success: true}}
	worker, err := NewMailWorker(queue, sender)
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return now.Add(time.Second) }
	worked, err := worker.RunOnce(context.Background())
	if err != nil || !worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if sender.calls != 1 || sender.recipient != "user@example.test" {
		t.Fatalf("sender calls=%d recipient=%q", sender.calls, sender.recipient)
	}
	if sender.rendered.Subject != "Ticket tkt-1" || !queue.completed || !queue.completeInput.Success {
		t.Fatalf("render/completion mismatch: rendered=%+v completion=%+v", sender.rendered, queue.completeInput)
	}
}

func TestMailWorkerTemplateFailureCompletesTerminalWithoutSend(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	job := MailJob{ID: "mail-2", TemplateKey: "bad", TemplateVersion: 1, RecipientKind: "requester", RecipientValue: "user@example.test", ResourceType: "ticket", ResourceID: "tkt-1", Status: MailSending, AttemptCount: 1, CreatedAt: now, UpdatedAt: now}
	queue := &fakeMailQueue{
		claimed: ClaimedMail{Job: job, Locale: "en"},
		payload: MailDeliveryPayload{Template: MailTemplate{Key: "bad", Version: 1, SubjectTemplate: "{{missing}}", VariableAllowlist: []string{"missing"}}, Values: map[string]string{}, Recipient: "user@example.test"},
	}
	sender := &fakeMailSender{}
	worker, err := NewMailWorker(queue, sender)
	if err != nil {
		t.Fatal(err)
	}
	worker.now = func() time.Time { return now.Add(time.Second) }
	worked, err := worker.RunOnce(context.Background())
	if !worked || !errors.Is(err, ErrTemplateVariable) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if sender.calls != 0 || !queue.completed || queue.completeInput.ErrorCode != "template_invalid" || queue.completeInput.Transient {
		t.Fatalf("sender/completion mismatch calls=%d completed=%v result=%+v", sender.calls, queue.completed, queue.completeInput)
	}
}
