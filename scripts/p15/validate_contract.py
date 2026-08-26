#!/usr/bin/env python3
from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

BASE = "9258cb0f3f913b37b03aa8cf3c2938711314d3aa"
CONTRACT_AUTHORITY = "9ba89a42281709087b40cdcf0cb2eebd54952a99"
TEST_PLAN_BLOB = "d48614b82da24074e4be44118405dd7761114160"
PENDING_REVIEW_BLOB = "2bcc0f9ecbc4f26b950b52042586c2b0831eabe5"
P14_SOURCE = "f079c938dbe49d0f55b8b09995e72201cd0aab6e"
P14_RUN = 32763705854
P14_ARTIFACT = 9533837642
P14_DIGEST = "sha256:3f334718539e8fdd9cf5896fffdca9c00b8d0fc9a57b03d39795e97e6af853a8"
P04_SOURCE = "694cc4d50c13fa76f3d35571287a146f4dc04025"
P04_INTEGRATION = "16cddfa89279d698f30607f4dec79f3ed2f55b59"
P04_RUN = 32395638418
P04_ARTIFACT = 9416550011
P04_DIGEST = "sha256:90a4c29844ced6ae934d769785085723c10840106417bbf0df2899e0f5b8fcdd"
PENDING = "Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**"
SIGNED = "Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**"

FROZEN_BLOBS = {
    "specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md": "29cb2b4e14076ce71b21747dbf2facc411ccb41a",
    "specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md": "20609139a0265d3f3a40a1c7c07894dc69220290",
    "specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md": "68ac7c581207570ae849a75132e3e54f03cea651",
    "contracts/traceability/capability-matrix.snapshot.md": "bcc9fef9e666e7b10d5e43ae627ba094d27a8026",
    "contracts/traceability/route-registry.snapshot.md": "35da40a95c1b66ca34741ea0f7996045c4633e72",
    "docs/security/SECURITY_INVARIANTS.md": "5d3178ee80bf46b4f00df729ab24d783a7af75dc",
}

EXPECTED_CONTRACT_FILES = {
    ".github/workflows/p15-authentication-oauth-account.yml",
    "artifacts/v10/P15/test-plan.json",
    "artifacts/v10/P15/review.md",
    "scripts/p15/validate_contract.py",
}
EXPECTED_CAPABILITIES = {
    ("CAP-AUTH", "REQUIRED", "P15", ("G3", "G5", "G6", "G10")),
    ("CAP-OAUTH", "REQUIRED", "P15", ("G3", "G6", "G10", "G13")),
    ("CAP-TURNSTILE", "REQUIRED", "P14/P15/P17", ("G6", "G10", "G13")),
}
EXPECTED_AUTH_ROUTES = {
    "AUTH-LOGIN /login",
    "AUTH-REGISTER /register",
    "AUTH-VERIFY /verify-email",
    "AUTH-FORGOT /forgot-password",
    "AUTH-RESET /reset-password?token={opaque}",
    "AUTH-OAUTH-CALLBACK /oauth/{provider}/callback",
    "AUTH-SOCIAL-REG /social-registration?code={opaque}",
}
EXPECTED_ACCOUNT_ROUTES = {
    "APP-SETTINGS /app/settings/profile",
    "APP-SETTINGS /app/settings/security",
    "APP-SETTINGS /app/settings/sessions",
    "APP-SETTINGS /app/settings/connected-accounts",
}
EXPECTED_PROVIDERS = ["google", "facebook", "github", "qq", "wechat", "rainbow"]
EXPECTED_EXACT_APIS = [
    "POST /api/auth/login",
    "POST /api/public/login-email-code",
    "GET /api/public/auth/providers",
    "POST /api/auth/register",
    "POST /api/public/email-code",
    "POST /api/public/register-email-code",
    "POST /api/auth/verifyemail",
    "POST /api/mail/verification",
    "POST /api/auth/forgotpassword",
    "POST /api/auth/resetpassword",
    "GET /api/public/auth/{provider}/callback",
    "POST /api/public/auth/handoff",
    "GET /api/public/auth/social-registration",
    "POST /api/public/auth/social-registration/complete",
]


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def main() -> int:
    errors: list[str] = []
    plan_path = Path("artifacts/v10/P15/test-plan.json")
    review_path = Path("artifacts/v10/P15/review.md")
    need(plan_path.is_file(), "missing P15 test-plan.json", errors)
    need(review_path.is_file(), "missing P15 review.md", errors)
    if errors:
        print(json.dumps({"node": "P15", "status": "FAIL", "errors": errors}, indent=2))
        return 1

    try:
        plan = json.loads(plan_path.read_text(encoding="utf-8"))
    except Exception as exc:
        print(json.dumps({"node": "P15", "status": "FAIL", "errors": [f"test-plan parse failed: {exc}"]}, indent=2))
        return 1
    review = review_path.read_text(encoding="utf-8")
    head = git("rev-parse", "HEAD")

    base_ok = False
    authority_ok = False
    try:
        base_ok = git("merge-base", BASE, "HEAD") == BASE
        need(base_ok, f"P15 history must descend from exact P14 integration {BASE}", errors)
    except Exception as exc:
        errors.append(f"cannot resolve P14 integration ancestry: {exc}")
    try:
        authority_ok = git("merge-base", CONTRACT_AUTHORITY, "HEAD") == CONTRACT_AUTHORITY
        need(authority_ok, f"P15 implementation must descend from frozen contract authority {CONTRACT_AUTHORITY}", errors)
    except Exception as exc:
        errors.append(f"cannot resolve P15 contract authority ancestry: {exc}")

    contract_only = False
    try:
        changed = {line for line in git("diff", "--name-only", f"{BASE}..HEAD").splitlines() if line}
        contract_only = head == CONTRACT_AUTHORITY and changed == EXPECTED_CONTRACT_FILES
        if head == CONTRACT_AUTHORITY:
            need(contract_only, f"contract authority base-to-head diff drift: {sorted(changed)}", errors)
    except Exception as exc:
        errors.append(f"cannot inspect base-to-head diff: {exc}")

    try:
        need(git("rev-parse", "HEAD:artifacts/v10/P15/test-plan.json") == TEST_PLAN_BLOB,
             "frozen P15 test-plan blob drift", errors)
    except Exception as exc:
        errors.append(f"cannot bind frozen P15 test-plan blob: {exc}")

    for path, expected in FROZEN_BLOBS.items():
        try:
            actual = git("rev-parse", f"HEAD:{path}")
            need(actual == expected, f"frozen authority blob drift {path}: {actual}", errors)
        except Exception as exc:
            errors.append(f"cannot bind frozen authority {path}: {exc}")

    need(plan.get("node") == "P15", "node must be P15", errors)
    need(plan.get("title") == "Authentication, OAuth and Account", "title drift", errors)
    need(plan.get("base_integration_commit") == BASE, "base integration drift", errors)
    need(plan.get("specification_ids") == [
        "GJ-V10-MP-GREENFIELD-2026-08-20",
        "GJ-V10-DS-GREENFIELD-2026-08-20",
        "GJ-V10-IA-GREENFIELD-2026-08-20",
    ], "specification IDs drift", errors)

    cap = plan.get("capability_contract", {})
    actual_caps = {
        (item.get("id"), item.get("status"), item.get("owner"), tuple(item.get("gates", [])))
        for item in cap.get("capabilities", []) if isinstance(item, dict)
    }
    need(actual_caps == EXPECTED_CAPABILITIES, f"capability contract drift: {actual_caps}", errors)
    need(cap.get("master_predecessors") == ["P04", "P14"], "P15 Master predecessor list must be P04/P14", errors)
    need(cap.get("master_required_tests") == ["CSRF", "Origin", "rate limit", "token expiry/reuse", "OAuth state", "session revoke"], "Master required-test list drift", errors)
    scope = str(cap.get("scope", ""))
    for marker in ("P04", "P12", "P14", "P17", "P19", "P20-P22"):
        need(marker in scope, f"scope missing inherited/later boundary {marker}", errors)

    pred = plan.get("predecessor_signed_authority", {})
    need(pred == {
        "node": "P14",
        "integration_commit": BASE,
        "signed_source_commit": P14_SOURCE,
        "closure_run_id": P14_RUN,
        "artifact_id": P14_ARTIFACT,
        "artifact_digest": P14_DIGEST,
        "phase": "signed",
        "merge_authoritative": True,
    }, "P14 predecessor signed authority drift", errors)

    p04 = plan.get("inherited_p04_authority", {})
    need(p04.get("signed_source_commit") == P04_SOURCE, "P04 signed source drift", errors)
    need(p04.get("integration_commit") == P04_INTEGRATION, "P04 integration drift", errors)
    need(p04.get("workflow_run_id") == P04_RUN and p04.get("artifact_id") == P04_ARTIFACT, "P04 run/artifact drift", errors)
    need(p04.get("artifact_digest") == P04_DIGEST, "P04 artifact digest drift", errors)

    routes = plan.get("route_contract", {})
    need(set(routes.get("p15_auth_routes", [])) == EXPECTED_AUTH_ROUTES, "P15 Auth route set drift", errors)
    need(set(routes.get("account_routes", [])) == EXPECTED_ACCOUNT_ROUTES, "P15 account route set drift", errors)
    need(routes.get("ia_exact_apis") == EXPECTED_EXACT_APIS, "P15 IA exact API list/order drift", errors)
    inherited = "\n".join(routes.get("inherited_not_owned", []))
    need("AUTH-INVITE" in inherited and "P12" in inherited, "AUTH-INVITE must remain inherited P12 authority", errors)
    admin_oauth = str(routes.get("admin_oauth_surface", ""))
    for marker in ("ADMIN-OAUTH", "CAP-OAUTH"):
        need(marker in admin_oauth, f"Admin OAuth ownership boundary missing {marker}", errors)
    route_rules = "\n".join(routes.get("rules", []))
    for marker in ("legacy", "noindex", "no-store", "AUTH-INVITE", "settings.manage", "P17"):
        need(marker.lower() in route_rules.lower(), f"route rule missing {marker}", errors)

    auth = plan.get("auth_contract", {})
    auth_rules = "\n".join(auth.get("rules", []))
    for marker in ("localStorage", "CSRF", "Origin", "Forgot-password", "one-time", "Session revocation", "raw verification/reset"):
        need(marker.lower() in auth_rules.lower(), f"auth rule missing {marker}", errors)
    need(auth.get("session_states") == ["active", "revoked", "expired"], "session state contract drift", errors)
    need(auth.get("account_browser_sections") == ["profile", "security", "sessions", "connected-accounts"], "account browser section drift", errors)
    need("P17" in str(auth.get("mfa_scope", "")), "MFA scope must preserve P17 boundary", errors)

    oauth = plan.get("oauth_contract", {})
    need(oauth.get("providers") == EXPECTED_PROVIDERS, "OAuth provider inventory drift", errors)
    oauth_rules = "\n".join(oauth.get("rules", []))
    for marker in ("state", "PKCE", "Redirect URI", "callback", "handoff", "Social registration", "bind/unbind", "client secrets"):
        need(marker.lower() in oauth_rules.lower(), f"OAuth rule missing {marker}", errors)

    mail = plan.get("mail_turnstile_contract", {})
    need(mail.get("mail_owner") == "P14 CAP-MAIL", "P14 mail ownership drift", errors)
    mail_rules = "\n".join(mail.get("rules", []))
    for marker in ("P14", "mailworker", "parallel SMTP", "Turnstile", "production bypass"):
        need(marker.lower() in mail_rules.lower(), f"mail/Turnstile rule missing {marker}", errors)

    browser = plan.get("browser_contract", {})
    expected_state_keys = {
        "auth_login", "auth_register", "auth_verify", "auth_forgot", "auth_reset",
        "auth_oauth_callback", "auth_social_registration", "app_settings_account", "admin_oauth",
    }
    need(set(browser.get("states", {}).keys()) == expected_state_keys, "browser state-family set drift", errors)
    browser_rules = "\n".join(browser.get("rules", []))
    for marker in ("Design System", "320", "password-manager", "toast-only", "server authorization"):
        need(marker.lower() in browser_rules.lower(), f"browser rule missing {marker}", errors)

    env = plan.get("environment_contract", {})
    for key in ("mysql", "redis", "platformapi", "mailworker", "oauth", "turnstile", "browser", "production_docker_compose_node"):
        need(bool(str(env.get(key, "")).strip()), f"environment contract missing {key}", errors)
    need(env.get("production_docker_compose_node") == "PROHIBITED", "production Docker/Compose/Node boundary drift", errors)

    closure = plan.get("closure", {})
    need(closure.get("same_exact_head_required") is True, "same exact head closure must be required", errors)
    need(closure.get("required_case_range") == "P15-T001..P15-T029", "P15 case range drift", errors)
    need(closure.get("review_required") is True, "accountable review must be required", errors)
    need(closure.get("defect_limits") == {"p0": 0, "p1": 0, "decision_required": 0}, "defect limits drift", errors)

    cases = plan.get("cases", [])
    expected_ids = [f"P15-T{i:03d}" for i in range(1, 30)]
    actual_ids = [item.get("id") for item in cases if isinstance(item, dict)]
    need(actual_ids == expected_ids, f"case ID/order drift: {actual_ids}", errors)
    for item in cases:
        if not isinstance(item, dict):
            errors.append("non-object case entry")
            continue
        for field in ("id", "name", "driver", "oracle", "evidence", "owner"):
            need(bool(str(item.get(field, "")).strip()), f"{item.get('id')} missing {field}", errors)
        need(str(item.get("evidence", "")).startswith("artifacts/v10/P15/"), f"{item.get('id')} evidence outside P15 root", errors)
    need(cases[-2].get("id") == "P15-T028" and "coherence" in cases[-2].get("name", "").lower(), "T028 must be coherence", errors)
    need(cases[-1].get("id") == "P15-T029" and "closure" in cases[-1].get("name", "").lower(), "T029 must be signed closure", errors)

    status_lines = [line.strip() for line in review.splitlines() if line.startswith("Status: **")]
    need(len(status_lines) == 1, f"review must contain exactly one active Status line, got {status_lines}", errors)
    active_status = status_lines[0] if len(status_lines) == 1 else ""
    pending = active_status == PENDING
    signed = active_status == SIGNED
    need(pending or signed, f"unrecognized active review status: {active_status}", errors)
    if pending:
        try:
            need(git("rev-parse", "HEAD:artifacts/v10/P15/review.md") == PENDING_REVIEW_BLOB,
                 "pending P15 review blob drift before accountable signing", errors)
        except Exception as exc:
            errors.append(f"cannot bind pending P15 review blob: {exc}")
        for marker in ("No P15 PASS", "P15-T001..P15-T029", P14_SOURCE, BASE, "AUTH-INVITE", "localStorage", "google", "rainbow", "P17"):
            need(marker.lower() in review.lower(), f"pending review missing {marker}", errors)
    if signed:
        need(bool(re.search(r"Pre-sign exact implementation SHA: `?[0-9a-f]{40}`?", review)), "signed review missing pre-sign implementation SHA", errors)
        for marker in ("P15-T029", "P0", "P1", "DECISION REQUIRED", "same-revision"):
            need(marker.lower() in review.lower(), f"signed review missing {marker}", errors)

    if head == CONTRACT_AUTHORITY:
        mode = "contract-freeze"
    elif pending:
        mode = "implementation-guard"
    elif signed:
        mode = "signed-review-guard"
    else:
        mode = "invalid"

    result = {
        "node": "P15",
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "implementation_commit": head,
        "base_integration_commit": BASE,
        "contract_authority": CONTRACT_AUTHORITY,
        "case_range": "P15-T001..P15-T029",
        "review_phase": "pending" if pending else "signed" if signed else "invalid",
        "mode": mode,
        "contract_only": contract_only,
        "frozen_contract_preserved": not errors and base_ok and authority_ok,
        "implementation_authorized": not errors and authority_ok and pending,
        "merge_authoritative": False,
    }
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == "__main__":
    raise SystemExit(main())
