#!/usr/bin/env python3
"""P08-T016 exact-head pre-sign/final signed closure validator."""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P08 = ROOT / "artifacts" / "v10" / "P08"
RESULTS = P08 / "results"
SCANNER = P08 / "scanner"
PLAN = P08 / "test-plan.json"
MANIFEST = P08 / "regression-manifest.json"
INDEX = P08 / "evidence-index.json"
REVIEW = P08 / "review.md"
CLOSURE = P08 / "closure.json"
T016 = RESULTS / "P08-T016.json"

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
    "P06 Closure",
    "P07 Analytics Contract",
    "P07 Real Integration",
    "P07 Workspace Analytics Browser",
    "P07 Closure",
    "P08 QR Contract",
    "P08 Real QR Integration",
    "P08 Workspace QR Browser",
    "P08 Evidence Coherence",
)
EXPECTED_CASES = tuple(f"P08-T{number:03d}" for number in range(1, 17))
INPUT_CASES = EXPECTED_CASES[:-1]
SIGNED_STATUS = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"
PENDING_STATUS = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"


def now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


def git_output(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def exact_head() -> str:
    return git_output("rev-parse", "HEAD")


def exact_parent() -> str:
    return git_output("rev-parse", "HEAD^")


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


def case_path(case_id: str) -> Path:
    if case_id == "P08-T003":
        return SCANNER / "P08-T003.json"
    return RESULTS / f"{case_id}.json"


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
        require(closure.get("required_case_range") == "P08-T001..P08-T016", "closure case range mismatch", errors)
        require(closure.get("review_required") is True, "closure must require review", errors)
        require(closure.get("p0_max") == 0, "closure p0_max drift", errors)
        require(closure.get("p1_max") == 0, "closure p1_max drift", errors)
        require(closure.get("decision_required_max") == 0, "closure decision_required_max drift", errors)
        require("P08-owned CAP-QR G3/G10 contribution only" in str(closure.get("gate_scope", "")), "closure gate scope drift", errors)
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
    require(manifest.get("implementation_commit") == head, f"regression manifest commit={manifest.get('implementation_commit')} expected={head}", errors)
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
        path = case_path(case_id)
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
        require(data.get("implementation_commit") == head, f"{case_id} implementation_commit={data.get('implementation_commit')} expected={head}", errors)
        case_errors = data.get("errors")
        require(isinstance(case_errors, list) and not case_errors, f"{case_id} errors={case_errors}", errors)
        if case_id == "P08-T015":
            details = data.get("details")
            require(isinstance(details, dict), "P08-T015 details missing", errors)
            if isinstance(details, dict):
                require(details.get("input_evidence_count") == 14, "P08-T015 input evidence count is not 14", errors)
                require(details.get("same_exact_head") is True, "P08-T015 did not prove same exact head", errors)
        entries.append({
            "case_id": case_id,
            "path": str(path.relative_to(ROOT)),
            "sha256": sha256(path),
            "status": data.get("status"),
            "implementation_commit": data.get("implementation_commit"),
        })
    require(tuple(item["case_id"] for item in entries) == INPUT_CASES, "closure evidence case order/set mismatch", errors)
    return entries


def validate_review(errors: list[str]) -> dict[str, Any]:
    require(REVIEW.is_file(), f"missing review: {REVIEW}", errors)
    if not REVIEW.is_file():
        return {"phase": "missing", "merge_authoritative": False, "defects": None}
    text = REVIEW.read_text(encoding="utf-8")
    status_match = re.search(r"^Status:\s*.+$", text, flags=re.MULTILINE)
    require(status_match is not None, "review status line missing", errors)
    status_line = status_match.group(0).strip() if status_match else ""
    signed = status_line == SIGNED_STATUS
    pending = status_line == PENDING_STATUS
    require(signed ^ pending, f"review current status is not an allowed exact status: {status_line}", errors)
    require("## 7. Signed-revision rule" in text or "## Signed-revision rule" in text, "review signed-revision rule missing", errors)
    require("release-wide G10" in text, "review must preserve release-wide G10 scope", errors)

    if not signed:
        require(pending, "pre-sign closure requires frozen pending review", errors)
        return {
            "phase": "pre-sign",
            "merge_authoritative": False,
            "status": "PENDING",
            "review_sha256": sha256(REVIEW),
            "defects": None,
        }

    parent = exact_parent()
    sha_match = re.search(r"Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`", text)
    require(sha_match is not None, "signed review pre-sign exact implementation SHA missing", errors)
    pre_sign_sha = sha_match.group(1) if sha_match else None
    require(pre_sign_sha == parent, f"signed review pre-sign SHA={pre_sign_sha} expected parent={parent}", errors)

    identity_match = re.search(r"Accountable reviewer identity:\s*\*\*(.+?)\*\*", text)
    date_match = re.search(r"Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*", text)
    require(identity_match is not None and bool(identity_match.group(1).strip()), "signed review accountable reviewer identity missing", errors)
    require(date_match is not None, "signed review date missing", errors)
    require("P08-T016: PASS — pre-sign closure / merge-authoritative=false" in text, "signed review pre-sign P08-T016 PASS record missing", errors)

    for marker in (
        "- Backend Lead: APPROVED",
        "- Frontend Lead: APPROVED",
        "- QA Lead: APPROVED",
        "- Accessibility Reviewer: APPROVED",
        "- Security Reviewer: APPROVED",
        "- Product/API Reviewer: APPROVED",
    ):
        require(marker in text, f"signed review approval missing: {marker}", errors)

    require("- P0 defects: 0" in text, "signed review P0 ledger is not zero", errors)
    require("- P1 defects: 0" in text, "signed review P1 ledger is not zero", errors)
    require("- `DECISION REQUIRED`: 0" in text, "signed review DECISION REQUIRED ledger is not zero", errors)
    require("G3 P08 functional/API subset: PASS" in text, "signed review G3 P08 subset disposition missing", errors)
    require("G10 P08 QR/generated-asset subset: PASS" in text, "signed review G10 P08 subset disposition missing", errors)
    require("signed revision itself must rerun" in text.lower(), "signed review same-revision rerun requirement missing", errors)

    return {
        "phase": "signed",
        "merge_authoritative": True,
        "status": "APPROVED",
        "review_sha256": sha256(REVIEW),
        "pre_sign_implementation_sha": pre_sign_sha,
        "accountable_reviewer_identity": identity_match.group(1).strip() if identity_match else None,
        "review_date": date_match.group(1) if date_match else None,
        "defects": {"p0": 0, "p1": 0, "decision_required": 0},
    }


def write_outputs(head: str, plan: dict[str, Any], manifest: dict[str, Any], entries: list[dict[str, Any]], review: dict[str, Any], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    status = "PASS" if not errors else "FAIL"
    phase = review.get("phase", "unknown")
    merge_authoritative = bool(review.get("merge_authoritative")) and not errors
    t016 = {
        "node": "P08",
        "case_id": "P08-T016",
        "name": "same-exact-head-signed-closure-and-affected-regression-matrix",
        "status": status,
        "generated_at": now_iso(),
        "implementation_commit": head,
        "driver": "python3 scripts/p08/validate.py --case P08-T016 --closure",
        "errors": list(errors),
        "details": {
            "closure_phase": phase,
            "merge_authoritative": merge_authoritative,
            "test_plan_cases": len(plan.get("cases", [])) if isinstance(plan, dict) else 0,
            "input_evidence_count": len(entries),
            "required_input_evidence_count": 15,
            "required_regression_workflows": list(REQUIRED_WORKFLOWS),
            "regression_workflow_count": len(manifest.get("required_workflows", {})) if isinstance(manifest, dict) else 0,
            "same_exact_head_required": True,
            "review": review,
            "gate_scope": "P08-owned CAP-QR G3/G10 contribution only; release-wide G10 remains open until its owning release gate closes.",
        },
    }
    T016.write_text(json.dumps(t016, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    defects = review.get("defects") if phase == "signed" else {"p0": None, "p1": None, "decision_required": None}
    closure = {
        "node": "P08",
        "status": status,
        "phase": phase,
        "merge_authoritative": merge_authoritative,
        "generated_at": now_iso(),
        "implementation_commit": head,
        "case_range": "P08-T001..P08-T016",
        "input_evidence_count": len(entries),
        "required_regression_workflow_count": len(REQUIRED_WORKFLOWS),
        "defects": defects,
        "review": review,
        "gate_scope": {
            "G3": "PASS — P08 CAP-QR subset only",
            "G10": "PASS — P08 QR/generated-asset subset only",
            "release_wide_G10": "OPEN — later-owned release gate",
        },
        "t016": {
            "path": str(T016.relative_to(ROOT)),
            "sha256": sha256(T016),
            "status": status,
            "implementation_commit": head,
        },
    }
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    index = {
        "node": "P08",
        "generated_at": now_iso(),
        "implementation_commit": head,
        "status": status,
        "test_plan_sha256": sha256(PLAN) if PLAN.is_file() else None,
        "regression_manifest_sha256": sha256(MANIFEST) if MANIFEST.is_file() else None,
        "review_sha256": sha256(REVIEW) if REVIEW.is_file() else None,
        "input_evidence": entries,
        "coherence_result": next((item for item in entries if item.get("case_id") == "P08-T015"), None),
        "closure_result": {
            "case_id": "P08-T016",
            "path": str(T016.relative_to(ROOT)),
            "sha256": sha256(T016),
            "status": status,
            "implementation_commit": head,
            "phase": phase,
            "merge_authoritative": merge_authoritative,
        },
        "closure_sha256": sha256(CLOSURE),
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def validate_written_outputs(head: str, entries: list[dict[str, Any]], review: dict[str, Any], errors: list[str]) -> None:
    require(T016.is_file(), f"missing T016 output: {T016}", errors)
    require(CLOSURE.is_file(), f"missing closure output: {CLOSURE}", errors)
    require(INDEX.is_file(), f"missing evidence index: {INDEX}", errors)
    if not (T016.is_file() and CLOSURE.is_file() and INDEX.is_file()):
        return
    try:
        t016 = load_json(T016)
        closure = load_json(CLOSURE)
        index = load_json(INDEX)
    except Exception as exc:
        errors.append(f"invalid written closure JSON: {exc}")
        return
    require(t016.get("implementation_commit") == head, "written T016 commit mismatch", errors)
    require(closure.get("implementation_commit") == head, "written closure commit mismatch", errors)
    require(index.get("implementation_commit") == head, "written evidence index commit mismatch", errors)
    require(index.get("input_evidence") == entries, "written evidence index inputs differ from validated closure evidence", errors)
    require(index.get("review_sha256") == sha256(REVIEW), "written evidence index review digest mismatch", errors)
    require(index.get("closure_sha256") == sha256(CLOSURE), "written evidence index closure digest mismatch", errors)
    require(closure.get("phase") == review.get("phase"), "written closure review phase mismatch", errors)


def run_closure(closure_flag: bool) -> int:
    if not closure_flag:
        print("P08-T016: --closure is required")
        return 2

    head = exact_head()
    errors: list[str] = []
    plan = validate_test_plan(errors)
    manifest = validate_regressions(head, errors)
    entries = validate_cases(head, errors)
    review = validate_review(errors)
    write_outputs(head, plan, manifest, entries, review, errors)

    output_errors: list[str] = []
    validate_written_outputs(head, entries, review, output_errors)
    if output_errors:
        errors.extend(output_errors)
        write_outputs(head, plan, manifest, entries, review, errors)

    if errors:
        for error in errors:
            print(f"P08-T016: {error}")
        return 1

    if review.get("phase") == "signed":
        print(f"P08-T016: PASS — 15/15 evidence inputs, {len(REQUIRED_WORKFLOWS)}/{len(REQUIRED_WORKFLOWS)} exact-head workflows and signed review green for {head}; merge-authoritative=true")
    else:
        print(f"P08-T016: PASS — pre-sign closure candidate with 15/15 evidence inputs and {len(REQUIRED_WORKFLOWS)}/{len(REQUIRED_WORKFLOWS)} exact-head workflows green for {head}; merge-authoritative=false")
    return 0
