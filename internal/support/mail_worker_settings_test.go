package support

import (
	"context"
	"errors"
	"testing"
)

type gatedFakeMailQueue struct {
	*fakeMailQueue
	enabled bool
	gateErr error
	checks  int
}

func (q *gatedFakeMailQueue) MailDispatchEnabled(_ context.Context) (bool, error) {
	q.checks++
	return q.enabled, q.gateErr
}

func TestMailWorkerDisabledGateDoesNotClaimOrSend(t *testing.T) {
	base := &fakeMailQueue{}
	queue := &gatedFakeMailQueue{fakeMailQueue: base, enabled: false}
	sender := &fakeMailSender{}
	worker, err := NewMailWorker(queue, sender)
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if err != nil || worked {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if queue.checks != 1 || base.claimToken != "" || sender.calls != 0 || base.completed {
		t.Fatalf("checks=%d claim=%q sender=%d completed=%v", queue.checks, base.claimToken, sender.calls, base.completed)
	}
}

func TestMailWorkerGateErrorFailsClosedBeforeClaim(t *testing.T) {
	gateErr := errors.New("mail settings unavailable")
	base := &fakeMailQueue{}
	queue := &gatedFakeMailQueue{fakeMailQueue: base, enabled: true, gateErr: gateErr}
	sender := &fakeMailSender{}
	worker, err := NewMailWorker(queue, sender)
	if err != nil {
		t.Fatal(err)
	}
	worked, err := worker.RunOnce(context.Background())
	if worked || !errors.Is(err, gateErr) {
		t.Fatalf("worked=%v err=%v", worked, err)
	}
	if queue.checks != 1 || base.claimToken != "" || sender.calls != 0 || base.completed {
		t.Fatalf("checks=%d claim=%q sender=%d completed=%v", queue.checks, base.claimToken, sender.calls, base.completed)
	}
}
