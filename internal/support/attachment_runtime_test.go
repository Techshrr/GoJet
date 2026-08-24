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

type memoryAttachmentStore struct {
	items         map[string]Attachment
	failFinalClean bool
}

func newMemoryAttachmentStore() *memoryAttachmentStore {
	return &memoryAttachmentStore{items: make(map[string]Attachment)}
}

func (s *memoryAttachmentStore) CreateAttachment(_ context.Context, attachment Attachment) error {
	if _, exists := s.items[attachment.ID]; exists {
		return ErrSupportConflict
	}
	s.items[attachment.ID] = attachment
	return nil
}

func (s *memoryAttachmentStore) GetAttachment(_ context.Context, attachmentID string) (Attachment, error) {
	attachment, ok := s.items[attachmentID]
	if !ok {
		return Attachment{}, ErrSupportNotFound
	}
	return attachment, nil
}

func (s *memoryAttachmentStore) TransitionAttachment(_ context.Context, attachmentID string, expected AttachmentStatus, next Attachment) error {
	current, ok := s.items[attachmentID]
	if !ok {
		return ErrSupportNotFound
	}
	if current.ScanStatus != expected {
		return ErrSupportConflict
	}
	if s.failFinalClean && expected == AttachmentScanning && next.ScanStatus == AttachmentClean {
		return ErrSupportConflict
	}
	s.items[attachmentID] = next
	return nil
}

type fixedAttachmentScanner struct {
	result filecore.ScanResult
	err    error
}

func (s fixedAttachmentScanner) Scan(_ context.Context, _ io.Reader) (filecore.ScanResult, error) {
	return s.result, s.err
}

func newAttachmentRuntimeFixture(t *testing.T, scanner P09AttachmentScanner) (*AttachmentRuntime, *memoryAttachmentStore, *filecore.NativeStorage) {
	t.Helper()
	store := newMemoryAttachmentStore()
	storage, err := filecore.NewNativeStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := filecore.ParseTypePolicy("txt=text/plain")
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewAttachmentRuntime(store, storage, policy, scanner, 64*1024)
	if err != nil {
		t.Fatal(err)
	}
	return runtime, store, storage
}

func TestAttachmentRuntimeCleanPublishesOnlyAfterCurrentVerdict(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	runtime, _, storage := newAttachmentRuntimeFixture(t, fixedAttachmentScanner{result: filecore.ScanResult{Verdict: filecore.VerdictClean}})
	attachment, err := runtime.Intake(context.Background(), "tkt-1", "msg-1", "evidence.txt", "text/plain", strings.NewReader("clean evidence"), now)
	if err != nil {
		t.Fatal(err)
	}
	if attachment.ScanStatus != AttachmentQuarantined || AttachmentDownloadAllowed(attachment) {
		t.Fatalf("intake state=%q", attachment.ScanStatus)
	}
	if _, err := storage.OpenPublished(attachment.StorageKey); err == nil {
		t.Fatal("quarantined bytes appeared in published storage")
	}

	clean, _, err := runtime.Scan(context.Background(), attachment.ID, now.Add(time.Second), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if clean.ScanStatus != AttachmentClean {
		t.Fatalf("scan state=%q", clean.ScanStatus)
	}
	_, file, err := runtime.OpenDownload(context.Background(), attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "clean evidence" {
		t.Fatalf("download body=%q", body)
	}
}

func TestAttachmentRuntimeInfectedNeverPublishes(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	runtime, _, storage := newAttachmentRuntimeFixture(t, fixedAttachmentScanner{result: filecore.ScanResult{Verdict: filecore.VerdictInfected}})
	attachment, err := runtime.Intake(context.Background(), "tkt-1", "msg-1", "evidence.txt", "text/plain", strings.NewReader("infected evidence"), now)
	if err != nil {
		t.Fatal(err)
	}
	infected, _, err := runtime.Scan(context.Background(), attachment.ID, now.Add(time.Second), now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if infected.ScanStatus != AttachmentInfected {
		t.Fatalf("scan state=%q", infected.ScanStatus)
	}
	if _, _, err := runtime.OpenDownload(context.Background(), attachment.ID); !errors.Is(err, ErrAttachmentBlocked) {
		t.Fatalf("download error=%v", err)
	}
	if _, err := storage.OpenPublished(attachment.StorageKey); err == nil {
		t.Fatal("infected bytes appeared in published storage")
	}
}

func TestAttachmentRuntimeScannerErrorFailsClosed(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	scanErr := errors.New("clamd unavailable")
	runtime, _, storage := newAttachmentRuntimeFixture(t, fixedAttachmentScanner{result: filecore.ScanResult{Verdict: filecore.VerdictError}, err: scanErr})
	attachment, err := runtime.Intake(context.Background(), "tkt-1", "msg-1", "evidence.txt", "text/plain", strings.NewReader("evidence"), now)
	if err != nil {
		t.Fatal(err)
	}
	failed, _, err := runtime.Scan(context.Background(), attachment.ID, now.Add(time.Second), now.Add(2*time.Second))
	if !errors.Is(err, scanErr) {
		t.Fatalf("scan error=%v", err)
	}
	if failed.ScanStatus != AttachmentScanError {
		t.Fatalf("scan state=%q", failed.ScanStatus)
	}
	if _, err := storage.OpenPublished(attachment.StorageKey); err == nil {
		t.Fatal("scan-error bytes appeared in published storage")
	}
}

func TestAttachmentRuntimeCleanCASFailureReturnsBytesToQuarantine(t *testing.T) {
	now := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	runtime, store, storage := newAttachmentRuntimeFixture(t, fixedAttachmentScanner{result: filecore.ScanResult{Verdict: filecore.VerdictClean}})
	attachment, err := runtime.Intake(context.Background(), "tkt-1", "msg-1", "evidence.txt", "text/plain", strings.NewReader("clean evidence"), now)
	if err != nil {
		t.Fatal(err)
	}
	store.failFinalClean = true
	if _, _, err := runtime.Scan(context.Background(), attachment.ID, now.Add(time.Second), now.Add(2*time.Second)); !errors.Is(err, ErrSupportConflict) {
		t.Fatalf("scan error=%v", err)
	}
	if _, err := storage.OpenPublished(attachment.StorageKey); err == nil {
		t.Fatal("failed clean CAS left bytes published")
	}
	file, err := storage.OpenQuarantine(attachment.StorageKey)
	if err != nil {
		t.Fatalf("quarantine missing after CAS failure: %v", err)
	}
	_ = file.Close()
}
