#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "08cb39bbe54717b711e2d09840ecde04b66bb50f"
CONTRACT_AUTHORITY = "__PIN_AFTER_FREEZE__"
FROZEN_TEST_PLAN_BLOB = "00d25507cd159a931b7c09a9db314c185b2fd74c"
PENDING_REVIEW_BLOB = "a3d3aa1bddeeb526fa45c746e2b2f6228c7424bb"
P17_SIGNED_SOURCE = "5818406072a131db1c7d8aa7bc5ef8a7adc8d51f"
P17_SIGNED_REVIEW_BLOB = "f3527671588a642fc937636e03e3a22cf2e79c58"
P04_SIGNED_REVIEW_BLOB = "4d1a69d1889fd78297247a013936c5aad86a97ed"

AUTHORITY_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
}
EXPECTED_CONTRACT_FILES = {
    "artifacts/v10/P18/test-plan.json",
    "artifacts/v10/P18/review.md",
    "scripts/p18/validate_contract.py",
    ".github/workflows/p18-docs-multilingual-discovery.yml",
}
EXPECTED_ROUTE_IDS = {
    "DOCS-EN-HOME", "DOCS-ZH-HOME", "DOCS-ARTICLE", "DOCS-API", "DOCS-SEARCH"
}
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def ancestor(older: str, newer: str) -> bool:
    return subprocess.run(
        ["git", "merge-base", "--is-ancestor", older, newer],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    ).returncode == 0


def blob(revision: str, path: str) -> str:
    return git("rev-parse", f"{revision}:{path}")


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P18/test-plan.json")
    review_path = Path("artifacts/v10/P18/review.md")
    need(plan_path.is_file(), "missing P18 test-plan.json", errors)
    need(review_path.is_file(), "missing P18 review.md", errors)
    if errors:
        print(json.dumps({"node": "P18", "status": "FAIL", "errors": errors}, indent=2))
        return 1

    head = git("rev-parse", "HEAD")
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    review = review_path.read_text(encoding="utf-8")

    need(re.fullmatch(r"[0-9a-f]{40}", CONTRACT_AUTHORITY) is not None,
         "P18 contract authority is not pinned", errors)
    if re.fullmatch(r"[0-9a-f]{40}", CONTRACT_AUTHORITY):
        need(ancestor(BASE, CONTRACT_AUTHORITY),
             "contract authority must descend from exact P17 integration base", errors)
        need(ancestor(CONTRACT_AUTHORITY, head),
             "P18 HEAD must descend from frozen contract authority", errors)
    need(ancestor(P17_SIGNED_SOURCE, BASE),
         "P17 signed source must remain in P18 integration ancestry", errors)

    if re.fullmatch(r"[0-9a-f]{40}", CONTRACT_AUTHORITY):
        authority_changed = {
            x for x in git("diff", "--name-only", f"{BASE}..{CONTRACT_AUTHORITY}").splitlines() if x
        }
        need(
            authority_changed == EXPECTED_CONTRACT_FILES,
            f"P18 contract-freeze path set must be exactly {sorted(EXPECTED_CONTRACT_FILES)}, got {sorted(authority_changed)}",
            errors,
        )

    try:
        need(blob("HEAD", "artifacts/v10/P18/test-plan.json") == FROZEN_TEST_PLAN_BLOB,
             "frozen P18 test-plan blob drift", errors)
        if re.fullmatch(r"[0-9a-f]{40}", CONTRACT_AUTHORITY):
            need(blob(CONTRACT_AUTHORITY, "artifacts/v10/P18/test-plan.json") == FROZEN_TEST_PLAN_BLOB,
                 "authority test-plan blob mismatch", errors)
        need(blob("HEAD", "artifacts/v10/P17/review.md") == P17_SIGNED_REVIEW_BLOB,
             "P17 signed review blob drift", errors)
        need(blob("HEAD", "artifacts/v10/P04/review.md") == P04_SIGNED_REVIEW_BLOB,
             "P04 signed Docs-shell review blob drift", errors)
        for path, expected in AUTHORITY_BLOBS.items():
            need(blob("HEAD", path) == expected, f"normative authority blob drift: {path}", errors)
    except Exception as exc:
        errors.append(f"cannot bind frozen authority blobs: {exc}")

    need(plan.get("node") == "P18", "node must remain P18", errors)
    need(plan.get("title") == "Docs and Multilingual Discovery", "P18 title drift", errors)
    need(plan.get("issue") == 48, "P18 issue drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P18 base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "P18 specification IDs/order drift", errors)

    cap = plan.get("capability_contract", {})
    caps = cap.get("capabilities", [])
    need(len(caps) == 1 and isinstance(caps[0], dict) and caps[0].get("id") == "CAP-TECHNICAL-SEO",
         "P18 capability boundary must be exactly CAP-TECHNICAL-SEO contribution", errors)
    need(cap.get("master_predecessors") == ["P04", "P17"],
         "P18 predecessor list drift", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P17",
        "integration_commit": BASE,
        "signed_source_commit": P17_SIGNED_SOURCE,
        "closure_run_id": 33232541982,
        "artifact_id": 9709093486,
        "artifact_digest": "sha256:72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1",
        "phase": "signed",
        "merge_authoritative": True,
        "case_range": "P17-T001..P17-T035",
        "affected_matrix": "66/66",
    }, "P17 predecessor signed authority drift", errors)

    p04 = plan.get("p04_docs_shell_authority", {})
    need(p04.get("signed_review_blob") == P04_SIGNED_REVIEW_BLOB,
         "P04 Docs-shell authority drift", errors)
    need(p04.get("required_tests") == "10/10",
         "P04 signed required-test authority drift", errors)

    routes = plan.get("route_contract", {})
    rows = routes.get("docs_routes", [])
    route_ids = {row.get("id") for row in rows if isinstance(row, dict)}
    need(route_ids == EXPECTED_ROUTE_IDS, f"P18 Docs route set drift: {sorted(route_ids)}", errors)
    need(routes.get("canonical_id") == "CAN-DOCS", "CAN-DOCS authority drift", errors)
    need(routes.get("alternate_id") == "ALT-DOCS", "ALT-DOCS authority drift", errors)
    need(routes.get("metadata_id") == "META-DOCS", "META-DOCS authority drift", errors)

    content = plan.get("docs_content_contract", {})
    need(content.get("locales") == ["en", "zh-CN"], "P18 locale set/order drift", errors)
    need(content.get("frontmatter_required") == [
        "title", "description", "locale", "lastUpdated", "canonicalPath", "translation", "contentOwner"
    ], "META-DOCS frontmatter field drift", errors)
    need(content.get("search_engine") == "Pagefind static index", "Pagefind search authority drift", errors)
    need(content.get("production_runtime") == "STATIC_NGINX_ONLY",
         "P18 static production runtime boundary drift", errors)

    api = plan.get("api_reference_contract", {})
    secret_classes = set(api.get("secret_classes", []))
    for required in ("tokens", "API-key raw secrets", "webhook secrets", "database credentials"):
        need(required in secret_classes, f"P18 secret-safe API docs boundary missing: {required}", errors)

    env = plan.get("environment_contract", {})
    need(env.get("production_node_http_ssr_pm2") == "PROHIBITED",
         "production Node HTTP/SSR/PM2 boundary drift", errors)
    need(env.get("production_docker_compose") == "PROHIBITED",
         "production Docker/Compose boundary drift", errors)

    seo = plan.get("seo_contract", {})
    need(seo.get("gate") == "G7 P18 Docs contribution", "P18 G7 boundary drift", errors)
    need(seo.get("sitemap_children") == ["Docs EN child", "Docs zh-CN child"],
         "P18 sitemap child contract drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "P18 same-head closure drift", errors)
    need(closure.get("review_only_signed_child_required") is True,
         "P18 review-only signed-child rule drift", errors)
    need(closure.get("required_case_range") == "P18-T001..P18-T026",
         "P18 case range drift", errors)
    need(closure.get("defect_limits") == {"p0": 0, "p1": 0, "decision_required": 0},
         "P18 defect limits drift", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P18-T{i:03d}" for i in range(1, 27)]
    actual_ids = [c.get("id") for c in cases if isinstance(c, dict)]
    need(actual_ids == expected_ids, f"P18 frozen case IDs/order drift: {actual_ids}", errors)
    for case in cases:
        for field in ("id", "name", "driver", "oracle", "evidence", "owner"):
            need(bool(str(case.get(field, "")).strip()),
                 f"{case.get('id')} missing {field}", errors)

    status_lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    review_phase = "invalid"
    if status_lines == [PENDING]:
        review_phase = "pending"
        try:
            need(blob("HEAD", "artifacts/v10/P18/review.md") == PENDING_REVIEW_BLOB,
                 "pending P18 review blob drift", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P18 review: {exc}")
    elif status_lines == [SIGNED]:
        review_phase = "signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`", review)),
             "signed P18 review missing reviewed pre-sign SHA", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review,
             "signed P18 review missing zero defect/decision ledger", errors)
        need("P18-T001..P18-T025" in review,
             "signed P18 review missing pre-sign evidence range", errors)
    else:
        need(False, f"invalid P18 review status lines: {status_lines}", errors)

    if head == CONTRACT_AUTHORITY:
        mode, implementation_authorized = "contract-freeze", False
    elif review_phase == "pending":
        mode, implementation_authorized = "implementation-guard", True
    elif review_phase == "signed":
        mode, implementation_authorized = "signed-review-guard", False
    else:
        mode, implementation_authorized = "invalid", False

    result = {
        "node": "P18",
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P18-T001..P18-T026",
        "review_phase": review_phase,
        "mode": mode,
        "implementation_authorized": implementation_authorized,
        "frozen_contract_preserved": not errors,
        "merge_authoritative": False,
        "predecessor_signed_source": P17_SIGNED_SOURCE,
        "predecessor_closure_run": 33232541982,
        "predecessor_artifact": 9709093486,
        "p04_signed_review_blob": P04_SIGNED_REVIEW_BLOB,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
