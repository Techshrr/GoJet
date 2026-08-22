package files

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHealthAuthorityReportsActionableSecretSafeState(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quarantine", "published"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		buffer := make([]byte, 128)
		_, _ = conn.Read(buffer)
		date := time.Now().UTC().Format("Mon Jan _2 15:04:05 2006")
		_, _ = fmt.Fprintf(conn, "ClamAV 1.5.3/28100/%s\x00", date)
	}()
	client, err := NewClamAVClient("tcp", listener.Addr().String(), time.Second, time.Second, 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewHealthAuthority(root, client)
	if err != nil {
		t.Fatal(err)
	}
	report := authority.Check(context.Background())
	if !report.Ready || report.Status != DependencyHealthy || report.Storage.State != DependencyHealthy || report.ClamAV.State != DependencyHealthy {
		t.Fatalf("unexpected report: %+v", report)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	if strings.Contains(text, root) || strings.Contains(text, listener.Addr().String()) {
		t.Fatalf("health report leaked private path/socket detail: %s", text)
	}
}

func TestStorageHealthRejectsPermissionDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"quarantine", "published"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "quarantine"), 0o755); err != nil {
		t.Fatal(err)
	}
	health := checkStorageHealth(root)
	if health.State != DependencyPermissionError || health.Writable {
		t.Fatalf("permission drift must fail closed: %+v", health)
	}
}
