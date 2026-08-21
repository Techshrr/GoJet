#!/usr/bin/env python3
"""Run P06 real-integration authority cases through the Go implementation."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "artifacts" / "v10" / "P06" / "results"
SUPPORTED = tuple(f"P06-T{number:03d}" for number in range(1, 21))


def exact_head() -> str:
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def run_driver(case_id: str) -> dict[str, Any]:
    driver_map = {
        "P06-T009": "ownership_driver",
        "P06-T010": "dns_driver",
        "P06-T011": "ingress_driver",
        "P06-T012": "tls_driver",
        "P06-T013": "risk_driver",
        "P06-T014": "revalidation_driver",
        "P06-T015": "downgrade_driver",
        "P06-T016": "security_driver",
        "P06-T017": "mutation_driver",
        "P06-T018": "link_assignment_driver",
        "P06-T019": "redirect_driver",
        "P06-T020": "parity_driver",
    }
    if case_id in driver_map:
        package = driver_map[case_id]
        command = ["go", "run", f"./scripts/p06/{package}", "--case", case_id]
        driver = f"scripts/p06/{package}/main.go"
    else:
        command = ["go", "run", "./scripts/p06/integration_driver.go", "--case", case_id]
        driver = "scripts/p06/integration_driver.go"
    completed = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, env=os.environ.copy())
    if not completed.stdout.strip():
        raise RuntimeError(
            f"P06 Go integration driver produced no JSON for {case_id}; "
            f"exit={completed.returncode}; stderr={completed.stderr.strip()}"
        )
    try:
        payload = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(
            f"invalid Go integration JSON for {case_id}: {exc}; "
            f"stdout={completed.stdout!r}; stderr={completed.stderr!r}"
        ) from exc
    result = payload.get(case_id)
    if not isinstance(result, dict):
        raise RuntimeError(f"driver result missing {case_id}: {payload!r}")
    result.setdefault("driver", driver)
    if completed.returncode != 0 and result.get("status") != "FAIL":
        raise RuntimeError(
            f"driver exited {completed.returncode} without FAIL result for {case_id}: {result!r}"
        )
    if completed.stderr.strip():
        result.setdefault("diagnostics", {})["stderr"] = completed.stderr.strip()
    return result


def write_evidence(case_id: str, result: dict[str, Any], head: str) -> None:
    evidence = {
        "node": "P06",
        "case_id": case_id,
        "implementation_commit": head,
        "status": result.get("status", "FAIL"),
        "driver": f"scripts/p06/integration.py -> {result.get('driver', 'unknown')}",
        "environment": {
            "mysql": "real MySQL service via GOJET_MYSQL_DSN",
            "redis": "real Redis service via GOJET_REDIS_ADDR for redirect destination-risk authority from T019 onward",
            "dns": "real local authoritative UDP DNS for T010/T011/T014",
            "tls": "real local TCP/TLS handshake endpoint for T012/T014",
            "domain_risk": "server-owned evaluator with real MySQL persistence for T013/T014",
            "time_authority": "deterministic UTC boundary instants for exact downgrade grace in T015 and immediate safety suspension in T016",
            "safety_authority": "server-side allowlisted abuse/fraud/security/ownership-loss state with real MySQL persistence for T016",
            "workspace_permission": "server-owned permission checker seam exercised before resolver/probe/database mutation checkpoints in T017",
            "link_assignment": "real Links HTTP create/update over P05 persistence with same-transaction P06 custom-domain row lock and current entitlement/trust checks in T018",
            "redirect_authority": "real RedirectHandler + MySQL current custom-domain routing authority + Redis exact-fingerprint destination-risk decision with final transactional recheck in T019",
            "risk_parity": "equivalent official/custom Links use the same normalized reachable-target fingerprint and Redis risk ordering; custom-domain authority is only an additional gate in T020",
            "migrations": [
                "migrations/000001_links_vertical_slice.sql",
                "migrations/000002_custom_domains.sql",
            ],
        },
        "details": result.get("details", {}),
        "errors": result.get("errors", []),
    }
    if result.get("diagnostics"):
        evidence["diagnostics"] = result["diagnostics"]
    RESULTS.mkdir(parents=True, exist_ok=True)
    (RESULTS / f"{case_id}.json").write_text(
        json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", default="all")
    args = parser.parse_args()
    if args.case == "all":
        cases = SUPPORTED
    elif args.case in SUPPORTED:
        cases = (args.case,)
    else:
        parser.error(f"unsupported early integration case {args.case!r}; supported={','.join(SUPPORTED)}")

    head = exact_head()
    failed: list[str] = []
    for case_id in cases:
        try:
            result = run_driver(case_id)
        except Exception as exc:
            result = {"status": "FAIL", "details": {}, "errors": [str(exc)]}
        write_evidence(case_id, result, head)
        status = result.get("status", "FAIL")
        print(f"{case_id}: {status}")
        if status != "PASS":
            failed.append(case_id)
    if failed:
        print(f"P06 early real integration failed: {', '.join(failed)}")
        return 1
    print(f"P06-T001..T020 real authority evidence PASS on exact head {head}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
