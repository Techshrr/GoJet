#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T020"
CASE_NAME = "Real support ticket reply and mail workflow"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T020 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T020 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T020 same-run {case_id} evidence is not exact-head PASS"
    errors = evidence.get("errors")
    if errors != []:
        return None, f"T020 same-run {case_id} evidence carries an error ledger"
    return evidence, None


def t020() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t019, error = read_exact_head("P20-T019")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t019 is not None
    t019_details = t019.get("details", {})
    if not isinstance(t019_details, dict) or t019_details.get("next_case") != CASE_ID:
        errors.append("T020 requires P20-T019 to unlock exactly P20-T020")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    if not str(t019_details.get("user_id") or "").strip() or not str(t019_details.get("workspace_id") or "").strip():
        errors.append("T020 requires P20-T019 correlated user and Workspace identities")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t020_probe"],
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    try:
        runtime = json.loads(runner.stdout)
    except json.JSONDecodeError:
        runtime = None

    if not isinstance(runtime, dict):
        errors.append("T020 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T020 runtime evidence is not bound to the exact candidate head")
    if runtime.get("status") != "PASS":
        errors.append("T020 full Support lifecycle runtime probe did not report PASS")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T020 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T020 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T020 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details["t019_user_correlation_preserved"] = details.get("user_id") == t019_details.get("user_id")
    details["t019_workspace_correlation_preserved"] = details.get("workspace_id") == t019_details.get("workspace_id")
    if details["t019_user_correlation_preserved"] is not True:
        errors.append("T020 lost the P20-T019 user identity correlation")
    if details["t019_workspace_correlation_preserved"] is not True:
        errors.append("T020 lost the P20-T019 Workspace identity correlation")

    required_true = (
        "real_mysql",
        "real_platform_api",
        "real_session_authenticated",
        "csrf_authority_issued",
        "session_id_present",
        "auth_rate_window_respected",
        "t019_evidence_bound",
        "p14_real_integration_bound",
        "p14_ticket_lifecycle_proven",
        "p14_attachment_clamav_proven",
        "p14_turnstile_proven",
        "p14_mail_smtp_retry_proven",
        "p14_admin_ticket_mail_permission_proven",
        "p14_audit_correlation_proven",
        "ticket_create_identity_bound",
        "ticket_id_present",
        "requester_reply_persisted",
        "p17_admin_session_authenticated",
        "p17_tickets_manage",
        "p17_mail_manage",
        "p17_admin_id_present",
        "admin_reply_persisted",
        "admin_reply_message_id_present",
        "admin_reply_mail_job_bound",
        "admin_reply_mail_job_id_present",
        "native_mailworker",
        "real_smtp_delivery",
        "attachment_id_present",
        "attachment_bound_to_admin_reply",
        "real_clamav_infected",
        "infected_attachment_download_blocked",
        "t020_full_lifecycle_proven",
        "t019_user_correlation_preserved",
        "t019_workspace_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T020 frozen oracle missing true authority field: {field}")

    expected_status = {
        "login_http_status": 200,
        "ticket_create_http_status": 201,
        "requester_reply_http_status": 201,
        "admin_reply_http_status": 201,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(f"T020 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    expected_delta = {
        "ticket_create_row_delta": 1,
        "ticket_message_row_delta": 1,
        "requester_reply_row_delta": 1,
        "admin_reply_row_delta": 1,
    }
    for field, expected in expected_delta.items():
        if details.get(field) != expected:
            errors.append(f"T020 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    if details.get("workspace_role") != "owner":
        errors.append("T020 predecessor identity is not preserved as owner Workspace authority")
    if details.get("admin_reply_mail_initial_status") != "queued":
        errors.append("T020 Admin reply mail job did not begin durably queued")
    if details.get("admin_reply_mail_final_status") != "sent":
        errors.append("T020 Admin reply mail job did not reach durable sent")
    if details.get("admin_reply_mail_attempt_status") != "sent":
        errors.append("T020 Admin reply mail attempt did not reach durable sent")
    if details.get("attachment_scan_status") != "infected":
        errors.append("T020 EICAR attachment did not persist infected")

    smtp_deliveries = details.get("smtp_deliveries")
    if not isinstance(smtp_deliveries, int) or smtp_deliveries < 1:
        errors.append("T020 real SMTP sink did not record a delivery")
    if details.get("smtp_message_sha256_length") != 64:
        errors.append("T020 SMTP evidence did not capture a 64-character message SHA-256")

    auth_window = details.get("auth_rate_window_seconds")
    if not isinstance(auth_window, int) or auth_window <= 0 or auth_window > 300:
        errors.append("T020 authentication rate-window evidence is invalid")

    for field in ("ticket_create_error_code", "requester_reply_error_code", "admin_reply_error_code"):
        if str(details.get(field) or "").strip():
            errors.append(f"T020 successful lifecycle unexpectedly carries {field}")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T020 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T020 runtime evidence reported secret material")

    details["next_case"] = "P20-T021" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t020()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
