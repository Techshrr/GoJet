#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "62d682a25532eef3cc207a5e9964a62f6072ede7"
CONTRACT_AUTHORITY = "30174f40df28678360f644b8fed79736906b0ea0"
FROZEN_TEST_PLAN_BLOB = "0ddba638ed882f6d665ad614a148246e80ed16e6"
PENDING_REVIEW_BLOB = "43865d66a1c601682b620071aee1188789341741"
P16_SIGNED_SOURCE = "c22d87102a8a691b5d1d1a31506def21112700e7"
P16_SIGNED_REVIEW_BLOB = "2dd74d8383cbeb6fa2d2bf4552fc31e78ce0aa34"
SPEC_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "docs/security/SECURITY_INVARIANTS.md": "5d3178ee80bf46b4f00df729ab24d783a7af75dc",
}
EXPECTED_CONTRACT_FILES = {
    "artifacts/v10/P17/test-plan.json",
    "artifacts/v10/P17/review.md",
    "scripts/p17/validate_contract.py",
    ".github/workflows/p17-admin-permissions-audit.yml",
}
EXPECTED_CAPS = {
    "CAP-ADMIN-ACCESS","CAP-OPS-AUDIT","CAP-API-KEYS","CAP-USER-WEBHOOKS",
    "CAP-OFFICIAL-DOMAINS","CAP-FILES","CAP-NOTIFICATIONS","CAP-TURNSTILE",
    "CAP-DOMAIN-ENTITLEMENT","CAP-DOMAIN-RISK","CAP-ABUSE","CAP-ANNOUNCEMENTS-SETTINGS",
}
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def ancestor(older: str, newer: str) -> bool:
    return subprocess.run(["git", "merge-base", "--is-ancestor", older, newer], check=False).returncode == 0


def blob(revision: str, path: str) -> str:
    return git("rev-parse", f"{revision}:{path}")


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P17/test-plan.json")
    review_path = Path("artifacts/v10/P17/review.md")
    need(plan_path.is_file(), "missing P17 test-plan.json", errors)
    need(review_path.is_file(), "missing P17 review.md", errors)
    if errors:
        print(json.dumps({"node":"P17","status":"FAIL","errors":errors}, indent=2))
        return 1

    head = git("rev-parse", "HEAD")
    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    review = review_path.read_text(encoding="utf-8")

    need(ancestor(BASE, CONTRACT_AUTHORITY), "contract authority must descend from exact P16 integration base", errors)
    need(ancestor(CONTRACT_AUTHORITY, head), "P17 HEAD must descend from frozen contract authority", errors)
    need(ancestor(P16_SIGNED_SOURCE, BASE), "P16 signed source must remain in P17 integration ancestry", errors)

    authority_changed = {x for x in git("diff", "--name-only", f"{BASE}..{CONTRACT_AUTHORITY}").splitlines() if x}
    need(authority_changed == EXPECTED_CONTRACT_FILES,
         f"P17 contract-freeze path set must be exactly {sorted(EXPECTED_CONTRACT_FILES)}, got {sorted(authority_changed)}", errors)

    try:
        need(blob("HEAD", "artifacts/v10/P17/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "frozen P17 test-plan blob drift", errors)
        need(blob(CONTRACT_AUTHORITY, "artifacts/v10/P17/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "authority test-plan blob mismatch", errors)
        need(blob("HEAD", "artifacts/v10/P16/review.md") == P16_SIGNED_REVIEW_BLOB, "P16 signed review blob drift", errors)
        for path, expected in SPEC_BLOBS.items():
            need(blob("HEAD", path) == expected, f"normative authority blob drift: {path}", errors)
    except Exception as exc:
        errors.append(f"cannot bind frozen authority blobs: {exc}")

    need(plan.get("node") == "P17", "node must remain P17", errors)
    need(plan.get("title") == "Admin, Permissions and Audit", "P17 title drift", errors)
    need(plan.get("issue") == 46, "P17 issue drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P17 base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "P17 specification IDs/order drift", errors)

    cap = plan.get("capability_contract", {})
    actual_caps = {row.get("id") for row in cap.get("capabilities", []) if isinstance(row, dict)}
    need(actual_caps == EXPECTED_CAPS, f"P17 capability set drift: {sorted(actual_caps)}", errors)
    need(cap.get("master_predecessors") == ["P06","P12","P13","P14","P16"], "P17 predecessor list drift", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node":"P16",
        "integration_commit":BASE,
        "signed_source_commit":P16_SIGNED_SOURCE,
        "closure_run_id":33010844881,
        "artifact_id":9630819391,
        "artifact_digest":"sha256:00dbba2180f88ecdb6b369cb97abfdcafd211789088837d39e02a2d331a75722",
        "phase":"signed",
        "merge_authoritative":True,
        "case_range":"P16-T001..P16-T029",
        "affected_matrix":"55/55",
    }, "P16 predecessor signed authority drift", errors)

    permissions = set(plan.get("permission_contract", {}).get("admin_permissions", []))
    for required in ("admins.manage","domains.entitlements.manage","domains.risk.manage","security.manage","operations.manage","settings.manage"):
        need(required in permissions, f"P17 required permission missing: {required}", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("workspace_routes", [])) == {"APP-API-KEYS /app/api-keys","APP-WEBHOOKS /app/webhooks"}, "P17 Workspace developer route drift", errors)
    route_text = "\n".join(routes.get("admin_routes", []))
    for required in ("/admin/access/administrators","/admin/access/roles","/admin/domain-entitlements","/admin/operations/services","/admin/audit","/admin/platform/official-domains","/admin/platform/turnstile","/admin/trust/abuse"):
        need(required in route_text, f"P17 required Admin route missing: {required}", errors)

    entitlement = plan.get("domain_entitlement_contract", {})
    need(entitlement.get("permission") == "domains.entitlements.manage", "P17 domain-entitlement permission drift", errors)
    need(entitlement.get("approve_required") == ["domain_limit","starts_at","expires_at","reason","support_ticket_id"], "P17 domain-entitlement approve contract drift", errors)

    hooks = plan.get("webhook_contract", {})
    need(hooks.get("service_identity") == "SVC-OPS-MONITOR operationsmonitor outbound-webhook delivery contribution", "P17 webhook service identity drift", errors)
    ops = plan.get("operations_audit_contract", {})
    need(ops.get("service_ids") == ["redirectengine","analyticsworker","analyticsreconciler","platformapi","mailworker","fileworker","operationsmonitor","logreceiver"], "P17 eight-service inventory drift", errors)
    env = plan.get("environment_contract", {})
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Compose/Node boundary drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "P17 same-head closure drift", errors)
    need(closure.get("required_case_range") == "P17-T001..P17-T035", "P17 case range drift", errors)
    need(closure.get("defect_limits") == {"p0":0,"p1":0,"decision_required":0}, "P17 defect limits drift", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P17-T{i:03d}" for i in range(1,36)]
    actual_ids = [c.get("id") for c in cases if isinstance(c, dict)]
    need(actual_ids == expected_ids, f"P17 frozen case IDs/order drift: {actual_ids}", errors)
    for case in cases:
        for field in ("id","name","driver","oracle","evidence","owner"):
            need(bool(str(case.get(field, "")).strip()), f"{case.get('id')} missing {field}", errors)

    status_lines = re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    review_phase = "invalid"
    if status_lines == [PENDING]:
        review_phase = "pending"
        try:
            need(blob("HEAD", "artifacts/v10/P17/review.md") == PENDING_REVIEW_BLOB, "pending P17 review blob drift", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P17 review: {exc}")
    elif status_lines == [SIGNED]:
        review_phase = "signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`", review)), "signed P17 review missing reviewed pre-sign SHA", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review, "signed P17 review missing zero defect/decision ledger", errors)
    else:
        need(False, f"invalid P17 review status lines: {status_lines}", errors)

    if head == CONTRACT_AUTHORITY:
        mode, implementation_authorized = "contract-freeze", False
    elif review_phase == "pending":
        mode, implementation_authorized = "implementation-guard", True
    elif review_phase == "signed":
        mode, implementation_authorized = "signed-review-guard", False
    else:
        mode, implementation_authorized = "invalid", False

    result = {
        "node":"P17",
        "status":"PASS" if not errors else "FAIL",
        "errors":errors,
        "implementation_commit":head,
        "base_integration_commit":BASE,
        "contract_authority":CONTRACT_AUTHORITY,
        "case_range":"P17-T001..P17-T035",
        "review_phase":review_phase,
        "mode":mode,
        "implementation_authorized":implementation_authorized,
        "frozen_contract_preserved":not errors,
        "merge_authoritative":False,
        "predecessor_signed_source":P16_SIGNED_SOURCE,
        "predecessor_closure_run":33010844881,
        "predecessor_artifact":9630819391,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
