#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

CONTRACT_AUTHORITY = "30174f40df28678360f644b8fed79736906b0ea0"

CASE_CONFIG = {
    "P17-T025": {
        "evidence": "artifacts/v10/P17/webhooks/P17-T025.json",
        "case_source": "scripts/p17/webhook_case_runner/t025.go",
        "environment": "real MySQL 8.x plus real Redis 7.x using production Workspace webhook ownership/config authority",
    },
    "P17-T026": {
        "evidence": "artifacts/v10/P17/webhooks/P17-T026.json",
        "case_source": "scripts/p17/webhook_case_runner/t026.go",
        "environment": "real MySQL 8.x plus real Redis 7.x using production webhook HMAC signing and encrypted secret-rotation authority",
    },
    "P17-T027": {
        "evidence": "artifacts/v10/P17/webhooks/P17-T027.json",
        "case_source": "scripts/p17/webhook_case_runner/t027.go",
        "environment": "real MySQL 8.x plus real Redis 7.x with deterministic DNS/socket fixture exercising production P16 SSRF/rebinding/redirect guards",
    },
    "P17-T028": {
        "evidence": "artifacts/v10/P17/webhooks/P17-T028.json",
        "case_source": "scripts/p17/webhook_case_runner/t028.go",
        "environment": "real MySQL 8.x plus real Redis 7.x exercising durable webhook retry/idempotency/disable/restart authority",
    },
    "P17-T029": {
        "evidence": "artifacts/v10/P17/webhooks/P17-T029.json",
        "case_source": "scripts/p17/webhook_case_runner/t029.go",
        "environment": "real MySQL 8.x plus real Redis 7.x exercising webhook tenant/RBAC/audit/redaction authority",
    },
}

FORBIDDEN_EVIDENCE_FRAGMENTS = (
    "gwhsec_",
    "authorization: bearer",
    "client_secret",
    "db_password",
    "p17-root-password-fixture-2026",
    "p17-governance-root-password-fixture",
    "p17-platform-governance-root-password-fixture",
    "t029-payload-must-not-leak",
)


def run(*args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, check=check)


def git(*args: str) -> str:
    return run("git", *args).stdout.strip()


def blob(path: str) -> str:
    return git("rev-parse", f"HEAD:{path}")


def fail(message: str) -> None:
    raise SystemExit(message)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="GoJet V10 P17 real Workspace webhook evidence driver")
    parser.add_argument("--case", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    config = CASE_CONFIG.get(args.case)
    if config is None:
        fail(f"P17 webhook integration case is not implemented at this stage: {args.case}")
    if not os.environ.get("GOJET_MYSQL_DSN", "").strip():
        fail("GOJET_MYSQL_DSN is required")
    if not os.environ.get("GOJET_REDIS_ADDR", "").strip():
        fail("GOJET_REDIS_ADDR is required")

    head = git("rev-parse", "HEAD")
    try:
        if git("merge-base", CONTRACT_AUTHORITY, "HEAD") != CONTRACT_AUTHORITY:
            fail(f"HEAD {head} does not descend from frozen P17 contract {CONTRACT_AUTHORITY}")
    except subprocess.CalledProcessError as exc:
        fail(f"cannot verify P17 contract ancestry: {exc.stderr.strip()}")

    proc = run("go", "run", "./scripts/p17/webhook_case_runner", "--case", args.case, check=False)
    if not proc.stdout.strip():
        sys.stderr.write(proc.stderr)
        fail(f"{args.case} runner produced no JSON output")
    try:
        runner = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        sys.stderr.write(proc.stdout)
        sys.stderr.write(proc.stderr)
        fail(f"{args.case} runner output is not JSON: {exc}")

    checks = runner.get("checks") or {}
    runner_pass = (
        proc.returncode == 0
        and runner.get("case") == args.case
        and runner.get("status") == "PASS"
        and bool(checks)
        and all(value is True for value in checks.values())
    )

    source_paths = {
        "webhook_migration": "migrations/000029_workspace_webhooks.sql",
        "webhook_authority": "internal/admin/webhooks.go",
        "webhook_http": "internal/admin/http_webhooks.go",
        "webhook_unit_tests": "internal/admin/webhooks_test.go",
        "p16_outbound_guard": "internal/trust/inspection.go",
        "operationsmonitor": "services/platformapi/cmd/operationsmonitor/main.go",
        "platform_admin_mount": "services/platformapi/cmd/server/admin_access.go",
        "runner_main": "scripts/p17/webhook_case_runner/main.go",
        "runner_helpers": "scripts/p17/webhook_case_runner/helpers.go",
        "case_runner": str(config["case_source"]),
        "integration_router": "scripts/p17/integration.py",
        "webhook_evidence_driver": "scripts/p17/webhook_integration.py",
        "frozen_test_plan": "artifacts/v10/P17/test-plan.json",
    }
    source_blobs = {name: blob(path) for name, path in source_paths.items()}

    evidence = {
        "node": "P17",
        "case": args.case,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": f"python3 scripts/p17/integration.py --case {args.case}",
        "service_identity": "SVC-OPS-MONITOR operationsmonitor outbound-webhook delivery contribution",
        "environment": {
            "mysql": "real MySQL 8.x service",
            "mysql_version": runner.get("mysql_version", ""),
            "redis": "real Redis 7.x service",
            "redis_version": runner.get("redis_version", ""),
            "fixture": runner.get("fixture", ""),
            "platform_state": config["environment"],
        },
        "record_counts": runner.get("record_counts", {}),
        "checks": checks,
        "source_blobs": source_blobs,
        "evidence_policy": {
            "raw_webhook_secret_present": False,
            "raw_password_present": False,
            "raw_session_present": False,
            "raw_payload_marker_present": False,
            "dsn_present": False,
        },
    }

    encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    lowered = encoded.lower()
    leaked = [fragment for fragment in FORBIDDEN_EVIDENCE_FRAGMENTS if fragment in lowered]
    dsn = os.environ.get("GOJET_MYSQL_DSN", "")
    if dsn and dsn in encoded:
        leaked.append("GOJET_MYSQL_DSN")
        evidence["evidence_policy"]["dsn_present"] = True
    if leaked:
        evidence["status"] = "FAIL"
        evidence["evidence_policy"]["raw_webhook_secret_present"] = "gwhsec_" in leaked
        evidence["evidence_policy"]["raw_password_present"] = any("password" in item for item in leaked)
        evidence["evidence_policy"]["raw_session_present"] = any("session" in item or "bearer" in item for item in leaked)
        evidence["evidence_policy"]["raw_payload_marker_present"] = "t029-payload-must-not-leak" in leaked
        encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
        runner_pass = False

    path = Path(str(config["evidence"]))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(encoded, encoding="utf-8")
    digest = hashlib.sha256(encoded.encode()).hexdigest()
    print(json.dumps({
        "case": args.case,
        "status": evidence["status"],
        "exact_head": head,
        "evidence": str(path),
        "evidence_sha256": digest,
    }, indent=2))
    if proc.stderr:
        sys.stderr.write(proc.stderr)
    return 0 if runner_pass else 1


if __name__ == "__main__":
    raise SystemExit(main())
