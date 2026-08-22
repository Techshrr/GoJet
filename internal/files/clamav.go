package files

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

type ClamAVClient struct {
	network         string
	address         string
	dialTimeout     time.Duration
	scanTimeout     time.Duration
	maxSignatureAge time.Duration
	now             func() time.Time
}

func NewClamAVClient(network, address string, dialTimeout, scanTimeout, maxSignatureAge time.Duration) (*ClamAVClient, error) {
	network = strings.TrimSpace(network)
	address = strings.TrimSpace(address)
	if (network != "tcp" && network != "unix") || address == "" || dialTimeout <= 0 || scanTimeout <= 0 || maxSignatureAge <= 0 {
		return nil, ErrInvalidInput
	}
	return &ClamAVClient{network: network, address: address, dialTimeout: dialTimeout, scanTimeout: scanTimeout, maxSignatureAge: maxSignatureAge, now: time.Now}, nil
}

func (c *ClamAVClient) dial(ctx context.Context, timeout time.Duration) (net.Conn, error) {
	if c == nil {
		return nil, ErrInvalidInput
	}
	dialer := net.Dialer{Timeout: c.dialTimeout}
	conn, err := dialer.DialContext(ctx, c.network, c.address)
	if err != nil {
		return nil, err
	}
	deadline := c.now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *ClamAVClient) Health(ctx context.Context) (ClamAVHealth, error) {
	conn, err := c.dial(ctx, c.dialTimeout)
	if err != nil {
		return ClamAVHealth{}, err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "zVERSION\x00"); err != nil {
		return ClamAVHealth{}, err
	}
	reply, err := readClamdReply(conn)
	if err != nil {
		return ClamAVHealth{}, err
	}
	health, err := parseVersionReply(reply, c.now().UTC(), c.maxSignatureAge)
	if err != nil {
		return ClamAVHealth{}, err
	}
	if !health.Fresh {
		return health, ErrSignatureStale
	}
	return health, nil
}

func (c *ClamAVClient) Scan(ctx context.Context, src io.Reader) (ScanResult, error) {
	if c == nil || src == nil {
		return ScanResult{Verdict: VerdictError, ErrorCode: "invalid_input"}, ErrInvalidInput
	}
	health, err := c.Health(ctx)
	if err != nil {
		result := ScanResult{Verdict: VerdictError, ErrorCode: "clamav_unavailable", Reason: "ClamAV health check failed closed."}
		if errors.Is(err, ErrSignatureStale) {
			result.EngineVersion = health.EngineVersion
			result.SignatureVersion = health.SignatureVersion
			result.SignatureDate = &health.SignatureDate
			result.ErrorCode = "signature_stale"
			result.Reason = "ClamAV signatures are stale."
		}
		return result, err
	}

	conn, err := c.dial(ctx, c.scanTimeout)
	if err != nil {
		return resultFromHealth(health, VerdictError, "clamav_unavailable", "ClamAV scanner is unavailable."), err
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return resultFromHealth(health, VerdictError, "scan_write_failed", "ClamAV scan request failed."), err
	}

	buffer := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buffer)
		if n > 0 {
			var size [4]byte
			binary.BigEndian.PutUint32(size[:], uint32(n))
			if _, err := conn.Write(size[:]); err != nil {
				return resultFromHealth(health, VerdictError, "scan_write_failed", "ClamAV scan request failed."), err
			}
			if _, err := conn.Write(buffer[:n]); err != nil {
				return resultFromHealth(health, VerdictError, "scan_write_failed", "ClamAV scan request failed."), err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return resultFromHealth(health, VerdictError, "source_read_failed", "Quarantine bytes could not be read."), readErr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return resultFromHealth(health, VerdictError, "scan_write_failed", "ClamAV scan request failed."), err
	}
	reply, err := readClamdReply(conn)
	if err != nil {
		return resultFromHealth(health, VerdictError, "scan_read_failed", "ClamAV scan response failed."), err
	}
	result, err := parseScanReply(reply, health)
	if err != nil {
		return result, err
	}
	return result, nil
}

func resultFromHealth(health ClamAVHealth, verdict ScanVerdict, code, reason string) ScanResult {
	date := health.SignatureDate
	return ScanResult{
		Verdict: verdict, EngineVersion: health.EngineVersion, SignatureVersion: health.SignatureVersion,
		SignatureDate: &date, ErrorCode: code, Reason: reason,
	}
}

func readClamdReply(r io.Reader) (string, error) {
	reader := bufio.NewReader(io.LimitReader(r, 64*1024))
	line, err := reader.ReadString(0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(strings.TrimSuffix(line, "\x00"))
	if line == "" {
		return "", ErrScanIndeterminate
	}
	return line, nil
}

func parseVersionReply(reply string, now time.Time, maxAge time.Duration) (ClamAVHealth, error) {
	parts := strings.SplitN(strings.TrimSpace(reply), "/", 3)
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "ClamAV ") {
		return ClamAVHealth{}, ErrScanIndeterminate
	}
	engine := strings.TrimSpace(strings.TrimPrefix(parts[0], "ClamAV "))
	signatureVersion := strings.TrimSpace(parts[1])
	dateText := strings.TrimSpace(parts[2])
	if engine == "" || signatureVersion == "" || dateText == "" {
		return ClamAVHealth{}, ErrScanIndeterminate
	}
	var signatureDate time.Time
	var parseErr error
	for _, layout := range []string{
		"Mon Jan _2 15:04:05 2006",
		"Mon Jan 2 15:04:05 2006",
		time.RFC1123,
		time.RFC1123Z,
	} {
		signatureDate, parseErr = time.ParseInLocation(layout, dateText, time.UTC)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		return ClamAVHealth{}, fmt.Errorf("%w: signature date", ErrScanIndeterminate)
	}
	now = now.UTC()
	age := now.Sub(signatureDate.UTC())
	fresh := age >= -5*time.Minute && age <= maxAge
	return ClamAVHealth{
		EngineVersion: engine, SignatureVersion: signatureVersion, SignatureDate: signatureDate.UTC(), CheckedAt: now, Fresh: fresh,
	}, nil
}

func parseScanReply(reply string, health ClamAVHealth) (ScanResult, error) {
	trimmed := strings.TrimSpace(reply)
	if strings.HasSuffix(trimmed, ": OK") {
		result := resultFromHealth(health, VerdictClean, "", "ClamAV returned a clean verdict.")
		result.VerdictCode = "OK"
		return result, nil
	}
	if strings.HasSuffix(trimmed, " FOUND") {
		colon := strings.Index(trimmed, ": ")
		found := strings.LastIndex(trimmed, " FOUND")
		if colon < 0 || found <= colon+2 {
			return resultFromHealth(health, VerdictError, "indeterminate_response", "ClamAV response was indeterminate."), ErrScanIndeterminate
		}
		signature := strings.TrimSpace(trimmed[colon+2 : found])
		if signature == "" {
			return resultFromHealth(health, VerdictError, "indeterminate_response", "ClamAV response was indeterminate."), ErrScanIndeterminate
		}
		result := resultFromHealth(health, VerdictInfected, "", "ClamAV detected malware.")
		result.VerdictCode = signature
		return result, nil
	}
	result := resultFromHealth(health, VerdictError, "indeterminate_response", "ClamAV response was not an unambiguous clean or infected verdict.")
	return result, ErrScanIndeterminate
}
