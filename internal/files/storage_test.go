package files

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeStorageLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	storage, err := NewNativeStorage(root)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	size, digest, err := storage.WriteQuarantine(key, bytes.NewBufferString("hello"), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 || len(digest) != 64 {
		t.Fatalf("unexpected write result: size=%d digest=%q", size, digest)
	}
	q, err := storage.OpenQuarantine(key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(q)
	_ = q.Close()
	if string(data) != "hello" {
		t.Fatalf("unexpected quarantine bytes: %q", data)
	}
	if err := storage.Publish(key); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.OpenQuarantine(key); !os.IsNotExist(err) {
		t.Fatalf("expected quarantine path absent after publish, got %v", err)
	}
	p, err := storage.OpenPublished(key)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	if err := storage.ReturnToQuarantine(key); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.OpenQuarantine(key); err != nil {
		t.Fatal(err)
	}
}

func TestStorageRejectsPathAuthority(t *testing.T) {
	storage, err := NewNativeStorage(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"../secret", strings.Repeat("A", 64), "abc", strings.Repeat("0", 63) + "/"} {
		if _, err := storage.OpenQuarantine(key); err == nil {
			t.Fatalf("expected invalid key rejection for %q", key)
		}
	}
}
