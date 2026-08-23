#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P10 Text Sharing contract."""
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "artifacts/v10/P10/test-plan.json"
REVIEW = ROOT / "artifacts/v10/P10/review.md"

BASE = "0c43b9e5fa9abb9da7231e4ab5bd6d8a76f6d9a8"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"
EXPECTED_CASES = tuple(f"P10-T{n:03d}" for n in range(1, 21))
SPEC_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
}
EXPECTED_IA_ROUTES = (
    "APP-TEXT /app/text",
    "APP-TEXT-DETAIL /app/text/{shareId}",
    "PUB-TEXT /t/{slug}",
    "PUB-ABUSE-REPORT /abuse/report",
    "API-PUBLIC-REQUIRED POST /api/public/text/{slug}",
)
EXPECTED_WORKSPACE_APIS = (
    "GET /api/workspaces/{id}/text-shares",
    "POST /api/workspaces/{id}/text-shares",
    "GET /api/workspaces/{id}/text-shares/{shareId}",
    "PATCH /api/workspaces/{id}/text-shares/{shareId}",
    "DELETE /api/workspaces/{id}/text-shares/{shareId}",
)
MASTER_TESTS = ("auth", "private", "public", "expired", "not-found", "noindex", "status codes")
IMPLEMENTATION_COLUMNS = (
    "Backend", "DB/Migration", "API", "UI", "RBAC",
    "States", "Browser", "Security", "Observability", "Release",
)


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def blob(path: str) -> str:
    return git("hash-object", path)


def load_plan(errors: list[str]) -> dict:
    if not PLAN.is_file():
        errors.append("missing P10 test-plan.json")
        return {}
    try:
        value = json.loads(PLAN.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"invalid P10 test-plan JSON: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append("P10 test-plan top level must be an object")
        return {}
    return value


def main() -> int:
    errors: list[str] = []
    plan = load_plan(errors)
    require(REVIEW.is_file(), "missing P10 review.md", errors)
    review = REVIEW.read_text(encoding="utf-8") if REVIEW.is_file() else ""

    for path, expected in SPEC_BLOBS.items():
        target = ROOT / path
        require(target.is_file(), f"missing frozen authority file: {path}", errors)
        if target.is_file():
            actual = blob(path)
            require(actual == expected, f"frozen authority blob drift: {path}={actual} expected={expected}", errors)

    require(plan.get("node") == "P10", "plan node drift", errors)
    require(plan.get("title") == "Text Sharing", "plan title drift", errors)
    require(plan.get("base_integration_commit") == BASE, "P10 base integration SHA drift", errors)
    require("P10 contract-freeze revision" in str(plan.get("case_ids_frozen_by", "")), "case-ID authority disclosure missing", errors)
    require(
        plan.get("specification_ids")
        == [
            "GJ-V10-MP-GREENFIELD-2026-08-20",
            "GJ-V10-DS-GREENFIELD-2026-08-20",
            "GJ-V10-IA-GREENFIELD-2026-08-20",
        ],
        "specification IDs/order drift",
        errors,
    )

    cap = plan.get("capability_contract", {})
    require(
        cap.get("capability")
        == {"id": "CAP-TEXT", "status": "REQUIRED", "owner": "P10", "gates": ["G3", "G7"]},
        "CAP-TEXT owner/gate drift",
        errors,
    )
    require(cap.get("p10_dependencies") == ["P04"], "P10 predecessor drift", errors)
    require(tuple(cap.get("master_required_tests", [])) == MASTER_TESTS, "Master required-test list drift", errors)
    require(tuple(cap.get("implementation_columns", [])) == IMPLEMENTATION_COLUMNS, "capability implementation-column contract drift", errors)
    gate_scope = str(cap.get("gate_scope", ""))
    require("P10" in gate_scope and "release-wide G7" in gate_scope and "P18/P19/P20/P22" in gate_scope, "P10 G7 subset/later-owner boundary missing", errors)

    trace = plan.get("master_test_trace", {})
    require(set(trace.keys()) == set(MASTER_TESTS), "Master test trace keys drift", errors)
    flattened_trace = json.dumps(trace, ensure_ascii=False)
    for case_id in ("P10-T003", "P10-T006", "P10-T007", "P10-T008", "P10-T009", "P10-T011", "P10-T013", "P10-T014"):
        require(case_id in flattened_trace, f"Master test trace missing {case_id}", errors)

    route = plan.get("route_contract", {})
    require(tuple(route.get("ia_authoritative_routes", [])) == EXPECTED_IA_ROUTES, "P10 IA route list drift", errors)
    note = str(route.get("workspace_api_authority_note", ""))
    require("does not freeze exact Workspace HTTP method/path families" in note, "IA-vs-P10 Workspace API authority distinction missing", errors)
    require("must not be represented as IA-exact authority" in note, "P10 implementation-authority warning missing", errors)
    require(tuple(route.get("p10_workspace_api_family", [])) == EXPECTED_WORKSPACE_APIS, "P10 Workspace implementation API family drift", errors)
    require(str(route.get("legacy_aliases", "")).startswith("PROHIBITED"), "legacy alias prohibition missing", errors)

    public = route.get("public_contract", {})
    require("GET /t/{slug}" in str(public.get("page", "")) and "POST /t/{slug}" in str(public.get("page", "")), "PUB-TEXT page action contract drift", errors)
    require("POST /api/public/text/{slug}" in str(public.get("machine", "")), "public Text machine endpoint drift", errors)
    require("GET /t/{slug}?download=1" in str(public.get("download", "")), "Text download query contract missing", errors)
    require(public.get("abuse_entry") == "GET /abuse/report", "Text abuse entry drift", errors)
    require(public.get("available_status") == 200, "Text available status drift", errors)
    require(public.get("auth_gate_statuses") == [401, 403], "Text auth gate status drift", errors)
    require(public.get("unknown_status") == 404, "Text unknown status drift", errors)
    require(public.get("expired_removed_consumed_status") == 410, "Text lifecycle 410 drift", errors)

    resource = plan.get("resource_contract", {})
    expected_fields = {
        "id", "workspace_id", "slug", "title", "content", "visibility", "password_verifier",
        "expires_at", "one_time", "consumed_at", "version", "created_by",
        "created_at", "updated_at", "deleted_at",
    }
    require(set(resource.get("authoritative_fields", [])) == expected_fields, "Text authoritative-field set drift", errors)
    require(resource.get("visibility") == ["private", "public"], "Text visibility set drift", errors)
    for key, token in (
        ("slug_authority", "server-generated"),
        ("content_authority", "not executable"),
        ("password_authority", "plaintext password"),
        ("expiry_authority", "410"),
        ("consume_authority", "atomic"),
        ("delete_authority", "410"),
        ("conflict_authority", "409"),
    ):
        require(token.lower() in str(resource.get(key, "")).lower(), f"Text {key} contract missing {token}", errors)

    seo = plan.get("seo_contract", {})
    require(seo.get("public_route") == "PUB-TEXT /t/{slug}", "Text SEO route drift", errors)
    require(seo.get("index_policy") == "noindex", "PUB-TEXT must remain noindex", errors)
    require(seo.get("canonical") == "none" and seo.get("locale_alternate") == "none", "PUB-TEXT canonical/hreflang must remain none", errors)
    require(seo.get("structured_data") == "none", "PUB-TEXT structured data must remain none", errors)
    require(seo.get("sitemap") is False, "PUB-TEXT must remain excluded from sitemap", errors)
    require(seo.get("internal_link_parent") == "Workspace share action only", "PUB-TEXT internal-link parent drift", errors)
    require(seo.get("machine_header") == "X-Robots-Tag: noindex, nofollow", "Text machine robots header drift", errors)
    require(seo.get("soft_404") == "PROHIBITED", "Text soft-404 prohibition missing", errors)
    require("later-owned" in str(seo.get("release_wide_g7", "")), "release-wide G7 boundary missing", errors)
    http = seo.get("http_contract", {})
    require(http == {"available": 200, "gate": [401, 403], "unknown": 404, "expired_removed_consumed": 410}, "Text SEO HTTP contract drift", errors)

    env = plan.get("environment_contract", {})
    require("Real MySQL" in str(env.get("mysql", "")), "real MySQL requirement missing", errors)
    require("Real native Go platformapi" in str(env.get("platformapi", "")), "real native platformapi requirement missing", errors)
    require("Built Workspace" in str(env.get("workspace_browser", "")), "real Workspace browser requirement missing", errors)
    require("raw status/header/body evidence" in str(env.get("public_http", "")), "raw public HTTP evidence requirement missing", errors)
    require(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Node boundary drift", errors)

    cases = plan.get("cases", [])
    ids = tuple(item.get("id") for item in cases if isinstance(item, dict))
    require(ids == EXPECTED_CASES, f"P10 case IDs/order drift: {ids}", errors)
    for case in cases:
        if not isinstance(case, dict):
            errors.append("P10 case entry is not an object")
            continue
        cid = case.get("id")
        require(case.get("owner") == "P10", f"{cid} owner drift", errors)
        require(case.get("expected_exit") == 0, f"{cid} expected_exit drift", errors)
        for field in ("name", "precondition", "driver", "oracle", "evidence"):
            require(bool(case.get(field)), f"{cid} missing {field}", errors)
        require(str(case.get("evidence", "")).startswith("artifacts/v10/P10/"), f"{cid} evidence outside P10 root", errors)

    by_id = {case.get("id"): case for case in cases if isinstance(case, dict)}
    for n in range(1, 13):
        require(str(by_id.get(f"P10-T{n:03d}", {}).get("driver", "")).startswith("python3 scripts/p10/integration.py"), f"P10-T{n:03d} integration driver drift", errors)
    for n in (13, 14):
        require(str(by_id.get(f"P10-T{n:03d}", {}).get("driver", "")).startswith("python3 scripts/p10/seo.py"), f"P10-T{n:03d} SEO driver drift", errors)
    require(str(by_id.get("P10-T015", {}).get("driver", "")).startswith("python3 scripts/p10/integration.py"), "P10-T015 abuse-entry driver drift", errors)
    for n in range(16, 19):
        require(str(by_id.get(f"P10-T{n:03d}", {}).get("driver", "")).startswith("node scripts/p10/browser.mjs"), f"P10-T{n:03d} browser driver drift", errors)
    require(by_id.get("P10-T019", {}).get("driver") == "python3 scripts/p10/validate.py --case P10-T019", "P10-T019 coherence driver drift", errors)
    require(by_id.get("P10-T020", {}).get("driver") == "python3 scripts/p10/validate.py --case P10-T020 --closure", "P10-T020 closure driver drift", errors)

    closure = plan.get("closure_contract", {})
    require(closure.get("version") == 1, "P10 closure contract version drift", errors)
    require(closure.get("same_exact_head_required") is True, "P10 closure must require exact head", errors)
    require(closure.get("required_case_range") == "P10-T001..P10-T020", "P10 closure case range drift", errors)
    require(closure.get("review_required") is True, "P10 accountable review must be required", errors)
    require(closure.get("p0_max") == 0 and closure.get("p1_max") == 0 and closure.get("decision_required_max") == 0, "P10 defect/decision thresholds drift", errors)
    require(closure.get("pre_sign_phase") == "pre-sign / merge_authoritative=false", "P10 pre-sign closure phase drift", errors)
    require(closure.get("signed_phase") == "signed / merge_authoritative=true", "P10 signed closure phase drift", errors)
    require("full G7" in str(closure.get("gate_scope", "")), "P10 closure later G7 boundary missing", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.strip().startswith("Status:")]
    has_pending = status_lines == [PENDING]
    has_signed = status_lines == [SIGNED]
    require(has_pending ^ has_signed, f"review.md must contain exactly one legal P10 status: {status_lines}", errors)

    for marker in (
        "Required P10 case range: **P10-T001..P10-T020**.",
        "does **not** freeze an exact Workspace HTTP method/path family",
        "`PUB-TEXT /t/{slug}` is permanently `noindex`",
        "Public Text UGC must not enter Website or Docs sitemaps.",
        "No legacy/compatibility alias is approved.",
        "SAME-REVISION CI REQUIRED",
    ):
        require(marker in review, f"P10 review marker missing: {marker}", errors)

    if has_pending:
        require("No P10 PASS or Exit claim is made in this state." in review, "pending review no-PASS marker missing", errors)
        require("Accountable reviewer identity:" not in review, "pending review must not contain accountable signature", errors)
        explicit_pass_claim = re.search(r"(?mi)^\s*(?:[-*]\s*)?P10-T\d{3}\s*[:=-]\s*PASS\b", review)
        require(explicit_pass_claim is None, "pending review must not contain P10 case PASS claims", errors)
    if has_signed:
        require(re.search(r"Pre-sign exact implementation SHA:\s*`[0-9a-f]{40}`", review) is not None, "signed review pre-sign SHA missing", errors)
        require("P10-T020" in review and "PASS" in review, "signed review final P10 closure PASS record missing", errors)
        for marker in ("- P0 defects: 0", "- P1 defects: 0", "- `DECISION REQUIRED`: 0"):
            require(marker in review, f"signed review defect marker missing: {marker}", errors)
        require("signed revision itself must rerun" in review.lower(), "signed review rerun rule missing", errors)

    head = git("rev-parse", "HEAD")
    require(bool(re.fullmatch(r"[0-9a-f]{40}", head)), f"invalid exact HEAD: {head}", errors)
    try:
        merge_base = git("merge-base", head, BASE)
        require(merge_base == BASE, f"P10 branch base drift: merge-base={merge_base} expected={BASE}", errors)
    except Exception as exc:
        errors.append(f"cannot verify P10 branch ancestry: {exc}")

    result = {
        "node": "P10",
        "contract": "Text Sharing",
        "status": "PASS" if not errors else "FAIL",
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "case_range": "P10-T001..P10-T020",
        "case_count": len(cases),
        "review_status": "SIGNED" if has_signed else "PENDING" if has_pending else "INVALID",
        "workspace_api_authority": "P10 implementation contract; IA freezes Text API semantics but not exact Workspace HTTP paths",
        "public_ugc_policy": "PUB-TEXT noindex / no sitemap / accurate 200,401,403,404,410",
        "errors": errors,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
