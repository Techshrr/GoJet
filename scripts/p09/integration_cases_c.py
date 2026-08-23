import datetime as dt

from integration_common import *
from integration_cases_a import clean_with_real

def case_t013():
    reset_case()
    status, _, _, created = upload("ws-a", "restart.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    with fault_server("hold", hold_seconds=20.0) as (address, _):
        first = worker_popen(address, scan_timeout="30s", claim_lease="1s", worker_id="p09-restart-a")
        wait_until(lambda: mysql(f"SELECT status FROM file_scan_attempts WHERE file_id={rid}") == "processing", 6, "processing before crash")
        first.kill()
        first.wait(timeout=3)
    time.sleep(1.2)
    run_worker(REAL_CLAMD, claim_lease="1s", worker_id="p09-restart-b")
    row, scan = db_resource(rid), db_scan(rid)
    count = int(mysql(f"SELECT COUNT(*) FROM file_scan_attempts WHERE file_id={rid}"))
    expect(row["scan_state"] == "safe" and row["published"] == 0 and scan["status"] == "clean" and count == 1, (row, scan, count))
    return {"attempt_count": count, "scan_attempt_id": scan["attempt_id"], "scan_generation": scan["generation"],
            "scan_state": row["scan_state"], "published": row["published"], "scan_status": scan["status"]}

def case_t014():
    reset_case()
    status, _, raw_created, created = upload("ws-a", "modes.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    row = db_resource(rid)
    key = row["storage_key"]
    obj = storage_path("quarantine", key)
    root_mode = stat.S_IMODE(STORAGE_ROOT.stat().st_mode)
    quarantine_mode = stat.S_IMODE((STORAGE_ROOT/"quarantine").stat().st_mode)
    published_mode = stat.S_IMODE((STORAGE_ROOT/"published").stat().st_mode)
    object_mode = stat.S_IMODE(obj.stat().st_mode)
    expect((root_mode, quarantine_mode, published_mode, object_mode) == (0o700, 0o700, 0o700, 0o600),
           (oct(root_mode), oct(quarantine_mode), oct(published_mode), oct(object_mode)))
    expect(key.encode() not in raw_created, "storage key leaked in API resource JSON")
    probes = [
        f"/quarantine/{key}",
        f"/published/{key}",
        f"/%2e%2e/quarantine/{key}",
    ]
    probe_statuses = []
    for path in probes:
        status, _, body = request("GET", path)
        expect(not (status == 200 and body == BENIGN), f"direct storage path leaked {path}")
        probe_statuses.append(status)
    return {"root_mode": oct(root_mode), "quarantine_mode": oct(quarantine_mode), "published_mode": oct(published_mode),
            "object_mode": oct(object_mode), "direct_probe_statuses": probe_statuses, "storage_key_leaked": False}

def case_t015():
    reset_case()
    status, _, _, created = upload("ws-a", "publish.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    premature, _, _, _ = action("ws-a", rid, "publish")
    expect(premature == 409, f"quarantined publish status {premature}")
    run_worker()
    safe = db_resource(rid)
    expect(safe["scan_state"] == "safe" and safe["published"] == 0, safe)
    viewer, _, _, _ = action("ws-a", rid, "publish", role="viewer")
    expect(viewer == 403, f"viewer publish status {viewer}")
    admin, _, _, result = action("ws-a", rid, "publish")
    expect(admin == 200 and result["published"] is True, f"admin publish {admin}: {result}")
    return {"premature_publish": premature, "safe_auto_published": False, "viewer_publish": viewer, "admin_publish": admin}

def case_t016():
    reset_case()
    created, row, _ = clean_with_real()
    rid, slug = int(created["id"]), row["public_slug"]
    password = "P09-secret-password"
    status, _, _, patched = patch_policy("ws-a", rid, {"password": password, "download_limit": 1})
    expect(status == 200 and patched["password_required"] is True and patched["download_limit"] == 1, patched)
    status, _, _, published = action("ws-a", rid, "publish")
    expect(status == 200 and published["published"] is True, published)
    denied, _, denied_body = public_binary(slug)
    expect(denied == 403 and BENIGN not in denied_body, f"password bypass {denied}")
    form_headers = {"Content-Type": "application/x-www-form-urlencoded"}
    wrong_body = urllib.parse.urlencode({"password": "wrong-password"}).encode()
    wrong, _, _ = request("POST", f"/f/{urllib.parse.quote(slug, safe='')}", wrong_body, form_headers)
    expect(wrong == 403, f"wrong password status {wrong}")
    correct_body = urllib.parse.urlencode({"password": password}).encode()
    correct, headers, _ = request("POST", f"/f/{urllib.parse.quote(slug, safe='')}", correct_body, form_headers)
    expect(correct == 303 and "Set-Cookie" in headers, f"correct password response {correct} headers={headers}")
    set_cookie = headers["Set-Cookie"]
    cookie = set_cookie.split(";", 1)[0]
    expect(password not in set_cookie and password not in cookie, "plaintext password leaked into cookie")
    first, _, first_body = public_binary(slug, cookie)
    expect(first == 200 and first_body == BENIGN, f"first download failed {first}")
    second, _, second_body = public_binary(slug, cookie)
    expect(second == 410 and BENIGN not in second_body, f"download limit failed {second}")
    dbrow = mysql(f"SELECT download_count,COALESCE(password_hash,'') FROM files WHERE id={rid}").split("\t")
    expect(dbrow[0] == "1" and dbrow[1] and dbrow[1] != password, f"password/download DB invariant failed {dbrow}")
    return {"preauth_status": denied, "wrong_password_status": wrong, "correct_password_status": correct,
            "first_download": first, "second_download": second, "download_count": int(dbrow[0]),
            "password_plaintext_stored": False, "cookie_plaintext": False}

def case_t017():
    reset_case()
    created, row, _ = clean_with_real(filename="expiry.txt")
    rid, slug = int(created["id"]), row["public_slug"]
    current = dt.datetime.now(dt.timezone.utc)
    past = (current - dt.timedelta(hours=1)).isoformat(timespec="seconds").replace("+00:00", "Z")
    future = (current + dt.timedelta(days=30)).isoformat(timespec="seconds").replace("+00:00", "Z")
    status, _, _, patched = patch_policy("ws-a", rid, {"expires_at": past, "retention_until": future})
    expect(status == 200 and patched["expires_at"] and patched["retention_until"], patched)
    status, _, _, published = action("ws-a", rid, "publish")
    expect(status == 200 and published["published"] is True, published)
    expired, _, expired_body = public_binary(slug)
    expect(expired == 410 and BENIGN not in expired_body, f"expired bytes leaked {expired}")

    status, _, _, second = upload("ws-a", "delete.txt", "text/plain", b"delete fixture\n")
    expect(status == 201, f"second upload failed {status}")
    rid2 = int(second["id"])
    row2 = db_resource(rid2)
    obj = storage_path("quarantine", row2["storage_key"])
    expect(obj.exists(), "delete fixture storage missing before delete")
    deleted, _, _, _ = delete_resource("ws-a", rid2)
    expect(deleted == 204, f"delete status {deleted}")
    expect(not obj.exists(), "deleted storage object remains")
    gone, _, gone_body = public_binary(row2["public_slug"])
    expect(gone == 410 and b"delete fixture" not in gone_body, f"deleted public bytes status {gone}")
    counter = mysql("SELECT active_count FROM file_workspace_counters WHERE workspace_id='ws-a'")
    expect(counter == "1", f"counter did not decrement: {counter}")
    return {"expired_status": expired, "deleted_status": gone, "retention_until": patched["retention_until"],
            "deleted_storage_removed": True, "active_count_after_delete": int(counter)}

def case_t018():
    reset_case()
    status, _, _, created = upload("ws-a", "errors.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    invalid, _, _ = request("GET", "/api/workspaces/ws-a/files/not-a-number", headers=ws_headers("ws-a"))
    unknown, _, _, _ = get_resource("ws-a", 999999)
    cross, _, _, _ = get_resource("ws-b", rid)
    mismatch_headers = ws_headers("ws-a")
    mismatch, _, _ = request("GET", f"/api/workspaces/ws-b/files/{rid}", headers=mismatch_headers)
    bad_headers = ws_headers("ws-a"); bad_headers["Content-Type"] = "application/json"
    malformed, _, malformed_body = request("PATCH", f"/api/workspaces/ws-a/files/{rid}", b"{bad", bad_headers)
    expect((invalid, unknown, cross, mismatch, malformed) == (400, 404, 404, 403, 400),
           (invalid, unknown, cross, mismatch, malformed))
    run_worker()
    row = db_resource(rid)
    obj = storage_path("quarantine", row["storage_key"])
    obj.unlink()
    missing, _, missing_body = request("GET", f"/api/workspaces/ws-a/files/{rid}/download", headers=ws_headers("ws-a"))
    expect(missing == 503, f"missing storage status {missing}")
    leaked_text = (malformed_body + missing_body).decode("utf-8", "replace")
    sensitive = [str(STORAGE_ROOT), REAL_CLAMD, "goroutine", "panic:"]
    expect(not any(value in leaked_text for value in sensitive), f"private dependency detail leaked: {leaked_text}")
    return {"invalid_id": invalid, "unknown": unknown, "cross_tenant": cross, "header_path_mismatch": mismatch,
            "malformed_json": malformed, "missing_storage": missing, "private_detail_leaked": False}
