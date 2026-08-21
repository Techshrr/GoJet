#!/usr/bin/env python3
"""Run P06 real-MySQL authority cases through the Go domain/store implementation."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "artifacts" / "v10" / "P06" / "results"
SUPPORTED = tuple(f"P06-T{number:03d}" for number in range(1, 10))


def exact_head() -> str:
    return subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True
    ).strip()


def run_driver(case_id: str) -> dict[str, Any]:
    if case_id == "P06-T009":
        command = ["go", "run", "./scripts/p06/ownership_driver", "--case", case_id]
        driver = "scripts/p06/ownership_driver/main.go"
    else:
        command = ["go", "run", "./scripts/p06/integration_driver.go", "--case", case_id]
        driver = "scripts/p06/integration_driver.go"
    completed = subprocess.run(
        command,
        cwd=ROOT,
        text=True,
        capture_output=True,
        env=os.environ.copy(),
    )
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
    path = RESULTS / f"{case_id}.json"
    path.write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", default="all")
    args = parser.parse_args()

    if args.case == "all":
        cases = SUPPORTED
    elif args.case in SUPPORTED:
        cases = (args.case,)
    else:
        parser.error(
            f"unsupported early integration case {args.case!r}; "
            f"supported={','.join(SUPPORTED)}"
        )

    head = exact_head()
    failed: list[str] = []
    for case_id in cases:
        try:
            result = run_driver(case_id)
        except Exception as exc:  # CI diagnostic boundary
            result = {"status": "FAIL", "details": {}, "errors": [str(exc)]}
        write_evidence(case_id, result, head)
        status = result.get("status", "FAIL")
        print(f"{case_id}: {status}")
        if status != "PASS":
            failed.append(case_id)

    if failed:
        print(f"P06 early real-MySQL integration failed: {', '.join(failed)}")
        return 1
    print(f"P06-T001..T009 real-MySQL evidence PASS on exact head {head}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
