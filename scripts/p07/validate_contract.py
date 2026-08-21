#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P07 Analytics contract before implementation."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "artifacts" / "v10" / "P07" / "test-plan.json"
REVIEW = ROOT / "artifacts" / "v10" / "P07" / "review.md"
EXPECTED_CASES = tuple(f"P07-T{number:03d}" for number in range(1, 21))
EXPECTED_SPECS = {
    "GJ-V10-MP-GREENFIELD-2026-08-20",
    "GJ-V10-DS-GREENFIELD-2026-08-20",
    "GJ-V10-IA-GREENFIELD-2026-08-20",
}
BASE_SHA = "3aa80b566d144963130b8f61fa63a4ee677ebc99"


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def load_json(path: Path, errors: list[str]) -> dict[str, Any]:
    if not path.is_file():
        errors.append(f"missing required file: {path.relative_to(ROOT)}")
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"invalid JSON {path.relative_to(ROOT)}: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append(f"root JSON value must be object: {path.relative_to(ROOT)}")
        return {}
    return value


def main() -> int:
    errors: list[str] = []
    plan = load_json(PLAN, errors)

    require(plan.get("node") == "P07", f"node={plan.get('node')!r} expected='P07'", errors)
    require(plan.get("title") == "Analytics", f"title={plan.get('title')!r}", errors)
    require(
        plan.get("base_integration_commit") == BASE_SHA,
        f"base_integration_commit={plan.get('base_integration_commit')!r} expected={BASE_SHA}",
        errors,
    )
    specs = plan.get("specification_ids")
    require(isinstance(specs, list) and set(specs) == EXPECTED_SPECS, f"specification_ids={specs!r}", errors)

    environment = plan.get("environment_contract")
    require(isinstance(environment, dict), "environment_contract must be object", errors)
    if isinstance(environment, dict):
        for key in (
            "mysql", "redis", "redirectengine", "analyticsworker",
            "analyticsreconciler", "platformapi", "browser", "mock_policy",
        ):
            require(
                isinstance(environment.get(key), str) and bool(environment.get(key).strip()),
                f"environment_contract.{key} missing",
                errors,
            )
        require(
            "do not satisfy" in str(environment.get("mock_policy", "")).lower(),
            "mock_policy must explicitly forbid mocks as authoritative Exit evidence",
            errors,
        )

    cases = plan.get("cases")
    require(isinstance(cases, list), "cases must be an array", errors)
    if isinstance(cases, list):
        ids = tuple(case.get("id") if isinstance(case, dict) else None for case in cases)
        require(ids == EXPECTED_CASES, f"case IDs/order mismatch: {ids}", errors)
        require(len(set(ids)) == 20, "case IDs must be unique", errors)
        for index, case in enumerate(cases, start=1):
            if not isinstance(case, dict):
                errors.append(f"case {index} must be object")
                continue
            case_id = f"P07-T{index:03d}"
            for key in ("name", "precondition", "driver", "oracle", "evidence", "owner"):
                require(
                    isinstance(case.get(key), str) and bool(case.get(key).strip()),
                    f"{case_id} missing {key}",
                    errors,
                )
            require(case.get("expected_exit") == 0, f"{case_id} expected_exit must be 0", errors)
            require(
                case.get("evidence") == f"artifacts/v10/P07/results/{case_id}.json",
                f"{case_id} evidence path={case.get('evidence')!r}",
                errors,
            )
            combined = " ".join(str(case.get(key, "")) for key in ("precondition", "driver", "oracle")).lower()
            require("legacy" not in combined, f"{case_id} must not depend on legacy implementation evidence", errors)

    closure = plan.get("closure_contract")
    require(isinstance(closure, dict), "closure_contract must be object", errors)
    if isinstance(closure, dict):
        require(closure.get("version") == 1, "closure_contract.version must be 1", errors)
        require(closure.get("same_exact_head_required") is True, "closure must require same exact head", errors)
        require(closure.get("required_case_range") == "P07-T001..P07-T020", "closure case range mismatch", errors)
        require(closure.get("review_required") is True, "closure must require accountable review", errors)
        require(closure.get("p0_max") == 0, "closure p0_max must be 0", errors)
        require(closure.get("p1_max") == 0, "closure p1_max must be 0", errors)
        require(closure.get("decision_required_max") == 0, "closure decision_required_max must be 0", errors)
        scope = str(closure.get("required_regression_scope", ""))
        for required in ("P00-P06", "P07 Analytics Contract", "P07 Real Integration", "P07 Workspace Browser", "P07 Closure"):
            require(required in scope, f"closure regression scope missing {required}", errors)

    require(REVIEW.is_file(), "missing artifacts/v10/P07/review.md", errors)
    if REVIEW.is_file():
        review = REVIEW.read_text(encoding="utf-8")
        require("Status: **PENDING" in review, "initial P07 review must remain PENDING", errors)
        for role in (
            "Backend Lead", "Analytics/Data Lead", "QA Lead", "Frontend Lead",
            "Accessibility Reviewer", "Performance Reviewer", "Security Reviewer",
        ):
            require(f"- {role}: PENDING" in review, f"review missing pending role: {role}", errors)
        for invariant in (
            "silently lost", "double-count", "idempotent", "tenant-isolated",
            "retention-limited", "invented/predictive metrics",
        ):
            require(invariant.lower() in review.lower(), f"review missing frozen invariant: {invariant}", errors)

    if errors:
        for error in errors:
            print(f"P07 contract: {error}")
        return 1

    print("P07 contract: PASS — 20/20 frozen cases, analytics invariants and exact-head closure contract validated")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
