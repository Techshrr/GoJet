#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T016"
CASE_NAME = "Real File quarantine scan publish and download"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T016 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T016 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T016 same-run {case_id} evidence is not exact-head PASS"
    return evidence, None


def read_p09(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P09" / "clamav" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T016 requires same-run {case_id} predecessor evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T016 could not read same-run {case_id} predecessor evidence"
    if (
        evidence.get("status") != "PASS"
        or evidence.get("implementation_commit") != HEAD
        or evidence.get("errors") not in ([], None)
    ):
        return None, f"T016 {case_id} predecessor evidence is not exact-head PASS"
    return evidence, None


def t016() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t015, error = read_exact_head("P20-T015")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t015 is not None
    t015_details = t015.get("details", {})
    if not isinstance(t015_details, dict) or t015_details.get("next_case") != CASE_ID:
        errors.append("T016 requires P20-T015 to unlock exactly P20-T016")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    p09_evidence: dict[str, dict[str, Any]] = {}
    for number in range(5, 11):
        case_id = f"P09-T{number:03d}"
        evidence, p09_error = read_p09(case_id)
        if p09_error:
            errors.append(p09_error)
        elif evidence is not None:
            p09_evidence[case_id] = evidence
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    clean_observations = p09_evidence["P09-T005"].get("observations", {})
    infected_observations = p09_evidence["P09-T006"].get("observations", {})
    if not isinstance(clean_observations, dict):
        errors.append("T016 P09-T005 observations are invalid")
    else:
        if clean_observations.get("scan_state") != "safe":
            errors.append("T016 P09-T005 does not prove a real safe ClamAV verdict")
        if not str(clean_observations.get("engine_version") or "").strip():
            errors.append("T016 P09-T005 lacks ClamAV engine authority")
        if not str(clean_observations.get("signature_version") or "").strip():
            errors.append("T016 P09-T005 lacks ClamAV signature authority")
    if not isinstance(infected_observations, dict):
        errors.append("T016 P09-T006 observations are invalid")
    elif (
        infected_observations.get("scan_state") != "blocked"
        or infected_observations.get("scan_status") != "infected"
    ):
        errors.append("T016 P09-T006 does not prove malware blocking")
    for number in range(7, 11):
        observations = p09_evidence[f"P09-T{number:03d}"].get("observations", {})
        if not isinstance(observations, dict) or observations.get("scan_state") != "scan_error":
            errors.append(
                f"T016 P09-T{number:03d} does not preserve fail-closed scan_error authority"
            )
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t016_probe"],
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
        errors.append("T016 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T016 runtime evidence is not bound to the exact candidate head")
    if runtime.get("status") != "PASS":
        errors.append("T016 runtime probe did not report PASS")
    if not isinstance(runtime_errors, list) or not all(
        isinstance(item, str) for item in runtime_errors
    ):
        errors.append("T016 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T016 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T016 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details["t015_user_correlation_preserved"] = (
        details.get("user_id") == t015_details.get("user_id")
    )
    details["t015_workspace_correlation_preserved"] = (
        details.get("workspace_id") == t015_details.get("workspace_id")
    )
    if details["t015_user_correlation_preserved"] is not True:
        errors.append("T016 lost the P20-T015 user identity correlation")
    if details["t015_workspace_correlation_preserved"] is not True:
        errors.append("T016 lost the P20-T015 Workspace identity correlation")

    required_true = (
        "real_mysql",
        "real_platform_api",
        "real_clamd_preflight",
        "native_fileworker",
        "real_session_authenticated",
        "csrf_authority_issued",
        "t015_evidence_bound",
        "p09_clamav_preflight_bound",
        "p09_fail_closed_preflight",
        "p09_t005_bound",
        "p09_t006_bound",
        "p09_t007_bound",
        "p09_t008_bound",
        "p09_t009_bound",
        "p09_t010_bound",
        "upload_quarantined",
        "upload_unpublished",
        "mysql_file_identity_bound",
        "quarantine_bytes_bound",
        "pre_scan_download_denied",
        "pre_publish_public_denied",
        "pre_scan_publish_denied",
        "real_clamav_scan",
        "scan_safe",
        "publish_authorized",
        "published_storage_bound",
        "workspace_download_digest_matches",
        "public_download_digest_matches",
        "t015_user_correlation_preserved",
        "t015_workspace_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T016 frozen oracle missing true authority field: {field}")

    expected_status = {
        "login_http_status": 200,
        "file_create_http_status": 201,
        "pre_scan_download_http_status": 409,
        "pre_publish_public_http_status": 403,
        "pre_scan_publish_http_status": 409,
        "publish_http_status": 200,
        "workspace_download_http_status": 200,
        "public_download_http_status": 200,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(
                f"T016 frozen oracle expected {field}={expected}, got {details.get(field)!r}"
            )

    if details.get("pre_scan_download_error_code") != "file_not_safe":
        errors.append("T016 pre-scan workspace download did not preserve file_not_safe")
    if details.get("pre_scan_publish_error_code") != "file_not_safe":
        errors.append("T016 pre-scan publish did not preserve file_not_safe")
    if details.get("file_create_row_delta") != 1:
        errors.append("T016 upload did not create exactly one durable Files row")
    if details.get("scan_status") != "clean" or details.get("scan_state") != "safe":
        errors.append("T016 scan did not resolve through a real clean/safe verdict")
    if not str(details.get("scan_engine_version") or "").strip():
        errors.append("T016 runtime clean verdict lacks ClamAV engine version")
    if not str(details.get("scan_signature_version") or "").strip():
        errors.append("T016 runtime clean verdict lacks ClamAV signature version")
    if str(details.get("scan_error_code") or "").strip():
        errors.append("T016 runtime clean verdict unexpectedly carries a scan error")
    if not isinstance(details.get("file_id"), int) or details.get("file_id", 0) <= 0:
        errors.append("T016 Files identifier is missing or invalid")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T016 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T016 runtime evidence reported secret material")

    details["next_case"] = "P20-T017" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t016()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
