#!/usr/bin/env python3
"""GoJet V10 P06 exact-head closure validator for P06-T024.

T024 consumes P06-T001..T023 evidence and a regression manifest produced by
the closure workflow for the same implementation commit. It never treats file
existence, prior-head evidence, or a partial workflow matrix as passing.
"""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P06 = ROOT / "artifacts" / "v10" / "P06"
RESULTS = P06 / "results"
PLAN = P06 / "test-plan.json"
MANIFEST = P06 / "regression-manifest.json"
INDEX = P06 / "evidence-index.json"
T024 = RESULTS / "P06-T024.json"

REQUIRED_WORKFLOWS = (
    "P00 Bootstrap and G0 Traceability",
    "P01 Engineering Foundation",
    "P02 Brand Foundation",
    "P03 Design System",
    "P04 Product Shells",
    "P05 Links Domain Contract",
    "P05 Real Integration",
    "P05 Workspace Browser",
    "P05 Closure",
    "P06 Custom Domains",
    "P06 Real Integration",
    "P06 Workspace Domains Browser",
)
EXPECTED_CASES = tuple(f"P06-T{number:03d}" for number in range(1, 25))
INPUT_CASES = EXPECTED_CASES[:-1]


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def exact_head() -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True
    )
    return result.stdout.strip()


def load_json(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(128 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def validate_test_plan(errors: list[str]) -> dict[str, Any]:
    require(PLAN.is_file(), f"missing test plan: {PLAN}", errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load_json(PLAN)
    except Exception as exc:
        errors.append(f"invalid test plan JSON: {exc}")
        return {}
    require(isinstance(plan, dict), "test plan root must be object", errors)
    if not isinstance(plan, dict):
        return {}
    ids = tuple(case.get("id") for case in plan.get("cases", []) if isinstance(case, dict))
    require(ids == EXPECTED_CASES, f"test-plan case IDs/order mismatch: {ids}", errors)
    closure = plan.get("closure_contract")
    require(isinstance(closure, dict), "test-plan closure_contract missing", errors)
    if isinstance(closure, dict):
        require(closure.get("same_exact_head_required") is True, "closure must require same exact head", errors)
        require(closure.get("required_case_range") == "P06-T001..P06-T024", "closure case range mismatch", errors)
    return plan


def validate_regressions(head: str, errors: list[str]) -> dict[str, Any]:
    require(MANIFEST.is_file(), f"missing regression manifest: {MANIFEST}", errors)
    if not MANIFEST.is_file():
        return {}
    try:
        manifest = load_json(MANIFEST)
    except Exception as exc:
        errors.append(f"invalid regression manifest JSON: {exc}")
        return {}
    require(isinstance(manifest, dict), "regression manifest root must be object", errors)
    if not isinstance(manifest, dict):
        return {}
    require(
        manifest.get("implementation_commit") == head,
        f"regression manifest commit={manifest.get('implementation_commit')} expected={head}",
        errors,
    )
    workflows = manifest.get("required_workflows")
    require(isinstance(workflows, dict), "regression manifest required_workflows must be object", errors)
    if not isinstance(workflows, dict):
        return manifest
    require(set(workflows) == set(REQUIRED_WORKFLOWS), f"regression workflow set mismatch: {sorted(workflows)}", errors)
    require(manifest.get("missing") == [], f"regression manifest missing={manifest.get('missing')}", errors)
    require(manifest.get("pending") == [], f"regression manifest pending={manifest.get('pending')}", errors)
    require(manifest.get("failed") == [], f"regression manifest failed={manifest.get('failed')}", errors)
    for name in REQUIRED_WORKFLOWS:
        item = workflows.get(name)
        if not isinstance(item, dict):
            errors.append(f"missing regression workflow record: {name}")
            continue
        require(item.get("head_sha") == head, f"{name} head_sha={item.get('head_sha')} expected={head}", errors)
        require(item.get("status") == "completed", f"{name} status={item.get('status')} expected=completed", errors)
        require(item.get("conclusion") == "success", f"{name} conclusion={item.get('conclusion')} expected=success", errors)
        require(isinstance(item.get("run_id"), int) and item.get("run_id", 0) > 0, f"{name} missing valid run_id", errors)
    return manifest


def validate_cases(head: str, errors: list[str]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for case_id in INPUT_CASES:
        path = RESULTS / f"{case_id}.json"
        require(path.is_file(), f"missing evidence: {path}", errors)
        if not path.is_file():
            continue
        try:
            data = load_json(path)
        except Exception as exc:
            errors.append(f"invalid JSON {path}: {exc}")
            continue
        require(isinstance(data, dict), f"{case_id} root must be object", errors)
        if not isinstance(data, dict):
            continue
        require(data.get("case_id") == case_id, f"{case_id} payload case_id={data.get('case_id')}", errors)
        require(data.get("status") == "PASS", f"{case_id} status={data.get('status')} expected=PASS", errors)
        require(
            data.get("implementation_commit") == head,
            f"{case_id} implementation_commit={data.get('implementation_commit')} expected={head}",
            errors,
        )
        case_errors = data.get("errors", [])
        require(isinstance(case_errors, list) and not case_errors, f"{case_id} contains errors: {case_errors}", errors)
        entries.append(
            {
                "case_id": case_id,
                "path": str(path.relative_to(ROOT)),
                "sha256": sha256(path),
                "status": data.get("status"),
                "implementation_commit": data.get("implementation_commit"),
            }
        )
    require(tuple(item["case_id"] for item in entries) == INPUT_CASES, "input evidence case order/set mismatch", errors)
    return entries


def write_outputs(
    head: str,
    plan: dict[str, Any],
    manifest: dict[str, Any],
    entries: list[dict[str, Any]],
    errors: list[str],
) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    status = "PASS" if not errors else "FAIL"
    t024 = {
        "case_id": "P06-T024",
        "name": "p00-p05-regression-and-evidence-index",
        "status": status,
        "generated_at": now_iso(),
        "implementation_commit": head,
        "errors": list(errors),
        "details": {
            "test_plan_cases": len(plan.get("cases", [])) if isinstance(plan, dict) else 0,
            "input_evidence_count": len(entries),
            "required_input_evidence_count": 23,
            "required_regression_workflows": list(REQUIRED_WORKFLOWS),
            "regression_workflow_count": len(manifest.get("required_workflows", {})) if isinstance(manifest, dict) else 0,
            "same_exact_head_required": True,
        },
    }
    T024.write_text(json.dumps(t024, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    index = {
        "node": "P06",
        "generated_at": now_iso(),
        "implementation_commit": head,
        "status": status,
        "test_plan_sha256": sha256(PLAN) if PLAN.is_file() else None,
        "regression_manifest_sha256": sha256(MANIFEST) if MANIFEST.is_file() else None,
        "input_evidence": entries,
        "closure_result": {
            "case_id": "P06-T024",
            "path": str(T024.relative_to(ROOT)),
            "sha256": sha256(T024),
            "status": status,
            "implementation_commit": head,
        },
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def validate_written_index(head: str, expected_entries: list[dict[str, Any]], errors: list[str]) -> None:
    require(INDEX.is_file(), f"missing evidence index: {INDEX}", errors)
    if not INDEX.is_file():
        return
    try:
        index = load_json(INDEX)
    except Exception as exc:
        errors.append(f"invalid evidence index JSON: {exc}")
        return
    require(index.get("node") == "P06", f"evidence index node={index.get('node')}", errors)
    require(index.get("implementation_commit") == head, f"evidence index commit={index.get('implementation_commit')} expected={head}", errors)
    indexed = index.get("input_evidence")
    require(indexed == expected_entries, "evidence index input_evidence differs from validated evidence", errors)
    closure = index.get("closure_result")
    require(isinstance(closure, dict), "evidence index closure_result missing", errors)
    if isinstance(closure, dict):
        require(closure.get("case_id") == "P06-T024", "evidence index closure case mismatch", errors)
        require(closure.get("implementation_commit") == head, "evidence index closure commit mismatch", errors)
        require(closure.get("sha256") == sha256(T024), "evidence index closure digest mismatch", errors)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P06-T024"])
    parser.parse_args()

    head = exact_head()
    errors: list[str] = []
    plan = validate_test_plan(errors)
    manifest = validate_regressions(head, errors)
    entries = validate_cases(head, errors)
    write_outputs(head, plan, manifest, entries, errors)

    index_errors: list[str] = []
    validate_written_index(head, entries, index_errors)
    if index_errors:
        errors.extend(index_errors)
        write_outputs(head, plan, manifest, entries, errors)

    if errors:
        for error in errors:
            print(f"P06-T024: {error}")
        return 1
    print(
        f"P06-T024: PASS — 23/23 exact-head evidence inputs and "
        f"{len(REQUIRED_WORKFLOWS)}/{len(REQUIRED_WORKFLOWS)} regression workflows green for {head}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
