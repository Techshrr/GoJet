package logging

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

const MaxRecordBytes = 64 * 1024

// SendLocal only transports already-sanitized records over a private Unix socket.
// The receiver's acknowledgement means written to its output, not durable audit.
func SendLocal(socket string, record []byte) error {
	if len(record) == 0 || len(record) > MaxRecordBytes {
		return errors.New("invalid log record size")
	}
	conn, err := net.DialTimeout("unix", socket, 100*time.Millisecond)
	if err != nil {
		return errors.New("log receiver unavailable")
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		return errors.New("log receiver deadline failed")
	}
	if _, err := conn.Write(record); err != nil {
		return errors.New("log receiver write failed")
	}
	ack, err := bufio.NewReader(io.LimitReader(conn, 32)).ReadString('\n')
	if err != nil || ack != "{\"ok\":true}\n" {
		return errors.New("log receiver rejected record")
	}
	return nil
}

type localForwarder struct {
	mu          sync.Mutex
	out         io.Writer
	socket      string
	service     string
	lastWarning time.Time
}

func (w *localForwarder) Write(record []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.out.Write(record)
	if err != nil || n != len(record) {
		return n, err
	}
	if SendLocal(w.socket, record) != nil && time.Since(w.lastWarning) >= 30*time.Second {
		// Never log transport errors or paths, and never recursively forward.
		slog.New(slog.NewJSONHandler(w.out, nil)).Warn("log forwarding unavailable; original retained locally", "service_id", w.service)
		w.lastWarning = time.Now()
	}
	return n, nil
}
