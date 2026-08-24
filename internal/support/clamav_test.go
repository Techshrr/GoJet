package support

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	filecore "github.com/Techshrr/GoJet/internal/files"
)

type fakeP09Scanner struct {
	result filecore.ScanResult
	err    error
	calls  int
}

func (f *fakeP09Scanner) Scan(_ context.Context, src io.Reader) (filecore.ScanResult, error) {
	f.calls++
	_, _ = io.ReadAll(src)
	return f.result, f.err
}

func newQuarantinedAttachment(now time.Time) Attachment {
	return Attachment{
		ID:               "att-p09-1",
		TicketID:         "tkt-p09-1",
		MessageID:        "msg-p09-1",
		StorageKey:       strings.Repeat("a", 64),
		OriginalNameSafe: "evidence.txt",
		MIMEType:         "text/plain",
		SizeBytes:        8,
		SHA256:           strings.Repeat("b", 64),
		ScanStatus:       AttachmentQuarantined,
		ScanUpdatedAt:    now,
		CreatedAt:        now,
	}
}

func TestP09CleanIsTheOnlyDownloadableScanOutcome(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		result       filecore.ScanResult
		scanErr      error
		wantStatus   AttachmentStatus
		wantDownload bool
		wantErr      error
	}{
		{name: "clean", result: filecore.ScanResult{Verdict: filecore.VerdictClean}, wantStatus: AttachmentClean, wantDownload: true},
		{name: "infected", result: filecore.ScanResult{Verdict: filecore.VerdictInfected, VerdictCode: "Eicar-Test-Signature"}, wantStatus: AttachmentInfected},
		{name: "scanner unavailable", result: filecore.ScanResult{Verdict: filecore.VerdictError, ErrorCode: "clamav_unavailable"}, scanErr: errors.New("dial failed"), wantStatus: AttachmentScanError},
		{name: "indeterminate", result: filecore.ScanResult{Verdict: filecore.VerdictError, ErrorCode: "indeterminate_response"}, wantStatus: AttachmentScanError, wantErr: filecore.ErrScanIndeterminate},
		{name: "unknown verdict", result: filecore.ScanResult{Verdict: filecore.ScanVerdict("future-value")}, wantStatus: AttachmentScanError, wantErr: filecore.ErrScanIndeterminate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scanner := &fakeP09Scanner{result: tc.result, err: tc.scanErr}
			attachment, _, err := ScanTicketAttachment(context.Background(), scanner, newQuarantinedAttachment(now), strings.NewReader("payload"), now.Add(time.Second), now.Add(2*time.Second))
			if tc.scanErr != nil {
				if !errors.Is(err, tc.scanErr) {
					t.Fatalf("scan error=%v want=%v", err, tc.scanErr)
				}
			} else if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("scan error=%v want=%v", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if scanner.calls != 1 {
				t.Fatalf("scanner calls=%d", scanner.calls)
			}
			if attachment.ScanStatus != tc.wantStatus {
				t.Fatalf("status=%q want=%q", attachment.ScanStatus, tc.wantStatus)
			}
			if AttachmentDownloadAllowed(attachment) != tc.wantDownload {
				t.Fatalf("downloadable=%v want=%v", AttachmentDownloadAllowed(attachment), tc.wantDownload)
			}
		})
	}
}

func TestP09ErrorOverridesOptimisticCleanVerdict(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	providerErr := errors.New("scanner transport failed after response")
	scanner := &fakeP09Scanner{result: filecore.ScanResult{Verdict: filecore.VerdictClean}, err: providerErr}
	attachment, _, err := ScanTicketAttachment(context.Background(), scanner, newQuarantinedAttachment(now), strings.NewReader("payload"), now.Add(time.Second), now.Add(2*time.Second))
	if !errors.Is(err, providerErr) {
		t.Fatalf("error=%v", err)
	}
	if attachment.ScanStatus != AttachmentScanError || AttachmentDownloadAllowed(attachment) {
		t.Fatalf("optimistic clean failed open: %+v", attachment)
	}
}

func TestP09ScannerIsNotCalledWhenAttachmentCannotEnterScanning(t *testing.T) {
	now := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	attachment := newQuarantinedAttachment(now)
	attachment.ScanStatus = AttachmentClean
	scanner := &fakeP09Scanner{result: filecore.ScanResult{Verdict: filecore.VerdictClean}}
	if _, _, err := ScanTicketAttachment(context.Background(), scanner, attachment, strings.NewReader("payload"), now.Add(time.Second), now.Add(2*time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error=%v", err)
	}
	if scanner.calls != 0 {
		t.Fatalf("scanner called for invalid state: %d", scanner.calls)
	}
}
