#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "62d682a25532eef3cc207a5e9964a62f6072ede7"
CONTRACT_AUTHORITY = "__PIN_AFTER_FREEZE__"
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
EXPECTED_CAPABILITIES = {
    "CAP-ADMIN-ACCESS": ("P17", ("G3","G6","G10")),
    "CAP-OPS-AUDIT": ("P17", ("G3","G6","G13")),
    "CAP-API-KEYS": ("P17", ("G3","G6")),
    "CAP-USER-WEBHOOKS": ("P17", ("G3","G6")),
    "CAP-OFFICIAL-DOMAINS": ("P05/P17", ("G3","G6")),
    "CAP-FILES": ("P09/P17", ("G3","G6","G10")),
    "CAP-NOTIFICATIONS": ("P12/P13-P17", ("G3","G5","G6","G10")),
    "CAP-TURNSTILE": ("P14/P15/P17", ("G6","G10","G13")),
    "CAP-DOMAIN-ENTITLEMENT": ("P06/P13/P14/P17", ("G6","G10")),
    "CAP-DOMAIN-RISK": ("P06/P16/P17", ("G6","G13")),
    "CAP-ABUSE": ("P16/P17", ("G6","G10")),
    "CAP-ANNOUNCEMENTS-SETTINGS": ("P17/P19", ("G3","G6","G7")),
}
EXPECTED_PERMISSIONS = {
    "platform.read","admins.manage","users.manage","workspaces.manage","links.manage","domains.manage",
    "domains.risk.manage","domains.entitlements.manage","security.manage","files.manage","tickets.manage",
    "operations.manage","billing.manage","mail.manage","settings.manage","content.manage",
}
EXPECTED_WORKSPACE_ROUTES = {"APP-API-KEYS /app/api-keys", "APP-WEBHOOKS /app/webhooks"}
EXPECTED_IA_APIS = [
    "POST /api/admin/auth/login","GET /api/admin/overview","GET /api/admin/domain-entitlements",
    "GET /api/admin/domain-entitlements/{workspaceId}","POST /api/admin/domain-entitlements/{workspaceId}/decisions",
    "GET /api/admin/audit","/api/workspaces/{id}/api-keys*","/api/workspaces/{id}/webhooks*","/api/admin/bot-protection",
]
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

    need(CONTRACT_AUTHORITY != "__PIN_AFTER_FREEZE__", "P17 validator authority is not pinned", errors)
    if CONTRACT_AUTHORITY != "__PIN_AFTER_FREEZE__":
        need(ancestor(BASE, CONTRACT_AUTHORITY), "P17 contract authority must descend from exact P16 integration base", errors)
        need(ancestor(CONTRACT_AUTHORITY, head), f"P17 HEAD must descend from frozen contract authority {CONTRACT_AUTHORITY}", errors)
        changed = {x for x in git("diff", "--name-only", f"{BASE}..{CONTRACT_AUTHORITY}").splitlines() if x}
        need(changed == EXPECTED_CONTRACT_FILES, f"P17 contract-freeze diff drift: {sorted(changed)}", errors)
    need(ancestor(BASE, head), f"P17 HEAD must descend from integration base {BASE}", errors)
    need(ancestor(P16_SIGNED_SOURCE, BASE), "P16 signed source must be an ancestor of P17 integration base", errors)

    try:
        need(blob("HEAD", "artifacts/v10/P17/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "frozen P17 test-plan blob drift", errors)
    except Exception as exc:
        errors.append(f"cannot bind frozen P17 test plan: {exc}")
    for path, expected in SPEC_BLOBS.items():
        try:
            need(blob("HEAD", path) == expected, f"normative authority blob drift: {path}", errors)
        except Exception as exc:
            errors.append(f"cannot bind normative authority {path}: {exc}")
    try:
        need(blob("HEAD", "artifacts/v10/P16/review.md") == P16_SIGNED_REVIEW_BLOB, "P16 signed review blob drift", errors)
    except Exception as exc:
        errors.append(f"cannot bind P16 signed review: {exc}")

    need(plan.get("node") == "P17", "node must be P17", errors)
    need(plan.get("title") == "Admin, Permissions and Audit", "P17 title drift", errors)
    need(plan.get("issue") == 46, "P17 issue drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P17 base integration drift", errors)
    need(plan.get("specification_ids") == ["GJ-V10-MP-GREENFIELD-2026-08-20","GJ-V10-DS-GREENFIELD-2026-08-20","GJ-V10-IA-GREENFIELD-2026-08-20"], "P17 specification IDs/order drift", errors)

    cap = plan.get("capability_contract", {})
    actual_caps = {row.get("id"): (row.get("owner"), tuple(row.get("gates", []))) for row in cap.get("capabilities", []) if isinstance(row, dict)}
    need(actual_caps == EXPECTED_CAPABILITIES, f"P17 capability ownership/gates drift: {actual_caps}", errors)
    need(cap.get("master_predecessors") == ["P06","P12","P13","P14","P16"], "P17 predecessor list drift", errors)
    required = set(cap.get("master_required_tests", []))
    for marker in ("permission denial","ticket-manager cannot approve","reason required","session/MFA","secret redaction"):
        need(marker in required, f"P17 required-test marker missing: {marker}", errors)
    need(any("API-key" in x for x in required), "P17 API-key required tests missing", errors)
    need(any("webhook" in x for x in required), "P17 webhook required tests missing", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred.get("node") == "P16", "P16 predecessor node drift", errors)
    need(pred.get("integration_commit") == BASE, "P16 integration authority drift", errors)
    need(pred.get("signed_source_commit") == P16_SIGNED_SOURCE, "P16 signed source drift", errors)
    need(pred.get("closure_run_id") == 33010844881, "P16 closure run drift", errors)
    need(pred.get("artifact_id") == 9630819391, "P16 closure artifact drift", errors)
    need(pred.get("artifact_digest") == "sha256:00dbba2180f88ecdb6b369cb97abfdcafd211789088837d39e02a2d331a75722", "P16 closure digest drift", errors)
    need(pred.get("phase") == "signed" and pred.get("merge_authoritative") is True, "P16 predecessor must remain signed/merge-authoritative", errors)
    need(pred.get("affected_matrix") == "55/55", "P16 affected matrix authority drift", errors)

    permissions = plan.get("permission_contract", {})
    need(set(permissions.get("admin_permissions", [])) == EXPECTED_PERMISSIONS, "P17 Admin permission catalog drift", errors)
    permission_rules = "\n".join(permissions.get("rules", []))
    for marker in ("tickets.manage","domains.entitlements.manage","domains.risk.manage","security.manage","Frontend","Workspace"):
        need(marker.lower() in permission_rules.lower(), f"P17 permission separation missing {marker}", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("workspace_routes", [])) == EXPECTED_WORKSPACE_ROUTES, "P17 Workspace route set drift", errors)
    need(routes.get("ia_exact_apis") == EXPECTED_IA_APIS, "P17 IA-exact API list/order drift", errors)
    admin_routes = "\n".join(routes.get("admin_routes", []))
    for marker in ("/admin/access/administrators","/admin/access/roles","/admin/domain-entitlements","/admin/operations/services","/admin/audit","/admin/platform/official-domains","/admin/platform/turnstile","/admin/trust/abuse"):
        need(marker in admin_routes, f"P17 Admin route missing {marker}", errors)
    route_rules = "\n".join(routes.get("rules", []))
    for marker in ("no-store","noindex","Page-Level IA","legacy","predecessor"):
        need(marker.lower() in route_rules.lower(), f"P17 route rule missing {marker}", errors)

    entitlement = plan.get("domain_entitlement_contract", {})
    need(entitlement.get("permission") == "domains.entitlements.manage", "P17 entitlement permission drift", errors)
    need(entitlement.get("approve_required") == ["domain_limit","starts_at","expires_at","reason","support_ticket_id"], "P17 approve fields drift", errors)
    entitlement_rules = "\n".join(entitlement.get("rules", []))
    for marker in ("Ticket","P06/P13","P16","expires_at"):
        need(marker.lower() in entitlement_rules.lower(), f"P17 entitlement rule missing {marker}", errors)

    admin_access = plan.get("admin_access_contract", {})
    need(admin_access.get("states") == ["active","suspended","totp-required","locked","session-revoked"], "P17 admin-access states drift", errors)

    keys = plan.get("api_key_contract", {})
    need(keys.get("states") == ["active","expired","revoked"], "P17 API-key states drift", errors)
    key_rules = "\n".join(keys.get("rules", []))
    for marker in ("exactly once","Workspace","Rotation","rate","raw secret"):
        need(marker.lower() in key_rules.lower(), f"P17 API-key rule missing {marker}", errors)

    hooks = plan.get("webhook_contract", {})
    need(hooks.get("service_identity") == "SVC-OPS-MONITOR operationsmonitor outbound-webhook delivery contribution", "P17 webhook worker identity drift", errors)
    hook_rules = "\n".join(hooks.get("rules", []))
    for marker in ("payment callbacks","Workspace","Signing","loopback","DNS","Retry","ninth"):
        need(marker.lower() in hook_rules.lower(), f"P17 webhook rule missing {marker}", errors)

    ops = plan.get("operations_audit_contract", {})
    need(ops.get("service_ids") == ["redirectengine","analyticsworker","analyticsreconciler","platformapi","mailworker","fileworker","operationsmonitor","logreceiver"], "P17 eight-service inventory drift", errors)
    for marker in ("actor","action","resource","result","request_id","reason"):
        need(marker in ops.get("audit_fields", []), f"P17 audit field missing {marker}", errors)

    browser = plan.get("browser_contract", {})
    need(set(browser.get("states", {}).keys()) == {"admin_login","admin_access","admin_entitlements","workspace_api_keys","workspace_webhooks","admin_operations","admin_audit"}, "P17 browser state families drift", errors)

    env = plan.get("environment_contract", {})
    for key in ("mysql","redis","platformapi","operationsmonitor","webhook_fixture","browser","production_docker_compose_node"):
        need(bool(str(env.get(key, "")).strip()), f"P17 environment contract missing {key}", errors)
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
            need(bool(str(case.get(field, "")).strip()), f"P17 case {case.get('id')} missing {field}", errors)

    primary = re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    need(len(primary) == 1, f"P17 review must have one primary status line: {primary}", errors)
    review_phase = "invalid"
    if primary == [PENDING]:
        review_phase = "pending"
        try:
            need(blob("HEAD", "artifacts/v10/P17/review.md") == PENDING_REVIEW_BLOB, "pending P17 review blob drift", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P17 review: {exc}")
    elif primary == [SIGNED]:
        review_phase = "signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`", review)), "signed P17 review missing reviewed SHA", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review, "signed P17 review missing zero ledger", errors)
    else:
        need(False, f"invalid P17 review status: {primary}", errors)

    contract_only = False
    mode = "invalid"
    implementation_authorized = False
    if CONTRACT_AUTHORITY != "__PIN_AFTER_FREEZE__":
        current_changed = {x for x in git("diff", "--name-only", f"{BASE}..HEAD").splitlines() if x}
        contract_only = current_changed.issubset(EXPECTED_CONTRACT_FILES)
        if head == CONTRACT_AUTHORITY:
            mode = "contract-freeze"
        elif review_phase == "pending":
            mode = "implementation-guard"
            implementation_authorized = True
        elif review_phase == "signed":
            mode = "signed-review-guard"

    status = "PASS" if not errors else "FAIL"
    result = {
        "node":"P17","status":status,"errors":errors,"implementation_commit":head,
        "base_integration_commit":BASE,"contract_authority":CONTRACT_AUTHORITY,"case_range":"P17-T001..P17-T035",
        "review_phase":review_phase,"mode":mode,"implementation_authorized":implementation_authorized,
        "frozen_contract_preserved":not errors,"contract_only":contract_only,"merge_authoritative":False,
        "predecessor_signed_source":P16_SIGNED_SOURCE,"predecessor_closure_run":33010844881,"predecessor_artifact":9630819391,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if status == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
