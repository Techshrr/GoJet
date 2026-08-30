#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T015"
CASE_NAME = "Real QR lifecycle and decode"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T015 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T015 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T015 same-run {case_id} evidence is not exact-head PASS"
    return evidence, None


def t015() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_redis": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t014, error = read_exact_head("P20-T014")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    t013, error = read_exact_head("P20-T013")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t014 is not None and t013 is not None

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t015_probe"],
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
        errors.append("T015 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T015 runtime evidence is not bound to the exact candidate head")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T015 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T015 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T015 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    t014_details = t014.get("details", {})
    t013_details = t013.get("details", {})
    if not isinstance(t014_details, dict) or not isinstance(t013_details, dict):
        errors.append("T015 predecessor detail evidence is invalid")
    else:
        same_workspace = details.get("workspace_id") == t014_details.get("workspace_id") == t013_details.get("workspace_id")
        same_link = details.get("source_link_id") == t014_details.get("link_id") == t013_details.get("link_id")
        same_user = details.get("user_id") == t013_details.get("user_id")
        details["t014_workspace_correlation_preserved"] = same_workspace
        details["t014_link_correlation_preserved"] = same_link
        details["t013_user_correlation_preserved"] = same_user
        details["t013_evidence_bound"] = True
        details["t014_evidence_bound"] = True
        if not all((same_workspace, same_link, same_user)):
            errors.append("T015 QR authority did not preserve predecessor user/Workspace/Link identity")

    required_true = (
        "real_mysql",
        "real_redis",
        "real_platform_api",
        "real_qr_renderer",
        "real_session_authenticated",
        "csrf_authority_issued",
        "source_risk_allow",
        "source_fingerprint_present",
        "mysql_qr_identity_bound",
        "detail_identity_bound",
        "independent_decoder_invoked",
        "independent_decode_correlated",
        "png_digest_header_matches",
        "svg_digest_header_matches",
        "permission_continuity",
        "workspace_role_restored",
        "t013_identity_bound",
        "t014_evidence_bound",
        "t014_workspace_correlation_preserved",
        "t014_link_correlation_preserved",
        "t013_user_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T015 frozen oracle missing true authority field: {field}")

    expected_status = {
        "qr_create_http_status": 201,
        "detail_http_status": 200,
        "preview_png_status": 200,
        "download_png_status": 200,
        "download_svg_status": 200,
        "viewer_detail_http_status": 200,
        "viewer_download_http_status": 200,
        "viewer_create_http_status": 403,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(f"T015 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    if details.get("qr_create_row_delta") != 1:
        errors.append("T015 QR creation did not produce exactly one durable QR row")
    if details.get("qr_create_state") != "ready":
        errors.append("T015 QR creation did not enter ready state")
    if details.get("viewer_create_error_code") != "read_only":
        errors.append("T015 viewer mutation denial did not preserve read_only semantics")
    if not isinstance(details.get("qr_id"), int) or details.get("qr_id", 0) <= 0:
        errors.append("T015 QR identifier is missing or invalid")

    expected_url = details.get("expected_public_url")
    correlated_urls = (
        details.get("qr_create_public_url"),
        details.get("png_decoded"),
        details.get("svg_decoded"),
    )
    if not isinstance(expected_url, str) or not expected_url.startswith("https://"):
        errors.append("T015 expected canonical QR target is invalid")
    elif any(value != expected_url for value in correlated_urls):
        errors.append("T015 rendered/decoded QR assets do not preserve the authoritative canonical target")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T015 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T015 runtime evidence reported secret material")

    details["next_case"] = "P20-T016" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t015()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
