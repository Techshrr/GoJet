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


def case(runner: str, evidence: str, source_paths: dict[str, str], environment: str, *, mysql: bool, redis: bool) -> dict[str, object]:
    return {
        "runner": runner,
        "evidence": evidence,
        "source_paths": source_paths,
        "environment": environment,
        "requires_mysql": mysql,
        "requires_redis": redis,
    }


CASE_CONFIG = {
    "P16-T001": case("./scripts/p16/t001_runner", "artifacts/v10/P16/api/P16-T001.json", {
        "migration": "migrations/000020_destination_risk.sql", "trust_model": "internal/trust/model.go",
        "trust_store": "internal/trust/store.go", "runner": "scripts/p16/t001_runner/main.go",
    }, "real MySQL 8.x durable destination-risk schema/correlation fixture", mysql=True, redis=False),
    "P16-T002": case("./scripts/p16/t002_runner", "artifacts/v10/P16/api/P16-T002.json", {
        "migration": "migrations/000020_destination_risk.sql", "links_fingerprint": "internal/links/model.go",
        "trust_model": "internal/trust/model.go", "trust_store": "internal/trust/store.go", "runner": "scripts/p16/t002_runner/main.go",
    }, "real MySQL 8.x plus native trust.Store exact-fingerprint enqueue/dedupe/rescan fixture", mysql=True, redis=False),
    "P16-T003": case("./scripts/p16/t003_runner", "artifacts/v10/P16/security/P16-T003.json", {
        "links_normalization": "internal/links/model.go", "inspection_guard": "internal/trust/inspection.go", "runner": "scripts/p16/t003_runner/main.go",
    }, "deterministic in-process DNS fixture proving canonical HTTP/HTTPS admission and pre-provider unsafe-form rejection", mysql=False, redis=False),
    "P16-T004": case("./scripts/p16/t004_runner", "artifacts/v10/P16/security/P16-T004.json", {
        "inspection_guard": "internal/trust/inspection.go", "runner": "scripts/p16/t004_runner/main.go",
    }, "controlled in-process DNS scripts and local HTTP redirect fixture proving SSRF, rebinding and redirect-to-private rejection", mysql=False, redis=False),
    "P16-T005": case("./scripts/p16/t005_runner", "artifacts/v10/P16/risk/P16-T005.json", {
        "migration": "migrations/000020_destination_risk.sql", "inspection_guard": "internal/trust/inspection.go",
        "provider": "internal/trust/provider.go", "policy": "internal/trust/policy.go", "policy_store": "internal/trust/policy_store.go",
        "runner": "scripts/p16/t005_runner/main.go",
    }, "real MySQL 8.x plus local deterministic semantic-provider HTTP fixture and versioned local policy authority", mysql=True, redis=False),
    "P16-T006": case("./scripts/p16/t006_runner", "artifacts/v10/P16/security/P16-T006.json", {
        "migration": "migrations/000020_destination_risk.sql", "inspection_guard": "internal/trust/inspection.go",
        "provider": "internal/trust/provider.go", "policy": "internal/trust/policy.go", "policy_store": "internal/trust/policy_store.go",
        "runner": "scripts/p16/t006_runner/main.go",
    }, "real MySQL 8.x plus deterministic timeout/transport/partial/malformed/unavailable provider protocol fixtures", mysql=True, redis=False),
    "P16-T007": case("./scripts/p16/t007_runner", "artifacts/v10/P16/risk/P16-T007.json", {
        "migration": "migrations/000020_destination_risk.sql", "worker_store": "internal/trust/worker_store.go",
        "worker": "internal/trust/worker.go", "policy_store": "internal/trust/policy_store.go",
        "operationsmonitor": "services/platformapi/cmd/operationsmonitor/main.go", "runner": "scripts/p16/t007_runner/main.go",
    }, "real MySQL 8.x durable SKIP LOCKED lease/retry/recovery plus native fixed SVC-OPS-MONITOR operationsmonitor execution", mysql=True, redis=True),
    "P16-T008": case("./scripts/p16/t008_runner", "artifacts/v10/P16/risk/P16-T008.json", {
        "migration": "migrations/000020_destination_risk.sql", "links_fingerprint": "internal/links/model.go",
        "inherited_redis_risk": "internal/links/risk_redis.go", "policy_store": "internal/trust/policy_store.go",
        "projection": "internal/trust/projection.go", "runner": "scripts/p16/t008_runner/main.go",
    }, "real MySQL 8.x durable-current decision authority projected through inherited P05 exact-fingerprint RedisRiskStore into real Redis 7.x", mysql=True, redis=True),
    "P16-T009": case("./scripts/p16/t009_runner", "artifacts/v10/P16/security/P16-T009.json", {
        "inherited_redirect": "internal/links/redirect_http.go", "inherited_redis_risk": "internal/links/risk_redis.go",
        "projection": "internal/trust/projection.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "runner": "scripts/p16/t009_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native redirectengine proving the complete frozen runtime non-allow matrix", mysql=True, redis=True),
    "P16-T010": case("./scripts/p16/t010_runner", "artifacts/v10/P16/security/P16-T010.json", {
        "links_fingerprint": "internal/links/model.go", "inherited_redirect": "internal/links/redirect_http.go",
        "custom_domain_authority": "internal/links/custom_domain_redirect.go", "domain_model": "internal/domains/domain.go",
        "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go", "runner": "scripts/p16/t010_runner/main.go",
    }, "real native redirectengine plus real P06-ready custom-domain axes proving official/custom primary/routing/A-B parity", mysql=True, redis=True),
    "P16-T011": case("./scripts/p16/t011_runner", "artifacts/v10/P16/security/P16-T011.json", {
        "links_fingerprint": "internal/links/model.go", "inherited_redirect": "internal/links/redirect_http.go",
        "inherited_redis_risk": "internal/links/risk_redis.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "runner": "scripts/p16/t011_runner/main.go",
    }, "real MySQL/Redis/native redirectengine proving target mutation invalidation and semantically equivalent reorder/dedup stability", mysql=True, redis=True),
    "P16-T012": case("./scripts/p16/t012_runner", "artifacts/v10/P16/audit/P16-T012.json", {
        "override_migration": "migrations/000021_destination_risk_overrides.sql", "override_authority": "internal/trust/override.go",
        "projection": "internal/trust/projection.go", "inherited_redis_risk": "internal/links/risk_redis.go",
        "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go", "runner": "scripts/p16/t012_runner/main.go",
    }, "real MySQL durable exact-authority override, security.manage consumer, immutable audit, Redis projection and native redirectengine", mysql=True, redis=True),
    "P16-T013": case("./scripts/p16/t013_runner", "artifacts/v10/P16/security/P16-T013.json", {
        "override_migration": "migrations/000021_destination_risk_overrides.sql", "override_authority": "internal/trust/override.go",
        "projection": "internal/trust/projection.go", "inherited_redis_risk": "internal/links/risk_redis.go",
        "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go", "runner": "scripts/p16/t013_runner/main.go",
    }, "real durable override authority invalidated by fingerprint/policy/expiry/new-decision/explicit-revocation paths with runtime enforcement", mysql=True, redis=True),
    "P16-T014": case("./scripts/p16/t014_runner", "artifacts/v10/P16/security/P16-T014.json", {
        "inherited_redirect": "internal/links/redirect_http.go", "custom_domain_authority": "internal/links/custom_domain_redirect.go",
        "links_password": "internal/links/password.go", "links_fingerprint": "internal/links/model.go",
        "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go", "runner": "scripts/p16/t014_runner/main.go",
    }, "real native redirectengine observable side effects proving frozen safety order and target/provider/bypass non-disclosure", mysql=True, redis=True),
    "P16-T015": case("./scripts/p16/t015_runner", "artifacts/v10/P16/domain/P16-T015.json", {
        "domain_migration": "migrations/000022_domain_reputation.sql", "domain_risk": "internal/trust/domain_risk.go",
        "domain_risk_store": "internal/trust/domain_risk_store.go", "inherited_domain_model": "internal/domains/domain.go",
        "inherited_domain_store": "internal/domains/domain_store_mysql.go", "domain_fixture": "scripts/p16/domainfixture/domainfixture.go",
        "runner": "scripts/p16/t015_runner/main.go",
    }, "real MySQL 8.x durable P16 domain reputation authority projected through inherited P06 independent domain axes", mysql=True, redis=False),
    "P16-T016": case("./scripts/p16/t016_runner", "artifacts/v10/P16/domain/P16-T016.json", {
        "domain_migration": "migrations/000022_domain_reputation.sql", "domain_risk": "internal/trust/domain_risk.go",
        "domain_risk_store": "internal/trust/domain_risk_store.go", "inherited_domain_model": "internal/domains/domain.go",
        "domain_fixture": "scripts/p16/domainfixture/domainfixture.go", "runner": "scripts/p16/t016_runner/main.go",
    }, "real MySQL 8.x domain-risk idempotency/rate/revalidation/stale lifecycle through inherited P06 risk projection", mysql=True, redis=False),
    "P16-T017": case("./scripts/p16/t017_runner", "artifacts/v10/P16/security/P16-T017.json", {
        "domain_migration": "migrations/000022_domain_reputation.sql", "domain_risk": "internal/trust/domain_risk.go",
        "domain_risk_store": "internal/trust/domain_risk_store.go", "provider": "internal/trust/provider.go",
        "domain_fixture": "scripts/p16/domainfixture/domainfixture.go", "runner": "scripts/p16/t017_runner/main.go",
    }, "real MySQL 8.x plus deterministic local semantic-provider HTTP fixtures for outage/partial/malformed/redaction paths", mysql=True, redis=False),
    "P16-T018": case("./scripts/p16/t018_runner", "artifacts/v10/P16/security/P16-T018.json", {
        "domain_migration": "migrations/000022_domain_reputation.sql", "domain_risk": "internal/trust/domain_risk.go",
        "domain_risk_store": "internal/trust/domain_risk_store.go", "inherited_security_suspension": "internal/domains/security_suspension_mysql.go",
        "inherited_custom_domain_redirect": "internal/links/custom_domain_redirect.go", "inherited_redis_risk": "internal/links/risk_redis.go",
        "domain_fixture": "scripts/p16/domainfixture/domainfixture.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "runner": "scripts/p16/t018_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native redirectengine proving immediate security/abuse suspension without grace or official-host fallback", mysql=True, redis=True),
    "P16-T019": case("./scripts/p16/t019_runner", "artifacts/v10/P16/abuse/P16-T019.json", {
        "abuse_migration": "migrations/000023_abuse_trust.sql", "abuse_authority": "internal/trust/abuse.go",
        "abuse_guard": "internal/trust/abuse_guard.go", "abuse_http": "internal/trust/abuse_http.go",
        "platform_trust": "services/platformapi/cmd/server/trust.go", "platform_server": "services/platformapi/cmd/server/main.go",
        "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go", "runner": "scripts/p16/t019_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native platformapi proving allowlisted public abuse intake, server-side Turnstile, idempotency and fail-closed rate authority", mysql=True, redis=True),
    "P16-T020": case("./scripts/p16/t020_runner", "artifacts/v10/P16/abuse/P16-T020.json", {
        "abuse_migration": "migrations/000023_abuse_trust.sql", "abuse_authority": "internal/trust/abuse.go",
        "abuse_guard": "internal/trust/abuse_guard.go", "abuse_http": "internal/trust/abuse_http.go",
        "platform_trust": "services/platformapi/cmd/server/trust.go", "platform_server": "services/platformapi/cmd/server/main.go",
        "notification_core": "internal/workspace/notifications.go", "runner": "scripts/p16/t020_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native platformapi proving server-resolved abuse correlation and reporter/provider-secret/PII minimization", mysql=True, redis=True),
    "P16-T021": case("./scripts/p16/t021_runner", "artifacts/v10/P16/abuse/P16-T021.json", {
        "abuse_migration": "migrations/000023_abuse_trust.sql", "admin_action_migration": "migrations/000024_abuse_admin_actions.sql",
        "abuse_authority": "internal/trust/abuse.go", "abuse_admin": "internal/trust/abuse_admin.go",
        "runner": "scripts/p16/t021_runner/main.go",
    }, "real MySQL 8.x admin abuse lifecycle proving security.manage, optimistic versioning, idempotent success replay and immutable transition audit", mysql=True, redis=False),
    "P16-T022": case("./scripts/p16/t022_runner", "artifacts/v10/P16/audit/P16-T022.json", {
        "abuse_migration": "migrations/000023_abuse_trust.sql", "admin_action_migration": "migrations/000024_abuse_admin_actions.sql",
        "abuse_action": "internal/trust/abuse_action.go", "projection": "internal/trust/projection.go",
        "domain_restore": "internal/domains/security_restore_mysql.go", "inherited_domain_suspension": "internal/domains/security_suspension_mysql.go",
        "inherited_redis_risk": "internal/links/risk_redis.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "domain_fixture": "scripts/p16/domainfixture/domainfixture.go", "runner": "scripts/p16/t022_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native redirectengine proving permission-bound abuse block/suspend, active-hold enforcement, safety-authorized restore and immutable before/after audit", mysql=True, redis=True),
    "P16-T023": case("./scripts/p16/t023_runner", "artifacts/v10/P16/notifications/P16-T023.json", {
        "notification_producer": "internal/trust/notifications.go", "inherited_notification_core": "internal/workspace/notifications.go",
        "inherited_notification_tx": "internal/workspace/notification_tx.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "runner": "scripts/p16/t023_runner/main.go",
    }, "real MySQL 8.x plus inherited P12 owner-recipient, dedupe, read-state and deep-link notification authority", mysql=True, redis=False),
    "P16-T024": case("./scripts/p16/t024_runner", "artifacts/v10/P16/api/P16-T024.json", {
        "admin_risk": "internal/trust/admin_risk.go", "override_authority": "internal/trust/override.go",
        "trust_admin_http": "services/platformapi/cmd/server/trust_admin.go", "trust_mount": "services/platformapi/cmd/server/trust.go",
        "admin_fixture": "scripts/p16/adminfixture/adminfixture.go", "runtime_fixture": "scripts/p16/runtimefixture/runtimefixture.go",
        "runner": "scripts/p16/t024_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native platformapi proving security.manage destination-risk list/detail/rescan/override APIs with inherited P15 session/CSRF and redacted control-plane DTOs", mysql=True, redis=True),
    "P16-T025": case("./scripts/p16/t025_runner", "artifacts/v10/P16/api/P16-T025.json", {
        "admin_risk": "internal/trust/admin_risk.go", "domain_risk": "internal/trust/domain_risk.go",
        "domain_risk_store": "internal/trust/domain_risk_store.go", "trust_admin_http": "services/platformapi/cmd/server/trust_admin.go",
        "trust_mount": "services/platformapi/cmd/server/trust.go", "admin_fixture": "scripts/p16/adminfixture/adminfixture.go",
        "domain_fixture": "scripts/p16/domainfixture/domainfixture.go", "runner": "scripts/p16/t025_runner/main.go",
    }, "real MySQL 8.x, real Redis 7.x and native platformapi proving domains.risk.manage list/detail/revalidate APIs with inherited P15 session/CSRF, independent P06 axes, idempotency/rate authority and provider-evidence non-disclosure", mysql=True, redis=True),
}

FORBIDDEN_EVIDENCE_FRAGMENTS = (
    "p16-provider-secret-fixture",
    "p16-provider-token-fixture",
    "p16-transport-secret-fixture",
    "p16-partial-secret-fixture",
    "p16-malformed-secret-fixture",
    "p16-unavailable-secret-fixture",
    "p16-domain-provider-secret-fixture",
    "p16-raw-secret-fixture",
    "p16-t024-provider-secret",
    "p16-t025-domain-provider-secret",
    "unsafe-admin-leak.example",
    "unsafe-domain-evidence.example",
    "p16bearercredential12345",
    "victim@example.com",
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
    parser = argparse.ArgumentParser(description="GoJet V10 P16 real trust integration evidence driver")
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
    runner_pass = proc.returncode == 0 and runner.get("case") == args.case and runner.get("status") == "PASS" and all_checks_pass

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
        "evidence_policy": {"raw_provider_secret_present": False, "raw_authorization_present": False, "dsn_present": False},
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