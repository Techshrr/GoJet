#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T017"
CASE_NAME = "Real Text sharing lifecycle"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T017 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T017 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T017 same-run {case_id} evidence is not exact-head PASS"
    return evidence, None


def t017() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t016, error = read_exact_head("P20-T016")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t016 is not None
    t016_details = t016.get("details", {})
    if not isinstance(t016_details, dict) or t016_details.get("next_case") != CASE_ID:
        errors.append("T017 requires P20-T016 to unlock exactly P20-T017")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t017_probe"],
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
        errors.append("T017 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T017 runtime evidence is not bound to the exact candidate head")
    if runtime.get("status") != "PASS":
        errors.append("T017 runtime probe did not report PASS")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T017 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T017 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T017 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details["t016_user_correlation_preserved"] = details.get("user_id") == t016_details.get("user_id")
    details["t016_workspace_correlation_preserved"] = details.get("workspace_id") == t016_details.get("workspace_id")
    if details["t016_user_correlation_preserved"] is not True:
        errors.append("T017 lost the P20-T016 user identity correlation")
    if details["t016_workspace_correlation_preserved"] is not True:
        errors.append("T017 lost the P20-T016 Workspace identity correlation")

    required_true = (
        "real_mysql",
        "real_platform_api",
        "real_session_authenticated",
        "csrf_authority_issued",
        "t016_evidence_bound",
        "text_create_identity_bound",
        "text_public_slug_present",
        "workspace_read_content_matches",
        "public_content_matches",
        "permanent_noindex",
        "canonical_abuse_entry",
        "public_update_visible",
        "expired_content_hidden",
        "t016_user_correlation_preserved",
        "t016_workspace_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T017 frozen oracle missing true authority field: {field}")

    expected_status = {
        "login_http_status": 200,
        "text_create_http_status": 201,
        "workspace_read_http_status": 200,
        "public_read_http_status": 200,
        "text_update_http_status": 200,
        "text_expire_update_http_status": 200,
        "expired_public_http_status": 410,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(f"T017 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    if details.get("text_create_row_delta") != 1:
        errors.append("T017 create did not produce exactly one durable Text row")
    if details.get("text_update_version") != 2:
        errors.append("T017 committed Text update did not advance exactly to version 2")
    if not isinstance(details.get("text_id"), int) or details.get("text_id", 0) <= 0:
        errors.append("T017 Text identifier is missing or invalid")
    if str(details.get("text_create_error_code") or "").strip():
        errors.append("T017 successful Text create unexpectedly carries an error code")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T017 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T017 runtime evidence reported secret material")

    details["next_case"] = "P20-T018" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t017()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
