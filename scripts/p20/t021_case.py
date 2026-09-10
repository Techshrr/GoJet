#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import hmac
import http.client as http_client
import json
import os
import subprocess
import time
from http.cookies import SimpleCookie
from typing import Any
from urllib.parse import urlsplit

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T021"
CASE_NAME = "Real billing invoice payment callback and entitlement workflow"
AMOUNT = 2100
LIMIT = 2


def predecessor(case: str) -> dict[str, Any]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case}.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"same-run {case} evidence unavailable: {exc}") from exc
    if data.get("status") != "PASS" or data.get("implementation_commit") != HEAD or data.get("errors") != []:
        raise RuntimeError(f"same-run {case} evidence is not exact-head PASS")
    return data


def fx_predecessor() -> dict[str, Any]:
    path = ROOT / "artifacts" / "v10" / "P13" / "api" / "P13-T013.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"same-run P13-T013 FX evidence unavailable: {exc}") from exc
    if data.get("case_id") != "P13-T013" or data.get("status") != "PASS" or data.get("implementation_commit") != HEAD or data.get("errors") != []:
        raise RuntimeError("same-run P13-T013 FX evidence is not exact-head PASS")
    observations = data.get("observations")
    if not isinstance(observations, dict):
        raise RuntimeError("same-run P13-T013 FX observations are invalid")
    required = {
        "order_currency": "EUR",
        "order_amount_minor": 1300,
        "stale_status": "stale",
        "provider_error_status": "provider-error",
        "bad_override_status": 400,
        "override_status": "override",
    }
    for field, expected in required.items():
        if observations.get(field) != expected:
            raise RuntimeError(f"same-run P13-T013 FX observation mismatch: {field}")
    actions = observations.get("audit_actions")
    if not isinstance(actions, list) or "billing.fx.provider_error" not in actions or "billing.fx.override" not in actions:
        raise RuntimeError("same-run P13-T013 FX audit authority is incomplete")
    if not str(observations.get("current_rate") or "").strip() or not str(observations.get("provider_error_preserved_rate") or "").strip():
        raise RuntimeError("same-run P13-T013 FX rate evidence is incomplete")
    return data


def q(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def mysql(sql: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
    proc = subprocess.run([
        "mysql", "--protocol=tcp",
        "-h", os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1"),
        "-P", os.environ.get("GOJET_TEST_MYSQL_PORT", "3306"),
        "-u", os.environ.get("GOJET_TEST_MYSQL_USER", "root"),
        "--default-character-set=utf8mb4", "-N", "-B",
        os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test"), "-e", sql,
    ], cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip())
    return proc.stdout.strip()


def scalar(sql: str) -> str:
    value = mysql(sql)
    return value.splitlines()[0] if value else ""


def http(method: str, path: str, raw: bytes | None = None, headers: dict[str, str] | None = None):
    base = urlsplit(os.environ.get("GOJET_P20_API_BASE", "http://127.0.0.1:18081"))
    conn = http_client.HTTPConnection(base.hostname, base.port or 80, timeout=20)
    merged = {"Accept": "application/json"}
    if raw is not None:
        merged["Content-Type"] = "application/json"
    if headers:
        merged.update(headers)
    conn.request(method, path, body=raw, headers=merged)
    response = conn.getresponse()
    body = response.read()
    result = (response.status, response.getheaders(), body)
    conn.close()
    return result


def http_json(method: str, path: str, body: dict[str, Any] | None = None, headers: dict[str, str] | None = None):
    raw = None if body is None else json.dumps(body, separators=(",", ":")).encode()
    status, response_headers, payload = http(method, path, raw, headers)
    try:
        decoded = json.loads(payload.decode()) if payload else {}
    except json.JSONDecodeError:
        decoded = {}
    return status, response_headers, decoded


def session_cookie(headers: list[tuple[str, str]]) -> str:
    for key, value in headers:
        if key.lower() == "set-cookie":
            cookie = SimpleCookie()
            cookie.load(value)
            morsel = cookie.get("__Host-gojet_session")
            if morsel and morsel.value:
                return morsel.value
    return ""


def counts(workspace: str) -> dict[str, int]:
    ws = q(workspace)
    expressions = {
        "orders": f"SELECT COUNT(*) FROM billing_orders WHERE workspace_id={ws}",
        "invoices": f"SELECT COUNT(*) FROM billing_invoices WHERE workspace_id={ws}",
        "transactions": f"SELECT COUNT(*) FROM billing_transactions WHERE workspace_id={ws}",
        "callback_events": "SELECT COUNT(*) FROM payment_callback_events WHERE provider='stripe'",
        "subscriptions": f"SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id={ws} AND status='active'",
        "billing_grants": f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={ws} AND source_type='billing' AND revoked_at IS NULL",
        "domain_plan_sources": f"SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id={ws} AND source='plan' AND source_key='p13:billing' AND status='active'",
        "payment_notifications": f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={ws} AND category='billing' AND event_key='payment_succeeded'",
    }
    return {name: int(scalar(sql) or "0") for name, sql in expressions.items()}


def delta(before: dict[str, int], after: dict[str, int]) -> dict[str, int]:
    return {name: after[name] - before[name] for name in before}


def stripe_event(event_id: str, event_type: str, payment_intent: str, amount: int = AMOUNT) -> bytes:
    return json.dumps({
        "id": event_id,
        "object": "event",
        "type": event_type,
        "livemode": False,
        "created": int(time.time()),
        "data": {
            "object": {
                "id": payment_intent,
                "object": "payment_intent",
                "amount": amount,
                "currency": "usd",
                "status": "succeeded",
            }
        },
    }, separators=(",", ":")).encode()


def signed_post(raw: bytes) -> tuple[int, bytes]:
    secret = os.environ.get("GOJET_BILLING_STRIPE_WEBHOOK_SECRET", "")
    if not secret:
        return 0, b""
    ts = int(time.time())
    digest = hmac.new(secret.encode(), str(ts).encode() + b"." + raw, hashlib.sha256).hexdigest()
    status, _, body = http("POST", "/api/payments/callbacks/stripe", raw, {"Stripe-Signature": f"t={ts},v1={digest}"})
    return status, body


def main_case() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_redis": True,
        "real_platform_api": True,
        "production_billing": True,
        "production_callback_verifier": True,
        "provider": "stripe",
        "formal_p20_t021_claim": True,
        "mock_authority": False,
        "test_header_authority": False,
        "browser_return_settlement_authority": False,
        "provider_intent_fixture_only": True,
        "outbound_provider_creation_authority": False,
        "raw_callback_recorded": False,
        "signature_recorded": False,
        "secret_material_recorded": False,
    }

    def require(condition: bool, message: str) -> None:
        if not condition:
            errors.append(message)

    try:
        t020 = predecessor("P20-T020")
        t019 = predecessor("P20-T019")
        p13_fx = fx_predecessor()
    except RuntimeError as exc:
        errors.append(str(exc))
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    d20 = t020.get("details", {})
    d19 = t019.get("details", {})
    fx = p13_fx.get("observations", {})
    require(isinstance(d20, dict) and d20.get("next_case") == CASE_ID, "T020 did not unlock T021")
    user = str(d20.get("user_id") or "")
    workspace = str(d20.get("workspace_id") or "")
    require(bool(user and workspace) and d20.get("workspace_role") == "owner", "T020 lacks correlated owner identity")
    require(isinstance(d19, dict) and d19.get("user_id") == user and d19.get("workspace_id") == workspace, "T019/T020 correlation mismatch")
    for field in ("p06_link_assignment_guard_proven", "p17_entitlement_and_safety_conjunctive", "not_ready_link_zero_write", "axes_conjunctive_ready"):
        require(isinstance(d19, dict) and d19.get(field) is True, f"T019 missing domain authority {field}")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    details.update({
        "user_id": user,
        "workspace_id": workspace,
        "workspace_role": "owner",
        "t020_evidence_bound": True,
        "t019_domain_safety_evidence_bound": True,
        "p13_t013_fx_evidence_bound": True,
        "fx_current_rate_present": bool(str(fx.get("current_rate") or "").strip()),
        "fx_stale_status": fx.get("stale_status"),
        "fx_provider_error_status": fx.get("provider_error_status"),
        "fx_provider_error_preserved_last_valid_rate": bool(str(fx.get("provider_error_preserved_rate") or "").strip()),
        "fx_bad_override_http_status": fx.get("bad_override_status"),
        "fx_override_status": fx.get("override_status"),
        "fx_audit_actions": fx.get("audit_actions"),
    })

    window = d20.get("auth_rate_window_seconds", 60)
    if not isinstance(window, int) or window <= 0 or window > 300:
        errors.append("T020 authentication rate window evidence is invalid")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    details["auth_rate_window_seconds"] = window
    details["auth_rate_window_respected"] = True

    suffix = HEAD[:12].lower()
    plan_code = f"p20_t021_formal_{suffix}"
    mysql(
        "INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version) VALUES "
        f"({q(plan_code)},{q('P20 T021 formal fixture')},'active','USD',{AMOUNT},'monthly',1)"
    )
    plan_id = int(scalar(f"SELECT id FROM billing_plans WHERE code={q(plan_code)}") or "0")
    require(plan_id > 0, "catalog fixture unavailable")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    mysql(
        "INSERT INTO billing_plan_entitlements (plan_id,capability,limit_value,unit,source_version) VALUES "
        f"({plan_id},'custom_domains',{LIMIT},'count',1)"
    )

    time.sleep(window + 1)
    login_status, login_headers, login_body = http_json("POST", "/api/auth/login", {
        "email": f"p20-t009-{suffix}@example.test",
        "password": os.environ.get("GOJET_P20_T009_LOGIN_FIXTURE", ""),
        "correlation_id": f"p20-t021-login-{suffix}",
    })
    cookie = session_cookie(login_headers)
    require(login_status == 200 and login_body.get("status") == "authenticated" and bool(cookie), "real P15 login failed")
    cookie_header = f"__Host-gojet_session={cookie}"
    me_status, _, me = http_json("GET", "/api/me", headers={"Cookie": cookie_header})
    csrf = str(me.get("csrf_token") or "")
    me_user = me.get("user", {})
    session = me.get("session", {})
    require(
        me_status == 200
        and isinstance(me_user, dict)
        and me_user.get("id") == user
        and bool(csrf)
        and isinstance(session, dict)
        and bool(session.get("id")),
        "real P15 session/CSRF binding failed",
    )
    details.update({
        "login_http_status": login_status,
        "me_http_status": me_status,
        "real_session_authenticated": True,
        "csrf_authority_issued": bool(csrf),
    })
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    before = counts(workspace)
    order_status, _, order_body = http_json(
        "POST",
        f"/api/workspaces/{workspace}/orders",
        {"plan_id": plan_id, "kind": "new"},
        {
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": csrf,
            "Idempotency-Key": f"p20-t021-order-{suffix}",
        },
    )
    order = order_body.get("order", {}) if isinstance(order_body, dict) else {}
    order_id = str(order.get("id") or "") if isinstance(order, dict) else ""
    order_delta = delta(before, counts(workspace))
    require(order_status == 201 and order_body.get("created") is True and bool(order_id) and order.get("workspace_id") == workspace, "real owner order creation failed")
    require(isinstance(order.get("money"), dict) and order["money"].get("currency") == "USD" and order["money"].get("amount_minor") == AMOUNT, "order Money mismatch")
    require(
        order_delta == {
            "orders": 1,
            "invoices": 1,
            "transactions": 0,
            "callback_events": 0,
            "subscriptions": 0,
            "billing_grants": 0,
            "domain_plan_sources": 0,
            "payment_notifications": 0,
        },
        "order write delta mismatch",
    )
    details.update({"order_http_status": order_status, "order_id_present": bool(order_id), "order_write_deltas": order_delta})
    require(mysql(f"SELECT status,currency,amount_minor FROM billing_orders WHERE id={q(order_id)}") == f"pending\tUSD\t{AMOUNT}", "pre-callback order state mismatch")
    require(mysql(f"SELECT status,currency,amount_minor,paid_at IS NOT NULL FROM billing_invoices WHERE order_id={q(order_id)}") == f"open\tUSD\t{AMOUNT}\t0", "pre-callback invoice state mismatch")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    payment_intent = f"pi_p20_t021_{suffix}"
    event_id = f"evt_p20_t021_paid_{suffix}"
    intent_id = f"pint_p20_t021_{suffix}"
    mysql(
        "INSERT INTO billing_provider_intents "
        "(id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at) VALUES "
        f"({q(intent_id)},{q(workspace)},{q(order_id)},'stripe',{q('gj_p20_t021_'+suffix)},{q(payment_intent)},'fiat','USD',{AMOUNT},NULL,'active',DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY))"
    )
    details["provider_intent_bound"] = scalar(
        f"SELECT COUNT(*) FROM billing_provider_intents WHERE id={q(intent_id)} AND provider='stripe' "
        f"AND provider_reference={q(payment_intent)} AND settlement_asset='USD' AND settlement_amount_units={AMOUNT} AND status='active'"
    ) == "1"
    require(details["provider_intent_bound"], "provider-intent binding failed")
    domains_before = mysql(
        f"SELECT id,hostname_ascii,routing_state,ownership_status,ingress_dns_status,https_status,risk_status "
        f"FROM custom_domains WHERE workspace_id={q(workspace)} ORDER BY id"
    )
    require(bool(domains_before), "same-run T019 durable Domain state missing")

    paid_raw = stripe_event(event_id, "payment_intent.succeeded", payment_intent)
    zero = {name: 0 for name in before}
    before_browser = counts(workspace)
    browser_status, _, _ = http("GET", f"/api/payments/callbacks/stripe?payment_intent={payment_intent}&redirect_status=succeeded")
    unsigned_status, _, _ = http("POST", "/api/payments/callbacks/stripe", paid_raw)
    browser_delta = delta(before_browser, counts(workspace))
    details.update({
        "browser_return_http_status": browser_status,
        "unsigned_callback_http_status": unsigned_status,
        "browser_return_write_deltas": browser_delta,
    })
    require(browser_status == 404 and unsigned_status == 401 and browser_delta == zero, "browser/unsigned callback obtained settlement authority")

    before_paid = counts(workspace)
    paid_status, paid_body = signed_post(paid_raw)
    paid_delta = delta(before_paid, counts(workspace))
    details.update({
        "paid_callback_http_status": paid_status,
        "paid_callback_empty_body": paid_body == b"",
        "paid_write_deltas": paid_delta,
    })
    require(paid_status == 200 and paid_body == b"", "signed Stripe callback ACK mismatch")
    require(
        paid_delta == {
            "orders": 0,
            "invoices": 0,
            "transactions": 1,
            "callback_events": 1,
            "subscriptions": 1,
            "billing_grants": 1,
            "domain_plan_sources": 1,
            "payment_notifications": 1,
        },
        "settlement write delta mismatch",
    )

    before_dup = counts(workspace)
    dup_status, dup_body = signed_post(paid_raw)
    dup_delta = delta(before_dup, counts(workspace))
    ignored_raw = stripe_event(f"evt_p20_t021_ignore_{suffix}", "charge.refunded", payment_intent)
    before_ignore = counts(workspace)
    ignored_status, ignored_body = signed_post(ignored_raw)
    ignored_delta = delta(before_ignore, counts(workspace))
    bad_raw = stripe_event(f"evt_p20_t021_bad_{suffix}", "payment_intent.succeeded", payment_intent, 9999)
    before_bad = counts(workspace)
    bad_status, _ = signed_post(bad_raw)
    bad_delta = delta(before_bad, counts(workspace))
    details.update({
        "duplicate_http_status": dup_status,
        "duplicate_write_deltas": dup_delta,
        "refund_ignore_http_status": ignored_status,
        "refund_ignore_write_deltas": ignored_delta,
        "bad_amount_http_status": bad_status,
        "bad_amount_write_deltas": bad_delta,
    })
    require(dup_status == 200 and dup_body == b"" and dup_delta == zero, "duplicate callback not idempotent")
    require(ignored_status == 200 and ignored_body == b"" and ignored_delta == zero, "authenticated refund-ignore mutated state")
    require(bad_status == 401 and bad_delta == zero, "bad amount did not fail closed")

    final_checks = {
        "transaction_count": f"SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(order_id)} AND provider='stripe' AND provider_transaction_id={q(payment_intent)} AND currency='USD' AND amount_minor={AMOUNT} AND status='paid'",
        "callback_event_count": f"SELECT COUNT(*) FROM payment_callback_events WHERE provider='stripe' AND provider_event_id={q(event_id)} AND provider_transaction_id={q(payment_intent)} AND status='processed'",
        "active_subscription_count": f"SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id={q(workspace)} AND plan_id={plan_id} AND status='active'",
        "active_billing_entitlement_count": f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={q(workspace)} AND capability='custom_domains' AND source_type='billing' AND limit_value={LIMIT} AND revoked_at IS NULL",
        "active_domain_plan_source_count": f"SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id={q(workspace)} AND source='plan' AND source_key='p13:billing' AND status='active' AND domain_limit={LIMIT}",
        "payment_notification_count": f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={q(workspace)} AND category='billing' AND event_key='payment_succeeded' AND resource_id={q(order_id)}",
        "active_provider_intent_count": f"SELECT COUNT(*) FROM billing_provider_intents WHERE id={q(intent_id)} AND status='active'",
        "refunded_order_count": f"SELECT COUNT(*) FROM billing_orders WHERE id={q(order_id)} AND status='refunded'",
        "refunded_transaction_count": f"SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(order_id)} AND status='refunded'",
    }
    for name, sql in final_checks.items():
        details[name] = int(scalar(sql) or "0")
    for name in (
        "transaction_count",
        "callback_event_count",
        "active_subscription_count",
        "active_billing_entitlement_count",
        "active_domain_plan_source_count",
        "payment_notification_count",
        "active_provider_intent_count",
    ):
        require(details[name] == 1, f"{name} expected 1")
    require(details["refunded_order_count"] == 0 and details["refunded_transaction_count"] == 0, "refund mutation occurred")

    order_final = mysql(f"SELECT status,currency,amount_minor FROM billing_orders WHERE id={q(order_id)}")
    invoice_final = mysql(f"SELECT status,currency,amount_minor,paid_at IS NOT NULL FROM billing_invoices WHERE order_id={q(order_id)}")
    details["money_immutable"] = order_final == f"paid\tUSD\t{AMOUNT}" and invoice_final == f"paid\tUSD\t{AMOUNT}\t1"
    details["domain_state_unchanged_by_settlement"] = mysql(
        f"SELECT id,hostname_ascii,routing_state,ownership_status,ingress_dns_status,https_status,risk_status "
        f"FROM custom_domains WHERE workspace_id={q(workspace)} ORDER BY id"
    ) == domains_before
    require(details["money_immutable"], "Money mutated across settlement")
    require(details["domain_state_unchanged_by_settlement"], "settlement bypassed Domain safety state")

    ent_status, _, ent = http_json(
        "GET",
        f"/api/workspaces/{workspace}/billing/entitlements/custom_domains",
        headers={"Cookie": cookie_header},
    )
    details.update({
        "billing_entitlement_http_status": ent_status,
        "billing_entitlement_allowed": ent.get("allowed") is True,
        "billing_entitlement_limit_value": ent.get("limit_value"),
    })
    require(ent_status == 200 and ent.get("allowed") is True and ent.get("limit_value") == LIMIT, "effective billing entitlement mismatch")
    require(details["mock_authority"] is False and details["test_header_authority"] is False and details["secret_material_recorded"] is False, "unsafe authority/evidence mode")
    details["next_case"] = "P20-T022" if not errors else None
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> int:
    payload = main_case()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
