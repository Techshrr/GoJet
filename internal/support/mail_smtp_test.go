package support

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type localSMTPSink struct {
	listener net.Listener
	mu       sync.Mutex
	message  string
}

func startLocalSMTPSink(t *testing.T) *localSMTPSink {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	sink := &localSMTPSink{listener: listener}
	go sink.serve()
	return sink
}

func (s *localSMTPSink) serve() {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeLine := func(value string) {
		_, _ = writer.WriteString(value + "\r\n")
		_ = writer.Flush()
	}
	writeLine("220 localhost ESMTP")
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		command := strings.ToUpper(strings.TrimSpace(line))
		switch {
		case strings.HasPrefix(command, "EHLO"):
			_, _ = writer.WriteString("250-localhost\r\n250 8BITMIME\r\n")
			_ = writer.Flush()
		case strings.HasPrefix(command, "HELO"):
			writeLine("250 localhost")
		case strings.HasPrefix(command, "MAIL FROM:"):
			writeLine("250 OK")
		case strings.HasPrefix(command, "RCPT TO:"):
			writeLine("250 OK")
		case command == "DATA":
			writeLine("354 End data with <CR><LF>.<CR><LF>")
			var data strings.Builder
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dataLine, "\r\n") == "." {
					break
				}
				data.WriteString(dataLine)
			}
			s.mu.Lock()
			s.message = data.String()
			s.mu.Unlock()
			writeLine("250 queued")
		case command == "QUIT":
			writeLine("221 bye")
			return
		default:
			writeLine("250 OK")
		}
	}
}

func (s *localSMTPSink) address() string { return s.listener.Addr().String() }

func (s *localSMTPSink) close() { _ = s.listener.Close() }

func (s *localSMTPSink) captured() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.message
}

func TestSMTPSenderPerformsRealLocalProtocolTransaction(t *testing.T) {
	sink := startLocalSMTPSink(t)
	defer sink.close()
	sender, err := NewSMTPSender(sink.address(), "support@example.test", "", "", 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	result := sender.Send(context.Background(), "user@example.test", RenderedMail{
		Subject: "Support update",
		Text:    "Plain body",
		HTML:    "<p>HTML body</p>",
	})
	if !result.Success || result.Transient || result.ErrorCode != "" {
		t.Fatalf("delivery result=%+v", result)
	}
	captured := sink.captured()
	for _, expected := range []string{"Subject:", "Support update", "Plain body", "HTML body"} {
		if !strings.Contains(captured, expected) {
			t.Fatalf("SMTP transaction missing %q: %s", expected, fmt.Sprintf("%q", captured))
		}
	}
}

func TestSMTPFailureClassificationIsBoundedAndSecretSafe(t *testing.T) {
	result := classifySMTPFailure(&net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded})
	if !result.Transient || result.ErrorCode != "smtp_transport" {
		t.Fatalf("classification=%+v", result)
	}
	if strings.Contains(strings.ToLower(result.ErrorCode), "password") || strings.Contains(strings.ToLower(result.ErrorCode), "recipient") {
		t.Fatalf("unsafe error code=%q", result.ErrorCode)
	}
}
