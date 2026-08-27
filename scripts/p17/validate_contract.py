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
    "artifacts/v10/P17/test-plan.json", "artifacts/v10/P17/review.md",
    "scripts/p17/validate_contract.py", ".github/workflows/p17-admin-permissions-audit.yml",
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
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()

def ancestor(a: str, b: str) -> bool:
    return subprocess.run(["git","merge-base","--is-ancestor",a,b], check=False).returncode == 0

def blob(rev: str, path: str) -> str:
    return git("rev-parse", f"{rev}:{path}")

def need(ok: bool, msg: str, errors: list[str]) -> None:
    if not ok: errors.append(msg)

def markers(text: str, values: tuple[str, ...], label: str, errors: list[str]) -> None:
    low=text.lower()
    for value in values: need(value.lower() in low, f"{label} missing {value}", errors)


def main() -> int:
    errors=[]
    plan_path=Path("artifacts/v10/P17/test-plan.json")
    review_path=Path("artifacts/v10/P17/review.md")
    need(plan_path.is_file(), "missing P17 test plan", errors)
    need(review_path.is_file(), "missing P17 review", errors)
    if errors:
        print(json.dumps({"node":"P17","status":"FAIL","errors":errors},indent=2)); return 1
    head=git("rev-parse","HEAD")
    plan=json.loads(plan_path.read_text(encoding="utf-8"))
    review=review_path.read_text(encoding="utf-8")

    need(ancestor(BASE, CONTRACT_AUTHORITY), "contract authority not based on P16 integration", errors)
    need(ancestor(CONTRACT_AUTHORITY, head), "HEAD not descendant of frozen P17 authority", errors)
    need(ancestor(P16_SIGNED_SOURCE, BASE), "P16 signed source not preserved in integration ancestry", errors)
    authority_changed={x for x in git("diff","--name-only",f"{BASE}..{CONTRACT_AUTHORITY}").splitlines() if x}
    need(authority_changed == EXPECTED_CONTRACT_FILES, f"contract-freeze path set drift: {sorted(authority_changed)}", errors)
    need(blob("HEAD","artifacts/v10/P17/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "P17 test-plan blob drift", errors)
    need(blob(CONTRACT_AUTHORITY,"artifacts/v10/P17/test-plan.json") == FROZEN_TEST_PLAN_BLOB, "authority test-plan mismatch", errors)
    for path, expected in SPEC_BLOBS.items(): need(blob("HEAD",path)==expected, f"normative blob drift: {path}", errors)
    need(blob("HEAD","artifacts/v10/P16/review.md") == P16_SIGNED_REVIEW_BLOB, "P16 signed review drift", errors)

    need(plan.get("node")=="P17" and plan.get("title")=="Admin, Permissions and Audit", "P17 identity drift", errors)
    need(plan.get("issue")==46 and plan.get("base_integration_commit")==BASE, "P17 issue/base drift", errors)
    need(plan.get("specification_ids")==["GJ-V10-MP-GREENFIELD-2026-08-20","GJ-V10-DS-GREENFIELD-2026-08-20","GJ-V10-IA-GREENFIELD-2026-08-20"], "specification authority drift", errors)

    cap=plan.get("capability_contract",{})
    actual={r.get("id"):(r.get("owner"),tuple(r.get("gates",[]))) for r in cap.get("capabilities",[]) if isinstance(r,dict)}
    need(actual==EXPECTED_CAPABILITIES, f"capability ownership/gates drift: {actual}", errors)
    need(cap.get("master_predecessors")==["P06","P12","P13","P14","P16"], "predecessor list drift", errors)
    markers("\n".join(cap.get("master_required_tests",[])), ("permission denial","ticket-manager cannot approve","reason required","API-key","webhook","session/MFA","secret redaction"), "required tests", errors)

    pred=plan.get("predecessor_signed_authority",{})
    need(pred.get("node")=="P16" and pred.get("integration_commit")==BASE, "P16 predecessor integration drift", errors)
    need(pred.get("signed_source_commit")==P16_SIGNED_SOURCE, "P16 signed source drift", errors)
    need(pred.get("closure_run_id")==33010844881 and pred.get("artifact_id")==9630819391, "P16 closure authority drift", errors)
    need(pred.get("artifact_digest")=="sha256:00dbba2180f88ecdb6b369cb97abfdcafd211789088837d39e02a2d331a75722", "P16 closure digest drift", errors)
    need(pred.get("phase")=="signed" and pred.get("merge_authoritative") is True and pred.get("affected_matrix")=="55/55", "P16 predecessor not signed/authoritative", errors)

    permissions=plan.get("permission_contract",{})
    need(set(permissions.get("admin_permissions",[]))==EXPECTED_PERMISSIONS, "Admin permission catalog drift", errors)
    markers("\n".join(permissions.get("rules",[])), ("tickets.manage","domains.entitlements.manage","domains.risk.manage","security.manage","Frontend","Workspace"), "permission rules", errors)

    routes=plan.get("route_contract",{})
    need(set(routes.get("workspace_routes",[]))=={"APP-API-KEYS /app/api-keys","APP-WEBHOOKS /app/webhooks"}, "Workspace developer routes drift", errors)
    admin="\n".join(routes.get("admin_routes",[]))
    markers(admin, ("/admin/access/administrators","/admin/access/roles","/admin/domain-entitlements","/admin/operations/services","/admin/audit","/admin/platform/official-domains","/admin/platform/turnstile","/admin/trust/abuse"), "Admin routes", errors)
    markers("\n".join(routes.get("rules",[])), ("no-store","noindex","Page-Level IA","legacy","predecessor"), "route rules", errors)

    entitlement=plan.get("domain_entitlement_contract",{})
    need(entitlement.get("permission")=="domains.entitlements.manage", "entitlement permission drift", errors)
    need(entitlement.get("approve_required")==["domain_limit","starts_at","expires_at","reason","support_ticket_id"], "approve fields drift", errors)
    markers("\n".join(entitlement.get("rules",[])), ("expires_at","Ticket","P06/P13","P16"), "entitlement rules", errors)

    keys=plan.get("api_key_contract",{})
    need(keys.get("states")==["active","expired","revoked"], "API-key states drift", errors)
    markers("\n".join(keys.get("rules",[])), ("exactly once","Workspace","Rotation","rate","raw secret"), "API-key rules", errors)

    hooks=plan.get("webhook_contract",{})
    need(hooks.get("service_identity")=="SVC-OPS-MONITOR operationsmonitor outbound-webhook delivery contribution", "webhook service identity drift", errors)
    markers("\n".join(hooks.get("rules",[])), ("payment callbacks","Workspace","Signing","loopback","DNS","Retry","ninth"), "webhook rules", errors)

    ops=plan.get("operations_audit_contract",{})
    need(ops.get("service_ids")==["redirectengine","analyticsworker","analyticsreconciler","platformapi","mailworker","fileworker","operationsmonitor","logreceiver"], "eight-service inventory drift", errors)
    need(set(("actor","action","resource","result","request_id","reason")).issubset(set(ops.get("audit_fields",[]))), "audit field contract incomplete", errors)

    browser=plan.get("browser_contract",{})
    need(set(browser.get("states",{}))=={"admin_login","admin_access","admin_entitlements","workspace_api_keys","workspace_webhooks","admin_operations","admin_audit"}, "browser state-family drift", errors)
    env=plan.get("environment_contract",{})
    need(env.get("production_docker_compose_node")=="PROHIBITED", "production runtime boundary drift", errors)
    markers("\n".join(str(v) for v in env.values()), ("MySQL","Redis","platformapi","operationsmonitor","DNS","browser"), "environment contract", errors)

    closure=plan.get("closure",{})
    need(closure.get("same_exact_head_required") is True and closure.get("required_case_range")=="P17-T001..P17-T035", "closure contract drift", errors)
    need(closure.get("defect_limits")=={"p0":0,"p1":0,"decision_required":0}, "defect limits drift", errors)
    cases=plan.get("cases",[])
    expected_ids=[f"P17-T{i:03d}" for i in range(1,36)]
    need([c.get("id") for c in cases if isinstance(c,dict)]==expected_ids, "frozen case IDs/order drift", errors)
    for case in cases:
        for field in ("id","name","driver","oracle","evidence","owner"): need(bool(str(case.get(field," ")).strip()), f"{case.get('id')} missing {field}", errors)

    status_lines=re.findall(r"^Status: \*\*[^\n]+\*\*$", review, flags=re.MULTILINE)
    review_phase="invalid"
    if status_lines==[PENDING]:
        review_phase="pending"; need(blob("HEAD","artifacts/v10/P17/review.md")==PENDING_REVIEW_BLOB, "pending review blob drift", errors)
    elif status_lines==[SIGNED]:
        review_phase="signed"
        need(bool(re.search(r"Reviewed pre-sign implementation SHA: `[0-9a-f]{40}`",review)), "signed review missing reviewed SHA", errors)
        need("P0/P1/DECISION REQUIRED: `0/0/0`" in review, "signed review missing zero ledger", errors)
    else: need(False, f"invalid review status lines: {status_lines}", errors)

    if head==CONTRACT_AUTHORITY: mode="contract-freeze"; implementation_authorized=False
    elif review_phase=="pending": mode="implementation-guard"; implementation_authorized=True
    elif review_phase=="signed": mode="signed-review-guard"; implementation_authorized=False
    else: mode="invalid"; implementation_authorized=False
    result={"node":"P17","status":"PASS" if not errors else "FAIL","errors":errors,"implementation_commit":head,"base_integration_commit":BASE,"contract_authority":CONTRACT_AUTHORITY,"case_range":"P17-T001..P17-T035","review_phase":review_phase,"mode":mode,"implementation_authorized":implementation_authorized,"frozen_contract_preserved":not errors,"merge_authoritative":False,"predecessor_signed_source":P16_SIGNED_SOURCE,"predecessor_closure_run":33010844881,"predecessor_artifact":9630819391}
    print(json.dumps(result,indent=2,sort_keys=True))
    return 0 if not errors else 1

if __name__=="__main__": raise SystemExit(main())
