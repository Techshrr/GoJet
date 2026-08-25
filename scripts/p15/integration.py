#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

CONTRACT_AUTHORITY = "9ba89a42281709087b40cdcf0cb2eebd54952a99"
CASE = "P15-T001"
EVIDENCE = Path("artifacts/v10/P15/api/P15-T001.json")


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


def main() -> int:
    args = parse_args()
    if args.case != CASE:
        fail(f"only {CASE} is implemented at this stage; got {args.case}")
    if not os.environ.get("GOJET_MYSQL_DSN", "").strip():
        fail("GOJET_MYSQL_DSN is required")

    head = git("rev-parse", "HEAD")
    try:
        if git("merge-base", CONTRACT_AUTHORITY, "HEAD") != CONTRACT_AUTHORITY:
            fail(f"HEAD {head} does not descend from frozen P15 contract {CONTRACT_AUTHORITY}")
    except subprocess.CalledProcessError as exc:
        fail(f"cannot verify P15 contract ancestry: {exc.stderr.strip()}")

    proc = run("go", "run", "./scripts/p15/t001_runner.go", check=False)
    if not proc.stdout.strip():
        sys.stderr.write(proc.stderr)
        fail("P15-T001 runner produced no JSON output")
    try:
        runner = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        sys.stderr.write(proc.stdout)
        sys.stderr.write(proc.stderr)
        fail(f"P15-T001 runner output is not JSON: {exc}")

    checks = runner.get("checks") or {}
    all_checks_pass = bool(checks) and all(value is True for value in checks.values())
    runner_pass = proc.returncode == 0 and runner.get("case") == CASE and runner.get("status") == "PASS" and all_checks_pass

    evidence = {
        "node": "P15",
        "case": CASE,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": "python3 scripts/p15/integration.py --case P15-T001",
        "environment": {
            "mysql": "real MySQL 8.x service",
            "mysql_version": runner.get("mysql_version", ""),
            "platform_state": "native Go internal/auth Store against durable MySQL",
        },
        "record_counts": runner.get("record_counts", {}),
        "checks": checks,
        "source_blobs": {
            "migration_000015": blob("migrations/000015_authentication_oauth_account.sql"),
            "auth_store": blob("internal/auth/store.go"),
            "auth_model": blob("internal/auth/model.go"),
            "auth_opaque": blob("internal/auth/opaque.go"),
            "t001_runner": blob("scripts/p15/t001_runner.go"),
            "integration_driver": blob("scripts/p15/integration.py"),
            "frozen_test_plan": blob("artifacts/v10/P15/test-plan.json"),
        },
        "evidence_policy": {
            "raw_password_present": False,
            "raw_session_token_present": False,
            "raw_csrf_secret_present": False,
            "raw_oauth_subject_present": False,
            "raw_oauth_token_present": False,
        },
    }

    encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
    forbidden_fragments = (
        "gst_",
        "gcs_",
        "p15-t001-provider-subject-",
        "p15-t001-schema-fixture:",
    )
    leaked = [fragment for fragment in forbidden_fragments if fragment in encoded]
    if leaked:
        evidence["status"] = "FAIL"
        evidence["evidence_policy"]["forbidden_fragment_detected"] = True
        encoded = json.dumps(evidence, ensure_ascii=False, indent=2, sort_keys=True) + "\n"
        runner_pass = False

    EVIDENCE.parent.mkdir(parents=True, exist_ok=True)
    EVIDENCE.write_text(encoded, encoding="utf-8")

    digest = hashlib.sha256(encoded.encode("utf-8")).hexdigest()
    print(json.dumps({
        "case": CASE,
        "status": evidence["status"],
        "exact_head": head,
        "evidence": str(EVIDENCE),
        "evidence_sha256": digest,
    }, indent=2))

    if proc.stderr:
        sys.stderr.write(proc.stderr)
    return 0 if runner_pass else 1


if __name__ == "__main__":
    raise SystemExit(main())
