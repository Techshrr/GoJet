#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "dd70eacf02d4dd79fe82063f3d43610ab11885e8"
P15_SIGNED_SOURCE = "6f39d87f1d94f71590fd79d4551cdd1cea652a76"
P15_REVIEW_BLOB = "676292cb454b42a0f4a30c1fef8089c901b96c51"
PENDING_REVIEW_BLOB = "a3a8091cdfa563d135d75d15bb3f3af693ecf20b"

SPEC_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "docs/security/SECURITY_INVARIANTS.md": "5d3178ee80bf46b4f00df729ab24d783a7af75dc",
}
SEAM_BLOBS = {
    "internal/links/fingerprint_test.go": "5cc3f2aec99fe44e826356c2b3b4cbc39d784f89",
    "internal/links/risk_redis.go": "2cecf2285262a3a161333bec3860eb9d02c87a56",
    "internal/links/redirect_http.go": "217201f11b5a7902272f155c24de61e04a196e46",
    "internal/domains/domain.go": "822dab7236331807dbcbc8072a41b09477bfff9c",
}
EXPECTED_CONTRACT_FILES = {
    "artifacts/v10/P16/test-plan.json",
    "artifacts/v10/P16/review.md",
    "scripts/p16/validate_contract.py",
    ".github/workflows/p16-trust-destination-risk-abuse.yml",
}
EXPECTED_CAPABILITIES = {
    "CAP-DESTINATION-RISK": ("P05/P16", ("G6", "G10", "G13")),
    "CAP-DOMAIN-RISK": ("P06/P16/P17", ("G6", "G13")),
    "CAP-ABUSE": ("P16/P17", ("G6", "G10")),
    "CAP-LINK-ROUTING": ("P05/P16", ("G3", "G6")),
    "CAP-LINK-AB": ("P05/P16", ("G3", "G6")),
    "CAP-NOTIFICATIONS": ("P12/P13-P17", ("G3", "G5", "G6", "G10")),
}
EXPECTED_PUBLIC_ROUTES = {
    "PUB-SHORT-OFFICIAL https://{official-short-host}/{code}",
    "PUB-SHORT-CUSTOM https://{custom-host}/{code}",
    "PUB-LINK-UNAVAILABLE /linkunavailable?reason={allowlisted}&code={safe-code}",
    "PUB-ABUSE-REPORT /abuse/report",
}
EXPECTED_ADMIN_ROUTES = {
    "ADMIN-DEST-RISK /admin/trust/destination-risk[/{riskId}]",
    "ADMIN-DOMAIN-RISK /admin/trust/domain-risk[/{domainId}]",
    "ADMIN-ABUSE /admin/trust/abuse[/{reportId}]",
}
EXPECTED_IA_APIS = [
    "POST /api/public/abuse-reports",
    "GET /api/admin/domain-risks",
    "GET /api/admin/domain-risks/{domainId}",
    "POST /api/admin/domain-risks/{domainId}/revalidate",
]
EXPECTED_IMPL_APIS = [
    "GET /api/admin/destination-risks",
    "GET /api/admin/destination-risks/{riskId}",
    "POST /api/admin/destination-risks/{riskId}/rescan",
    "POST /api/admin/destination-risks/{riskId}/override",
    "GET /api/admin/abuse",
    "GET /api/admin/abuse/{reportId}",
    "POST /api/admin/abuse/{reportId}/actions",
]
PENDING = "Status: **PENDING — CONTRACT DRAFTING / IMPLEMENTATION NOT AUTHORIZED**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def blob_at_head(path: str) -> str:
    return git("rev-parse", f"HEAD:{path}")


def main() -> int:
    errors: list[str] = []
    root = Path("artifacts/v10/P16")
    plan_path = root / "test-plan.json"
    review_path = root / "review.md"
    need(plan_path.is_file(), "missing P16 test-plan.json", errors)
    need(review_path.is_file(), "missing P16 review.md", errors)
    if errors:
        print(json.dumps({"node":"P16","status":"FAIL","errors":errors}, indent=2))
        return 1

    plan = json.loads(plan_path.read_text(encoding="utf-8"))
    review = review_path.read_text(encoding="utf-8")
    head = git("rev-parse", "HEAD")

    base_ok = subprocess.run(["git", "merge-base", "--is-ancestor", BASE, head], check=False).returncode == 0
    need(base_ok, f"P16 HEAD must descend from integration base {BASE}", errors)
    changed = {line for line in git("diff", "--name-only", f"{BASE}..HEAD").splitlines() if line}
    contract_only = changed == EXPECTED_CONTRACT_FILES
    need(contract_only, f"P16 contract-freeze diff must be exactly {sorted(EXPECTED_CONTRACT_FILES)}, got {sorted(changed)}", errors)

    for path, expected in SPEC_BLOBS.items():
        try:
            need(blob_at_head(path) == expected, f"normative authority blob drift: {path}", errors)
        except Exception as exc:
            errors.append(f"cannot bind normative authority {path}: {exc}")
    for path, expected in SEAM_BLOBS.items():
        try:
            need(blob_at_head(path) == expected, f"inherited seam blob drift: {path}", errors)
        except Exception as exc:
            errors.append(f"cannot bind inherited seam {path}: {exc}")
    try:
        need(git("rev-parse", "HEAD:artifacts/v10/P15/review.md") == P15_REVIEW_BLOB, "P15 signed review blob drift", errors)
    except Exception as exc:
        errors.append(f"cannot bind P15 signed review: {exc}")

    need(plan.get("node") == "P16", "node must be P16", errors)
    need(plan.get("title") == "Trust, Destination Risk and Abuse", "P16 title drift", errors)
    need(plan.get("base_integration_commit") == BASE, "P16 base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "P16 specification IDs/order drift", errors)

    cap = plan.get("capability_contract", {})
    rows = cap.get("capabilities", [])
    actual_caps = {row.get("id"): (row.get("owner"), tuple(row.get("gates", []))) for row in rows if isinstance(row, dict)}
    need(actual_caps == EXPECTED_CAPABILITIES, f"P16 capability ownership/gates drift: {actual_caps}", errors)
    need(cap.get("master_predecessors") == ["P05","P06","P09","P15"], "P16 predecessor list drift", errors)
    required_tests = set(cap.get("master_required_tests", []))
    need(required_tests == {"official/custom parity","all target variants","SSRF","provider failure","manual override invalidation","abuse suspension"}, "P16 Master Plan required-test set drift", errors)
    scope = str(cap.get("scope", ""))
    for marker in ("P05", "P06", "P09", "P12", "P15", "P17", "G10", "G13"):
        need(marker in scope or marker in "\n".join(cap.get("inherited_authorities", [])), f"P16 ownership boundary missing {marker}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred.get("node") == "P15", "P15 predecessor node drift", errors)
    need(pred.get("integration_commit") == BASE, "P15 integration authority drift", errors)
    need(pred.get("signed_source_commit") == P15_SIGNED_SOURCE, "P15 signed source drift", errors)
    need(pred.get("closure_run_id") == 32931945354, "P15 closure run drift", errors)
    need(pred.get("artifact_id") == 9593689993, "P15 closure artifact drift", errors)
    need(pred.get("artifact_digest") == "sha256:5a43c87ea26f86081523d371de260e100a20c5c05b3581f48223fb70e68cd233", "P15 closure digest drift", errors)
    need(pred.get("phase") == "signed" and pred.get("merge_authoritative") is True, "P15 predecessor must remain signed/merge-authoritative", errors)

    seams = plan.get("inherited_seam_authority", {})
    for key, expected in {
        "p05_fingerprint_blob": SEAM_BLOBS["internal/links/fingerprint_test.go"],
        "p05_risk_redis_blob": SEAM_BLOBS["internal/links/risk_redis.go"],
        "p05_redirect_http_blob": SEAM_BLOBS["internal/links/redirect_http.go"],
        "p06_domain_model_blob": SEAM_BLOBS["internal/domains/domain.go"],
    }.items():
        need(seams.get(key) == expected, f"P16 seam manifest drift: {key}", errors)
    seam_scope = str(seams.get("scope", ""))
    for marker in ("fingerprint", "risk-before-routing", "domain-axis"):
        need(marker.lower() in seam_scope.lower(), f"P16 seam scope missing {marker}", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("public_routes", [])) == EXPECTED_PUBLIC_ROUTES, "P16 public route set drift", errors)
    need(set(routes.get("admin_routes", [])) == EXPECTED_ADMIN_ROUTES, "P16 Admin route set drift", errors)
    need(routes.get("ia_exact_apis") == EXPECTED_IA_APIS, "P16 IA-exact API list/order drift", errors)
    need(routes.get("p16_implementation_api_authority") == EXPECTED_IMPL_APIS, "P16 implementation API list/order drift", errors)
    route_rules = "\n".join(routes.get("rules", []))
    for marker in ("noindex", "no-store", "security.manage", "domains.risk.manage", "P17", "P19"):
        need(marker.lower() in route_rules.lower(), f"P16 route rule missing {marker}", errors)

    risk = plan.get("destination_risk_contract", {})
    need(risk.get("durable_states") == ["pending","allow","review","block","unknown"], "P16 durable risk states drift", errors)
    need(risk.get("runtime_non_allow") == ["missing","unknown","malformed","stale","pending","review","block","provider-unavailable"], "P16 runtime non-allow set/order drift", errors)
    risk_rules = "\n".join(risk.get("rules", []))
    for marker in ("primary", "routing", "A/B", "allow", "fingerprint", "Official", "custom", "Redis", "signals"):
        need(marker.lower() in risk_rules.lower(), f"P16 destination-risk rule missing {marker}", errors)

    ssrf = plan.get("ssrf_contract", {})
    need(ssrf.get("allowed_schemes") == ["http","https"], "P16 SSRF allowed schemes drift", errors)
    need(set(ssrf.get("deny_classes", [])) == {"loopback","private","link-local","unspecified","multicast","reserved","metadata-service","userinfo-authority"}, "P16 SSRF deny-class drift", errors)
    ssrf_rules = "\n".join(ssrf.get("rules", []))
    for marker in ("server-side", "Redirect", "DNS rebinding", "reviewed server configuration"):
        need(marker.lower() in ssrf_rules.lower(), f"P16 SSRF rule missing {marker}", errors)

    domain = plan.get("domain_risk_contract", {})
    need(domain.get("states") == ["missing","pending","allow","review","block","malformed","stale","provider-partial","revalidating"], "P16 domain-risk states drift", errors)
    domain_rules = "\n".join(domain.get("rules", []))
    for marker in ("entitlement", "ownership", "ingress DNS", "HTTPS", "P06", "grace", "provider"):
        need(marker.lower() in domain_rules.lower(), f"P16 domain-risk rule missing {marker}", errors)

    abuse = plan.get("abuse_contract", {})
    need(abuse.get("states") == ["open","investigating","resolved","dismissed"], "P16 abuse states drift", errors)
    need(abuse.get("p16_action_scope") == ["destination-fingerprint","short-link-risk","custom-domain-risk"], "P16 abuse action scope drift", errors)
    abuse_rules = "\n".join(abuse.get("rules", []))
    for marker in ("Turnstile", "idempotency", "P17", "permission", "actor", "reason", "correlation", "Restore", "PII"):
        need(marker.lower() in abuse_rules.lower(), f"P16 abuse rule missing {marker}", errors)

    override = plan.get("override_contract", {})
    need(override.get("permission") == "security.manage", "P16 override permission drift", errors)
    need(override.get("required_fields") == ["exact_fingerprint","decision","reason","expires_at","policy_context"], "P16 override fields drift", errors)
    override_rules = "\n".join(override.get("rules", []))
    for marker in ("fingerprint", "expiry", "policy", "P17", "domain"):
        need(marker.lower() in override_rules.lower(), f"P16 override rule missing {marker}", errors)

    worker = plan.get("worker_contract", {})
    need(worker.get("service_identity") == "SVC-OPS-MONITOR operationsmonitor risk-task contribution", "P16 worker service identity drift", errors)
    need(worker.get("target_source_path") == "services/platformapi/cmd/operationsmonitor", "P16 worker target path drift", errors)
    worker_rules = "\n".join(worker.get("rules", []))
    need("ninth" in worker_rules.lower() and "idempotent" in worker_rules.lower() and "implicit allow" in worker_rules.lower(), "P16 fixed service/worker safety contract incomplete", errors)

    browser = plan.get("browser_contract", {})
    expected_state_keys = {"admin_destination_risk","admin_domain_risk","admin_abuse","public_link_unavailable","public_abuse_report"}
    need(set(browser.get("states", {}).keys()) == expected_state_keys, "P16 browser state-family set drift", errors)
    browser_rules = "\n".join(browser.get("rules", []))
    for marker in ("Design System", "320", "reason", "audit", "target URL", "provider", "permission"):
        need(marker.lower() in browser_rules.lower(), f"P16 browser rule missing {marker}", errors)

    notifications = plan.get("notification_contract", {})
    need(notifications.get("owner") == "P12 CAP-NOTIFICATIONS core", "P12 notification ownership drift", errors)
    need("producer" in str(notifications.get("p16_contribution", "")).lower(), "P16 notification contribution drift", errors)

    env = plan.get("environment_contract", {})
    for key in ("mysql","redis","platformapi","redirectengine","risk_worker","semantic_provider","ssrf","turnstile","browser","production_docker_compose_node"):
        need(bool(str(env.get(key, "")).strip()), f"P16 environment contract missing {key}", errors)
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Compose/Node boundary drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "same exact head closure must be required", errors)
    need(closure.get("required_case_range") == "P16-T001..P16-T029", "P16 case range drift", errors)
    need(closure.get("review_required") is True, "P16 accountable review must be required", errors)
    need(closure.get("defect_limits") == {"p0":0,"p1":0,"decision_required":0}, "P16 defect limits drift", errors)
    closure_scope = str(closure.get("scope_rule", ""))
    for marker in ("G6", "P17", "P20", "P22", "G10", "G13"):
        need(marker in closure_scope, f"P16 closure scope missing {marker}", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P16-T{i:03d}" for i in range(1, 30)]
    actual_ids = [item.get("id") for item in cases if isinstance(item, dict)]
    need(actual_ids == expected_ids, f"P16 case ID/order drift: {actual_ids}", errors)
    for item in cases:
        if not isinstance(item, dict):
            errors.append("non-object P16 case entry")
            continue
        for field in ("id","name","driver","oracle","evidence","owner"):
            need(bool(str(item.get(field, "")).strip()), f"{item.get('id')} missing {field}", errors)
        need(str(item.get("evidence", "")).startswith("artifacts/v10/P16/"), f"{item.get('id')} evidence outside P16 root", errors)
    if len(cases) >= 2:
        need(cases[-2].get("id") == "P16-T028" and "coherence" in cases[-2].get("name", "").lower(), "T028 must be exact-head coherence", errors)
        need(cases[-1].get("id") == "P16-T029" and "closure" in cases[-1].get("name", "").lower(), "T029 must be signed closure", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.startswith("Status: **")]
    need(len(status_lines) == 1, f"review must contain exactly one active Status line, got {status_lines}", errors)
    active_status = status_lines[0] if len(status_lines) == 1 else ""
    pending = active_status == PENDING
    signed = active_status == SIGNED
    need(pending or signed, f"unrecognized active P16 review status: {active_status}", errors)
    if pending:
        try:
            need(blob_at_head("artifacts/v10/P16/review.md") == PENDING_REVIEW_BLOB, "pending P16 review blob drift before accountable signing", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P16 review blob: {exc}")
        for marker in ("No P16 PASS", "case range", "P15", BASE, "fingerprint", "ClamAV", "security.manage", "domains.risk.manage", "P17", "operationsmonitor"):
            need(marker.lower() in review.lower(), f"pending P16 review missing {marker}", errors)
    if signed:
        need(bool(re.search(r"Pre-sign exact implementation SHA: `?[0-9a-f]{40}`?", review)), "signed P16 review missing pre-sign implementation SHA", errors)
        for marker in ("P16-T029", "P0", "P1", "DECISION REQUIRED", "same-revision"):
            need(marker.lower() in review.lower(), f"signed P16 review missing {marker}", errors)

    result = {
        "node": "P16",
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "contract_authority": head if contract_only and pending else None,
        "case_range": "P16-T001..P16-T029",
        "review_phase": "pending" if pending else "signed" if signed else "invalid",
        "mode": "contract-freeze" if contract_only and pending else "invalid",
        "contract_only": contract_only,
        "frozen_contract_preserved": not errors and base_ok and contract_only,
        "implementation_authorized": False,
        "merge_authoritative": False,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
