package support

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

type MailQueue interface {
	ClaimNext(ctx context.Context, rawClaimToken string, now time.Time) (ClaimedMail, error)
	LoadDelivery(ctx context.Context, claimed ClaimedMail) (MailDeliveryPayload, error)
	Complete(ctx context.Context, claimed ClaimedMail, rawClaimToken string, delivery MailDeliveryResult, now time.Time) (MailJob, error)
}

type MailSender interface {
	Send(ctx context.Context, recipient string, rendered RenderedMail) MailDeliveryResult
}

type MailWorker struct {
	queue  MailQueue
	sender MailSender
	now    func() time.Time
}

func NewMailWorker(queue MailQueue, sender MailSender) (*MailWorker, error) {
	if queue == nil || sender == nil {
		return nil, ErrInvalidInput
	}
	return &MailWorker{queue: queue, sender: sender, now: func() time.Time { return time.Now().UTC() }}, nil
}

// RunOnce claims at most one durable logical job. Raw claim material exists only
// in worker memory; MySQL stores only the SHA-256 claim hash.
func (w *MailWorker) RunOnce(ctx context.Context) (bool, error) {
	if w == nil || w.queue == nil || w.sender == nil || w.now == nil {
		return false, ErrInvalidInput
	}
	claimToken, err := newMailClaimToken()
	if err != nil {
		return false, err
	}
	claimed, err := w.queue.ClaimNext(ctx, claimToken, w.now())
	if errors.Is(err, ErrNoMailAvailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	payload, err := w.queue.LoadDelivery(ctx, claimed)
	if err != nil {
		_, completeErr := w.queue.Complete(ctx, claimed, claimToken, MailDeliveryResult{ErrorCode: "payload_invalid"}, w.now())
		if completeErr != nil {
			return true, completeErr
		}
		return true, err
	}
	rendered, err := RenderMailTemplate(payload.Template, payload.Values)
	if err != nil {
		_, completeErr := w.queue.Complete(ctx, claimed, claimToken, MailDeliveryResult{ErrorCode: "template_invalid"}, w.now())
		if completeErr != nil {
			return true, completeErr
		}
		return true, err
	}
	result := w.sender.Send(ctx, payload.Recipient, rendered)
	_, err = w.queue.Complete(ctx, claimed, claimToken, result, w.now())
	return true, err
}

func newMailClaimToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
