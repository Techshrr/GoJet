#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "43e693b10c0118e32d7f14c61156e0b06c155111"
P18_SIGNED_SOURCE = "e8746159b02c729a877e3dcbd9655d415a5cc269"
P18_SIGNED_REVIEW_BLOB = "2ac0af53f30578da7e7cdb17a1910b853f96036f"
FROZEN_TEST_PLAN_BLOB = "10d302e784e84b77782c88129a6082363e87fd12"
PENDING_REVIEW_BLOB = "f70074b2a5edee7bd9f2e223dbb8cad7c492448e"
BASE_SITE_PACKAGE_BLOB = "4fdc8469a8487591d29f4e84c19eca2e3bf37e98"

AUTHORITY_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
    "artifacts/v10/P18/review.md": P18_SIGNED_REVIEW_BLOB,
}
EXPECTED_CONTRACT_FILES = {
    "artifacts/v10/P19/test-plan.json",
    "artifacts/v10/P19/review.md",
    "scripts/p19/validate_contract.py",
    ".github/workflows/p19-website-technical-seo.yml",
}
EXPECTED_ROUTE_IDS = [
    "WEB-HOME","WEB-PRODUCTS","WEB-LINKS","WEB-QR","WEB-FILES","WEB-TEXT","WEB-BIO","WEB-ANALYTICS","WEB-ROUTING","WEB-DOMAINS",
    "WEB-SOLUTIONS","WEB-SOL-MARKETING","WEB-SOL-CREATORS","WEB-SOL-TEAMS","WEB-SOL-DEVELOPERS","WEB-DEVELOPERS","WEB-PRICING","WEB-SECURITY",
    "WEB-GUIDES","WEB-GUIDE","WEB-ABOUT","WEB-CONTACT","WEB-LEGAL-TERMS","WEB-LEGAL-PRIVACY","WEB-LEGAL-AUP","WEB-LEGAL-ABUSE",
]
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def ancestor(older: str, newer: str) -> bool:
    return subprocess.run(["git", "merge-base", "--is-ancestor", older, newer], check=False, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0


def blob(revision: str, path: str) -> str:
    return git("rev-parse", f"{revision}:{path}")


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def derive_contract_authority(head: str) -> str:
    commits = [x for x in git("rev-list", "--ancestry-path", "--reverse", f"{BASE}..{head}").splitlines() if x]
    if not commits:
        return ""
    return commits[0]


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P19/test-plan.json")
    review_path = Path("artifacts/v10/P19/review.md")
    need(plan_path.is_file(), "missing P19 test-plan.json", errors)
    need(review_path.is_file(), "missing P19 review.md", errors)
    if errors:
        print(json.dumps({"node":"P19","status":"FAIL","errors":errors}, indent=2))
        return 1

    head = git("rev-parse", "HEAD")
    authority = derive_contract_authority(head)
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    review = review_path.read_text(encoding="utf-8")

    need(bool(authority) and re.fullmatch(r"[0-9a-f]{40}", authority) is not None, "cannot derive P19 contract authority", errors)
    if authority:
        need(git("rev-parse", f"{authority}^") == BASE, "P19 contract authority must be direct child of P18 integration", errors)
        need(ancestor(authority, head), "P19 HEAD must descend from contract authority", errors)
        changed = {x for x in git("diff", "--name-only", f"{BASE}..{authority}").splitlines() if x}
        need(changed == EXPECTED_CONTRACT_FILES, f"P19 contract-freeze path set drift: {sorted(changed)}", errors)
        for path in ("scripts/p19/validate_contract.py", ".github/workflows/p19-website-technical-seo.yml"):
            try:
                need(blob("HEAD", path) == blob(authority, path), f"frozen P19 contract tooling drift: {path}", errors)
            except Exception as exc:
                errors.append(f"cannot bind frozen P19 tooling {path}: {exc}")

    need(ancestor(P18_SIGNED_SOURCE, BASE), "P18 signed source must remain in P19 base ancestry", errors)
    try:
        need(blob("HEAD", "artifacts/v10/P19/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "frozen P19 test-plan blob drift", errors)
        if authority:
            need(blob(authority, "artifacts/v10/P19/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "authority test-plan blob mismatch", errors)
            need(blob(authority, "artifacts/v10/P19/review.md") == PENDING_REVIEW_BLOB, "authority pending review blob mismatch", errors)
        need(blob(BASE, "frontend/apps/site/package.json") == BASE_SITE_PACKAGE_BLOB, "P19 base Website package authority drift", errors)
        for path, expected in AUTHORITY_BLOBS.items():
            need(blob("HEAD", path) == expected, f"normative authority blob drift: {path}", errors)
    except Exception as exc:
        errors.append(f"cannot bind P19 frozen authority blobs: {exc}")

    need(plan.get("node") == "P19", "node must remain P19", errors)
    need(plan.get("title") == "Website and Technical SEO", "P19 title drift", errors)
    need(plan.get("issue") == 50, "P19 issue drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P19 base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "P19 specification IDs/order drift", errors)

    cap = plan.get("capability_contract", {})
    caps = cap.get("capabilities", [])
    need([c.get("id") for c in caps if isinstance(c, dict)] == ["CAP-TECHNICAL-SEO","CAP-ANNOUNCEMENTS-SETTINGS"], "P19 capability boundary drift", errors)
    need(cap.get("master_predecessors") == ["P18","P05-P17 public capability owners"], "P19 predecessor boundary drift", errors)
    need(cap.get("exit_gates") == ["G4","G5","G7","G8","G9"], "P19 exit-gate boundary drift", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node":"P18","integration_commit":BASE,"signed_source_commit":P18_SIGNED_SOURCE,
        "closure_run_id":33260817755,"artifact_id":9717210947,
        "artifact_digest":"sha256:3e403765409b3ab273be1c35a9d88b565505c416a47364d9a6f0339cc130efe4",
        "phase":"signed","review_phase":"signed","merge_authoritative":True,
        "case_range":"P18-T001..P18-T026","affected_matrix":"10/10",
    }, "P18 predecessor signed authority drift", errors)

    routes = plan.get("route_contract", {})
    need(routes.get("route_ids") == EXPECTED_ROUTE_IDS, "P19 Website route ID/order drift", errors)
    need(routes.get("route_id_count") == 26, "P19 Website route count drift", errors)
    need(routes.get("locales") == ["en","zh-CN"], "P19 locale set/order drift", errors)
    need(routes.get("canonical_id") == "CAN-WEB" and routes.get("alternate_id") == "ALT-WEB" and routes.get("metadata_id") == "META-WEB", "P19 URL/metadata identifiers drift", errors)
    need(routes.get("invented_paths") == "PROHIBITED", "P19 invented-path prohibition drift", errors)

    content = plan.get("content_contract", {})
    need(content.get("required_metadata") == ["title","description","h1","locale","updatedTime","contentOwner","canonicalPath","translation"], "META-WEB fields drift", errors)
    for key in ("thin_keyword_pages","crawler_only_rendering","fake_dashboard_or_product_ui","fabricated_ratings_reviews_compliance"):
        need(content.get(key) == "PROHIBITED", f"P19 prohibition drift: {key}", errors)
    need(content.get("capability_claims") == "SIGNED_RELEASE_AUTHORITY_ONLY", "P19 capability-claim authority drift", errors)

    seo = plan.get("seo_contract", {})
    need(seo.get("gate") == "G7 P19 Website contribution", "P19 G7 boundary drift", errors)
    need(seo.get("sitemap_child") == "Website child" and seo.get("raw_html_primary_content") is True, "P19 Website sitemap/raw-HTML boundary drift", errors)
    need(seo.get("soft_404") == "PROHIBITED" and seo.get("orphan_index_pages") == 0, "P19 soft-404/orphan boundary drift", errors)

    perf = plan.get("performance_contract", {})
    need(perf.get("website_initial_js_gzip_max_kb") == 150, "P19 JS budget drift", errors)
    need(perf.get("lcp_seconds_max") == 2.5 and perf.get("inp_ms_max") == 200 and perf.get("cls_max") == 0.1, "P19 CWV budget drift", errors)
    need(perf.get("website_loads_workspace_admin_bundle") == "PROHIBITED", "P19 bundle isolation drift", errors)

    env = plan.get("environment_contract", {})
    need(env.get("production_site_runtime") == "STATIC_NGINX_ONLY", "P19 static runtime boundary drift", errors)
    need(env.get("production_node_http_ssr_pm2") == "PROHIBITED", "P19 Node production boundary drift", errors)
    need(env.get("production_docker_compose") == "PROHIBITED", "P19 Docker production boundary drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True and closure.get("review_only_signed_child_required") is True, "P19 closure discipline drift", errors)
    need(closure.get("required_case_range") == "P19-T001..P19-T032", "P19 case range drift", errors)
    need(closure.get("pre_sign_evidence_range") == "P19-T001..P19-T031", "P19 pre-sign evidence range drift", errors)
    need(closure.get("defect_limits") == {"p0":0,"p1":0,"decision_required":0}, "P19 defect limits drift", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P19-T{i:03d}" for i in range(1, 33)]
    actual_ids = [c.get("id") for c in cases if isinstance(c, dict)]
    need(actual_ids == expected_ids, f"P19 frozen case IDs/order drift: {actual_ids}", errors)
    for case in cases:
        for field in ("id","name","driver","oracle","evidence","owner"):
            need(bool(str(case.get(field, "")).strip()), f"{case.get('id')} missing {field}", errors)

    status_lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    review_phase = "invalid"
    if status_lines == [PENDING]:
        review_phase = "pending"
        try:
            need(blob("HEAD", "artifacts/v10/P19/review.md") == PENDING_REVIEW_BLOB, "pending P19 review blob drift", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P19 review: {exc}")
    elif status_lines == [SIGNED]:
        review_phase = "signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`", review)), "signed P19 review missing reviewed pre-sign SHA", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review, "signed P19 review missing zero defect/decision ledger", errors)
        need("P19-T001..P19-T031" in review, "signed P19 review missing pre-sign evidence range", errors)
    else:
        need(False, f"invalid P19 review status lines: {status_lines}", errors)

    if authority and head == authority:
        mode, implementation_authorized = "contract-freeze", False
    elif review_phase == "pending":
        mode, implementation_authorized = "implementation-guard", True
    elif review_phase == "signed":
        mode, implementation_authorized = "signed-review-guard", False
    else:
        mode, implementation_authorized = "invalid", False

    result = {
        "node":"P19",
        "status":"PASS" if not errors else "FAIL",
        "errors":errors,
        "implementation_commit":head,
        "base_integration_commit":BASE,
        "contract_authority":authority,
        "case_range":"P19-T001..P19-T032",
        "review_phase":review_phase,
        "mode":mode,
        "implementation_authorized":implementation_authorized,
        "frozen_contract_preserved":not errors,
        "merge_authoritative":False,
        "predecessor_signed_source":P18_SIGNED_SOURCE,
        "predecessor_closure_run":33260817755,
        "predecessor_artifact":9717210947,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
