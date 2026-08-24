package support

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCustomDomainTicketProjectsRequestOnlyAcrossLifecycle(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt_opaque_01", "ws-1", "user-1", CustomDomainAccessCategory, "Request domain access", "corr-1", now)
	if err != nil {
		t.Fatal(err)
	}
	assertRequestOnlyProjection(t, ticket)

	requester := TicketMessage{ID: "msg-1", TicketID: ticket.ID, ActorType: ActorRequester, ActorID: "user-1", Kind: MessageRequesterReply, Body: "Please review", CreatedAt: now.Add(time.Second), CorrelationID: "corr-2"}
	ticket, err = ApplyTicketMessage(ticket, requester, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != TicketAwaitingSupport {
		t.Fatalf("requester reply status=%q", ticket.Status)
	}
	assertRequestOnlyProjection(t, ticket)

	supportReply := TicketMessage{ID: "msg-2", TicketID: ticket.ID, ActorType: ActorSupport, ActorID: "agent-1", Kind: MessageSupportReply, Body: "We are reviewing it", CreatedAt: now.Add(3 * time.Second), CorrelationID: "corr-3"}
	ticket, err = ApplyTicketMessage(ticket, supportReply, now.Add(4*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != TicketAwaitingUser {
		t.Fatalf("support reply status=%q", ticket.Status)
	}
	assertRequestOnlyProjection(t, ticket)

	note := TicketMessage{ID: "msg-3", TicketID: ticket.ID, ActorType: ActorSupport, ActorID: "agent-1", Kind: MessageInternalNote, Body: "Internal only", CreatedAt: now.Add(5 * time.Second), CorrelationID: "corr-4"}
	before := ticket.Status
	ticket, err = ApplyTicketMessage(ticket, note, now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != before {
		t.Fatalf("internal note changed status from %q to %q", before, ticket.Status)
	}
	assertRequestOnlyProjection(t, ticket)

	ticket, err = CloseTicket(ticket, now.Add(7*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != TicketClosedStatus {
		t.Fatalf("closed status=%q", ticket.Status)
	}
	assertRequestOnlyProjection(t, ticket)
}

func assertRequestOnlyProjection(t *testing.T, ticket Ticket) {
	t.Helper()
	projection, err := ProjectDomainAccessRequest(ticket)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Status != "requested" || projection.Source != "none" || projection.GrantAuthority != "NONE" {
		t.Fatalf("unexpected projection: %+v", projection)
	}
}

func TestMessageActorKindBoundaryAndClosedFailClosed(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	ticket, err := NewWorkspaceTicket("tkt-2", "ws-1", "user-1", "general", "Help", "corr-1", now)
	if err != nil {
		t.Fatal(err)
	}
	forged := TicketMessage{ID: "msg-x", TicketID: ticket.ID, ActorType: ActorRequester, ActorID: "user-1", Kind: MessageInternalNote, Body: "forged", CreatedAt: now.Add(time.Second), CorrelationID: "corr-x"}
	if _, err := ApplyTicketMessage(ticket, forged, now.Add(2*time.Second)); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("forged internal note error=%v", err)
	}
	closed, err := CloseTicket(ticket, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	reply := TicketMessage{ID: "msg-y", TicketID: ticket.ID, ActorType: ActorRequester, ActorID: "user-1", Kind: MessageRequesterReply, Body: "late", CreatedAt: now.Add(4 * time.Second), CorrelationID: "corr-y"}
	if _, err := ApplyTicketMessage(closed, reply, now.Add(5*time.Second)); !errors.Is(err, ErrTicketClosed) {
		t.Fatalf("closed reply error=%v", err)
	}
}

func TestAttachmentOnlyCleanIsDownloadable(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	base := Attachment{ID: "att-1", TicketID: "tkt-1", MessageID: "msg-1", StorageKey: strings.Repeat("a", 64), OriginalNameSafe: "evidence.txt", MIMEType: "text/plain", SizeBytes: 12, SHA256: strings.Repeat("b", 64), ScanStatus: AttachmentQuarantined, CreatedAt: now, ScanUpdatedAt: now}
	if AttachmentDownloadAllowed(base) {
		t.Fatal("quarantined attachment became downloadable")
	}
	scanning, err := BeginAttachmentScan(base, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if AttachmentDownloadAllowed(scanning) {
		t.Fatal("scanning attachment became downloadable")
	}
	infected, err := CompleteAttachmentScan(scanning, AttachmentInfected, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if AttachmentDownloadAllowed(infected) {
		t.Fatal("infected attachment became downloadable")
	}

	scanning, err = BeginAttachmentScan(Attachment{ID: "att-2", TicketID: "tkt-1", MessageID: "msg-1", StorageKey: strings.Repeat("c", 64), OriginalNameSafe: "safe.txt", MIMEType: "text/plain", SizeBytes: 4, SHA256: strings.Repeat("d", 64), ScanStatus: AttachmentQuarantined, CreatedAt: now, ScanUpdatedAt: now}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	clean, err := CompleteAttachmentScan(scanning, AttachmentClean, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !AttachmentDownloadAllowed(clean) {
		t.Fatal("clean attachment was not downloadable")
	}

	scanning, err = BeginAttachmentScan(Attachment{ID: "att-3", TicketID: "tkt-1", MessageID: "msg-1", StorageKey: strings.Repeat("e", 64), OriginalNameSafe: "error.txt", MIMEType: "text/plain", SizeBytes: 4, SHA256: strings.Repeat("f", 64), ScanStatus: AttachmentQuarantined, CreatedAt: now, ScanUpdatedAt: now}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	scanError, err := CompleteAttachmentScan(scanning, AttachmentScanError, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if AttachmentDownloadAllowed(scanError) {
		t.Fatal("scanner error failed open")
	}
}

type recordingVerifier struct {
	token string
	ok    bool
}

func (v *recordingVerifier) Verify(_ context.Context, token string) (TurnstileVerification, error) {
	v.token = token
	return TurnstileVerification{Success: v.ok}, nil
}

type recordingReplayStore struct {
	digest [32]byte
	claim  bool
}

func (s *recordingReplayStore) ClaimDigest(_ context.Context, digest [32]byte) (bool, error) {
	s.digest = digest
	return s.claim, nil
}

func TestTurnstileReplayStoreReceivesDigestOnly(t *testing.T) {
	raw := "0x-turnstile-sensitive-token"
	verifier := &recordingVerifier{ok: true}
	replay := &recordingReplayStore{claim: true}
	if err := VerifyProtectedSubmission(context.Background(), raw, verifier, replay); err != nil {
		t.Fatal(err)
	}
	if verifier.token != raw {
		t.Fatalf("verifier did not receive provider token")
	}
	want := sha256.Sum256([]byte(raw))
	if replay.digest != want {
		t.Fatalf("replay digest mismatch")
	}
	if string(replay.digest[:]) == raw {
		t.Fatal("raw token reached replay store")
	}

	replay.claim = false
	if err := VerifyProtectedSubmission(context.Background(), raw, verifier, replay); !errors.Is(err, ErrTurnstileReplay) {
		t.Fatalf("replay error=%v", err)
	}
}

func TestMailTemplateAllowlistEscapingAndSensitiveRejection(t *testing.T) {
	template := MailTemplate{Key: "ticket-reply", Version: 1, SubjectTemplate: "Reply for {{ticket_id}}", TextTemplate: "Hello {{display_name}}", HTMLTemplate: "<p>Hello {{display_name}}</p>", VariableAllowlist: []string{"ticket_id", "display_name"}}
	rendered, err := RenderMailTemplate(template, map[string]string{"ticket_id": "tkt-1", "display_name": `<img src=x onerror="bad">`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.HTML, "<img") || !strings.Contains(rendered.HTML, "&lt;img") {
		t.Fatalf("HTML variable was not escaped: %q", rendered.HTML)
	}
	if _, err := RenderMailTemplate(template, map[string]string{"ticket_id": "tkt-1", "display_name": "A", "smtp_password": "secret"}); !errors.Is(err, ErrTemplateVariable) {
		t.Fatalf("unknown variable error=%v", err)
	}
	badTemplate := MailTemplate{Key: "bad", Version: 1, SubjectTemplate: "{{access_token}}", VariableAllowlist: []string{"access_token"}}
	if _, err := RenderMailTemplate(badTemplate, map[string]string{"access_token": "secret"}); !errors.Is(err, ErrSensitiveVariable) {
		t.Fatalf("sensitive variable error=%v", err)
	}
}

func TestMailClaimHashRetryAndTerminalBound(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	logical, err := MailLogicalIdempotencyHash("ticket-reply", 1, "requester", "ticket", "tkt-1")
	if err != nil {
		t.Fatal(err)
	}
	job := MailJob{ID: "mail-1", TemplateKey: "ticket-reply", TemplateVersion: 1, RecipientKind: "requester", RecipientValue: "user@example.test", ResourceType: "ticket", ResourceID: "tkt-1", Status: MailQueued, IdempotencyKeyHash: logical, CreatedAt: now, UpdatedAt: now}
	claimToken := "claim-token-must-not-be-stored-raw"

	for attempt := uint32(1); attempt <= DefaultMailMaxAttempts; attempt++ {
		job, err = ClaimMailJob(job, claimToken, now)
		if err != nil {
			t.Fatalf("claim attempt %d: %v", attempt, err)
		}
		wantClaimHash := sha256.Sum256([]byte(claimToken))
		if job.ClaimTokenHash != wantClaimHash {
			t.Fatalf("claim hash mismatch on attempt %d", attempt)
		}
		job, err = CompleteMailJob(job, claimToken, MailDeliveryResult{Transient: true, ErrorCode: "smtp_temp"}, now.Add(time.Second))
		if err != nil {
			t.Fatalf("complete attempt %d: %v", attempt, err)
		}
		if attempt < DefaultMailMaxAttempts {
			if job.Status != MailRetrying || job.NextAttemptAt == nil {
				t.Fatalf("attempt %d did not enter retrying: %+v", attempt, job)
			}
			now = job.NextAttemptAt.UTC()
			continue
		}
		if job.Status != MailFailed || job.NextAttemptAt != nil {
			t.Fatalf("terminal attempt did not fail durably: %+v", job)
		}
	}
	if job.AttemptCount != DefaultMailMaxAttempts {
		t.Fatalf("attempt count=%d", job.AttemptCount)
	}
}

func TestMailLogicalIdempotencyIsDeterministicAndScoped(t *testing.T) {
	a, err := MailLogicalIdempotencyHash("ticket-reply", 1, "requester", "ticket", "tkt-1")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := MailLogicalIdempotencyHash("ticket-reply", 1, "requester", "ticket", "tkt-1")
	c, _ := MailLogicalIdempotencyHash("ticket-reply", 1, "requester", "ticket", "tkt-2")
	if a != b {
		t.Fatal("same logical mail produced different idempotency hashes")
	}
	if a == c {
		t.Fatal("different resource identity produced same logical hash")
	}
}
