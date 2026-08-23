from integration_common import *
from integration_cases_a import clean_with_real

def case_t007():
    reset_case()
    status, _, _, created = upload("ws-a", "down.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    port = free_port()
    run_worker(f"127.0.0.1:{port}", dial_timeout="200ms", scan_timeout="500ms")
    row, scan = db_resource(rid), db_scan(rid)
    expect(row["scan_state"] == "scan_error" and row["published"] == 0, row)
    expect(scan["error_code"] == "clamav_unavailable", scan)
    return {"scan_state": row["scan_state"], "error_code": scan["error_code"], "published": row["published"]}

def case_t008():
    reset_case()
    status, _, _, created = upload("ws-a", "timeout.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    with fault_server("timeout", hold_seconds=2.0) as (address, _):
        run_worker(address, scan_timeout="300ms", dial_timeout="300ms")
    row, scan = db_resource(rid), db_scan(rid)
    expect(row["scan_state"] == "scan_error" and row["published"] == 0, row)
    expect(scan["error_code"] in {"scan_read_failed", "scan_write_failed"}, scan)
    pstatus, _, pbody = public_binary(row["public_slug"])
    expect(pstatus == 403 and BENIGN not in pbody, f"timeout leaked bytes {pstatus}")
    return {"scan_state": row["scan_state"], "error_code": scan["error_code"], "public_status": pstatus}

def case_t009():
    reset_case()
    status, _, _, created = upload("ws-a", "stale.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    with fault_server("stale") as (address, _):
        run_worker(address, signature_age="1h", scan_timeout="1s")
    row, scan = db_resource(rid), db_scan(rid)
    expect(row["scan_state"] == "scan_error" and scan["error_code"] == "signature_stale", (row, scan))
    return {"scan_state": row["scan_state"], "error_code": scan["error_code"], "signature_version": scan["signature_version"]}

def case_t010():
    reset_case()
    status, _, _, created = upload("ws-a", "indeterminate.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    with fault_server("indeterminate") as (address, _):
        run_worker(address, scan_timeout="1s")
    row, scan = db_resource(rid), db_scan(rid)
    expect(row["scan_state"] == "scan_error" and scan["error_code"] == "indeterminate_response", (row, scan))
    return {"scan_state": row["scan_state"], "error_code": scan["error_code"]}

def case_t011():
    reset_case()
    created, row, _ = clean_with_real()
    rid, slug = int(created["id"]), row["public_slug"]
    status, _, _, published = action("ws-a", rid, "publish")
    expect(status == 200 and published["published"] is True, f"publish failed {status}: {published}")
    before, _, body = public_binary(slug)
    expect(before == 200 and body == BENIGN, f"published clean bytes unavailable {before}")
    status, _, _, rescanned = action("ws-a", rid, "rescan")
    expect(status == 202 and rescanned["scan_state"] == "quarantined" and rescanned["published"] is False, rescanned)
    during, _, during_body = public_binary(slug)
    expect(during == 403 and BENIGN not in during_body, f"rescan leaked prior authority {during}")
    run_worker()
    final = db_resource(rid)
    expect(final["scan_state"] == "safe" and final["published"] == 0 and final["scan_generation"] == 2, final)
    after, _, after_body = public_binary(slug)
    expect(after == 403 and BENIGN not in after_body, f"rescan auto-published {after}")
    return {"before_rescan_status": before, "during_rescan_status": during, "after_clean_rescan_status": after,
            "generation": final["scan_generation"], "published": final["published"]}

def case_t012():
    reset_case()
    status, _, _, created = upload("ws-a", "claim.txt", "text/plain", BENIGN)
    expect(status == 201, f"upload failed {status}")
    rid = int(created["id"])
    with fault_server("hold", hold_seconds=3.0) as (address, _):
        first = worker_popen(address, scan_timeout="8s", worker_id="p09-claim-a")
        try:
            wait_until(lambda: mysql(f"SELECT status FROM file_scan_attempts WHERE file_id={rid}") == "processing", 6, "first worker claim")
            second = worker_popen(address, scan_timeout="8s", worker_id="p09-claim-b")
            try:
                time.sleep(0.5)
                attempt_count = int(mysql(f"SELECT COUNT(*) FROM file_scan_attempts WHERE file_id={rid}"))
                processing = int(mysql(f"SELECT COUNT(*) FROM file_scan_attempts WHERE file_id={rid} AND status='processing'"))
                expect(attempt_count == 1 and processing == 1, f"duplicate claim attempts={attempt_count} processing={processing}")
            finally:
                terminate(second)
            out, _ = first.communicate(timeout=12)
            expect(first.returncode == 0, f"first worker failed: {out[-2000:]}")
        finally:
            terminate(first)
    scan = db_scan(rid)
    expect(scan["status"] == "clean", scan)
    return {"scan_attempt_count": 1, "concurrent_processing_count": 1, "final_scan_status": scan["status"]}

