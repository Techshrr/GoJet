from integration_common import *

def case_t001():
    reset_case()
    status, _, _, created = upload("ws-a", "clean.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload status {status}: {created}")
    rid = int(created["id"])
    row = db_resource(rid)
    expect(row["scan_state"] == "quarantined" and row["published"] == 0 and row["scan_generation"] == 1, row)
    expect(len(row["storage_key"]) == 64 and all(c in "0123456789abcdef" for c in row["storage_key"]), "storage key not randomized hex")
    expect(row["storage_key"] != row["original_name"], "original filename became storage authority")
    obj = storage_path("quarantine", row["storage_key"])
    expect(obj.is_file() and obj.read_bytes() == BENIGN, "quarantine object mismatch")
    pstatus, _, pbody = public_binary(row["public_slug"])
    expect(pstatus == 403 and BENIGN not in pbody, f"quarantined public binary leaked: {pstatus}")
    return {"file_id": rid, "scan_state": row["scan_state"], "published": row["published"], "public_status": pstatus,
            "storage_key_length": len(row["storage_key"]), "quarantine_mode": oct(stat.S_IMODE(obj.stat().st_mode))}

def case_t002():
    reset_case()
    fake_pdf = b"%PDF-1.7\n1 0 obj\n"
    status, _, _, body = upload("ws-a", "mismatch.png", "image/png", fake_pdf)
    expect(status == 400, f"mismatch upload status {status}: {body}")
    expect(mysql("SELECT COUNT(*) FROM files") == "0", "denied type created DB resource")
    stored = [p for p in (STORAGE_ROOT/"quarantine").rglob("*") if p.is_file()]
    expect(stored == [], f"denied type left quarantine bytes: {stored}")
    return {"status": status, "file_count": 0, "quarantine_files": 0}

def case_t003():
    reset_case()
    def one(index: int):
        return upload("ws-quota", f"q{index}.txt", "text/plain", f"quota-{index}\n".encode())[0]
    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
        statuses = list(pool.map(one, range(3)))
    expect(statuses.count(201) == 2 and statuses.count(429) == 1, f"unexpected quota statuses {statuses}")
    count = int(mysql("SELECT COUNT(*) FROM files WHERE workspace_id='ws-quota'"))
    counter = mysql("SELECT active_count FROM file_workspace_counters WHERE workspace_id='ws-quota'")
    expect(count == 2 and counter == "2", f"quota drift files={count} counter={counter}")
    return {"statuses": sorted(statuses), "file_count": count, "counter": int(counter)}

def case_t004():
    reset_case()
    status, _, _, created = upload("ws-a", "tenant.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    status_view, _, _, _ = get_resource("ws-a", rid, role="viewer")
    status_mut, _, _, _ = action("ws-a", rid, "publish", role="viewer")
    status_cross, _, _, _ = get_resource("ws-b", rid, role="admin")
    headers = ws_headers("ws-a")
    mismatch, _, _ = request("GET", f"/api/workspaces/ws-b/files/{rid}", headers=headers)
    expect((status_view, status_mut, status_cross, mismatch) == (200, 403, 404, 403),
           f"RBAC/isolation statuses {(status_view,status_mut,status_cross,mismatch)}")
    return {"viewer_get": status_view, "viewer_mutation": status_mut, "cross_tenant": status_cross, "header_path_mismatch": mismatch}

def clean_with_real(workspace: str = "ws-a", filename: str = "clean.txt", payload: bytes = BENIGN):
    status, _, _, created = upload(workspace, filename, "text/plain", payload)
    expect(status == 201, f"upload failed {status}: {created}")
    rid = int(created["id"])
    run_worker()
    row = db_resource(rid)
    scan = db_scan(rid)
    return created, row, scan

def case_t005():
    reset_case()
    created, row, scan = clean_with_real()
    expect(row["scan_state"] == "safe" and row["published"] == 0, row)
    expect(scan["status"] == "clean" and scan["engine_version"] and scan["signature_version"], scan)
    return {"file_id": row["id"], "scan_state": row["scan_state"], "published": row["published"],
            "engine_version": scan["engine_version"], "signature_version": scan["signature_version"], "scan_status": scan["status"]}

def case_t006():
    reset_case()
    status, _, _, created = upload("ws-a", "eicar.txt", "text/plain", EICAR)
    expect(status == 201, f"EICAR upload failed {status}: {created}")
    rid = int(created["id"])
    run_worker()
    row, scan = db_resource(rid), db_scan(rid)
    expect(row["scan_state"] == "blocked" and row["published"] == 0, row)
    expect(scan["status"] == "infected" and scan["verdict_code"], scan)
    pstatus, _, pbody = public_binary(row["public_slug"])
    expect(pstatus == 403 and EICAR not in pbody, f"blocked bytes leaked status={pstatus}")
    return {"file_id": rid, "scan_state": row["scan_state"], "scan_status": scan["status"],
            "verdict_code": scan["verdict_code"], "engine_version": scan["engine_version"], "public_status": pstatus}

