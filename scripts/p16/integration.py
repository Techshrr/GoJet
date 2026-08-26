#!/usr/bin/env python3
from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
from pathlib import Path

CONTRACT_AUTHORITY = "43c5d4d7e1833c593ceacb48016abac6e3133893"

CASE_CONFIG = {
    "P16-T001": {
        "runner": "./scripts/p16/t001_runner",
        "evidence": "artifacts/v10/P16/api/P16-T001.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "trust_model": "internal/trust/model.go",
            "trust_store": "internal/trust/store.go",
            "runner": "scripts/p16/t001_runner/main.go",
        },
        "environment": "real MySQL 8.x durable destination-risk schema/correlation fixture",
        "requires_mysql": True,
        "requires_redis": False,
    },
    "P16-T002": {
        "runner": "./scripts/p16/t002_runner",
        "evidence": "artifacts/v10/P16/api/P16-T002.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "links_fingerprint": "internal/links/model.go",
            "trust_model": "internal/trust/model.go",
            "trust_store": "internal/trust/store.go",
            "runner": "scripts/p16/t002_runner/main.go",
        },
        "environment": "real MySQL 8.x plus native trust.Store exact-fingerprint enqueue/dedupe/rescan fixture",
        "requires_mysql": True,
        "requires_redis": False,
    },
    "P16-T003": {
        "runner": "./scripts/p16/t003_runner",
        "evidence": "artifacts/v10/P16/security/P16-T003.json",
        "source_paths": {
            "links_normalization": "internal/links/model.go",
            "inspection_guard": "internal/trust/inspection.go",
            "runner": "scripts/p16/t003_runner/main.go",
        },
        "environment": "deterministic in-process DNS fixture proving canonical HTTP/HTTPS admission and pre-provider unsafe-form rejection",
        "requires_mysql": False,
        "requires_redis": False,
    },
    "P16-T004": {
        "runner": "./scripts/p16/t004_runner",
        "evidence": "artifacts/v10/P16/security/P16-T004.json",
        "source_paths": {
            "inspection_guard": "internal/trust/inspection.go",
            "runner": "scripts/p16/t004_runner/main.go",
        },
        "environment": "controlled in-process DNS scripts and local HTTP redirect fixture proving SSRF, rebinding and redirect-to-private rejection",
        "requires_mysql": False,
        "requires_redis": False,
    },
    "P16-T005": {
        "runner": "./scripts/p16/t005_runner",
        "evidence": "artifacts/v10/P16/risk/P16-T005.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "inspection_guard": "internal/trust/inspection.go",
            "provider": "internal/trust/provider.go",
            "policy": "internal/trust/policy.go",
            "policy_store": "internal/trust/policy_store.go",
            "runner": "scripts/p16/t005_runner/main.go",
        },
        "environment": "real MySQL 8.x plus local deterministic semantic-provider HTTP fixture and versioned local policy authority",
        "requires_mysql": True,
        "requires_redis": False,
    },
    "P16-T006": {
        "runner": "./scripts/p16/t006_runner",
        "evidence": "artifacts/v10/P16/security/P16-T006.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "inspection_guard": "internal/trust/inspection.go",
            "provider": "internal/trust/provider.go",
            "policy": "internal/trust/policy.go",
            "policy_store": "internal/trust/policy_store.go",
            "runner": "scripts/p16/t006_runner/main.go",
        },
        "environment": "real MySQL 8.x plus deterministic timeout/transport/partial/malformed/unavailable provider protocol fixtures",
        "requires_mysql": True,
        "requires_redis": False,
    },
    "P16-T007": {
        "runner": "./scripts/p16/t007_runner",
        "evidence": "artifacts/v10/P16/risk/P16-T007.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "worker_store": "internal/trust/worker_store.go",
            "worker": "internal/trust/worker.go",
            "policy_store": "internal/trust/policy_store.go",
            "operationsmonitor": "services/platformapi/cmd/operationsmonitor/main.go",
            "runner": "scripts/p16/t007_runner/main.go",
        },
        "environment": "real MySQL 8.x durable SKIP LOCKED lease/retry/recovery plus native fixed SVC-OPS-MONITOR operationsmonitor execution",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T008": {
        "runner": "./scripts/p16/t008_runner",
        "evidence": "artifacts/v10/P16/risk/P16-T008.json",
        "source_paths": {
            "migration": "migrations/000020_destination_risk.sql",
            "links_fingerprint": "internal/links/model.go",
            "inherited_redis_risk": "internal/links/risk_redis.go",
            "policy_store": "internal/trust/policy_store.go",
            "projection": "internal/trust/projection.go",
            "runner": "scripts/p16/t008_runner/main.go",
        },
        "environment": "real MySQL 8.x durable-current decision authority projected through inherited P05 exact-fingerprint RedisRiskStore into real Redis 7.x",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T009": {
        "runner": "./scripts/p16/t009_runner",
        "evidence": "artifacts/v10/P16/security/P16-T009.json",
        "source_paths": {
            "inherited_redirect": "internal/links/redirect_http.go",
            "inherited_redis_risk": "internal/links/risk_redis.go",
            "projection": "internal/trust/projection.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t009_runner/main.go",
        },
        "environment": "real MySQL 8.x, real Redis 7.x and native redirectengine proving the complete frozen runtime non-allow matrix",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T010": {
        "runner": "./scripts/p16/t010_runner",
        "evidence": "artifacts/v10/P16/security/P16-T010.json",
        "source_paths": {
            "links_fingerprint": "internal/links/model.go",
            "inherited_redirect": "internal/links/redirect_http.go",
            "custom_domain_authority": "internal/links/custom_domain_redirect.go",
            "domain_model": "internal/domains/domain.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t010_runner/main.go",
        },
        "environment": "real native redirectengine plus real P06-ready custom-domain axes proving official/custom primary/routing/A-B parity",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T011": {
        "runner": "./scripts/p16/t011_runner",
        "evidence": "artifacts/v10/P16/security/P16-T011.json",
        "source_paths": {
            "links_fingerprint": "internal/links/model.go",
            "inherited_redirect": "internal/links/redirect_http.go",
            "inherited_redis_risk": "internal/links/risk_redis.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t011_runner/main.go",
        },
        "environment": "real MySQL/Redis/native redirectengine proving target mutation invalidation and semantically equivalent reorder/dedup stability",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T012": {
        "runner": "./scripts/p16/t012_runner",
        "evidence": "artifacts/v10/P16/audit/P16-T012.json",
        "source_paths": {
            "override_migration": "migrations/000021_destination_risk_overrides.sql",
            "override_authority": "internal/trust/override.go",
            "projection": "internal/trust/projection.go",
            "inherited_redis_risk": "internal/links/risk_redis.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t012_runner/main.go",
        },
        "environment": "real MySQL durable exact-authority override, security.manage consumer, immutable audit, Redis projection and native redirectengine",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T013": {
        "runner": "./scripts/p16/t013_runner",
        "evidence": "artifacts/v10/P16/security/P16-T013.json",
        "source_paths": {
            "override_migration": "migrations/000021_destination_risk_overrides.sql",
            "override_authority": "internal/trust/override.go",
            "projection": "internal/trust/projection.go",
            "inherited_redis_risk": "internal/links/risk_redis.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t013_runner/main.go",
        },
        "environment": "real durable override authority invalidated by fingerprint/policy/expiry/new-decision/explicit-revocation paths with runtime enforcement",
        "requires_mysql": True,
        "requires_redis": True,
    },
    "P16-T014": {
        "runner": "./scripts/p16/t014_runner",
        "evidence": "artifacts/v10/P16/security/P16-T014.json",
        "source_paths": {
            "inherited_redirect": "internal/links/redirect_http.go",
            "custom_domain_authority": "internal/links/custom_domain_redirect.go",
            "links_password": "internal/links/password.go",
            "links_fingerprint": "internal/links/model.go",
            "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
            "runner": "scripts/p16/t014_runner/main.go",
        },
        "environment": "real native redirectengine observable side effects proving frozen safety order and target/provider/bypass non-disclosure",
        "requires_mysql": True,
        "requires_redis": True,
    },
}

FORBIDDEN_EVIDENCE_FRAGMENTS = (
    "p16-provider-secret-fixture",
    "p16-provider-token-fixture",
    "p16-transport-secret-fixture",
    "p16-partial-secret-fixture",
    "p16-malformed-secret-fixture",
    "p16-unavailable-secret-fixture",
    "authorization: bearer",
    "client_secret",
    "api_secret",
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
    parser = argparse.ArgumentParser(description="GoJet V10 P16 real destination-risk integration evidence driver")
    parser.add_argument("--case", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    config = CASE_CONFIG.get(args.case)
    if config is None:
        fail(f"P16 integration case is not implemented at this stage: {args.case}")
    if config["requires_mysql"] and not os.environ.get("GOJET_MYSQL_DSN", "").strip():
        fail("GOJET_MYSQL_DSN is required")
    if config["requires_redis"] and not os.environ.get("GOJET_REDIS_ADDR", "").strip():
        fail("GOJET_REDIS_ADDR is required")

    head = git("rev-parse", "HEAD")
    try:
        if git("merge-base", CONTRACT_AUTHORITY, "HEAD") != CONTRACT_AUTHORITY:
            fail(f"HEAD {head} does not descend from frozen P16 contract {CONTRACT_AUTHORITY}")
    except subprocess.CalledProcessError as exc:
        fail(f"cannot verify P16 contract ancestry: {exc.stderr.strip()}")

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
    runner_pass = (
        proc.returncode == 0
        and runner.get("case") == args.case
        and runner.get("status") == "PASS"
        and all_checks_pass
    )

    source_blobs = {name: blob(path) for name, path in dict(config["source_paths"]).items()}
    source_blobs["integration_driver"] = blob("scripts/p16/integration.py")
    source_blobs["frozen_test_plan"] = blob("artifacts/v10/P16/test-plan.json")

    evidence = {
        "node": "P16",
        "case": args.case,
        "status": "PASS" if runner_pass else "FAIL",
        "exact_head": head,
        "contract_authority": CONTRACT_AUTHORITY,
        "driver": f"python3 scripts/p16/integration.py --case {args.case}",
        "environment": {
            "mysql": "real MySQL 8.x service" if config["requires_mysql"] else "not required by this case",
            "mysql_version": runner.get("mysql_version", ""),
            "redis": "real Redis 7.x service" if config["requires_redis"] else "not required by this case",
            "fixture": runner.get("fixture", ""),
            "platform_state": config["environment"],
        },
        "record_counts": runner.get("record_counts", {}),
        "checks": checks,
        "source_blobs": source_blobs,
        "evidence_policy": {
            "raw_provider_secret_present": False,
            "raw_authorization_present": False,
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
        evidence["evidence_policy"]["raw_provider_secret_present"] = any("secret" in x or "token" in x for x in leaked)
        evidence["evidence_policy"]["raw_authorization_present"] = any("authorization" in x for x in leaked)
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
