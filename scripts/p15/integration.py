#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

from case_config_account_oauth import ACCOUNT_OAUTH_CASE_CONFIG
from case_config_core import CORE_CASE_CONFIG
from case_config_handoff_mail_audit import HANDOFF_MAIL_AUDIT_CASE_CONFIG
from case_config_security import SECURITY_CASE_CONFIG

CASE_CONFIG = {
    **CORE_CASE_CONFIG,
    **SECURITY_CASE_CONFIG,
    **ACCOUNT_OAUTH_CASE_CONFIG,
    **HANDOFF_MAIL_AUDIT_CASE_CONFIG,
}

CONTRACT_AUTHORITY = "9ba89a42281709087b40cdcf0cb2eebd54952a99"


def run(*args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(args, text=True, capture_output=True, check=check)


def git(*args: str) -> str:
    return run("git", *args).stdout.strip()


def blob(path: str) -> str:
    return git("rev-parse", f"HEAD:{path}")


def fail(message: str) -> None:
    raise SystemExit(message)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="GoJet V10 P15 real integration evidence driver")
    parser.add_argument("--case", required=True)
    return parser.parse_args()


def require_runtime(config: dict[str, object], case: str) -> None:
    if not os.environ.get("GOJET_MYSQL_DSN", "").strip():
        fail("GOJET_MYSQL_DSN is required")
    if config.get("requires_grant_key"):
        if not os.environ.get("GOJET_AUTH_GRANT_KEY_ID", "").strip() or not os.environ.get("GOJET_AUTH_GRANT_KEY_HEX", "").strip():
            fail(f"{case} grant-key runtime configuration is required")
    if config.get("requires_redis") and not os.environ.get("GOJET_REDIS_ADDR", "").strip():
        fail(f"{case} GOJET_REDIS_ADDR is required")
    if config.get("requires_csrf_key") and not os.environ.get("GOJET_AUTH_CSRF_KEY_HEX", "").strip():
        fail(f"{case} GOJET_AUTH_CSRF_KEY_HEX is required")
    if config.get("requires_oauth_key"):
        if not os.environ.get("GOJET_OAUTH_KEY_ID", "").strip() or not os.environ.get("GOJET_OAUTH_KEY_HEX", "").strip():
            fail(f"{case} OAuth encryption runtime configuration is required")


def main() -> int:
    args = parse_args()
    config = CASE_CONFIG.get(args.case)
    if config is None:
        fail(f"P15 integration case is not implemented at this stage: {args.case}")
    require_runtime(config, args.case)
    head = git("rev-parse", "HEAD")
    try:
        if git("merge-base", CONTRACT_AUTHORITY, "HEAD") != CONTRACT_AUTHORITY:
            fail(f"HEAD {head} does not descend from frozen P15 contract {CONTRACT_AUTHORITY}")
    except subprocess.CalledProcessError as exc:
        fail(f"cannot verify P15 contract ancestry: {exc.stderr.strip()}")

    proc = run("go", "run", str(config["runner"]), check=False)
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
    all_checks_pass = bool(checks) and all(value is True for value in checks.values())
    runner_pass = proc.returncode == 0 and runner.get("case") == args.case and runner.get("status") == "PASS" and all_checks_pass
    source_blobs = {name: blob(path) for name, path in dict(config["source_paths"]).items()}
    source_blobs["integration_driver"] = blob("scripts/p15/integration.py")
    source_blobs["core_case_configuration"] = blob("scripts/p15/case_config_core.py")
    source_blobs["security_case_configuration"] = blob("scripts/p15/case_config_security.py")
    source_blobs["account_oauth_case_configuration"] = blob("scripts/p15/case_config_account_oauth.py")
    source_blobs["handoff_mail_audit_case_configuration"] = blob("scripts/p15/case_config_handoff_mail_audit.py")
    source_blobs["frozen_test_plan"] = blob("artifacts/v10/P15/test-plan.json")

    environment = {
        "mysql": "real MySQL 8.x service",
        "mysql_version": runner.get("mysql_version", ""),
        "platform_state": config["environment"],
    }
    if config.get("requires_redis"):
        environment["redis"] = "real Redis service"
    if config.get("requires_csrf_key"):
        environment["csrf_key"] = "server-held 32-byte HMAC fixture; raw key excluded from evidence"
    if config.get("requires_oauth_key"):
        environment["oauth_key"] = "server-held 32-byte authenticated-encryption fixture; raw key excluded from evidence"

    evidence = {
        "node": "P15",
        "case": args.case,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": f"python3 scripts/p15/integration.py --case {args.case}",
        "environment": environment,
        "record_counts": runner.get("record_counts", {}),
        "checks": checks,
        "source_blobs": source_blobs,
        "evidence_policy": dict(config["evidence_policy"]),
    }
    encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    leaked = [fragment for fragment in config["forbidden_fragments"] if fragment in encoded]
    if leaked:
        evidence["status"] = "FAIL"
        evidence["evidence_policy"]["forbidden_fragment_detected"] = True
        encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
        runner_pass = False

    path = Path(str(config["evidence"]))
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(encoded, encoding="utf-8")
    digest = hashlib.sha256(encoded.encode()).hexdigest()
    print(json.dumps({"case": args.case, "status": evidence["status"], "exact_head": head, "evidence": str(path), "evidence_sha256": digest}, indent=2))
    if proc.stderr:
        sys.stderr.write(proc.stderr)
    return 0 if runner_pass else 1


if __name__ == "__main__":
    raise SystemExit(main())
