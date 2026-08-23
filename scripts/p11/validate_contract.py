#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P11 Bio contract."""
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "artifacts/v10/P11/test-plan.json"
REVIEW = ROOT / "artifacts/v10/P11/review.md"

BASE = "4d2186da8b2958c7618a233f53908f2914c389a3"
P10_SIGNED_SOURCE = "7db4fca49ba3fd8e60600ecdf41847c7e2f94776"
P10_CLOSURE_RUN = 32643830718
P10_ARTIFACT_ID = 9494371271
P10_ARTIFACT_DIGEST = "sha256:6a4bcaed870c6432df40e1fe71cb38dd05a84789d3539ab10dabcbfefe450c50"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"
EXPECTED_CASES = tuple(f"P11-T{n:03d}" for n in range(1, 21))
SPEC_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
}
EXPECTED_IA_ROUTES = (
    "APP-BIO /app/bio",
    "APP-BIO-DETAIL /app/bio/{pageId}",
    "PUB-BIO /p/{slug}",
    "PUB-BIO GET /api/public/bio/{slug}",
)
EXPECTED_WORKSPACE_APIS = (
    "GET /api/workspaces/{id}/bio-pages",
    "POST /api/workspaces/{id}/bio-pages",
    "GET /api/workspaces/{id}/bio-pages/{pageId}",
    "PATCH /api/workspaces/{id}/bio-pages/{pageId}",
    "DELETE /api/workspaces/{id}/bio-pages/{pageId}",
    "POST /api/workspaces/{id}/bio-pages/{pageId}/publish",
    "POST /api/workspaces/{id}/bio-pages/{pageId}/pause",
)
MASTER_TESTS = ("ownership", "risk-blocked link", "mobile", "noindex", "sitemap exclusion")
IMPLEMENTATION_COLUMNS = (
    "Backend", "DB/Migration", "API", "UI", "RBAC",
    "States", "Browser", "Security", "Observability", "Release",
)
PAGE_FIELDS = {
    "id", "workspace_id", "slug", "title", "bio", "status", "version",
    "published_at", "created_by", "created_at", "updated_at", "deleted_at",
}
CHILD_FIELDS = {
    "id", "bio_page_id", "position", "label", "destination_url",
    "destination_fingerprint", "risk_status", "risk_checked_at",
}


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def blob(path: str) -> str:
    return git("hash-object", path)


def load_plan(errors: list[str]) -> dict:
    if not PLAN.is_file():
        errors.append("missing P11 test-plan.json")
        return {}
    try:
        value = json.loads(PLAN.read_text(encoding="utf-8"))
    except Exception as exc:
        errors.append(f"invalid P11 test-plan JSON: {exc}")
        return {}
    if not isinstance(value, dict):
        errors.append("P11 test-plan top level must be an object")
        return {}
    return value


def main() -> int:
    errors: list[str] = []
    plan = load_plan(errors)
    require(REVIEW.is_file(), "missing P11 review.md", errors)
    review = REVIEW.read_text(encoding="utf-8") if REVIEW.is_file() else ""

    for path, expected in SPEC_BLOBS.items():
        target = ROOT / path
        require(target.is_file(), f"missing frozen authority file: {path}", errors)
        if target.is_file():
            actual = blob(path)
            require(actual == expected, f"frozen authority blob drift: {path}={actual} expected={expected}", errors)

    require(plan.get("node") == "P11", "plan node drift", errors)
    require(plan.get("title") == "Bio", "plan title drift", errors)
    require(plan.get("base_integration_commit") == BASE, "P11 base integration SHA drift", errors)
    require("P11 contract-freeze revision" in str(plan.get("case_ids_frozen_by", "")), "case-ID authority disclosure missing", errors)
    require(
        plan.get("specification_ids") == [
            "GJ-V10-MP-GREENFIELD-2026-08-20",
            "GJ-V10-DS-GREENFIELD-2026-08-20",
            "GJ-V10-IA-GREENFIELD-2026-08-20",
        ],
        "specification IDs/order drift",
        errors,
    )

    cap = plan.get("capability_contract", {})
    require(
        cap.get("capability") == {"id": "CAP-BIO", "status": "REQUIRED", "owner": "P11", "gates": ["G3", "G7"]},
        "CAP-BIO owner/gate drift",
        errors,
    )
    require(cap.get("p11_dependencies") == ["P05"], "P11 predecessor drift", errors)
    require(tuple(cap.get("master_required_tests", [])) == MASTER_TESTS, "Master required-test list drift", errors)
    require(tuple(cap.get("implementation_columns", [])) == IMPLEMENTATION_COLUMNS, "capability implementation-column contract drift", errors)
    gate_scope = str(cap.get("gate_scope", ""))
    for token in ("P11", "P16", "release-wide G7", "P18/P19/P20/P22"):
        require(token in gate_scope, f"P11 gate/later-owner boundary missing {token}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    require(pred.get("node") == "P10", "P11 predecessor node drift", errors)
    require(pred.get("integration_commit") == BASE, "P10 integration baseline drift", errors)
    require(pred.get("signed_source_commit") == P10_SIGNED_SOURCE, "P10 signed source drift", errors)
    require(pred.get("closure_run_id") == P10_CLOSURE_RUN, "P10 closure run drift", errors)
    require(pred.get("artifact_id") == P10_ARTIFACT_ID, "P10 artifact id drift", errors)
    require(pred.get("artifact_digest") == P10_ARTIFACT_DIGEST, "P10 artifact digest drift", errors)
    require(pred.get("phase") == "signed" and pred.get("merge_authoritative") is True, "P10 predecessor authority is not signed/authoritative", errors)

    trace = plan.get("master_test_trace", {})
    require(set(trace.keys()) == set(MASTER_TESTS), "Master test trace keys drift", errors)
    flattened_trace = json.dumps(trace, ensure_ascii=False)
    for case_id in ("P11-T003", "P11-T009", "P11-T010", "P11-T013", "P11-T014", "P11-T015", "P11-T018"):
        require(case_id in flattened_trace, f"Master test trace missing {case_id}", errors)

    route = plan.get("route_contract", {})
    require(tuple(route.get("ia_authoritative_routes", [])) == EXPECTED_IA_ROUTES, "P11 IA route list drift", errors)
    note = str(route.get("workspace_api_authority_note", ""))
    require("does not freeze exact Workspace HTTP method/path families" in note, "IA-vs-P11 Workspace API authority distinction missing", errors)
    require("must not be represented as IA-exact authority" in note, "P11 implementation-authority warning missing", errors)
    require(tuple(route.get("p11_workspace_api_family", [])) == EXPECTED_WORKSPACE_APIS, "P11 Workspace implementation API family drift", errors)
    public = route.get("public_contract", {})
    require(public.get("page") == "GET /p/{slug} is IA-exact PUB-BIO browser authority.", "PUB-BIO page authority drift", errors)
    require("GET /api/public/bio/{slug}" in str(public.get("machine", "")) and "IA-exact" in str(public.get("machine", "")), "public Bio API authority drift", errors)
    require(public.get("published_status") == 200, "Bio published status drift", errors)
    require(public.get("paused_status") == 200, "Bio paused status drift", errors)
    require(public.get("unknown_or_unpublished_status") == 404, "Bio unknown/draft status drift", errors)
    require(public.get("removed_status") == 410, "Bio removed status drift", errors)
    blocked = str(public.get("blocked_child_behavior", ""))
    require("remain 200" in blocked and "must not remain active href/navigation targets" in blocked, "Bio blocked-child fail-closed contract missing", errors)
    require(str(public.get("legacy_aliases", "")).startswith("PROHIBITED"), "legacy alias prohibition missing", errors)

    resource = plan.get("resource_contract", {})
    require(set(resource.get("page_fields", [])) == PAGE_FIELDS, "Bio page authoritative-field set drift", errors)
    require(set(resource.get("child_link_fields", [])) == CHILD_FIELDS, "Bio child-link authoritative-field set drift", errors)
    require(resource.get("page_statuses") == ["draft", "published", "paused"], "Bio page status set drift", errors)
    require(resource.get("child_risk_statuses") == ["review", "allowed", "blocked"], "Bio child risk status set drift", errors)
    for key, token in (
        ("slug_authority", "server-generated"),
        ("content_authority", "must never execute"),
        ("link_authority", "http/https"),
        ("risk_authority", "fingerprint"),
        ("publication_authority", "Draft is public 404"),
        ("conflict_authority", "409"),
        ("delete_authority", "410"),
    ):
        require(token.lower() in str(resource.get(key, "")).lower(), f"Bio {key} contract missing {token}", errors)

    deferred = plan.get("deferred_contract", {})
    require(deferred.get("capability") == "CAP-BIO-OPT-IN-INDEX", "deferred Bio index capability drift", errors)
    require(deferred.get("status") == "DEFERRED", "CAP-BIO-OPT-IN-INDEX must remain DEFERRED", errors)
    deferred_rule = str(deferred.get("p11_rule", ""))
    require("No P11 API" in deferred_rule and "Bio remains noindex" in deferred_rule and "absent from sitemaps" in deferred_rule, "P11 deferred indexing boundary missing", errors)

    seo = plan.get("seo_contract", {})
    require(seo.get("public_route") == "PUB-BIO /p/{slug}", "Bio SEO route drift", errors)
    require(seo.get("index_policy") == "noindex", "PUB-BIO must remain noindex", errors)
    require(seo.get("canonical") == "none" and seo.get("locale_alternate") == "none", "PUB-BIO canonical/hreflang must remain none", errors)
    require(seo.get("structured_data") == "none", "PUB-BIO structured data must remain none", errors)
    require(seo.get("sitemap") is False, "PUB-BIO must remain excluded from sitemap", errors)
    require(seo.get("metadata_source") == "resource-safe title only", "PUB-BIO metadata source drift", errors)
    require(seo.get("machine_header") == "X-Robots-Tag: noindex, nofollow", "Bio machine robots header drift", errors)
    require(seo.get("outbound_rel") == "ugc nofollow", "Bio outbound UGC rel drift", errors)
    require(
        seo.get("http_contract") == {"published": 200, "paused": 200, "unknown_or_unpublished": 404, "removed": 410},
        "Bio SEO HTTP contract drift",
        errors,
    )
    require("later-owned" in str(seo.get("release_wide_g7", "")), "release-wide G7 boundary missing", errors)

    env = plan.get("environment_contract", {})
    require("Real MySQL" in str(env.get("mysql", "")), "real MySQL requirement missing", errors)
    require("Real native Go platformapi" in str(env.get("platformapi", "")), "real native platformapi requirement missing", errors)
    require("P05" in str(env.get("destination_risk", "")) and "P16" in str(env.get("destination_risk", "")), "destination-risk predecessor/later-owner boundary missing", errors)
    require("Built Workspace" in str(env.get("workspace_browser", "")), "real Workspace browser requirement missing", errors)
    require("raw status/header/body evidence" in str(env.get("public_http", "")), "raw public HTTP evidence requirement missing", errors)
    require(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Node boundary drift", errors)

    cases = plan.get("cases", [])
    ids = tuple(item.get("id") for item in cases if isinstance(item, dict))
    require(ids == EXPECTED_CASES, f"P11 case IDs/order drift: {ids}", errors)
    for case in cases:
        if not isinstance(case, dict):
            errors.append("P11 case entry is not an object")
            continue
        cid = case.get("id")
        require(case.get("owner") == "P11", f"{cid} owner drift", errors)
        require(case.get("expected_exit") == 0, f"{cid} expected_exit drift", errors)
        for field in ("name", "precondition", "driver", "oracle", "evidence"):
            require(bool(case.get(field)), f"{cid} missing {field}", errors)
        require(str(case.get("evidence", "")).startswith("artifacts/v10/P11/"), f"{cid} evidence outside P11 root", errors)

    by_id = {case.get("id"): case for case in cases if isinstance(case, dict)}
    for n in range(1, 13):
        require(str(by_id.get(f"P11-T{n:03d}", {}).get("driver", "")).startswith("python3 scripts/p11/integration.py"), f"P11-T{n:03d} integration driver drift", errors)
    for n in range(13, 16):
        require(str(by_id.get(f"P11-T{n:03d}", {}).get("driver", "")).startswith("python3 scripts/p11/seo.py"), f"P11-T{n:03d} SEO driver drift", errors)
    for n in range(16, 19):
        require(str(by_id.get(f"P11-T{n:03d}", {}).get("driver", "")).startswith("node scripts/p11/browser.mjs"), f"P11-T{n:03d} browser driver drift", errors)
    require(by_id.get("P11-T019", {}).get("driver") == "python3 scripts/p11/validate.py --case P11-T019", "P11-T019 coherence driver drift", errors)
    require(by_id.get("P11-T020", {}).get("driver") == "python3 scripts/p11/validate.py --case P11-T020 --closure", "P11-T020 closure driver drift", errors)

    closure = plan.get("closure_contract", {})
    require(closure.get("version") == 1, "P11 closure contract version drift", errors)
    require(closure.get("same_exact_head_required") is True, "P11 closure must require exact head", errors)
    require(closure.get("required_case_range") == "P11-T001..P11-T020", "P11 closure case range drift", errors)
    require(closure.get("review_required") is True, "P11 accountable review must be required", errors)
    require(closure.get("p0_max") == 0 and closure.get("p1_max") == 0 and closure.get("decision_required_max") == 0, "P11 defect/decision thresholds drift", errors)
    require(closure.get("pre_sign_phase") == "pre-sign / merge_authoritative=false", "P11 pre-sign closure phase drift", errors)
    require(closure.get("signed_phase") == "signed / merge_authoritative=true", "P11 signed closure phase drift", errors)
    predecessor_rule = str(closure.get("predecessor_rule", ""))
    require("P10" in predecessor_rule and P10_SIGNED_SOURCE in predecessor_rule and "must not rerun P10" in predecessor_rule, "P11 predecessor signed-closure inheritance rule missing", errors)
    closure_scope = str(closure.get("gate_scope", ""))
    require("P11" in closure_scope and "P16" in closure_scope and "full G7" in closure_scope, "P11 closure later-owner boundary missing", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.strip().startswith("Status:")]
    has_pending = status_lines == [PENDING]
    has_signed = status_lines == [SIGNED]
    require(has_pending ^ has_signed, f"review.md must contain exactly one legal P11 status: {status_lines}", errors)

    for marker in (
        "Required P11 case range: **P11-T001..P11-T020**.",
        "does **not** freeze an exact Workspace HTTP method/path family",
        "`PUB-BIO /p/{slug}` is permanently `noindex`",
        "Public Bio UGC must not enter Website or Docs sitemaps.",
        "`CAP-BIO-OPT-IN-INDEX` is **DEFERRED**",
        "No extra legacy route family or compatibility alias is approved.",
        "SAME-REVISION CI REQUIRED",
    ):
        require(marker in review, f"P11 review marker missing: {marker}", errors)

    if has_pending:
        require("No P11 PASS or Exit claim is made in this state." in review, "pending review no-PASS marker missing", errors)
        require("Accountable reviewer identity:" not in review, "pending review must not contain accountable signature", errors)
        explicit_pass_claim = re.search(r"(?mi)^\s*(?:[-*]\s*)?P11-T\d{3}\s*[:=-]\s*PASS\b", review)
        require(explicit_pass_claim is None, "pending review must not contain P11 case PASS claims", errors)
    if has_signed:
        require(re.search(r"Pre-sign exact implementation SHA:\s*`[0-9a-f]{40}`", review) is not None, "signed review pre-sign SHA missing", errors)
        identity = re.search(r"Accountable reviewer identity:\s*\*\*(.+?)\*\*", review)
        require(identity is not None and identity.group(1).strip() == "GPT-5.6 Sol — CAP-BIO Technical Review", "signed review identity missing/drifted", errors)
        require(re.search(r"Review date:\s*\*\*\d{4}-\d{2}-\d{2}\*\*", review) is not None, "signed review date missing", errors)
        require("P11-T020" in review and "PASS" in review, "signed review final P11 closure PASS record missing", errors)
        for marker in ("- P0 defects: 0", "- P1 defects: 0", "- `DECISION REQUIRED`: 0"):
            require(marker in review, f"signed review defect marker missing: {marker}", errors)
        for role in ("Backend Lead", "Frontend Lead", "QA Lead", "Accessibility Reviewer", "Security Reviewer", "Product/API Reviewer"):
            require(f"- {role}: APPROVED" in review, f"signed review role approval missing: {role}", errors)
        require("G3 P11" in review and "PASS" in review, "G3 P11 disposition missing", errors)
        require("G7 P11" in review and "PASS" in review, "G7 P11 disposition missing", errors)
        require("P16" in review and "P18/P19/P20/P22" in review and "later-owned" in review.lower(), "signed review later-owner boundary missing", errors)
        require("signed revision itself must rerun" in review.lower(), "signed review rerun rule missing", errors)

    head = git("rev-parse", "HEAD")
    require(bool(re.fullmatch(r"[0-9a-f]{40}", head)), f"invalid exact HEAD: {head}", errors)
    try:
        merge_base = git("merge-base", head, BASE)
        require(merge_base == BASE, f"P11 branch base drift: merge-base={merge_base} expected={BASE}", errors)
    except Exception as exc:
        errors.append(f"cannot verify P11 branch ancestry: {exc}")

    result = {
        "node": "P11",
        "contract": "Bio",
        "status": "PASS" if not errors else "FAIL",
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "case_range": "P11-T001..P11-T020",
        "case_count": len(cases),
        "review_status": "SIGNED" if has_signed else "PENDING" if has_pending else "INVALID",
        "workspace_api_authority": "P11 implementation contract; IA freezes public Bio GET and Bio semantics but not exact Workspace HTTP paths",
        "public_ugc_policy": "PUB-BIO permanent noindex / no sitemap / outbound rel=ugc nofollow / CAP-BIO-OPT-IN-INDEX DEFERRED",
        "predecessor_signed_source": P10_SIGNED_SOURCE,
        "errors": errors,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
