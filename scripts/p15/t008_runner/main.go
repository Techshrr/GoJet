package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	_ "github.com/go-sql-driver/mysql"
)

type result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func main() {
	db, ctx, cancel := setup()
	defer cancel()
	defer db.Close()
	var mysqlVersion string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&mysqlVersion); err != nil {
		fail(err)
	}
	store := auth.NewStore(db)
	stamp := time.Now().UTC().UnixNano()
	user, err := store.CreateUser(ctx, auth.CreateUserInput{Email: fmt.Sprintf("p15-t008-%d@example.test", stamp), DisplayName: "P15 T008"})
	if err != nil {
		fail(err)
	}
	sessionSecret, err := store.CreateSession(ctx, user.ID, time.Hour, fmt.Sprintf("p15-t008-%d", stamp))
	if err != nil {
		fail(err)
	}
	cookie, err := auth.NewSessionCookie(sessionSecret.Token, sessionSecret.Session.ExpiresAt)
	if err != nil {
		fail(err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://app.gojet.test/app/settings/security", nil)
	if err != nil {
		fail(err)
	}
	request.AddCookie(cookie)
	resolved, resolveErr := auth.AuthenticateRequest(ctx, store, request, time.Now().UTC())
	recorder := httptest.NewRecorder()
	auth.ApplyPrivateAuthHeaders(recorder.Header())
	frontendSafe, scannedFiles := frontendAuthStorageSafe("frontend")
	activeSessions := count(ctx, db, `SELECT COUNT(*) FROM auth_sessions WHERE user_id=? AND status='active'`, user.ID)
	checks := map[string]bool{
		"session_cookie_uses_host_prefix":              cookie.Name == auth.SessionCookieName && strings.HasPrefix(cookie.Name, "__Host-"),
		"session_cookie_is_secure_http_only":           cookie.Secure && cookie.HttpOnly,
		"session_cookie_has_host_prefix_constraints":   cookie.Path == "/" && cookie.Domain == "",
		"session_cookie_uses_reviewed_samesite_policy": cookie.SameSite == http.SameSiteLaxMode,
		"server_side_session_resolution_succeeds":      resolveErr == nil && resolved.ID == sessionSecret.Session.ID && resolved.UserID == user.ID,
		"authenticated_response_is_no_store":           strings.Contains(strings.ToLower(recorder.Header().Get("Cache-Control")), "no-store"),
		"authenticated_response_is_noindex":            strings.Contains(strings.ToLower(recorder.Header().Get("X-Robots-Tag")), "noindex"),
		"frontend_has_no_formal_auth_token_storage":    frontendSafe,
		"durable_session_authority_exists":             activeSessions == 1,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T008", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"active_sessions": activeSessions, "frontend_files_scanned": scannedFiles}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func frontendAuthStorageSafe(root string) (bool, int) {
	forbidden := []string{"auth_token", "access_token", "refresh_token", "session_token", "gojet_session", "bearer_token", "gst_"}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".html", ".vue", ".svelte":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanned++
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "localstorage") || strings.Contains(lower, "sessionstorage") {
			for _, fragment := range forbidden {
				if strings.Contains(lower, fragment) {
					return fmt.Errorf("formal auth token storage fragment found in %s", path)
				}
			}
		}
		return nil
	})
	return err == nil, scanned
}

func setup() (*sql.DB, context.Context, context.CancelFunc) {
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		fail(fmt.Errorf("GOJET_MYSQL_DSN is required"))
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		fail(err)
	}
	return db, ctx, cancel
}

func count(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		fail(err)
	}
	return n
}
