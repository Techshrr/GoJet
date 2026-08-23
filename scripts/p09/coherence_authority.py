from coherence_common import *

def valid_sha256(value: Any) -> bool:
    return isinstance(value, str) and len(value) == 64 and all(char in "0123456789abcdef" for char in value)


def validate_storage_scan(cases: dict[str, dict[str, Any]], errors: list[str]) -> dict[str, Any]:
    t001, t005, t006 = (details(cases.get(case_id, {})) for case_id in ("P09-T001", "P09-T005", "P09-T006"))
    req(t001.get("scan_state") == "quarantined" and t001.get("published") == 0 and t001.get("public_status") == 403, "T001 quarantine/private authority drift", errors)
    req(t001.get("storage_key_length") == 64 and t001.get("quarantine_mode") == "0o600", "T001 randomized secure object evidence drift", errors)
    for label, record in (("T001", t001), ("T005", t005), ("T006", t006)):
        req(isinstance(record.get("file_id"), int) and record["file_id"] > 0, f"{label} file_id missing", errors)
        req(valid_sha256(record.get("content_sha256")), f"{label} content_sha256 missing", errors)
        req(record.get("storage_sha256") == record.get("content_sha256"), f"{label} storage/content digest mismatch", errors)
    for label, record in (("T005", t005), ("T006", t006)):
        req(isinstance(record.get("scan_attempt_id"), int) and record["scan_attempt_id"] > 0, f"{label} scan_attempt_id missing", errors)
        req(record.get("scan_generation") == 1, f"{label} scan_generation={record.get('scan_generation')}", errors)
    req(t005.get("scan_state") == "safe" and t005.get("scan_status") == "clean" and t005.get("published") == 0, "T005 clean/safe/private authority drift", errors)
    req(t006.get("scan_state") == "blocked" and t006.get("scan_status") == "infected" and t006.get("public_status") == 403, "T006 EICAR block authority drift", errors)
    req(bool(t006.get("verdict_code")), "T006 infected verdict code missing", errors)
    req(bool(t005.get("engine_version")) and bool(t005.get("signature_version")), "T005 real ClamAV identity missing", errors)
    req(t005.get("engine_version") == t006.get("engine_version") and t005.get("signature_version") == t006.get("signature_version"), "real ClamAV clean/infected engine-signature mismatch within Integration producer", errors)
    return {"clean": t005, "infected": t006, "quarantine": t001}


def validate_fail_closed(cases: dict[str, dict[str, Any]], errors: list[str]) -> None:
    expected = {"P09-T007": "clamav_unavailable", "P09-T009": "signature_stale", "P09-T010": "indeterminate_response"}
    for case_id, code in expected.items():
        record = details(cases.get(case_id, {}))
        req(record.get("scan_state") == "scan_error" and record.get("error_code") == code, f"{case_id} fail-closed drift: {record}", errors)
    t008 = details(cases.get("P09-T008", {}))
    req(t008.get("scan_state") == "scan_error" and t008.get("error_code") in {"scan_read_failed", "scan_write_failed"} and t008.get("public_status") == 403, f"T008 timeout fail-closed drift: {t008}", errors)


def validate_authority(cases: dict[str, dict[str, Any]], errors: list[str]) -> None:
    t002, t003, t004 = (details(cases.get(case_id, {})) for case_id in ("P09-T002", "P09-T003", "P09-T004"))
    req(t002 == {"status": 400, "file_count": 0, "quarantine_files": 0}, f"T002 type policy evidence drift: {t002}", errors)
    req(t003.get("statuses") == [201, 201, 429] and t003.get("file_count") == 2 and t003.get("counter") == 2, f"T003 quota drift: {t003}", errors)
    req((t004.get("viewer_get"), t004.get("viewer_mutation"), t004.get("cross_tenant"), t004.get("header_path_mismatch")) == (200, 403, 404, 403), f"T004 RBAC drift: {t004}", errors)
    t011 = details(cases.get("P09-T011", {}))
    req((t011.get("before_rescan_status"), t011.get("during_rescan_status"), t011.get("after_clean_rescan_status"), t011.get("generation"), t011.get("published")) == (200, 403, 403, 2, 0), f"T011 rescan authority drift: {t011}", errors)
    t012 = details(cases.get("P09-T012", {}))
    req(t012.get("scan_attempt_count") == 1 and t012.get("concurrent_processing_count") == 1 and t012.get("final_scan_status") == "clean", f"T012 claim idempotency drift: {t012}", errors)
    t013 = details(cases.get("P09-T013", {}))
    req(t013.get("attempt_count") == 1 and isinstance(t013.get("scan_attempt_id"), int) and t013.get("scan_generation") == 1 and t013.get("scan_state") == "safe" and t013.get("published") == 0 and t013.get("scan_status") == "clean", f"T013 recovery drift: {t013}", errors)
    t014 = details(cases.get("P09-T014", {}))
    req(t014.get("root_mode") == "0o700" and t014.get("quarantine_mode") == "0o700" and t014.get("published_mode") == "0o700" and t014.get("object_mode") == "0o600" and t014.get("storage_key_leaked") is False and all(status != 200 for status in t014.get("direct_probe_statuses", [])), f"T014 storage isolation drift: {t014}", errors)
    t015 = details(cases.get("P09-T015", {}))
    req((t015.get("premature_publish"), t015.get("safe_auto_published"), t015.get("viewer_publish"), t015.get("admin_publish")) == (409, False, 403, 200), f"T015 publish authority drift: {t015}", errors)
    t016 = details(cases.get("P09-T016", {}))
    req((t016.get("preauth_status"), t016.get("wrong_password_status"), t016.get("correct_password_status"), t016.get("first_download"), t016.get("second_download"), t016.get("download_count")) == (403, 403, 303, 200, 410, 1) and t016.get("password_plaintext_stored") is False and t016.get("cookie_plaintext") is False, f"T016 public auth/limit drift: {t016}", errors)
    t017 = details(cases.get("P09-T017", {}))
    req(t017.get("expired_status") == 410 and t017.get("deleted_status") == 410 and t017.get("deleted_storage_removed") is True and t017.get("active_count_after_delete") == 1, f"T017 lifecycle drift: {t017}", errors)
    t018 = details(cases.get("P09-T018", {}))
    req((t018.get("invalid_id"), t018.get("unknown"), t018.get("cross_tenant"), t018.get("header_path_mismatch"), t018.get("malformed_json"), t018.get("missing_storage")) == (400, 404, 404, 403, 400, 503) and t018.get("private_detail_leaked") is False, f"T018 stable errors drift: {t018}", errors)


def validate_health(cases: dict[str, dict[str, Any]], errors: list[str]) -> None:
    t019, t020 = details(cases.get("P09-T019", {})), details(cases.get("P09-T020", {}))
    req(t019.get("healthy_exit") == 0 and all(t019.get(key) == 2 for key in ("daemon_down_exit", "timeout_exit", "stale_exit", "indeterminate_exit", "permission_error_exit")), f"T019 mandatory preflight matrix drift: {t019}", errors)
    req(t019.get("installer_healthy_state") == "step-pass" and t019.get("installer_fault_state") == "hard-failure" and t019.get("preflight_secret_safe") is True and t019.get("p22_release_closure_claimed") is False, f"T019 installer authority drift: {t019}", errors)
    req(bool(t019.get("engine_version")) and bool(t019.get("signature_version")), "T019 real ClamAV identity missing", errors)
    req(t020.get("ready") is True and t020.get("status") == "healthy" and t020.get("storage_state") == "healthy" and t020.get("storage_writable") is True and t020.get("clamav_state") == "healthy" and t020.get("secret_safe") is True, f"T020 health authority drift: {t020}", errors)
    req((t020.get("authorized_status"), t020.get("viewer_status"), t020.get("missing_actor_status")) == (200, 403, 403), f"T020 health RBAC drift: {t020}", errors)
    req(t020.get("p17_admin_completion_claimed") is False and t020.get("p22_installer_completion_claimed") is False, "T020 later-node overclaim", errors)
    req(bool(t020.get("engine_version")) and bool(t020.get("signature_version")), "T020 real ClamAV identity missing", errors)

