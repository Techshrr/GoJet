#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P12 Workspace/Members/Organization contract."""
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PLAN = ROOT / "artifacts/v10/P12/test-plan.json"
REVIEW = ROOT / "artifacts/v10/P12/review.md"
BASE = "638a6988c03eed6d287af0d2fdc63a3a3355ef68"
P11_SOURCE = "b59dfbe794f7d2f7bf63fdc79116217c5d893e87"
P11_RUN = 32649713397
P11_ARTIFACT = 9495896748
P11_DIGEST = "sha256:fe0edc8308cb4520929590efb261b87052423805ef02099066e818ff4cc5ae4f"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"
CASES = tuple(f"P12-T{n:03d}" for n in range(1, 26))
SPEC_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
}
CAPS = [
    {"id": "CAP-WORKSPACE", "status": "REQUIRED", "owner": "P12", "gates": ["G3", "G6", "G10"]},
    {"id": "CAP-FOLDERS-TAGS", "status": "REQUIRED", "owner": "P12", "gates": ["G3", "G6"]},
    {"id": "CAP-CAMPAIGNS", "status": "REQUIRED", "owner": "P07/P12", "gates": ["G3"]},
    {"id": "CAP-NOTIFICATIONS", "status": "REQUIRED", "owner": "P12/P13-P17", "gates": ["G3", "G5", "G6", "G10"]},
]
MASTER_TESTS = [
    "owner/admin/member/viewer", "cross-workspace", "expired invitation", "last-owner protection",
    "notification read/unread/mark-all-read", "dedupe", "deep-link authorization", "secret redaction", "API partial/offline",
]
COLUMNS = ["Backend", "DB/Migration", "API", "UI", "RBAC", "States", "Browser", "Security", "Observability", "Release"]
ROUTES = [
    "APP-OVERVIEW /app", "APP-NOTIFICATIONS /app/notifications", "APP-ORGANIZATION /app/organization",
    "APP-CAMPAIGNS /app/campaigns", "APP-TAGS /app/tags", "APP-MEMBERS /app/members",
    "AUTH-INVITE /invite/{token}", "APP-SETTINGS P12 subset /app/settings/workspace",
]
IA_APIS = [
    "GET /api/workspaces/{id}/overview", "GET /api/workspaces/{id}/notifications",
    "POST /api/invitations/accept", "POST /api/invitations/reject",
]


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT, text=True).strip()


def need(ok: bool, message: str, errors: list[str]) -> None:
    if not ok:
        errors.append(message)


def main() -> int:
    errors: list[str] = []
    try:
        plan = json.loads(PLAN.read_text(encoding="utf-8"))
    except Exception as exc:
        plan = {}
        errors.append(f"invalid/missing P12 plan: {exc}")
    review = REVIEW.read_text(encoding="utf-8") if REVIEW.is_file() else ""
    need(bool(review), "missing P12 review.md", errors)

    for path, expected in SPEC_BLOBS.items():
        target = ROOT / path
        need(target.is_file(), f"missing authority file {path}", errors)
        if target.is_file():
            actual = git("hash-object", path)
            need(actual == expected, f"authority blob drift {path}: {actual} != {expected}", errors)

    need(plan.get("node") == "P12", "node drift", errors)
    need(plan.get("title") == "Workspace, Members and Organization", "title drift", errors)
    need(plan.get("base_integration_commit") == BASE, "base integration drift", errors)
    need("P12 contract-freeze revision" in str(plan.get("case_ids_frozen_by", "")), "case authority missing", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20", "GJ-V10-DS-GREENFIELD-2026-08-20", "GJ-V10-IA-GREENFIELD-2026-08-20"
    ], "spec IDs drift", errors)

    cap = plan.get("capability_contract", {})
    need(cap.get("capabilities") == CAPS, "P12 capability/gate ownership drift", errors)
    need(cap.get("dependencies") == ["P03", "P04", "P07"], "P12 dependencies drift", errors)
    need(cap.get("master_required_tests") == MASTER_TESTS, "Master required tests drift", errors)
    need(cap.get("implementation_columns") == COLUMNS, "implementation columns drift", errors)
    for token in ("P07", "P15", "P13-P17"):
        need(token in str(cap.get("scope", "")), f"scope boundary missing {token}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P11", "integration_commit": BASE, "signed_source_commit": P11_SOURCE,
        "closure_run_id": P11_RUN, "artifact_id": P11_ARTIFACT, "artifact_digest": P11_DIGEST,
        "phase": "signed", "merge_authoritative": True,
    }, "P11 predecessor authority drift", errors)

    route = plan.get("route_contract", {})
    need(route.get("ia_routes") == ROUTES, "IA route set drift", errors)
    need(route.get("ia_exact_apis") == IA_APIS, "IA exact API set drift", errors)
    family = route.get("p12_api_family", [])
    for path in IA_APIS + [
        "GET /api/workspaces", "POST /api/workspaces", "GET /api/workspaces/{id}/members",
        "POST /api/workspaces/{id}/invitations", "GET /api/invitations/{token}",
        "PATCH /api/workspaces/{id}/links/organization", "POST /api/workspaces/{id}/notifications/read-all",
    ]:
        need(path in family, f"P12 API family missing {path}", errors)
    rules = "\n".join(route.get("rules", []))
    for token in ("not IA-exact", "No /app/folders route", "P15 owns", "No user-facing arbitrary-notification", "server tenant/RBAC", "noindex", "no-store"):
        need(token in rules, f"route rule missing {token}", errors)

    ident = plan.get("identity_rbac_contract", {})
    need(ident.get("roles") == ["owner", "admin", "member", "viewer"], "role set drift", errors)
    need("does not claim P15" in str(ident.get("identity", "")), "P15 identity boundary missing", errors)
    need("re-resolves Workspace membership/role from MySQL" in str(ident.get("membership", "")), "MySQL membership authority missing", errors)
    need("never P12 authorization" in str(ident.get("membership", "")), "client role non-authority missing", errors)
    irules = "\n".join(ident.get("rules", []))
    for token in ("last active owner", "never owner", "member reads", "viewer read-only", "owner promotion", "must not disclose", "invitation inspection"):
        need(token in irules, f"RBAC rule missing {token}", errors)

    resources = plan.get("resource_contract", {})
    for key in ("workspace", "membership", "invitation", "organization", "campaign", "tag", "folder", "link_organization", "notification", "notification_state", "audit"):
        need(bool(resources.get(key)), f"resource fields missing {key}", errors)
    need(resources.get("statuses", {}).get("invitation") == ["pending", "accepted", "rejected", "revoked", "expired"], "invitation status drift", errors)
    need(resources.get("statuses", {}).get("notification_state") == ["complete", "partial", "stale"], "notification state drift", errors)
    rrules = "\n".join(resources.get("rules", []))
    for token in ("409", "normalized uniqueness", "same P07 analytics campaign_id", "explicit Link IDs", "cryptographic hash", "authenticated invitation inspection"):
        need(token in rrules, f"resource rule missing {token}", errors)

    notifications = plan.get("notification_contract", {})
    need(notifications.get("categories") == ["security", "domains", "billing", "support", "resources"], "notification category drift", errors)
    nrules = "\n".join(notifications.get("rules", []))
    for token in ("internal producer only", "dedupe_key", "recipient scoped", "reauthorized", "/app/notifications", "secret/PII", "may be optimistic", "complete/partial/stale"):
        need(token in nrules, f"notification rule missing {token}", errors)

    browser = plan.get("browser_contract", {})
    states = browser.get("states", {})
    need(states.get("overview") == ["loading", "empty-new-workspace", "partial-analytics", "attention", "API-error"], "overview states drift", errors)
    need(states.get("notifications") == ["loading", "empty", "unread", "filtered", "partial", "stale", "error"], "notification states drift", errors)
    need(states.get("members") == ["loading", "empty-no-invites", "invite", "read-only", "last-owner-protected", "invitation-expired", "error"], "members states drift", errors)
    need(states.get("invite") == ["unauthenticated", "valid", "account-mismatch", "expired", "revoked", "accepted", "rejected"], "invite states drift", errors)
    for token in ("Esc", "focus return", "320px", "reduced motion"):
        need(token in str(browser.get("shell", "")), f"browser shell rule missing {token}", errors)

    env = plan.get("environment_contract", {})
    need(env.get("mysql") == "Real MySQL 8.x", "real MySQL requirement drift", errors)
    need(env.get("platformapi") == "Real native Go platformapi", "native platformapi requirement drift", errors)
    need("MySQL membership/role" in str(env.get("identity", "")), "environment membership authority missing", errors)
    need("P07" in str(env.get("p07", "")) and "P05" in str(env.get("p05", "")), "P05/P07 integration boundary missing", errors)
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production runtime boundary drift", errors)

    items = plan.get("cases", [])
    ids = [item.get("id") for item in items if isinstance(item, dict)]
    need(tuple(ids) == CASES, f"case IDs/order drift {ids}", errors)
    by_id = {item.get("id"): item for item in items if isinstance(item, dict)}
    for cid in CASES:
        item = by_id.get(cid, {})
        need(item.get("owner") == "P12", f"{cid} owner drift", errors)
        need(item.get("expected_exit") == 0, f"{cid} exit drift", errors)
        for field in ("name", "precondition", "driver", "oracle", "evidence"):
            need(bool(item.get(field)), f"{cid} missing {field}", errors)
        need(str(item.get("evidence", "")).startswith("artifacts/v10/P12/"), f"{cid} evidence root drift", errors)
    for n in range(1, 19):
        need(str(by_id.get(f"P12-T{n:03d}", {}).get("driver", "")).startswith("python3 scripts/p12/integration.py"), f"T{n:03d} driver drift", errors)
    for n in range(19, 24):
        need(str(by_id.get(f"P12-T{n:03d}", {}).get("driver", "")).startswith("node scripts/p12/browser.mjs"), f"T{n:03d} driver drift", errors)
    need(by_id.get("P12-T024", {}).get("driver") == "python3 scripts/p12/validate.py --case P12-T024", "T024 driver drift", errors)
    need(by_id.get("P12-T025", {}).get("driver") == "python3 scripts/p12/validate.py --case P12-T025 --closure", "T025 driver drift", errors)
    need("inspection" in str(by_id.get("P12-T007", {}).get("oracle", "")), "T007 invitation-inspection oracle missing", errors)
    need("safe inspection" in str(by_id.get("P12-T020", {}).get("oracle", "")), "T020 browser inspection oracle missing", errors)

    closure = plan.get("closure_contract", {})
    need(closure.get("version") == 1 and closure.get("same_exact_head_required") is True, "closure version/exact-head drift", errors)
    need(closure.get("required_case_range") == "P12-T001..P12-T025", "closure case range drift", errors)
    need(closure.get("review_required") is True, "review requirement drift", errors)
    need((closure.get("p0_max"), closure.get("p1_max"), closure.get("decision_required_max")) == (0, 0, 0), "defect thresholds drift", errors)
    need(closure.get("pre_sign_phase") == "pre-sign / merge_authoritative=false", "pre-sign phase drift", errors)
    need(closure.get("signed_phase") == "signed / merge_authoritative=true", "signed phase drift", errors)
    need(P11_SOURCE in str(closure.get("predecessor_rule", "")) and "do not rerun/reinterpret P11" in str(closure.get("predecessor_rule", "")), "predecessor inheritance rule missing", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.strip().startswith("Status:")]
    pending = status_lines == [PENDING]
    signed = status_lines == [SIGNED]
    need(pending ^ signed, f"illegal review status {status_lines}", errors)
    for marker in (
        "Required P12 case range: **P12-T001..P12-T025**.",
        "no `/app/folders` route", "does **not** implement or claim P15",
        "GET /api/invitations/{token}", "SAME-REVISION CI REQUIRED",
    ):
        need(marker in review, f"review marker missing {marker}", errors)
    if pending:
        need("No P12 PASS or Exit claim is made in this state." in review, "pending no-PASS marker missing", errors)
        need("Accountable reviewer identity:" not in review, "pending review contains signature", errors)
        need(re.search(r"(?mi)^\s*(?:[-*]\s*)?P12-T\d{3}\s*[:=-]\s*PASS\b", review) is None, "pending review contains case PASS", errors)
    if signed:
        need(re.search(r"Pre-sign exact implementation SHA:\s*`[0-9a-f]{40}`", review) is not None, "signed pre-sign SHA missing", errors)
        need("Accountable reviewer identity: **GPT-5.6 Sol — P12 Technical Review**" in review, "signed identity drift", errors)
        need(re.search(r"Review date:\s*\*\*\d{4}-\d{2}-\d{2}\*\*", review) is not None, "signed review date missing", errors)
        for marker in ("- P0 defects: 0", "- P1 defects: 0", "- `DECISION REQUIRED`: 0"):
            need(marker in review, f"signed defect marker missing {marker}", errors)
        for role in ("Backend Lead", "Frontend Lead", "QA Lead", "Accessibility Reviewer", "Security Reviewer", "Product/API Reviewer"):
            need(f"- {role}: APPROVED" in review, f"signed role approval missing {role}", errors)
        need("P12-T025" in review and "PASS" in review, "signed final closure record missing", errors)
        need("signed revision itself must rerun" in review.lower(), "signed rerun rule missing", errors)

    head = git("rev-parse", "HEAD")
    need(bool(re.fullmatch(r"[0-9a-f]{40}", head)), f"invalid HEAD {head}", errors)
    try:
        need(git("merge-base", head, BASE) == BASE, "P12 branch base/ancestry drift", errors)
    except Exception as exc:
        errors.append(f"cannot verify ancestry: {exc}")

    result = {
        "node": "P12", "contract": "Workspace, Members and Organization",
        "status": "PASS" if not errors else "FAIL", "implementation_commit": head,
        "base_integration_commit": BASE, "case_range": "P12-T001..P12-T025", "case_count": len(items),
        "review_status": "SIGNED" if signed else "PENDING" if pending else "INVALID",
        "predecessor_signed_source": P11_SOURCE, "errors": errors,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
