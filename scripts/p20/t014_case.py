#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T014"
CASE_NAME = "Real analytics transport aggregate and reconciliation"


def t014() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_redis_transport": True,
        "real_analyticsworker": True,
        "real_analyticsreconciler": True,
        "real_platform_api": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t013_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T013.json"
    if not t013_path.is_file():
        errors.append("T014 requires same-run T013 evidence")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    try:
        t013_evidence = json.loads(t013_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        errors.append("T014 could not read same-run T013 evidence")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    if t013_evidence.get("status") != "PASS" or t013_evidence.get("implementation_commit") != HEAD:
        errors.append("T014 same-run T013 evidence is not exact-head PASS")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t014_probe"],
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
        errors.append("T014 runtime runner did not produce safe JSON evidence")
        details["runner_exit_code"] = runner.returncode
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if runtime.get("implementation_commit") != HEAD:
        errors.append("T014 runtime evidence is not bound to the exact candidate head")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T014 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T014 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)
    if runner.returncode != 0 and not errors:
        errors.append("T014 runtime runner failed before completing the frozen oracle")
    if not isinstance(runtime_details, dict):
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    t013_details = t013_evidence.get("details", {})
    if not isinstance(t013_details, dict):
        errors.append("T014 T013 detail evidence is invalid")
    else:
        same_workspace = details.get("workspace_id") == t013_details.get("workspace_id")
        same_link = details.get("link_id") == t013_details.get("link_id")
        same_event = details.get("click_event_id") == t013_details.get("click_event_id")
        same_sequence = details.get("click_sequence") == t013_details.get("click_sequence") == 1
        details["t013_workspace_correlation_preserved"] = same_workspace
        details["t013_link_correlation_preserved"] = same_link
        details["t013_click_event_correlation_preserved"] = same_event
        details["t013_click_sequence_correlation_preserved"] = same_sequence
        details["t013_evidence_bound"] = True
        if not all((same_workspace, same_link, same_event, same_sequence)):
            errors.append("T014 analytics authority did not preserve the T013 Workspace/Link/click identity")

    required_true = (
        "real_mysql",
        "real_redis_transport",
        "real_analyticsworker",
        "real_analyticsreconciler",
        "real_platform_api",
        "real_session_authenticated",
        "t013_click_correlation_preserved",
        "t013_outbox_transport_bound",
        "published_stream_id_present",
        "analyticsworker_event_correlated",
        "analyticsworker_logged_processing",
        "first_reconciliation_idempotent",
        "first_reconciliation_logged_cycle",
        "second_reconciliation_idempotent",
        "second_reconciliation_logged_cycle",
        "reconciliation_idempotent",
        "analytics_report_correlated",
        "real_analytics_flow",
    )
    for field in required_true:
        if details.get(field) is not True:
            errors.append(f"T014 frozen oracle missing true authority field: {field}")

    if details.get("mysql_reporting_aggregate_before_reconcile") != 1:
        errors.append("T014 MySQL reporting aggregate is not exactly the correlated T013 click")
    if details.get("workspace_reporting_state") != "complete" or details.get("workspace_reporting_state_reason") != "reconciled":
        errors.append("T014 reporting completeness state is not complete/reconciled")
    if details.get("report_http_status") != 200:
        errors.append("T014 authenticated Analytics API report did not return HTTP 200")
    if details.get("report_total_clicks") != 1:
        errors.append("T014 authenticated Analytics API report did not return exactly one correlated click")
    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T014 runtime evidence reported mock/test-header authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T014 runtime evidence reported secret material")

    details["next_case"] = "P20-T015" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = t014()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
