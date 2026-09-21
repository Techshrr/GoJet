#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T018"
CASE_NAME = "Real Bio lifecycle and outbound safety"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T018 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T018 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T018 same-run {case_id} evidence is not exact-head PASS"
    return evidence, None


def t018() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_redis": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t017, error = read_exact_head("P20-T017")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t017 is not None
    t017_details = t017.get("details", {})
    if not isinstance(t017_details, dict) or t017_details.get("next_case") != CASE_ID:
        errors.append("T018 requires P20-T017 to unlock exactly P20-T018")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t018_probe"],
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
        errors.append("T018 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T018 runtime evidence is not bound to the exact candidate head")
    if runtime.get("status") != "PASS":
        errors.append("T018 runtime probe did not report PASS")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T018 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T018 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T018 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details["t017_user_correlation_preserved"] = details.get("user_id") == t017_details.get("user_id")
    details["t017_workspace_correlation_preserved"] = details.get("workspace_id") == t017_details.get("workspace_id")
    if details["t017_user_correlation_preserved"] is not True:
        errors.append("T018 lost the P20-T017 user identity correlation")
    if details["t017_workspace_correlation_preserved"] is not True:
        errors.append("T018 lost the P20-T017 Workspace identity correlation")

    required_true = (
        "real_mysql",
        "real_redis",
        "real_platform_api",
        "real_session_authenticated",
        "csrf_authority_issued",
        "t017_evidence_bound",
        "auth_rate_window_respected",
        "bio_create_identity_bound",
        "bio_slug_present",
        "unresolved_publish_state_unchanged",
        "risk_allow_seeded",
        "permanent_noindex",
        "outbound_allow_visible",
        "outbound_ugc_nofollow",
        "public_api_allow_url_matches",
        "sitemap_excluded",
        "destination_fingerprint_changed",
        "risk_invalidated_to_review",
        "invalidated_new_url_hidden",
        "stale_old_url_hidden",
        "t017_user_correlation_preserved",
        "t017_workspace_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T018 frozen oracle missing true authority field: {field}")

    expected_status = {
        "login_http_status": 200,
        "bio_create_http_status": 201,
        "unresolved_publish_http_status": 409,
        "publish_http_status": 200,
        "public_html_http_status": 200,
        "public_api_http_status": 200,
        "bio_update_http_status": 200,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(f"T018 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    if details.get("bio_create_row_delta") != 1:
        errors.append("T018 create did not produce exactly one durable Bio row")
    if details.get("publish_version") != 2:
        errors.append("T018 publish did not advance the Bio page exactly to version 2")
    if not isinstance(details.get("bio_id"), int) or details.get("bio_id", 0) <= 0:
        errors.append("T018 Bio identifier is missing or invalid")
    if not isinstance(details.get("bio_child_id"), int) or details.get("bio_child_id", 0) <= 0:
        errors.append("T018 Bio child identifier is missing or invalid")
    if str(details.get("bio_create_error_code") or "").strip():
        errors.append("T018 successful Bio create unexpectedly carries an error code")
    if details.get("unresolved_publish_error_code") != "child_link_risk_unresolved":
        errors.append("T018 unresolved child risk did not return child_link_risk_unresolved")
    if details.get("sitemap_bio_hits") not in ([], None):
        errors.append("T018 Bio UGC appeared in sitemap evidence")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T018 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T018 runtime evidence reported secret material")

    details["next_case"] = "P20-T019" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t018()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
