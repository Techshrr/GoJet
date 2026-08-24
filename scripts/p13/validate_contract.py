#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "7f39da389052b08f145e69dac2a715b9d303294d"
P12_SOURCE = "9d49d5ebf0e697ae9cd6537c432c27a15edc60bd"
P12_RUN = 32663159008
P12_ARTIFACT = 9499336765
P12_DIGEST = "sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a"
P06_SOURCE = "4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23"
P06_INTEGRATION = "3aa80b566d144963130b8f61fa63a4ee677ebc99"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

FROZEN_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
}
EXPECTED_CAPABILITIES = {
    ("CAP-BILLING", "REQUIRED", "P13", ("G3", "G10")),
    ("CAP-PAYMENTS", "REQUIRED", "P13", ("G3", "G6", "G10")),
    ("CAP-PAYMENT-CALLBACKS", "REQUIRED", "P13", ("G3", "G6", "G10")),
    ("CAP-DOMAIN-ENTITLEMENT", "REQUIRED", "P06/P13", ("G3", "G6")),
}
EXPECTED_ROUTES = {
    "APP-BILLING /app/billing",
    "WEB-PRICING /pricing",
    "ADMIN-PLANS /admin/commerce/plans",
    "ADMIN-PAYMENTS /admin/commerce/payments[/{paymentId}]",
    "ADMIN-FX /admin/commerce/fx",
}
EXPECTED_PROVIDERS = ["alipay", "wechat", "epay", "paypal", "stripe", "crypto"]
EXPECTED_STATES = {
    "app_billing": ["loading", "active", "payment-pending", "payment-failed", "overdue", "canceled", "provider-partial", "error"],
    "admin_plans": ["loading", "empty", "draft", "active", "archived", "validation-error", "conflict"],
    "admin_payments": ["loading", "empty", "pending", "paid", "failed", "refunded", "callback-invalid", "partial"],
    "admin_fx": ["loading", "current", "stale", "provider-error", "override-confirm", "validation-error"],
}


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P13/test-plan.json")
    review_path = Path("artifacts/v10/P13/review.md")
    need(plan_path.is_file(), "missing P13 test-plan.json", errors)
    need(review_path.is_file(), "missing P13 review.md", errors)
    if errors:
        print(json.dumps({"node": "P13", "status": "FAIL", "errors": errors}, indent=2))
        return 1

    try:
        plan = json.loads(plan_path.read_text(encoding="utf-8"))
    except Exception as exc:
        print(json.dumps({"node": "P13", "status": "FAIL", "errors": [f"test-plan parse failed: {exc}"]}, indent=2))
        return 1
    review = review_path.read_text(encoding="utf-8")

    for path, expected in FROZEN_BLOBS.items():
        try:
            actual = git("rev-parse", f"HEAD:{path}")
            need(actual == expected, f"frozen authority blob drift {path}: {actual}", errors)
        except Exception as exc:
            errors.append(f"cannot bind frozen authority {path}: {exc}")

    need(plan.get("node") == "P13", "node must be P13", errors)
    need(plan.get("title") == "Billing, Payments and Entitlements", "title drift", errors)
    need(plan.get("base_integration_commit") == BASE, "base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "specification IDs drift", errors)

    cap = plan.get("capability_contract", {})
    actual_caps = {
        (item.get("id"), item.get("status"), item.get("owner"), tuple(item.get("gates", [])))
        for item in cap.get("capabilities", [])
    }
    need(actual_caps == EXPECTED_CAPABILITIES, f"capability contract drift: {actual_caps}", errors)
    need(cap.get("dependencies") == ["P06", "P12"], "P13 dependencies must be P06/P12", errors)
    need(cap.get("master_required_tests") == ["paid", "failed", "refund", "duplicate callback", "currency", "upgrade", "downgrade expiry"], "Master Plan required-test list drift", errors)
    for marker in ("features JSON", "P06", "P12", "P17", "P19"):
        need(marker in str(cap.get("scope", "")), f"capability scope missing {marker}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P12",
        "integration_commit": BASE,
        "signed_source_commit": P12_SOURCE,
        "closure_run_id": P12_RUN,
        "artifact_id": P12_ARTIFACT,
        "artifact_digest": P12_DIGEST,
        "phase": "signed",
        "merge_authoritative": True,
    }, "P12 predecessor authority drift", errors)

    inherited = plan.get("inherited_functional_authority", {})
    need(inherited.get("node") == "P06", "P06 inherited authority missing", errors)
    need(inherited.get("integration_commit") == P06_INTEGRATION, "P06 integration drift", errors)
    need(inherited.get("signed_source_commit") == P06_SOURCE, "P06 source drift", errors)
    need(inherited.get("closure_run_id") == 32519298309 and inherited.get("artifact_id") == 9460016077, "P06 closure binding drift", errors)
    need(inherited.get("artifact_digest") == "sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8", "P06 artifact digest drift", errors)
    for marker in ("ownership", "DNS", "HTTPS", "risk", "not duplicate"):
        need(marker.lower() in str(inherited.get("scope", "")).lower(), f"P06 inherited scope missing {marker}", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("ia_routes", [])) == EXPECTED_ROUTES, "IA route set drift", errors)
    need(routes.get("ia_exact_apis") == ["GET /api/public/plans"], "IA exact API drift", errors)
    impl_apis = routes.get("p13_implementation_api_authority", [])
    need("POST /api/payments/callbacks/{provider} — P13 implementation authority, not IA-exact" in impl_apis, "callback implementation route missing", errors)
    rules = "\n".join(routes.get("rules", []))
    for marker in (
        "Only GET /api/public/plans is IA-exact",
        "never generic APP-WEBHOOKS",
        "No invented /app/billing/plans",
        "P19",
        "billing.manage",
        "P15/P17",
    ):
        need(marker in rules, f"route rule missing {marker}", errors)

    providers = plan.get("provider_contract", {})
    need(providers.get("providers") == EXPECTED_PROVIDERS, "provider inventory/order drift", errors)
    need(providers.get("callback_route") == "POST /api/payments/callbacks/{provider}", "callback route drift", errors)
    provider_rules = "\n".join(providers.get("rules", []))
    for marker in ("Unknown providers fail closed", "signature/credential", "idempotent", "Raw callback body", "no live production credentials", "never payment settlement authority"):
        need(marker.lower() in provider_rules.lower(), f"provider rule missing {marker}", errors)

    ent = plan.get("entitlement_contract", {})
    need("features JSON" in ent.get("authoritative_resolver", ""), "features JSON non-authority rule missing", errors)
    need(ent.get("source_order") == [
        "hard security/workspace/admin suspension or explicit durable revoke denies",
        "active durable manual/inherited grant with provenance",
        "active billing-plan grant with provenance and term/grace boundaries",
        "baseline/free default",
    ], "entitlement source precedence drift", errors)
    need("non-additive maximum" in ent.get("combination", ""), "non-additive grant rule missing", errors)
    need("BOTH" in ent.get("domain_rule", "") and "P06" in ent.get("domain_rule", ""), "P06 conjunctive domain rule missing", errors)
    need("preserve existing resources" in ent.get("downgrade_rule", ""), "non-destructive downgrade rule missing", errors)

    resource = plan.get("resource_contract", {})
    required_resources = {"plan", "plan_entitlement", "workspace_subscription", "entitlement_grant", "order", "invoice", "transaction", "callback_event", "fx_rate", "audit"}
    need(required_resources.issubset(resource.keys()), "resource contract incomplete", errors)
    resource_rules = "\n".join(resource.get("rules", []))
    need("integer minor units" in resource_rules and "never floating-point money" in resource_rules, "integer money rule missing", errors)
    need("one-way hash" in resource_rules, "idempotency secret-hash rule missing", errors)

    rbac = plan.get("rbac_contract", {})
    workspace_rules = "\n".join(rbac.get("workspace", []))
    for marker in ("owner:", "admin:", "member/viewer:"):
        need(marker in workspace_rules, f"workspace RBAC marker missing {marker}", errors)
    need("billing.manage" in rbac.get("admin", ""), "admin billing.manage authority missing", errors)
    rbac_rules = "\n".join(rbac.get("rules", []))
    for marker in ("P12 membership", "Cross-workspace", "Client/test role headers"):
        need(marker.lower() in rbac_rules.lower(), f"RBAC rule missing {marker}", errors)

    browser = plan.get("browser_contract", {})
    need(browser.get("states") == EXPECTED_STATES, "browser states drift from IA contract", errors)
    browser_rules = "\n".join(browser.get("rules", []))
    for marker in ("320 CSS px", "reduced motion", "no color-only", "noindex/no-store", "P19"):
        need(marker.lower() in browser_rules.lower(), f"browser rule missing {marker}", errors)

    notif = plan.get("notification_contract", {})
    need(notif.get("owner") == "P13 producer events only; P12 owns notification core.", "notification ownership drift", errors)
    need(notif.get("category") == "billing", "notification category drift", errors)
    need(set(notif.get("events", [])) == {"payment_succeeded", "payment_failed", "refund_processed", "plan_upgraded", "downgrade_scheduled", "entitlement_expiring"}, "billing notification event set drift", errors)

    env = plan.get("environment_contract", {})
    need(env.get("mysql") == "Real MySQL 8.x for billing/order/transaction/entitlement evidence.", "real MySQL requirement drift", errors)
    need(env.get("platformapi") == "Real native Go platformapi.", "native platformapi requirement drift", errors)
    need("no live production payment credentials or real charge" in env.get("providers", ""), "CI provider safety rule missing", errors)
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Compose/Node must stay prohibited", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "same exact head closure missing", errors)
    need(closure.get("required_case_range") == "P13-T001..P13-T027", "closure case range drift", errors)
    need(closure.get("review_required") is True, "accountable review requirement missing", errors)
    need(closure.get("defect_limits") == {"p0": 0, "p1": 0, "decision_required": 0}, "defect limits drift", errors)
    need(closure.get("phases") == {
        "pre-sign": {"review_status": "PENDING", "merge_authoritative": False},
        "signed": {"review_status": "SIGNED", "merge_authoritative": True},
    }, "closure phase contract drift", errors)
    predecessor_rule = str(closure.get("predecessor_rule", ""))
    for marker in (P12_SOURCE, BASE, str(P12_RUN), str(P12_ARTIFACT), P12_DIGEST, "do not rerun/reinterpret P12", "P06"):
        need(marker in predecessor_rule, f"closure predecessor rule missing {marker}", errors)
    for marker in ("P06", "P12", "P17", "P19"):
        need(marker in str(closure.get("scope_rule", "")), f"closure scope missing {marker}", errors)

    items = plan.get("cases", [])
    expected_ids = [f"P13-T{i:03d}" for i in range(1, 28)]
    ids = [item.get("id") for item in items]
    need(ids == expected_ids, f"case range/order drift: {ids}", errors)
    need(len(items) == 27, "P13 case count must be 27", errors)
    for i, item in enumerate(items, 1):
        cid = f"P13-T{i:03d}"
        need(item.get("owner") == "P13", f"{cid} owner drift", errors)
        need(item.get("expected_exit") == 0, f"{cid} expected_exit drift", errors)
        need(str(item.get("driver", "")).endswith(cid) or (i == 27 and cid in str(item.get("driver", ""))), f"{cid} driver drift", errors)
        need(str(item.get("evidence", "")).endswith(f"{cid}.json"), f"{cid} evidence path drift", errors)
        need(bool(str(item.get("oracle", "")).strip()), f"{cid} oracle missing", errors)
    need(items[25].get("name") == "Exact-head evidence coherence", "T026 must be coherence", errors)
    need(items[26].get("name") == "Signed accountable closure", "T027 must be signed closure", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.strip().startswith("Status:")]
    pending = status_lines == [PENDING]
    signed = status_lines == [SIGNED]
    need(pending ^ signed, f"illegal review status {status_lines}", errors)
    for marker in (
        "Required P13 case range: **P13-T001..P13-T027**.",
        "features` JSON",
        "GET /api/public/plans",
        "POST /api/payments/callbacks/{provider}",
        "There are no invented `/app/billing/plans`",
        "Payment cannot bypass P06 safety.",
        "P12 remains owner of notification",
        "SAME-REVISION CI REQUIRED",
    ):
        need(marker in review, f"review marker missing {marker}", errors)

    if pending:
        need("No P13 PASS or Exit claim is made in this state." in review, "pending no-PASS marker missing", errors)
        need("Accountable reviewer identity: **GPT-5.6 Sol — P13 Technical Review**" in review, "signed-format identity template missing", errors)
        need(re.search(r"(?mi)^\s*(?:[-*]\s*)?P13-T\d{3}\s*[:=-]\s*PASS\b", review) is None, "pending review contains case PASS", errors)
    if signed:
        need(re.search(r"Pre-sign exact implementation SHA:\s*`[0-9a-f]{40}`", review) is not None, "signed pre-sign SHA missing", errors)
        need("Accountable reviewer identity: **GPT-5.6 Sol — P13 Technical Review**" in review, "signed identity drift", errors)
        need(re.search(r"Review date:\s*\*\*\d{4}-\d{2}-\d{2}\*\*", review) is not None, "signed review date missing", errors)
        for marker in ("- P0 defects: 0", "- P1 defects: 0", "- `DECISION REQUIRED`: 0"):
            need(marker in review, f"signed defect marker missing {marker}", errors)
        need("P13-T027" in review and "PASS" in review, "signed final closure record missing", errors)
        need("signed revision itself must rerun" in review.lower(), "signed rerun rule missing", errors)

    head = git("rev-parse", "HEAD")
    need(bool(re.fullmatch(r"[0-9a-f]{40}", head)), f"invalid HEAD {head}", errors)
    try:
        need(git("merge-base", head, BASE) == BASE, "P13 branch base/ancestry drift", errors)
    except Exception as exc:
        errors.append(f"cannot verify ancestry: {exc}")

    result = {
        "node": "P13",
        "contract": "Billing, Payments and Entitlements",
        "status": "PASS" if not errors else "FAIL",
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "case_range": "P13-T001..P13-T027",
        "case_count": len(items),
        "review_status": "SIGNED" if signed else "PENDING" if pending else "INVALID",
        "predecessor_signed_source": P12_SOURCE,
        "inherited_p06_signed_source": P06_SOURCE,
        "provider_count": len(providers.get("providers", [])),
        "errors": errors,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
