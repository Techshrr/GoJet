package adminfixture

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

type Response struct {
	Status  int
	Headers http.Header
	Body    map[string]any
	Raw     string
}

func EnsureIdentity(ctx context.Context, db *sql.DB, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if db == nil || userID == "" || len(userID) > 128 {
		return "", fmt.Errorf("invalid admin fixture user")
	}
	email := userID + "@p16.invalid"
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
INSERT INTO auth_users
(id,email,email_normalized,display_name,status,email_verified_at,version,created_at,updated_at)
VALUES (?,?,?,'P16 Trust Admin','active',?,1,?,?)
ON DUPLICATE KEY UPDATE status='active',email_verified_at=VALUES(email_verified_at),updated_at=VALUES(updated_at)`,
		userID, email, email, now, now, now); err != nil {
		return "", err
	}
	return email, nil
}

func EnsureSession(ctx context.Context, db *sql.DB, userID string) (string, error) {
	if _, err := EnsureIdentity(ctx, db, userID); err != nil {
		return "", err
	}
	secret, err := authn.NewStore(db).CreateSession(ctx, strings.TrimSpace(userID), time.Hour, "p16-admin-session-"+strings.TrimSpace(userID))
	if err != nil {
		return "", err
	}
	return secret.Token, nil
}

func Request(ctx context.Context, method, path, sessionToken, csrfToken, idempotencyKey, correlationID string, body any) (Response, error) {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_PLATFORMAPI_URL")), "/")
	if base == "" {
		return Response{}, fmt.Errorf("GOJET_PLATFORMAPI_URL is required")
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return Response{}, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, reader)
	if err != nil {
		return Response{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(sessionToken) != "" {
		req.Header.Set("Cookie", authn.SessionCookieName+"="+strings.TrimSpace(sessionToken))
	}
	if strings.TrimSpace(csrfToken) != "" {
		req.Header.Set(authn.CSRFHeaderName, strings.TrimSpace(csrfToken))
	}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set("Origin", base)
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", strings.TrimSpace(idempotencyKey))
	}
	if strings.TrimSpace(correlationID) != "" {
		req.Header.Set("X-Request-ID", strings.TrimSpace(correlationID))
	}
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return Response{}, err
	}
	decoded := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return Response{}, fmt.Errorf("decode admin response status=%d: %w; body=%s", resp.StatusCode, err, string(raw))
		}
	}
	return Response{Status: resp.StatusCode, Headers: resp.Header.Clone(), Body: decoded, Raw: string(raw)}, nil
}

func CSRF(resp Response) string {
	value, _ := resp.Body["csrf_token"].(string)
	return strings.TrimSpace(value)
}

func NoStoreNoIndex(resp Response) bool {
	return strings.Contains(strings.ToLower(resp.Headers.Get("Cache-Control")), "no-store") && strings.Contains(strings.ToLower(resp.Headers.Get("X-Robots-Tag")), "noindex")
}
