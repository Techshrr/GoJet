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
CASE_CONFIG = {
    "P15-T001": {
        "runner": "./scripts/p15/t001_runner",
        "evidence": Path("artifacts/v10/P15/api/P15-T001.json"),
        "environment": "native Go internal/auth Store against durable MySQL",
        "source_paths": {
            "migration_000015": "migrations/000015_authentication_oauth_account.sql",
            "auth_store": "internal/auth/store.go",
            "auth_model": "internal/auth/model.go",
            "auth_opaque": "internal/auth/opaque.go",
            "t001_runner": "scripts/p15/t001_runner/main.go",
        },
        "evidence_policy": {
            "raw_password_present": False,
            "raw_session_token_present": False,
            "raw_csrf_secret_present": False,
            "raw_oauth_subject_present": False,
            "raw_oauth_token_present": False,
        },
        "forbidden_fragments": (
            "gst_",
            "gcs_",
            "p15-t001-provider-subject-",
            "p15-t001-schema-fixture:",
        ),
    },
    "P15-T002": {
        "runner": "./scripts/p15/t002_runner",
        "evidence": Path("artifacts/v10/P15/api/P15-T002.json"),
        "environment": "native Go P15 registration service with durable MySQL and inherited P14 mail queue/template authority",
        "source_paths": {
            "migration_000015": "migrations/000015_authentication_oauth_account.sql",
            "migration_000016": "migrations/000016_auth_verification_mail.sql",
            "auth_registration": "internal/auth/registration.go",
            "secure_token_derivation": "internal/securetoken/grant.go",
            "p14_mail_enqueue_boundary": "internal/support/mail_enqueue.go",
            "p14_mail_core": "internal/support/mail.go",
            "p14_mail_store": "internal/support/mail_store.go",
            "t002_runner": "scripts/p15/t002_runner/main.go",
        },
        "evidence_policy": {
            "raw_password_present": False,
            "raw_verification_code_present": False,
            "raw_grant_key_present": False,
            "raw_session_token_present": False,
            "raw_oauth_token_present": False,
        },
        "forbidden_fragments": (
            "gvc_",
            "p15-t002-schema-secret",
        ),
    },
    "P15-T003": {
        "runner": "./scripts/p15/t003_runner",
        "evidence": Path("artifacts/v10/P15/security/P15-T003.json"),
        "environment": "native Go P15 email-verification consumption service with durable MySQL one-time grant authority",
        "source_paths": {
            "migration_000015": "migrations/000015_authentication_oauth_account.sql",
            "migration_000016": "migrations/000016_auth_verification_mail.sql",
            "auth_registration": "internal/auth/registration.go",
            "auth_verification": "internal/auth/verification.go",
            "auth_store": "internal/auth/store.go",
            "secure_token_derivation": "internal/securetoken/grant.go",
            "t003_runner": "scripts/p15/t003_runner/main.go",
        },
        "evidence_policy": {
            "raw_password_present": False,
            "raw_verification_code_present": False,
            "raw_grant_key_present": False,
            "raw_session_token_present": False,
            "raw_oauth_token_present": False,
        },
        "forbidden_fragments": (
            "gvc_",
            "GOJET_AUTH_GRANT_KEY_HEX",
        ),
    },
    "P15-T004": {
        "runner": "./scripts/p15/t004_runner",
        "evidence": Path("artifacts/v10/P15/api/P15-T004.json"),
        "environment": "native Go P15 password credential/login services with durable MySQL account/session authority",
        "source_paths": {
            "migration_000015": "migrations/000015_authentication_oauth_account.sql",
            "migration_000016": "migrations/000016_auth_verification_mail.sql",
            "auth_password": "internal/auth/password.go",
            "auth_login": "internal/auth/login.go",
            "auth_registration": "internal/auth/registration.go",
            "auth_verification": "internal/auth/verification.go",
            "auth_store": "internal/auth/store.go",
            "auth_opaque": "internal/auth/opaque.go",
            "t004_runner": "scripts/p15/t004_runner/main.go",
        },
        "evidence_policy": {
            "raw_password_present": False,
            "raw_verification_code_present": False,
            "raw_session_token_present": False,
            "raw_csrf_secret_present": False,
            "raw_grant_key_present": False,
            "client_asserted_identity_present": False,
        },
        "forbidden_fragments": (
            "P15-T004 Active Password!",
            "P15-T004 Pending Password!",
            "P15-T004 Locked Password!",
            "gst_",
            "gcs_",
            "GOJET_AUTH_GRANT_KEY_HEX",
        ),
    },
}


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
    config = CASE_CONFIG.get(args.case)
    if config is None:
        fail(f"P15 integration case is not implemented at this stage: {args.case}")
    if not os.environ.get("GOJET_MYSQL_DSN", "").strip():
        fail("GOJET_MYSQL_DSN is required")
    if args.case in ("P15-T002", "P15-T003", "P15-T004"):
        if not os.environ.get("GOJET_AUTH_GRANT_KEY_ID", "").strip() or not os.environ.get("GOJET_AUTH_GRANT_KEY_HEX", "").strip():
            fail(f"{args.case} grant-key runtime configuration is required")

    head = git("rev-parse", "HEAD")
    try:
        if git("merge-base", CONTRACT_AUTHORITY, "HEAD") != CONTRACT_AUTHORITY:
            fail(f"HEAD {head} does not descend from frozen P15 contract {CONTRACT_AUTHORITY}")
    except subprocess.CalledProcessError as exc:
        fail(f"cannot verify P15 contract ancestry: {exc.stderr.strip()}")

    proc = run("go", "run", config["runner"], check=False)
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

    source_blobs = {
        name: blob(path)
        for name, path in config["source_paths"].items()
    }
    source_blobs["integration_driver"] = blob("scripts/p15/integration.py")
    source_blobs["frozen_test_plan"] = blob("artifacts/v10/P15/test-plan.json")

    evidence = {
        "node": "P15",
        "case": args.case,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": f"python3 scripts/p15/integration.py --case {args.case}",
        "environment": {
            "mysql": "real MySQL 8.x service",
            "mysql_version": runner.get("mysql_version", ""),
            "platform_state": config["environment"],
        },
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

    evidence_path: Path = config["evidence"]
    evidence_path.parent.mkdir(parents=True, exist_ok=True)
    evidence_path.write_text(encoded, encoding="utf-8")

    digest = hashlib.sha256(encoded.encode("utf-8")).hexdigest()
    print(json.dumps({
        "case": args.case,
        "status": evidence["status"],
        "exact_head": head,
        "evidence": str(evidence_path),
        "evidence_sha256": digest,
    }, indent=2))

    if proc.stderr:
        sys.stderr.write(proc.stderr)
    return 0 if runner_pass else 1


if __name__ == "__main__":
    raise SystemExit(main())
