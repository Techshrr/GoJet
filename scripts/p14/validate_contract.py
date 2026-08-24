#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "a94f1d9894916b995a2379571f6ab3de520fc4ba"

P13_SOURCE = "24cdbdf848bf722e53e38ed15dce12e1d42eb9d2"
P13_RUN = 32711262325
P13_ARTIFACT = 9514396804
P13_DIGEST = "sha256:494a7942272afac7588eab153c07daf5a1f557c10b58b0dbd915eeda8709e998"

P12_SOURCE = "9d49d5ebf0e697ae9cd6537c432c27a15edc60bd"
P12_RUN = 32663159008
P12_ARTIFACT = 9499336765
P12_DIGEST = "sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a"

P06_SOURCE = "4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23"
P06_RUN = 32519298309
P06_ARTIFACT = 9460016077
P06_DIGEST = "sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8"

P09_SOURCE = "eafa369a9c150c22c2c14c9f21848a9544f4f96a"
P09_RUN = 32618657967
P09_ARTIFACT = 9487743843
P09_DIGEST = "sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7"

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
    ("CAP-TICKETS", "REQUIRED", "P14", ("G3", "G6", "G10")),
    ("CAP-MAIL", "REQUIRED", "P14", ("G3", "G6", "G10")),
    ("CAP-TURNSTILE", "REQUIRED", "P14/P15/P17", ("G6", "G10", "G13")),
    ("CAP-DOMAIN-ENTITLEMENT", "REQUIRED", "P06/P13/P14/P17", ("G6", "G10")),
    ("CAP-NOTIFICATIONS", "REQUIRED", "P12/P13-P17", ("G3", "G5", "G6", "G10")),
}

EXPECTED_ROUTES = {
    "WEB-CONTACT /contact (localized peer)",
    "APP-SUPPORT /app/support",
    "APP-SUPPORT-NEW /app/support/new",
    "APP-SUPPORT-THREAD /app/support/{ticketId}",
    "ADMIN-TICKETS /admin/tickets[/{ticketId}]",
    "ADMIN-MAIL /admin/mail",
}

EXPECTED_EXACT_APIS = [
    "POST /api/public/contact",
    "GET /api/support/tickets",
    "POST /api/support/tickets",
]

EXPECTED_STATES = {
    "web_contact": ["input", "submitting", "success-persistent", "validation-error", "Turnstile-error", "rate-limited"],
    "app_support": ["loading", "empty", "open", "awaiting-user", "awaiting-support", "closed", "error"],
    "app_support_new": ["input", "attachment", "Turnstile-required", "submitting", "success", "rate-limited", "error"],
    "app_support_thread": ["loading", "open", "replying", "awaiting", "closed", "forbidden", "attachment-blocked", "error"],
    "admin_tickets": ["loading", "empty", "open", "awaiting", "closed", "replying", "attachment-blocked", "error"],
    "admin_mail": ["loading", "empty", "queued", "sending", "sent", "failed", "retrying", "partial", "error"],
}

EXPECTED_NOTIFICATION_EVENTS = {
    "ticket_created",
    "ticket_reply_received",
    "ticket_reply_sent",
    "ticket_closed",
    "mail_delivery_failed",
}


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def authority_by_node(items: object, node: str) -> dict:
    if not isinstance(items, list):
        return {}
    for item in items:
        if isinstance(item, dict) and item.get("node") == node:
            return item
    return {}


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P14/test-plan.json")
    review_path = Path("artifacts/v10/P14/review.md")

    need(plan_path.is_file(), "missing P14 test-plan.json", errors)
    need(review_path.is_file(), "missing P14 review.md", errors)
    if errors:
        print(json.dumps({"node": "P14", "status": "FAIL", "errors": errors}, indent=2))
        return 1

    try:
        plan = json.loads(plan_path.read_text(encoding="utf-8"))
    except Exception as exc:
        print(json.dumps({"node": "P14", "status": "FAIL", "errors": [f"test-plan parse failed: {exc}"]}, indent=2))
        return 1
    review = review_path.read_text(encoding="utf-8")

    for path, expected in FROZEN_BLOBS.items():
        try:
            actual = git("rev-parse", f"HEAD:{path}")
            need(actual == expected, f"frozen authority blob drift {path}: {actual}", errors)
        except Exception as exc:
            errors.append(f"cannot bind frozen authority {path}: {exc}")

    need(plan.get("node") == "P14", "node must be P14", errors)
    need(plan.get("title") == "Support Tickets and Mail", "title drift", errors)
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
        if isinstance(item, dict)
    }
    need(actual_caps == EXPECTED_CAPABILITIES, f"capability contract drift: {actual_caps}", errors)
    need(cap.get("dependencies") == ["P06", "P09", "P12", "P13"], "P14 dependencies must be P06/P09/P12/P13", errors)
    need(cap.get("master_required_tests") == [
        "requester ownership",
        "attachment safety/ClamAV",
        "Turnstile",
        "mail retry/idempotency",
        "ticket create/reply/close without entitlement",
        "request-to-ticket linkage integrity",
    ], "Master Plan required-test list drift", errors)
    scope = str(cap.get("scope", ""))
    for marker in ("P06/P13", "P09", "P12", "P15", "P17", "P19"):
        need(marker in scope, f"capability scope missing {marker}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P13",
        "integration_commit": BASE,
        "signed_source_commit": P13_SOURCE,
        "closure_run_id": P13_RUN,
        "artifact_id": P13_ARTIFACT,
        "artifact_digest": P13_DIGEST,
        "phase": "signed",
        "merge_authoritative": True,
    }, "P13 predecessor signed authority drift", errors)

    inherited = plan.get("inherited_authorities", [])
    expected_inherited = {
        "P12": (P12_SOURCE, P12_RUN, P12_ARTIFACT, P12_DIGEST),
        "P06": (P06_SOURCE, P06_RUN, P06_ARTIFACT, P06_DIGEST),
        "P09": (P09_SOURCE, P09_RUN, P09_ARTIFACT, P09_DIGEST),
    }
    for node, (source, run, artifact, digest) in expected_inherited.items():
        item = authority_by_node(inherited, node)
        need(bool(item), f"missing inherited {node} authority", errors)
        need(item.get("signed_source_commit") == source, f"{node} signed source drift", errors)
        need(item.get("closure_run_id") == run and item.get("artifact_id") == artifact, f"{node} run/artifact drift", errors)
        need(item.get("artifact_digest") == digest, f"{node} artifact digest drift", errors)
        need(bool(str(item.get("scope", "")).strip()), f"{node} inherited scope missing", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("ia_routes", [])) == EXPECTED_ROUTES, "IA route set drift", errors)
    need(routes.get("ia_exact_apis") == EXPECTED_EXACT_APIS, "IA exact API list/order drift", errors)
    impl = routes.get("p14_implementation_api_authority", [])
    for route in (
        "GET /api/support/tickets/{ticketId}",
        "POST /api/support/tickets/{ticketId}/replies",
        "POST /api/support/tickets/{ticketId}/close",
        "GET /api/admin/support/tickets",
        "GET /api/admin/mail/queue",
        "POST /api/admin/mail/test",
    ):
        need(route in impl, f"P14 implementation API missing {route}", errors)
    route_rules = "\n".join(routes.get("rules", []))
    for marker in ("three listed IA exact APIs", "custom-domain-access", "No invented /app/tickets", "tickets.manage/mail.manage", "P19", "noindex"):
        need(marker.lower() in route_rules.lower(), f"route rule missing {marker}", errors)

    ticket = plan.get("ticket_contract", {})
    need(ticket.get("durable_statuses") == ["open", "awaiting_user", "awaiting_support", "closed"], "ticket status contract drift", errors)
    need(ticket.get("message_kinds") == ["requester_reply", "support_reply", "internal_note"], "ticket message-kind contract drift", errors)
    need(ticket.get("special_category") == "custom-domain-access", "custom-domain ticket category drift", errors)
    ticket_rules = "\n".join(ticket.get("rules", []))
    for marker in ("opaque", "workspace_id", "Internal notes", "idempotent", "Cross-workspace", "does not settle payment"):
        need(marker.lower() in ticket_rules.lower(), f"ticket rule missing {marker}", errors)

    domain_request = plan.get("domain_request_contract", {})
    need(domain_request.get("entry_url") == "/app/support/new?category=custom-domain-access", "domain request entry drift", errors)
    need(domain_request.get("submission_api") == "POST /api/support/tickets", "domain request submission API drift", errors)
    need(domain_request.get("projection_status") == "requested", "domain request projection must be requested", errors)
    need(domain_request.get("grant_authority") == "NONE", "ticket must have no grant authority", errors)
    forbidden = set(domain_request.get("forbidden_side_effects", []))
    for marker in ("active entitlement", "manual_approval grant", "plan grant", "custom-domain row", "ownership token", "DNS/HTTPS/risk state advance"):
        need(marker in forbidden, f"domain request forbidden side effect missing {marker}", errors)
    domain_rules = str(domain_request.get("projection", "")) + "\n" + "\n".join(domain_request.get("rules", []))
    for marker in ("P06 AccessRequest", "independent", "reply", "close"):
        need(marker.lower() in domain_rules.lower(), f"domain request rule missing {marker}", errors)

    attachment = plan.get("attachment_contract", {})
    need(attachment.get("states") == ["quarantined", "scanning", "clean", "infected", "scan-error", "rejected"], "attachment state contract drift", errors)
    attachment_rules = "\n".join(attachment.get("rules", []))
    for marker in ("quarantine", "Only clean", "P09 ClamAV", "server-side", "hashes/size/state/correlation"):
        need(marker.lower() in attachment_rules.lower(), f"attachment rule missing {marker}", errors)

    turnstile = plan.get("turnstile_contract", {})
    need(turnstile.get("surfaces") == ["POST /api/public/contact", "POST /api/support/tickets"], "Turnstile protected surfaces drift", errors)
    turnstile_rules = "\n".join(turnstile.get("rules", []))
    for marker in ("server-side", "replayed token fails closed", "Raw Turnstile token", "production test bypass is prohibited", "rate limits"):
        need(marker.lower() in turnstile_rules.lower(), f"Turnstile rule missing {marker}", errors)

    mail = plan.get("mail_contract", {})
    need(mail.get("service") == "SVC-MAIL-WORKER services/platformapi/cmd/mailworker", "mailworker target drift", errors)
    need(mail.get("job_states") == ["queued", "sending", "sent", "retrying", "failed"], "mail job states drift", errors)
    mail_rules = "\n".join(mail.get("rules", []))
    for marker in ("allowlisted template", "variable", "idempotent", "bounded backoff", "SMTP protocol", "never authorization"):
        need(marker.lower() in mail_rules.lower(), f"mail rule missing {marker}", errors)

    notif = plan.get("notification_contract", {})
    need(notif.get("owner") == "P14 producer events only; P12 owns notification core.", "notification ownership drift", errors)
    need(notif.get("category") == "support", "notification category drift", errors)
    need(set(notif.get("events", [])) == EXPECTED_NOTIFICATION_EVENTS, "support notification event set drift", errors)
    notif_rules = "\n".join(notif.get("rules", []))
    for marker in ("internal-only", "dedupe_key", "/app/support", "arbitrary notification emit"):
        need(marker.lower() in notif_rules.lower(), f"notification rule missing {marker}", errors)

    resources = plan.get("resource_contract", {})
    required_resources = {"ticket", "ticket_message", "ticket_attachment", "public_contact", "mail_template", "mail_job", "mail_attempt", "audit"}
    need(required_resources.issubset(resources.keys()), "resource contract incomplete", errors)
    resource_rules = "\n".join(resources.get("rules", []))
    for marker in ("idempotency", "Turnstile", "SMTP", "PII", "optimistic concurrency", "secret-safe"):
        need(marker.lower() in resource_rules.lower(), f"resource rule missing {marker}", errors)

    rbac = plan.get("rbac_contract", {})
    workspace_rules = "\n".join(rbac.get("workspace", []))
    admin_rules = "\n".join(rbac.get("admin", []))
    all_rbac = workspace_rules + "\n" + admin_rules + "\n" + "\n".join(rbac.get("rules", []))
    for marker in ("requester", "foreign Workspace", "tickets.manage", "mail.manage", "P12", "P17", "Client/test role headers"):
        need(marker.lower() in all_rbac.lower(), f"RBAC rule missing {marker}", errors)

    browser = plan.get("browser_contract", {})
    need(browser.get("states") == EXPECTED_STATES, "browser states drift from IA contract", errors)
    browser_rules = "\n".join(browser.get("rules", []))
    for marker in ("320 CSS px", "keyboard", "reduced-motion", "toast-only", "offline", "noindex/no-store", "P19", "Direct URL"):
        need(marker.lower() in browser_rules.lower(), f"browser rule missing {marker}", errors)

    env = plan.get("environment_contract", {})
    need(env.get("mysql") == "Real MySQL 8.x for ticket/message/attachment/mail/audit evidence.", "real MySQL requirement drift", errors)
    need(env.get("platformapi") == "Real native Go platformapi.", "native platformapi requirement drift", errors)
    need(env.get("mailworker") == "Real native Go services/platformapi/cmd/mailworker.", "native mailworker requirement drift", errors)
    need("Real local SMTP protocol sink" in env.get("smtp", ""), "real local SMTP protocol requirement missing", errors)
    need("P09 ClamAV" in env.get("clamav", ""), "P09 ClamAV inheritance missing", errors)
    need("production bypass prohibited" in env.get("turnstile", ""), "Turnstile production bypass prohibition missing", errors)
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Compose/Node must stay prohibited", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "same exact head closure missing", errors)
    need(closure.get("required_case_range") == "P14-T001..P14-T025", "closure case range drift", errors)
    need(closure.get("review_required") is True, "accountable review requirement missing", errors)
    need(closure.get("defect_limits") == {"p0": 0, "p1": 0, "decision_required": 0}, "defect limits drift", errors)
    need(closure.get("phases") == {
        "pre-sign": {"review_status": "PENDING", "merge_authoritative": False},
        "signed": {"review_status": "SIGNED", "merge_authoritative": True},
    }, "closure phase contract drift", errors)
    predecessor_rule = str(closure.get("predecessor_rule", ""))
    for marker in (P13_SOURCE, BASE, str(P13_RUN), str(P13_ARTIFACT), P13_DIGEST, "do not rerun/reinterpret P13", "P12", "P06", "P09"):
        need(marker in predecessor_rule, f"closure predecessor rule missing {marker}", errors)
    scope_rule = str(closure.get("scope_rule", ""))
    for marker in ("P06/P13", "P09", "P12", "P15", "P17", "P19"):
        need(marker in scope_rule, f"closure scope missing {marker}", errors)

    items = plan.get("cases", [])
    expected_ids = [f"P14-T{i:03d}" for i in range(1, 26)]
    ids = [item.get("id") for item in items if isinstance(item, dict)]
    need(ids == expected_ids, f"case range/order drift: {ids}", errors)
    need(len(items) == 25, "P14 case count must be 25", errors)
    for i, item in enumerate(items, 1):
        cid = f"P14-T{i:03d}"
        need(item.get("owner") == "P14", f"{cid} owner drift", errors)
        need(item.get("expected_exit") == 0, f"{cid} expected_exit drift", errors)
        driver = str(item.get("driver", ""))
        need(cid in driver, f"{cid} driver drift", errors)
        need(str(item.get("evidence", "")).endswith(f"{cid}.json"), f"{cid} evidence path drift", errors)
        need(bool(str(item.get("oracle", "")).strip()), f"{cid} oracle missing", errors)
    if len(items) == 25:
        need(items[23].get("name") == "Exact-head evidence coherence", "T024 must be coherence", errors)
        need(items[24].get("name") == "Signed accountable closure", "T025 must be signed closure", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.strip().startswith("Status:")]
    pending = status_lines == [PENDING]
    signed = status_lines == [SIGNED]
    need(pending ^ signed, f"illegal review status {status_lines}", errors)

    frozen_review_markers = (
        "Required P14 case range: **P14-T001..P14-T025**.",
        "A ticket, ticket reply, ticket closure, mail delivery, notification, frontend state, public contact record or support category can never substitute for an independent custom-domain entitlement decision.",
        "`Request access` enters `/app/support/new?category=custom-domain-access` and submission calls `POST /api/support/tickets` only.",
        "P14 reuses the inherited P09 mandatory ClamAV boundary and MUST NOT introduce an alternate permissive scanner path.",
        "Required native service target is `SVC-MAIL-WORKER services/platformapi/cmd/mailworker`.",
        "P12 remains owner of notification store/read-state/API/UI/deep-link authorization.",
        "No P14 PASS or Exit claim is made in this state.",
        "the signed revision itself must rerun and pass the complete affected exact-head matrix",
    )
    for marker in frozen_review_markers:
        need(marker in review, f"review frozen marker missing: {marker}", errors)

    if pending:
        need("No P14 PASS or Exit claim is made in this state." in review, "pending no-PASS marker missing", errors)
        need("Accountable reviewer identity: **GPT-5.6 Sol — P14 Technical Review**" in review, "signed-format identity template missing", errors)
        need(re.search(r"(?mi)^\s*(?:[-*]\s*)?P14-T\d{3}\s*[:=-]\s*PASS\b", review) is None, "pending review contains case PASS", errors)

    if signed:
        need(re.search(r"Pre-sign exact implementation SHA:\s*`[0-9a-f]{40}`", review) is not None, "signed pre-sign SHA missing", errors)
        need("Accountable reviewer identity: **GPT-5.6 Sol — P14 Technical Review**" in review, "signed identity drift", errors)
        need(re.search(r"Review date:\s*\*\*\d{4}-\d{2}-\d{2}\*\*", review) is not None, "signed review date missing", errors)
        for marker in ("- P0 defects: 0", "- P1 defects: 0", "- `DECISION REQUIRED`: 0"):
            need(marker in review, f"signed defect marker missing {marker}", errors)
        need("P14-T025" in review and "PASS" in review, "signed final closure record missing", errors)

    result = {
        "node": "P14",
        "status": "PASS" if not errors else "FAIL",
        "base_integration_commit": BASE,
        "case_range": "P14-T001..P14-T025",
        "review_phase": "signed" if signed else "pending" if pending else "invalid",
        "errors": errors,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
