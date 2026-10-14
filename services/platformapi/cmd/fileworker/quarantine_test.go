package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	filedomain "github.com/Techshrr/GoJet/internal/files"
)

func TestClaimedQuarantineRecoversPublishedBytes(t *testing.T) {
	storage, err := filedomain.NewNativeStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	original := []byte("original bytes retained after administrator quarantine")
	if _, _, err := storage.WriteQuarantine(key, bytes.NewReader(original), 1024); err != nil {
		t.Fatal(err)
	}
	if err := storage.Publish(key); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		file, err := openClaimedQuarantine(storage, key)
		if err != nil {
			t.Fatal(err)
		}
		got, readErr := io.ReadAll(file)
		_ = file.Close()
		if readErr != nil || !bytes.Equal(got, original) {
			t.Fatalf("recovery changed original bytes: %v", readErr)
		}
		published, err := storage.OpenPublished(key)
		if published != nil {
			_ = published.Close()
		}
		if !os.IsNotExist(err) {
			t.Fatalf("recovered bytes remain in published storage: %v", err)
		}
	}
}

func TestClaimedQuarantineRejectsMissingAndInvalidKeys(t *testing.T) {
	storage, err := filedomain.NewNativeStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{strings.Repeat("b", 64), "../published/object", ""} {
		file, err := openClaimedQuarantine(storage, key)
		if file != nil {
			_ = file.Close()
		}
		if err == nil || file != nil {
			t.Fatalf("unavailable or invalid object gained scan bytes: %q", key)
		}
	}
}
