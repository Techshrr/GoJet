package support

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"
)

type SMTPSender struct {
	address  string
	host     string
	from     string
	username string
	password string
	timeout  time.Duration
}

func NewSMTPSender(address, from, username, password string, timeout time.Duration) (*SMTPSender, error) {
	address = strings.TrimSpace(address)
	from = strings.TrimSpace(from)
	if address == "" || from == "" || timeout <= 0 {
		return nil, ErrInvalidInput
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return nil, ErrInvalidInput
	}
	fromAddress, err := mail.ParseAddress(from)
	if err != nil || fromAddress.Address == "" {
		return nil, ErrInvalidInput
	}
	return &SMTPSender{address: address, host: host, from: fromAddress.Address, username: strings.TrimSpace(username), password: password, timeout: timeout}, nil
}

func (s *SMTPSender) Send(ctx context.Context, recipient string, rendered RenderedMail) MailDeliveryResult {
	if s == nil || strings.TrimSpace(rendered.Subject) == "" || strings.ContainsAny(rendered.Subject, "\r\n") {
		return MailDeliveryResult{ErrorCode: "smtp_message_invalid"}
	}
	to, err := mail.ParseAddress(strings.TrimSpace(recipient))
	if err != nil || to.Address == "" {
		return MailDeliveryResult{ErrorCode: "smtp_recipient_invalid"}
	}
	message, err := buildSMTPMessage(s.from, to.Address, rendered)
	if err != nil {
		return MailDeliveryResult{ErrorCode: "smtp_message_invalid"}
	}

	dialer := net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", s.address)
	if err != nil {
		return classifySMTPFailure(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(s.timeout))
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return classifySMTPFailure(err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return classifySMTPFailure(err)
		}
	}
	if s.username != "" || s.password != "" {
		if s.username == "" || s.password == "" {
			return MailDeliveryResult{ErrorCode: "smtp_auth_config_invalid"}
		}
		if ok, _ := client.Extension("AUTH"); !ok {
			return MailDeliveryResult{ErrorCode: "smtp_auth_unavailable"}
		}
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return classifySMTPFailure(err)
		}
	}
	if err := client.Mail(s.from); err != nil {
		return classifySMTPFailure(err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return classifySMTPFailure(err)
	}
	writer, err := client.Data()
	if err != nil {
		return classifySMTPFailure(err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return classifySMTPFailure(err)
	}
	if err := writer.Close(); err != nil {
		return classifySMTPFailure(err)
	}
	if err := client.Quit(); err != nil {
		return classifySMTPFailure(err)
	}
	return MailDeliveryResult{Success: true}
}

func buildSMTPMessage(from, to string, rendered RenderedMail) ([]byte, error) {
	if strings.ContainsAny(from+to+rendered.Subject, "\r\n") {
		return nil, ErrInvalidInput
	}
	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	boundary := multipartWriter.Boundary()

	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", `text/plain; charset="utf-8"`)
	textHeader.Set("Content-Transfer-Encoding", "8bit")
	textPart, err := multipartWriter.CreatePart(textHeader)
	if err != nil {
		return nil, err
	}
	if _, err := textPart.Write([]byte(rendered.Text)); err != nil {
		return nil, err
	}
	if strings.TrimSpace(rendered.HTML) != "" {
		htmlHeader := textproto.MIMEHeader{}
		htmlHeader.Set("Content-Type", `text/html; charset="utf-8"`)
		htmlHeader.Set("Content-Transfer-Encoding", "8bit")
		htmlPart, err := multipartWriter.CreatePart(htmlHeader)
		if err != nil {
			return nil, err
		}
		if _, err := htmlPart.Write([]byte(rendered.HTML)); err != nil {
			return nil, err
		}
	}
	if err := multipartWriter.Close(); err != nil {
		return nil, err
	}

	var message bytes.Buffer
	writer := bufio.NewWriter(&message)
	_, _ = fmt.Fprintf(writer, "From: <%s>\r\n", from)
	_, _ = fmt.Fprintf(writer, "To: <%s>\r\n", to)
	_, _ = fmt.Fprintf(writer, "Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", rendered.Subject))
	_, _ = fmt.Fprintf(writer, "MIME-Version: 1.0\r\n")
	_, _ = fmt.Fprintf(writer, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	_, _ = fmt.Fprintf(writer, "\r\n")
	if err := writer.Flush(); err != nil {
		return nil, err
	}
	if _, err := message.Write(body.Bytes()); err != nil {
		return nil, err
	}
	return message.Bytes(), nil
}

func classifySMTPFailure(err error) MailDeliveryResult {
	if err == nil {
		return MailDeliveryResult{Success: true}
	}
	var protocolErr *textproto.Error
	if errors.As(err, &protocolErr) {
		if protocolErr.Code >= 400 && protocolErr.Code <= 499 {
			return MailDeliveryResult{Transient: true, ErrorCode: "smtp_transient"}
		}
		return MailDeliveryResult{ErrorCode: "smtp_terminal"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return MailDeliveryResult{Transient: true, ErrorCode: "smtp_transport"}
	}
	return MailDeliveryResult{Transient: true, ErrorCode: "smtp_unknown"}
}
