#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P08 QR contract.

This validator intentionally checks contract shape and review discipline only. It does
not claim that the P08 implementation or evidence exists. Final implementation
validation is owned by scripts/p08/validate.py once P08 code is present.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN_PATH = ROOT / "artifacts/v10/P08/test-plan.json"
REVIEW_PATH = ROOT / "artifacts/v10/P08/review.md"
WORKFLOW_PATH = ROOT / ".github/workflows/p08-qr.yml"

BASE = "04941afc59db763e6c7db8a67721dea542c72a43"
SPEC_IDS = [
    "GJ-V10-MP-GREENFIELD-2026-08-20",
    "GJ-V10-DS-GREENFIELD-2026-08-20",
    "GJ-V10-IA-GREENFIELD-2026-08-20",
]
CASE_IDS = [f"P08-T{i:03d}" for i in range(1, 17)]
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

errors: list[str] = []


def fail(message: str) -> None:
    errors.append(message)


def require(condition: bool, message: str) -> None:
    if not condition:
        fail(message)


def load_json(path: Path) -> dict:
    if not path.is_file():
        fail(f"missing required file: {path.relative_to(ROOT)}")
        return {}
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:  # pragma: no cover - defensive contract diagnostics
        fail(f"invalid JSON in {path.relative_to(ROOT)}: {exc}")
        return {}
    if not isinstance(value, dict):
        fail(f"top-level JSON must be an object: {path.relative_to(ROOT)}")
        return {}
    return value


plan = load_json(PLAN_PATH)
review = REVIEW_PATH.read_text(encoding="utf-8") if REVIEW_PATH.is_file() else ""
workflow = WORKFLOW_PATH.read_text(encoding="utf-8") if WORKFLOW_PATH.is_file() else ""

if not review:
    fail("missing or empty artifacts/v10/P08/review.md")
if not workflow:
    fail("missing or empty .github/workflows/p08-qr.yml")

# Identity and authority.
require(plan.get("node") == "P08", "test plan node must be P08")
require(plan.get("title") == "QR", "test plan title must be QR")
require(plan.get("base_integration_commit") == BASE, "P08 base integration commit must be the P07 main integration SHA")
require(plan.get("specification_ids") == SPEC_IDS, "P08 specification IDs/order must match the frozen Master/DS/IA authority")

cap = plan.get("capability_contract")
require(isinstance(cap, dict), "capability_contract must be an object")
if isinstance(cap, dict):
    require(cap.get("capability") == "CAP-QR", "P08 must own CAP-QR")
    require(cap.get("owner") == "P08", "CAP-QR owner must be P08")
    require(cap.get("gates") == ["G3", "G10"], "P08 gate contribution must be exactly G3/G10")
    require(cap.get("workspace_routes") == ["APP-QR /app/qr", "APP-QR-DETAIL /app/qr/{qrId}"], "P08 browser routes must match the IA registry exactly")
    require(cap.get("supported_download_formats") == ["png", "svg"], "P08 supported download formats must be frozen as png/svg")
    api_family = cap.get("api_family")
    require(isinstance(api_family, list) and len(api_family) == 6, "P08 API family must contain the six frozen collection/detail/render/download routes")
    flattened_cap = json.dumps(cap, ensure_ascii=False)
    for token in [
        "GET /api/workspaces/{id}/qr-codes",
        "POST /api/workspaces/{id}/qr-codes",
        "GET /api/workspaces/{id}/qr-codes/{qrId}",
        "DELETE /api/workspaces/{id}/qr-codes/{qrId}",
        "preview?format={format}",
        "download?format={format}",
        "same-Workspace authoritative source Link",
        "server-derived current public short URL",
        "pending",
        "review",
        "block",
        "missing",
        "malformed",
        "stale",
    ]:
        require(token in flattened_cap, f"capability contract missing frozen token: {token}")

# Real-dependency environment contract.
env = plan.get("environment_contract")
require(isinstance(env, dict), "environment_contract must be an object")
if isinstance(env, dict):
    required_env = {"mysql", "redis", "platformapi", "redirectengine", "workspace_browser", "renderer", "scanner", "fixture_policy"}
    require(required_env.issubset(env.keys()), "environment_contract is missing required real-dependency fields")
    env_text = json.dumps(env, ensure_ascii=False).lower()
    for token in ["real mysql", "real redis", "real native go platformapi", "real native go redirectengine", "independent machine decoder"]:
        require(token in env_text, f"environment contract missing real-dependency requirement: {token}")
    for forbidden in ["mock is sufficient", "fixture is sufficient", "visual inspection is sufficient", "manual-only"]:
        require(forbidden not in env_text, f"environment contract contains forbidden substitution: {forbidden}")

# Stable test cases.
cases = plan.get("cases")
require(isinstance(cases, list), "cases must be a list")
if isinstance(cases, list):
    actual_ids = [case.get("id") if isinstance(case, dict) else None for case in cases]
    require(actual_ids == CASE_IDS, "P08 case IDs must be exactly P08-T001..P08-T016 in order")
    for index, case in enumerate(cases, start=1):
        if not isinstance(case, dict):
            fail(f"P08-T{index:03d} must be an object")
            continue
        expected_keys = {"id", "name", "precondition", "driver", "oracle", "evidence", "expected_exit", "owner"}
        require(expected_keys.issubset(case.keys()), f"{case.get('id', index)} is missing required case fields")
        for key in ["name", "precondition", "driver", "oracle", "evidence", "owner"]:
            require(isinstance(case.get(key), str) and bool(case.get(key, "").strip()), f"{case.get('id', index)} {key} must be non-empty text")
        require(case.get("expected_exit") == 0, f"{case.get('id', index)} expected_exit must be 0")
        require(case.get("owner") == "P08", f"{case.get('id', index)} owner must be P08")
        driver = str(case.get("driver", ""))
        require(driver.startswith(("python3 scripts/p08/", "node scripts/p08/", "go run ./scripts/p08/")), f"{case.get('id', index)} driver must be an exact P08 executable command")
        require("manual" not in driver.lower(), f"{case.get('id', index)} driver cannot be manual-only")
        evidence = str(case.get("evidence", ""))
        require(evidence.startswith("artifacts/v10/P08/"), f"{case.get('id', index)} evidence must remain under artifacts/v10/P08/")
        require(case.get("id", "") in driver, f"{case.get('id', index)} driver must name its stable case ID")

case_text = json.dumps(cases, ensure_ascii=False) if isinstance(cases, list) else ""
for token in [
    "independently decodes",
    "SHA-256",
    "tenant isolation",
    "quota",
    "source-link-review",
    "source-link-block",
    "390x844",
    "1024x768",
    "1440x900",
    "keyboard",
    "P08-T001..P08-T016",
    "P00-P07",
]:
    require(token.lower() in case_text.lower(), f"P08 cases do not freeze required acceptance concept: {token}")

# Closure discipline.
closure = plan.get("closure_contract")
require(isinstance(closure, dict), "closure_contract must be an object")
if isinstance(closure, dict):
    require(closure.get("version") == 1, "closure contract version must be 1")
    require(closure.get("same_exact_head_required") is True, "P08 closure must require one exact head")
    require(closure.get("required_case_range") == "P08-T001..P08-T016", "closure case range must be P08-T001..P08-T016")
    require(closure.get("review_required") is True, "accountable technical review must be required")
    require(closure.get("p0_max") == 0 and closure.get("p1_max") == 0, "P0/P1 maxima must both be zero")
    require(closure.get("decision_required_max") == 0, "unresolved DECISION REQUIRED maximum must be zero")
    require("release-wide G10 remains open" in str(closure.get("gate_scope", "")), "P08 must not claim release-wide G10 closure")

# Pending or signed review is the only allowed review state.
if review:
    has_pending = PENDING in review
    has_signed = SIGNED in review
    require(has_pending ^ has_signed, "review.md must contain exactly one allowed P08 review status")
    for token in [
        BASE,
        "P08-T001..P08-T016",
        "CAP-QR",
        "G3",
        "G10",
        "/app/qr",
        "/app/qr/{qrId}",
        "PNG",
        "SVG",
        "independent",
        "No P08 PASS or Exit claim is made",
    ]:
        require(token in review, f"review.md missing frozen review token: {token}")
    if has_pending:
        require(not re.search(r"P08-T\d{3}\s*[:=-]?\s*PASS", review, re.IGNORECASE), "pending review must not record P08 case PASS claims")
        require("implementation_commit:" not in review.lower(), "pending review must not fabricate a signed implementation commit")
    if has_signed:
        require(re.search(r"implementation commit.*`[0-9a-f]{40}`", review, re.IGNORECASE) is not None, "signed review must record a 40-hex implementation commit")
        require("P0=0" in review and "P1=0" in review, "signed review must record P0=0 and P1=0")
        require("P08-T016" in review and "PASS" in review, "signed review must record final P08 closure PASS evidence")

# Contract CI must pin the PR head and execute this validator without write credentials.
if workflow:
    for token in [
        "name: P08 QR Contract",
        "pull_request:",
        "branches: [main]",
        "persist-credentials: false",
        "github.event.pull_request.head.sha || github.sha",
        "python3 -m py_compile scripts/p08/validate_contract.py",
        "python3 scripts/p08/validate_contract.py",
        "go test ./internal/links/... ./internal/domains/...",
        "actions/upload-artifact@v4",
    ]:
        require(token in workflow, f"P08 contract workflow missing required token: {token}")
    lower_workflow = workflow.lower()
    require("docker" not in lower_workflow and "compose" not in lower_workflow, "P08 contract workflow must not introduce Docker/Compose")
    require("npm run dev" not in lower_workflow and "pnpm dev" not in lower_workflow, "P08 contract workflow must not introduce a production/dev-server runtime")

result = {
    "node": "P08",
    "contract": "QR",
    "base_integration_commit": BASE,
    "case_range": "P08-T001..P08-T016",
    "review_state": "signed" if SIGNED in review else "pending",
    "errors": errors,
}
print(json.dumps(result, ensure_ascii=False, indent=2))
sys.exit(1 if errors else 0)
