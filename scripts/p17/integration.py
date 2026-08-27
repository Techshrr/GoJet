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


def case(evidence: str, case_source: str, environment: str, runner: str = "case_runner") -> dict[str, object]:
    return {
        "evidence": evidence,
        "case_source": case_source,
        "environment": environment,
        "runner": runner,
    }


CASE_CONFIG = {
    "P17-T001": case(
        "artifacts/v10/P17/api/P17-T001.json",
        "scripts/p17/case_runner/t001.go",
        "real MySQL 8.x plus real Redis 7.x using production admin service for durable administrator/role/permission/audit authority",
    ),
    "P17-T002": case(
        "artifacts/v10/P17/security/P17-T002.json",
        "scripts/p17/case_runner/t002.go",
        "real MySQL 8.x plus real Redis 7.x and production Admin HTTP handler for login/session/TOTP/lock/rate/Origin/CSRF authority",
    ),
    "P17-T003": case(
        "artifacts/v10/P17/security/P17-T003.json",
        "scripts/p17/case_runner/t003.go",
        "real MySQL 8.x durable exact permission catalog and production server-side independent authorization authority",
    ),
    "P17-T004": case(
        "artifacts/v10/P17/security/P17-T004.json",
        "scripts/p17/case_runner/t004.go",
        "real MySQL 8.x administrator MFA/session authority for actor-bound listing, immediate revoke and replay-safe audit",
    ),
    "P17-T005": case(
        "artifacts/v10/P17/audit/P17-T005.json",
        "scripts/p17/case_runner/t005.go",
        "real MySQL 8.x high-risk mutation/idempotency/correlation authority plus database-enforced append-only secret-safe audit",
    ),
    "P17-T006": case(
        "artifacts/v10/P17/domain/P17-T006.json",
        "scripts/p17/domain_case_runner/t006.go",
        "real MySQL 8.x P06/P13/P14 entitlement queue/detail authority with production administrator permission checks",
        "domain_case_runner",
    ),
    "P17-T007": case(
        "artifacts/v10/P17/domain/P17-T007.json",
        "scripts/p17/domain_case_runner/t007.go",
        "real MySQL 8.x structured manual_approval and deny decisions resolved by inherited P06 entitlement authority",
        "domain_case_runner",
    ),
    "P17-T008": case(
        "artifacts/v10/P17/domain/P17-T008.json",
        "scripts/p17/domain_case_runner/t008.go",
        "real MySQL 8.x entitlement control/source materialization, routing impact, P16 safety check and immutable decision ledger",
        "domain_case_runner",
    ),
    "P17-T009": case(
        "artifacts/v10/P17/domain/P17-T009.json",
        "scripts/p17/domain_case_runner/t009.go",
        "real MySQL 8.x inherited P06/P13 source precedence/domain_limit/grace plus P16 conjunctive safety authority",
        "domain_case_runner",
    ),
}

FORBIDDEN_EVIDENCE_FRAGMENTS = (
    "p17-root-password-fixture-2026",
    "p17-t005-child-raw-password-marker",
    "p17-tickets-only-password-fixture",
    "definitely-wrong-password",
    "authorization: bearer",
    "client_secret",
    "db_password",
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
    parser = argparse.ArgumentParser(description="GoJet V10 P17 real administrator authority evidence driver")
    parser.add_argument("--case", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    config = CASE_CONFIG.get(args.case)
    if config is None:
        fail(f"P17 integration case is not implemented at this stage: {args.case}")
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

    runner_dir = str(config["runner"])
    proc = run("go", "run", f"./scripts/p17/{runner_dir}", "--case", args.case, check=False)
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
    runner_pass = (
        proc.returncode == 0
        and runner.get("case") == args.case
        and runner.get("status") == "PASS"
        and all_checks_pass
    )

    source_paths = {
        "migration": "migrations/000025_admin_access_audit.sql",
        "admin_model": "internal/admin/model.go",
        "admin_service": "internal/admin/service.go",
        "admin_audit": "internal/admin/audit.go",
        "admin_governance_roles": "internal/admin/governance_roles.go",
        "admin_governance_administrators": "internal/admin/governance_administrators.go",
        "admin_governance_mfa_sessions": "internal/admin/governance_mfa_sessions.go",
        "redis_login_limiter": "internal/admin/redis_limiter.go",
        "admin_http_auth": "internal/admin/http_auth.go",
        "admin_http_governance": "internal/admin/http_governance.go",
        "platform_admin_mount": "services/platformapi/cmd/server/admin_access.go",
        "fixture": "scripts/p17/adminfixture/fixture.go",
        "runner_main": "scripts/p17/case_runner/main.go" if runner_dir == "case_runner" else "scripts/p17/domain_case_runner/main.go",
        "case_runner": str(config["case_source"]),
    }
    if args.case in {"P17-T006", "P17-T007", "P17-T008", "P17-T009"}:
        source_paths.update({
            "domain_entitlement_migration": "migrations/000026_admin_domain_entitlements.sql",
            "admin_domain_entitlements_read": "internal/admin/domain_entitlements_read.go",
            "admin_domain_entitlements_decision": "internal/admin/domain_entitlements_decision.go",
            "admin_domain_entitlements_mutation": "internal/admin/domain_entitlements_mutation.go",
            "admin_domain_entitlement_http": "internal/admin/http_domain_entitlements.go",
            "p06_entitlement_resolver": "internal/domains/entitlement.go",
            "p06_entitlement_store": "internal/domains/store_mysql.go",
            "p06_domain_store": "internal/domains/domain_store_mysql.go",
            "p17_entitlement_control_overlay": "internal/domains/entitlement_admin_control.go",
            "p06_domain_mutation_authority": "internal/domains/mutation_authority.go",
            "domain_runner_helpers": "scripts/p17/domain_case_runner/helpers.go",
        })
    source_blobs = {name: blob(path) for name, path in source_paths.items()}
    source_blobs["integration_driver"] = blob("scripts/p17/integration.py")
    source_blobs["frozen_test_plan"] = blob("artifacts/v10/P17/test-plan.json")

    evidence = {
        "node": "P17",
        "case": args.case,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": f"python3 scripts/p17/integration.py --case {args.case}",
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
            "raw_password_present": False,
            "raw_totp_present": False,
            "raw_session_present": False,
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
        evidence["evidence_policy"]["raw_password_present"] = any("password" in item for item in leaked)
        evidence["evidence_policy"]["raw_totp_present"] = any("totp" in item for item in leaked)
        evidence["evidence_policy"]["raw_session_present"] = any("session" in item or "token" in item for item in leaked)
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
