package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func seedManagedFile(ctx context.Context, runtime *adminfixture.Runtime, ws, slug, name, hex string, size int, scanState string, generation int, published bool, now time.Time) (uint64, error) {
	pub := 0
	var pubAt any
	if published {
		pub = 1
		pubAt = now
	}
	res, err := runtime.DB.ExecContext(ctx, `INSERT INTO files(workspace_id,public_slug,original_name,storage_key,size_bytes,content_sha256,declared_mime,detected_mime,scan_state,scan_generation,published,published_at,created_by,created_at,updated_at) VALUES (?,?,?,?,?,?, 'application/octet-stream','application/octet-stream',?,?,?,?, 'p09-fixture',?,?)`, ws, slug, name, hex, size, hex, scanState, generation, pub, pubAt, now, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	attemptStatus := "clean"
	if scanState == "blocked" {
		attemptStatus = "infected"
	}
	if scanState == "scan_error" {
		attemptStatus = "error"
	}
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO file_scan_attempts(file_id,workspace_id,generation,status,engine_version,signature_version,verdict_code,created_at) VALUES (?,?,?,?, 'clamav-p09','sig-p09','p09-fixture',?)`, id, ws, generation, attemptStatus, now); err != nil {
		return 0, err
	}
	return uint64(id), nil
}

func runT012(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real P09 file state authority with P17 quarantine/rescan/restore/delete and mandatory safe-only ClamAV restore boundary")
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	service, root, filesLogin, err := bootstrapCaseRoot(ctx, runtime, "T012", []string{adminaccess.PermissionFilesManage}, now)
	if err != nil {
		return out, err
	}
	_, contentLogin, err := createScopedMFAAdmin(ctx, service, root, "T012", "content", adminaccess.PermissionContentManage, now.Add(15*time.Second))
	if err != nil {
		return out, err
	}

	const ws = "ws_p17_t012"
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO file_workspace_counters(workspace_id,active_count,active_bytes) VALUES (?,3,300)`, ws); err != nil {
		return out, err
	}
	safeID, err := seedManagedFile(ctx, runtime, ws, "t012-safe-slug-00000001", "safe.bin", fmt.Sprintf("%064x", 12), 100, "safe", 1, true, now)
	if err != nil {
		return out, err
	}
	blockedID, err := seedManagedFile(ctx, runtime, ws, "t012-blocked-slug-0001", "blocked.bin", fmt.Sprintf("%064x", 13), 100, "blocked", 1, false, now)
	if err != nil {
		return out, err
	}
	deleteID, err := seedManagedFile(ctx, runtime, ws, "t012-delete-slug-00001", "delete.bin", fmt.Sprintf("%064x", 14), 100, "safe", 1, false, now)
	if err != nil {
		return out, err
	}

	server, err := adminfixture.NewExtendedHTTPServer(service, nil)
	if err != nil {
		return out, err
	}
	defer server.Close()
	list, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/files", "", filesLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}
	contentDenied, err := adminfixture.Request(ctx, server, http.MethodGet, "/api/admin/files", "", contentLogin.Token, "", "", "", nil)
	if err != nil {
		return out, err
	}

	blockedRestore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(blockedID)+"/restore", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-blocked-restore-key", "p17-t012-blocked-restore", map[string]any{"reason": "blocked file must not be restored without P09 safe verdict"})
	if err != nil {
		return out, err
	}
	rescan, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(blockedID)+"/rescan", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-rescan-key", "p17-t012-rescan", map[string]any{"reason": "request a new mandatory ClamAV scan"})
	if err != nil {
		return out, err
	}
	rescanReplay, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(blockedID)+"/rescan", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-rescan-key", "p17-t012-rescan", map[string]any{"reason": "request a new mandatory ClamAV scan"})
	if err != nil {
		return out, err
	}
	blockedState, err := scalarString(ctx, runtime.DB, `SELECT scan_state FROM files WHERE id=?`, blockedID)
	if err != nil {
		return out, err
	}
	blockedGen, err := scalarInt(ctx, runtime.DB, `SELECT scan_generation FROM files WHERE id=?`, blockedID)
	if err != nil {
		return out, err
	}
	blockedQueued, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM file_scan_attempts WHERE file_id=? AND generation=2 AND status='queued'`, blockedID)
	if err != nil {
		return out, err
	}

	quarantine, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(safeID)+"/quarantine", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-quarantine-key", "p17-t012-quarantine", map[string]any{"reason": "quarantine previously safe file for re-evaluation"})
	if err != nil {
		return out, err
	}
	unsafeRestore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(safeID)+"/restore", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-unsafe-restore-key", "p17-t012-unsafe-restore", map[string]any{"reason": "must remain denied while P09 verdict is quarantined"})
	if err != nil {
		return out, err
	}
	// This direct fixture transition represents the inherited P09 fileworker after
	// an actual ClamAV-clean verdict. P17 never performs or exposes this transition.
	if _, err := runtime.DB.ExecContext(ctx, `UPDATE files SET scan_state='safe',updated_at=? WHERE id=? AND scan_state='quarantined' AND scan_generation=2`, now.Add(40*time.Second), safeID); err != nil {
		return out, err
	}
	if _, err := runtime.DB.ExecContext(ctx, `UPDATE file_scan_attempts SET status='clean',engine_version='clamav-p09',signature_version='sig-current',verdict_code='clean',completed_at=? WHERE file_id=? AND generation=2 AND status='queued'`, now.Add(40*time.Second), safeID); err != nil {
		return out, err
	}
	restore, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(safeID)+"/restore", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-safe-restore-key", "p17-t012-safe-restore", map[string]any{"reason": "republish only after inherited P09 safe state"})
	if err != nil {
		return out, err
	}
	finalSafe, err := scalarString(ctx, runtime.DB, `SELECT CONCAT(scan_state,':',published) FROM files WHERE id=?`, safeID)
	if err != nil {
		return out, err
	}

	deleteResp, err := adminfixture.Request(ctx, server, http.MethodPost, "/api/admin/files/"+itoa64(deleteID)+"/delete", adminfixture.AllowedOrigin, filesLogin.Token, filesLogin.CSRFToken, "p17-t012-delete-key", "p17-t012-delete", map[string]any{"reason": "delete administered file with accountable quota adjustment"})
	if err != nil {
		return out, err
	}
	counter, err := scalarString(ctx, runtime.DB, `SELECT CONCAT(active_count,':',active_bytes) FROM file_workspace_counters WHERE workspace_id=?`, ws)
	if err != nil {
		return out, err
	}
	auditSafe, err := scalarInt(ctx, runtime.DB, `SELECT COUNT(*) FROM admin_audit_events WHERE action IN ('admin.file.quarantine','admin.file.rescan','admin.file.restore','admin.file.delete') AND JSON_EXTRACT(metadata_json,'$.clamav_bypass')=false`)
	if err != nil {
		return out, err
	}
	rescanAuditCount, err := auditCount(ctx, runtime.DB, "admin.file.rescan", itoa64(blockedID))
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{"files": 3, "p09_scan_attempts": 5, "admin_file_audits": auditSafe}
	out.Checks = map[string]bool{
		"files_manage_required_and_content_manage_cannot_escalate":                    list.Status == http.StatusOK && contentDenied.Status == http.StatusForbidden && adminfixture.NoStoreNoIndex(list),
		"blocked_file_restore_fails_closed":                                           blockedRestore.Status == http.StatusConflict,
		"rescan_returns_to_quarantine_and_queues_exact_new_generation":                rescan.Status == http.StatusOK && rescanReplay.Status == http.StatusOK && blockedState == "quarantined" && blockedGen == 2 && blockedQueued == 1 && rescanAuditCount == 1,
		"admin_quarantine_clears_publication_and_restore_stays_denied_until_p09_safe": quarantine.Status == http.StatusOK && unsafeRestore.Status == http.StatusConflict,
		"restore_consumes_p09_safe_state_without_creating_safe_verdict":               restore.Status == http.StatusOK && finalSafe == "safe:1",
		"delete_preserves_workspace_quota_accounting":                                 deleteResp.Status == http.StatusOK && counter == "2:200",
		"successful_file_mutations_audit_clamav_bypass_false":                         auditSafe == 4,
	}
	pass(&out)
	return out, nil
}
