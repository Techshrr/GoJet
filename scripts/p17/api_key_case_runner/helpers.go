package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
)

func seedWorkspaceRoles(ctx context.Context, db *sql.DB, workspaceID string, roles map[string]string, now time.Time) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO workspaces(id,name,status,version,created_by,created_at,updated_at) VALUES (?,?,'active',1,?,?,?)`, workspaceID, "P17 API Key "+workspaceID, "fixture", now, now); err != nil {
		return err
	}
	for userID, role := range roles {
		email := strings.ToLower(userID) + "@p17.test"
		if _, err := db.ExecContext(ctx, `INSERT INTO workspace_memberships(workspace_id,user_id,email,display_name,role,joined_at,updated_at) VALUES (?,?,?,?,?,?,?)`, workspaceID, userID, email, userID, role, now, now); err != nil {
			return err
		}
	}
	return nil
}
func scalar(ctx context.Context, db *sql.DB, q string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}
func auditContains(ctx context.Context, db *sql.DB, workspaceID, needle string) (bool, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_audit_events WHERE workspace_id=? AND (metadata_json LIKE ? OR reason LIKE ?)`, workspaceID, "%"+needle+"%", "%"+needle+"%").Scan(&n)
	return n > 0, err
}
func apiRequest(handler http.Handler, method, path, actor string, body any, extra map[string]string) (int, map[string]any, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("X-GoJet-Test-Actor", actor)
	req.Header.Set("X-GoJet-Test-Email", strings.ToLower(actor)+"@p17.test")
	req.Header.Set("X-Request-ID", "p17-api-key-http")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var decoded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	return rec.Code, decoded, nil
}
