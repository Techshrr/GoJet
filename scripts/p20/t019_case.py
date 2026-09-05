#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T019"
CASE_NAME = "Real custom-domain entitlement ownership HTTPS and risk workflow"


def read_exact_head(case_id: str) -> tuple[dict[str, Any] | None, str | None]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case_id}.json"
    if not path.is_file():
        return None, f"T019 requires same-run {case_id} evidence"
    try:
        evidence = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None, f"T019 could not read same-run {case_id} evidence"
    if evidence.get("status") != "PASS" or evidence.get("implementation_commit") != HEAD:
        return None, f"T019 same-run {case_id} evidence is not exact-head PASS"
    return evidence, None


def t019() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t018, error = read_exact_head("P20-T018")
    if error:
        errors.append(error)
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    assert t018 is not None
    t018_details = t018.get("details", {})
    if not isinstance(t018_details, dict) or t018_details.get("next_case") != CASE_ID:
        errors.append("T019 requires P20-T018 to unlock exactly P20-T019")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t019_formal_probe"],
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
        errors.append("T019 formal runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T019 runtime evidence is not bound to the exact candidate head")
    if runtime.get("status") != "PASS":
        errors.append("T019 formal runtime probe did not report PASS")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T019 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T019 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T019 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details["t018_user_correlation_preserved"] = details.get("user_id") == t018_details.get("user_id")
    details["t018_workspace_correlation_preserved"] = details.get("workspace_id") == t018_details.get("workspace_id")
    if details["t018_user_correlation_preserved"] is not True:
        errors.append("T019 lost the P20-T018 user identity correlation")
    if details["t018_workspace_correlation_preserved"] is not True:
        errors.append("T019 lost the P20-T018 Workspace identity correlation")

    required_true = (
        "real_mysql",
        "real_platform_api",
        "real_session_authenticated",
        "csrf_authority_issued",
        "auth_rate_window_respected",
        "t018_evidence_bound",
        "p06_entitlement_ticket_independence_proven",
        "p06_ownership_dns_https_risk_axes_proven",
        "p06_revalidation_history_proven",
        "p06_link_assignment_guard_proven",
        "p17_ticket_manager_cannot_grant_entitlement",
        "p17_entitlement_and_safety_conjunctive",
        "structured_entitlement_active",
        "support_ticket_reference_not_authority",
        "domain_create_identity_bound",
        "ingress_before_ownership_denied",
        "ingress_preflight_avoided_dns",
        "ownership_verified",
        "https_before_ingress_denied",
        "ingress_dns_valid",
        "risk_before_https_denied",
        "https_active",
        "real_dns_udp",
        "real_tls_handshake",
        "risk_review_persisted",
        "not_ready_link_zero_write",
        "risk_allow_persisted",
        "ready_for_new_links",
        "axes_conjunctive_ready",
        "ready_link_workspace_bound",
        "ready_link_hostname_bound",
        "revalidation_history_bound",
        "t018_user_correlation_preserved",
        "t018_workspace_correlation_preserved",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T019 frozen oracle missing true authority field: {field}")

    expected_status = {
        "login_http_status": 200,
        "domain_create_http_status": 201,
        "ready_link_http_status": 201,
    }
    for field, expected in expected_status.items():
        if details.get(field) != expected:
            errors.append(f"T019 frozen oracle expected {field}={expected}, got {details.get(field)!r}")

    if details.get("domain_create_row_delta") != 1:
        errors.append("T019 production create did not produce exactly one durable custom-domain row")
    if details.get("ready_link_row_delta") != 1:
        errors.append("T019 ready custom-domain assignment did not produce exactly one durable Link row")
    if not isinstance(details.get("domain_id"), int) or details.get("domain_id", 0) <= 0:
        errors.append("T019 Domain identifier is missing or invalid")
    if not isinstance(details.get("ready_link_id"), int) or details.get("ready_link_id", 0) <= 0:
        errors.append("T019 custom-domain Link identifier is missing or invalid")
    if str(details.get("domain_create_error_code") or "").strip():
        errors.append("T019 successful Domain create unexpectedly carries an error code")
    if details.get("ready_link_domain_kind") != "custom":
        errors.append("T019 ready Link did not persist custom domain kind")
    if details.get("domain_initial_routing_state") != "pending":
        errors.append("T019 new Domain did not begin with pending routing")
    if details.get("domain_initial_ownership_status") != "pending":
        errors.append("T019 new Domain did not begin with pending ownership")
    if details.get("domain_initial_ingress_status") != "pending":
        errors.append("T019 new Domain did not begin with pending ingress DNS")
    if details.get("domain_initial_https_status") != "pending":
        errors.append("T019 new Domain did not begin with pending HTTPS")
    if details.get("domain_initial_risk_status") != "missing":
        errors.append("T019 new Domain did not begin with missing domain risk")
    if details.get("final_ownership_status") != "verified":
        errors.append("T019 final ownership axis is not verified")
    if details.get("final_ingress_dns_status") != "valid":
        errors.append("T019 final ingress DNS axis is not valid")
    if details.get("final_https_status") != "active":
        errors.append("T019 final HTTPS axis is not active")
    if details.get("final_risk_status") != "allow":
        errors.append("T019 final domain risk axis is not allow")
    if not isinstance(details.get("domain_revalidation_rows"), int) or details.get("domain_revalidation_rows", 0) < 5:
        errors.append("T019 did not persist the required Domain revalidation/history evidence")
    if details.get("not_ready_link_http_status") == 201:
        errors.append("T019 not-ready custom-domain assignment unexpectedly succeeded")

    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T019 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False or details.get("audit_secret_leak") is not False:
        errors.append("T019 runtime evidence reported secret material")

    details["next_case"] = "P20-T020" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t019()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
