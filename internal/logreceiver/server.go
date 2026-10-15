// Package logreceiver accepts bounded structured records from same-account
// services over a private Unix socket. It creates no public API or audit store.
package logreceiver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/logging"
)

var services = map[string]bool{
	"redirectengine": true, "analyticsworker": true, "analyticsreconciler": true,
	"platformapi": true, "mailworker": true, "fileworker": true,
	"operationsmonitor": true, "logreceiver": true,
}

// Open requires an existing private directory. Never removes an occupied or
// stale path: doing so could disconnect a live receiver or follow a symlink.
func Open(socket string) (*net.UnixListener, error) {
	if !filepath.IsAbs(socket) {
		return nil, errors.New("absolute private socket required")
	}
	dir, err := os.Lstat(filepath.Dir(socket))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		return nil, errors.New("private socket directory required")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		return nil, errors.New("socket bind failed")
	}
	if err := os.Chmod(socket, 0600); err != nil {
		listener.Close()
		return nil, errors.New("socket permission failed")
	}
	return listener, nil
}

// Serve bounds concurrent clients, size and read time; a bad client cannot
// inject raw bytes into output or hold shutdown indefinitely.
func Serve(ctx context.Context, listener *net.UnixListener, out io.Writer) error {
	var wg sync.WaitGroup
	var outputMu sync.Mutex
	slots := make(chan struct{}, 16)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			listener.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer wg.Wait()
	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return errors.New("receiver accept failed")
		}
		select {
		case slots <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(time.Second))
				reader := bufio.NewReader(io.LimitReader(conn, logging.MaxRecordBytes+1))
				line, err := reader.ReadBytes('\n')
				if err != nil || len(line) > logging.MaxRecordBytes {
					return
				}
				outputMu.Lock()
				err = Receive(ctx, out, line)
				outputMu.Unlock()
				if err == nil {
					io.WriteString(conn, "{\"ok\":true}\n")
				}
			}()
		default:
			conn.Close()
		}
	}
}

// Receive re-applies redaction; only authenticated local service processes may
// supply messages/IDs. Free-form user content is not an allowed producer field.
func Receive(ctx context.Context, out io.Writer, raw []byte) error {
	if len(raw) > logging.MaxRecordBytes {
		return errors.New("record too large")
	}
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil {
		return errors.New("invalid record")
	}
	var kind string
	json.Unmarshal(row["type"], &kind)
	if kind == "health" && len(row) == 1 {
		return nil
	}
	var service, message, levelText, sourceTime string
	json.Unmarshal(row["service_id"], &service)
	json.Unmarshal(row["msg"], &message)
	json.Unmarshal(row["level"], &levelText)
	json.Unmarshal(row["time"], &sourceTime)
	if !services[service] || message == "" || len(message) > 1024 || strings.ContainsAny(message, "\r\n") {
		return errors.New("invalid service record")
	}
	if _, err := time.Parse(time.RFC3339Nano, sourceTime); err != nil {
		return errors.New("invalid time")
	}
	levels := map[string]slog.Level{"INFO": slog.LevelInfo, "WARN": slog.LevelWarn, "ERROR": slog.LevelError}
	level, ok := levels[levelText]
	if !ok {
		return errors.New("invalid level")
	}
	attrs := []slog.Attr{slog.String("source_time", sourceTime)}
	for key, rawValue := range row {
		if key == "time" || key == "service_id" || key == "msg" || key == "level" {
			continue
		}
		if key == "source_time" || key == "type" {
			return errors.New("reserved field")
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(rawValue)))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return errors.New("invalid attribute")
		}
		switch v := value.(type) {
		case string:
			attrs = append(attrs, slog.String(key, v))
		case bool:
			attrs = append(attrs, slog.Bool(key, v))
		case json.Number:
			// Preserve integer identifiers without float64 precision loss.
			if i, err := v.Int64(); err == nil {
				attrs = append(attrs, slog.Int64(key, i))
			} else {
				attrs = append(attrs, slog.String(key, string(v)))
			}
		default:
			attrs = append(attrs, slog.Any(key, value))
		}
	}
	// Handle directly so output failures prevent an acknowledgement.
	logger := logging.NewLocal(out, service)
	record := slog.NewRecord(time.Now(), level, message, 0)
	record.AddAttrs(attrs...)
	return logger.Handler().Handle(ctx, record)
}
