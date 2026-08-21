#!/usr/bin/env python3
"""GoJet V10 P05 closure validator for P05-T020.

T020 is intentionally cross-workflow: it consumes T001..T019 evidence produced
by the exact same implementation commit plus a regression manifest containing
P00..P05 workflow conclusions for that commit. File existence alone is never a
passing condition.
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
P05 = ROOT / "artifacts" / "v10" / "P05"
RESULTS = P05 / "results"
PLAN = P05 / "test-plan.json"
MANIFEST = P05 / "regression-manifest.json"
INDEX = P05 / "evidence-index.json"
T020 = RESULTS / "P05-T020.json"

REQUIRED_WORKFLOWS = (
    "P00 Bootstrap and G0 Traceability",
    "P01 Engineering Foundation",
    "P02 Brand Foundation",
    "P03 Design System",
    "P04 Product Shells",
    "P05 Links Domain Contract",
    "P05 Real Integration",
    "P05 Workspace Browser",
)

EXPECTED_CASES = tuple(f"P05-T{number:03d}" for number in range(1, 21))


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


def detail(data: dict[str, Any], *keys: str) -> Any:
    value: Any = data
    for key in keys:
        if not isinstance(value, dict) or key not in value:
            return None
        value = value[key]
    return value


def validate_test_plan(errors: list[str]) -> dict[str, Any]:
    require(PLAN.is_file(), f"missing test plan: {PLAN}", errors)
    if not PLAN.is_file():
        return {}
    plan = load_json(PLAN)
    ids = tuple(case.get("id") for case in plan.get("cases", []))
    require(ids == EXPECTED_CASES, f"test-plan case IDs are not exactly {EXPECTED_CASES}: {ids}", errors)
    return plan


def validate_regressions(head: str, errors: list[str]) -> dict[str, Any]:
    require(MANIFEST.is_file(), f"missing regression manifest: {MANIFEST}", errors)
    if not MANIFEST.is_file():
        return {}
    manifest = load_json(MANIFEST)
    require(manifest.get("implementation_commit") == head,
            f"regression manifest commit={manifest.get('implementation_commit')} expected={head}", errors)
    workflows = manifest.get("required_workflows")
    require(isinstance(workflows, dict), "regression manifest required_workflows must be an object", errors)
    if not isinstance(workflows, dict):
        return manifest
    require(set(workflows) == set(REQUIRED_WORKFLOWS),
            f"regression workflow set mismatch: {sorted(workflows)}", errors)
    for name in REQUIRED_WORKFLOWS:
        item = workflows.get(name)
        if not isinstance(item, dict):
            errors.append(f"missing regression workflow record: {name}")
            continue
        require(item.get("head_sha") == head,
                f"{name} head_sha={item.get('head_sha')} expected={head}", errors)
        require(item.get("status") == "completed",
                f"{name} status={item.get('status')} expected=completed", errors)
        require(item.get("conclusion") == "success",
                f"{name} conclusion={item.get('conclusion')} expected=success", errors)
        require(isinstance(item.get("run_id"), int) and item.get("run_id", 0) > 0,
                f"{name} missing valid run_id", errors)
    return manifest


def validate_password_contract(t010: dict[str, Any], errors: list[str]) -> None:
    contract = detail(t010, "details", "password_contract")
    require(isinstance(contract, dict), "T010 password_contract evidence missing", errors)
    if not isinstance(contract, dict):
        return
    expected = {
        "hash_algorithm": "pbkdf2-sha256",
        "hash_version": 1,
        "pbkdf2_iterations": 600000,
        "verifier_exposed_by_api": False,
        "plaintext_persisted": False,
        "risk_precedes_password": True,
        "challenge_status": 200,
        "wrong_password_status": 401,
        "rate_limited_status": 429,
        "correct_password_status": 302,
        "password_attempt_limit": 10,
        "clear_preserved_fingerprint": True,
        "restore_password_states": [True, False, True],
        "click_count_after_restore": 2,
    }
    for key, value in expected.items():
        require(contract.get(key) == value,
                f"T010 password_contract {key}={contract.get(key)!r} expected={value!r}", errors)


def validate_password_browser(t017: dict[str, Any], t019: dict[str, Any], errors: list[str]) -> None:
    workspace = detail(t017, "details", "password_workspace_flow")
    require(isinstance(workspace, dict), "T017 password_workspace_flow evidence missing", errors)
    if isinstance(workspace, dict):
        expected_workspace = {
            "create_status": 201,
            "replace_status": 200,
            "clear_status": 200,
            "versions_verified": [1, 2, 3],
            "password_plaintext_echoed": False,
            "password_protected_after_create": True,
            "password_protected_after_replace": True,
            "password_protected_after_clear": False,
            "password_mutations_preserved_fingerprint": True,
        }
        for key, value in expected_workspace.items():
            require(workspace.get(key) == value,
                    f"T017 password_workspace_flow {key}={workspace.get(key)!r} expected={value!r}", errors)

    public = detail(t019, "details", "password_public_gate")
    require(isinstance(public, dict), "T019 password_public_gate evidence missing", errors)
    if isinstance(public, dict):
        expected_public = {
            "challenge_status": 200,
            "old_password_status": 401,
            "correct_password_status": 302,
            "destination_absent_from_challenge": True,
            "destination_absent_from_wrong_password": True,
            "bypass_links": 0,
            "no_store": True,
            "no_referrer": True,
            "noindex": True,
            "csp_form_action_self": True,
            "clear_removed_public_challenge": True,
        }
        for key, value in expected_public.items():
            require(public.get(key) == value,
                    f"T019 password_public_gate {key}={public.get(key)!r} expected={value!r}", errors)
        location = public.get("correct_password_location")
        require(isinstance(location, str) and location.startswith("http://127.0.0.1:4174/app/links"),
                f"T019 correct_password_location unexpected: {location!r}", errors)


def validate_cases(head: str, errors: list[str]) -> list[dict[str, Any]]:
    index_entries: list[dict[str, Any]] = []
    loaded: dict[str, dict[str, Any]] = {}
    for case_id in EXPECTED_CASES[:-1]:
        path = RESULTS / f"{case_id}.json"
        require(path.is_file(), f"missing evidence: {path}", errors)
        if not path.is_file():
            continue
        try:
            data = load_json(path)
        except Exception as exc:
            errors.append(f"invalid JSON {path}: {exc}")
            continue
        loaded[case_id] = data
        require(data.get("case_id") == case_id,
                f"{case_id} payload case_id={data.get('case_id')}", errors)
        require(data.get("status") == "PASS",
                f"{case_id} status={data.get('status')} expected=PASS", errors)
        require(data.get("implementation_commit") == head,
                f"{case_id} implementation_commit={data.get('implementation_commit')} expected={head}", errors)
        case_errors = data.get("errors", [])
        require(isinstance(case_errors, list) and len(case_errors) == 0,
                f"{case_id} contains errors: {case_errors}", errors)
        index_entries.append({
            "case_id": case_id,
            "path": str(path.relative_to(ROOT)),
            "sha256": sha256(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit"),
        })

    if "P05-T010" in loaded:
        validate_password_contract(loaded["P05-T010"], errors)
    if "P05-T017" in loaded and "P05-T019" in loaded:
        validate_password_browser(loaded["P05-T017"], loaded["P05-T019"], errors)
    return index_entries


def write_outputs(head: str, plan: dict[str, Any], manifest: dict[str, Any], entries: list[dict[str, Any]], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    status = "PASS" if not errors else "FAIL"
    t020 = {
        "case_id": "P05-T020",
        "name": "p00-p04-regression-and-evidence-index",
        "status": status,
        "generated_at": now_iso(),
        "implementation_commit": head,
        "errors": errors,
        "details": {
            "test_plan_cases": len(plan.get("cases", [])) if isinstance(plan, dict) else 0,
            "input_evidence_count": len(entries),
            "required_input_evidence_count": 19,
            "required_regression_workflows": list(REQUIRED_WORKFLOWS),
            "regression_workflow_count": len(manifest.get("required_workflows", {})) if isinstance(manifest, dict) else 0,
            "password_contract_bound_to_t010": any(item["case_id"] == "P05-T010" for item in entries),
            "password_browser_bound_to_t017_t019": all(any(item["case_id"] == case_id for item in entries) for case_id in ("P05-T017", "P05-T019")),
        },
    }
    T020.write_text(json.dumps(t020, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    evidence_index = {
        "node": "P05",
        "generated_at": now_iso(),
        "implementation_commit": head,
        "status": status,
        "test_plan_sha256": sha256(PLAN) if PLAN.is_file() else None,
        "regression_manifest_sha256": sha256(MANIFEST) if MANIFEST.is_file() else None,
        "input_evidence": entries,
        "closure_result": {
            "case_id": "P05-T020",
            "path": str(T020.relative_to(ROOT)),
            "status": status,
        },
    }
    INDEX.write_text(json.dumps(evidence_index, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P05-T020"])
    parser.parse_args()

    head = exact_head()
    errors: list[str] = []
    plan = validate_test_plan(errors)
    manifest = validate_regressions(head, errors)
    entries = validate_cases(head, errors)
    write_outputs(head, plan, manifest, entries, errors)

    if errors:
        for error in errors:
            print(f"P05-T020: {error}")
        return 1
    print(f"P05-T020: PASS — 19/19 exact-head inputs and {len(REQUIRED_WORKFLOWS)}/{len(REQUIRED_WORKFLOWS)} regressions green for {head}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
