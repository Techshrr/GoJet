#!/usr/bin/env python3
from __future__ import annotations

import http.client
import json
import os
import subprocess
from http.cookies import SimpleCookie
from pathlib import Path
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
HEAD = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
OUT = ROOT / "artifacts" / "v10" / "P20" / "runtime" / "t021-post-d015-discovery" / "discovery.json"


def mysql(query: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
    cmd = [
        "mysql", "--protocol=tcp",
        "-h", os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1"),
        "-P", os.environ.get("GOJET_TEST_MYSQL_PORT", "3306"),
        "-u", os.environ.get("GOJET_TEST_MYSQL_USER", "root"),
        "--default-character-set=utf8mb4", "-N", "-B",
        os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test"),
        "-e", query,
    ]
    result = subprocess.run(cmd, cwd=ROOT, env=env, text=True, capture_output=True, check=False)
    if result.returncode != 0:
        raise RuntimeError(f"MySQL query failed: {result.stderr.strip()}")
    return result.stdout.strip()


def scalar(query: str) -> str:
    output = mysql(query)
    return output.splitlines()[0] if output else ""


def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def request_json(method: str, path: str, body: dict | None = None, headers: dict[str, str] | None = None):
    base = urlsplit(os.environ.get("GOJET_P20_API_BASE", "http://127.0.0.1:18081"))
    conn = http.client.HTTPConnection(base.hostname, base.port or 80, timeout=20)
    raw = None if body is None else json.dumps(body, separators=(",", ":")).encode("utf-8")
    merged = {"Accept": "application/json"}
    if raw is not None:
        merged["Content-Type"] = "application/json"
    if headers:
        merged.update(headers)
    conn.request(method, path, body=raw, headers=merged)
    response = conn.getresponse()
    payload = response.read()
    status = response.status
    response_headers = response.getheaders()
    conn.close()
    try:
        decoded = json.loads(payload.decode("utf-8")) if payload else {}
    except json.JSONDecodeError:
        decoded = {"_non_json": True}
    return status, response_headers, decoded


def error_code(body: dict) -> str:
    error = body.get("error")
    return str(error.get("code") or "") if isinstance(error, dict) else ""


def session_cookie(headers: list[tuple[str, str]]) -> str:
    for key, value in headers:
        if key.lower() != "set-cookie":
            continue
        cookie = SimpleCookie()
        cookie.load(value)
        morsel = cookie.get("__Host-gojet_session")
        if morsel is not None and morsel.value:
            return morsel.value
    return ""


def counts(workspace_id: str) -> dict[str, int]:
    ws = sql_quote(workspace_id)
    return {
        "orders": int(scalar(f"SELECT COUNT(*) FROM billing_orders WHERE workspace_id={ws}") or "0"),
        "invoices": int(scalar(f"SELECT COUNT(*) FROM billing_invoices WHERE workspace_id={ws}") or "0"),
        "transactions": int(scalar(f"SELECT COUNT(*) FROM billing_transactions WHERE workspace_id={ws}") or "0"),
        "billing_grants": int(scalar(f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={ws} AND source_type='billing'") or "0"),
        "callback_events": int(scalar("SELECT COUNT(*) FROM payment_callback_events") or "0"),
    }


def delta(before: dict[str, int], after: dict[str, int]) -> dict[str, int]:
    return {key: after[key] - before[key] for key in before}


def write(payload: dict) -> None:
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))


def fail(payload: dict, message: str) -> int:
    payload["errors"].append(message)
    write(payload)
    return 1


def main() -> int:
    payload = {
        "schema": "gojet.p20-t021-post-d015-discovery.v1",
        "case": "P20-T021-POST-D015-DISCOVERY",
        "implementation_commit": HEAD,
        "status": "FAIL",
        "errors": [],
        "details": {
            "real_mysql": True,
            "real_platform_api": True,
            "real_p15_session": True,
            "real_workspace_membership": True,
            "production_callback_mode": True,
            "test_header_authority": False,
            "mock_authority": False,
            "formal_p20_t021_claim": False,
            "secret_material_recorded": False,
        },
    }

    t011_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T011.json"
    try:
        t011 = json.loads(t011_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return fail(payload, "T021 post-D015 discovery requires same-run P20-T011 evidence")
    if t011.get("status") != "PASS" or t011.get("implementation_commit") != HEAD or t011.get("errors") != []:
        return fail(payload, "T021 post-D015 discovery predecessor P20-T011 is not exact-head PASS")

    predecessor = t011.get("details") if isinstance(t011.get("details"), dict) else {}
    user_id = str(predecessor.get("user_id") or "").strip()
    workspace_id = str(predecessor.get("workspace_id") or "").strip()
    if not user_id or not workspace_id or predecessor.get("workspace_role") != "owner":
        return fail(payload, "T021 post-D015 discovery lacks the correlated active owner identity")
    payload["details"].update({
        "user_id": user_id,
        "workspace_id": workspace_id,
        "workspace_role": "owner",
        "t011_evidence_bound": True,
    })

    suffix = HEAD[:12].lower()
    plan_code = f"p20_t021_{suffix}"
    mysql(
        "INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version) VALUES ("
        + ",".join([
            sql_quote(plan_code),
            sql_quote("P20 T021 discovery fixture"),
            "'active'",
            "'USD'",
            "2100",
            "'monthly'",
            "1",
        ])
        + ")"
    )
    plan_id = int(scalar(f"SELECT id FROM billing_plans WHERE code={sql_quote(plan_code)}") or "0")
    if plan_id <= 0:
        return fail(payload, "T021 post-D015 discovery could not establish the catalog-only plan fixture")
    mysql(
        "INSERT INTO billing_plan_entitlements (plan_id,capability,limit_value,unit,source_version) VALUES "
        f"({plan_id},'links',210,'count',1),({plan_id},'custom_domains',1,'count',1)"
    )
    payload["details"].update({
        "catalog_fixture_only": True,
        "plan_id": plan_id,
        "plan_currency": "USD",
        "plan_amount_minor": 2100,
        "plan_custom_domains_limit": 1,
    })

    login_status, login_headers, login_body = request_json(
        "POST",
        "/api/auth/login",
        {
            "email": f"p20-t009-{suffix}@example.test",
            "password": "P20-T009!Strong-Passphrase-2026",
            "correlation_id": f"p20-t021-post-d015-login-{suffix}",
        },
    )
    cookie = session_cookie(login_headers)
    payload["details"]["login_http_status"] = login_status
    if login_status != 200 or login_body.get("status") != "authenticated" or not cookie:
        return fail(payload, "T021 post-D015 discovery could not establish a real P15 customer session")

    cookie_header = f"__Host-gojet_session={cookie}"
    me_status, _, me_body = request_json("GET", "/api/me", headers={"Cookie": cookie_header})
    csrf = str(me_body.get("csrf_token") or "").strip()
    user = me_body.get("user") if isinstance(me_body.get("user"), dict) else {}
    session = me_body.get("session") if isinstance(me_body.get("session"), dict) else {}
    if me_status != 200 or user.get("id") != user_id or not csrf or not str(session.get("id") or "").strip():
        return fail(payload, "T021 post-D015 discovery could not bind real P15 session/CSRF authority")
    payload["details"].update({
        "me_http_status": me_status,
        "real_session_authenticated": True,
        "session_identity_preserved": True,
        "csrf_authority_issued": True,
        "session_id_present": True,
    })

    before_order = counts(workspace_id)
    order_status, _, order_body = request_json(
        "POST",
        f"/api/workspaces/{workspace_id}/orders",
        {"plan_id": plan_id, "kind": "new"},
        headers={
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": csrf,
            "Idempotency-Key": f"p20-t021-post-d015-order-{suffix}",
            "X-Request-ID": f"p20-t021-post-d015-order-{suffix}",
            "X-GoJet-Test-Actor": "spoofed-t021-actor",
            "X-GoJet-Test-Email": "spoofed-t021@example.test",
            "X-GoJet-Test-Workspace-Role": "viewer",
        },
    )
    after_order = counts(workspace_id)
    order_delta = delta(before_order, after_order)
    order = order_body.get("order") if isinstance(order_body.get("order"), dict) else {}
    order_id = str(order.get("id") or "").strip()
    order_ok = (
        order_status == 201
        and order_body.get("created") is True
        and bool(order_id)
        and order.get("workspace_id") == workspace_id
        and isinstance(order.get("money"), dict)
        and order["money"].get("currency") == "USD"
        and int(order["money"].get("amount_minor") or 0) == 2100
        and order_delta == {"orders": 1, "invoices": 1, "transactions": 0, "billing_grants": 0, "callback_events": 0}
    )
    payload["details"].update({
        "order_http_status": order_status,
        "order_error_code": error_code(order_body),
        "order_id_present": bool(order_id),
        "order_created": order_ok,
        "order_write_deltas": order_delta,
        "spoofed_test_headers_ignored": order_ok,
    })
    if not order_ok:
        return fail(
            payload,
            f"T021 post-D015 discovery did not restore the real owner order/invoice boundary: HTTP {order_status}, code={error_code(order_body)!r}, deltas={order_delta}",
        )

    invoice_rows = mysql(
        "SELECT status,currency,amount_minor FROM billing_invoices "
        f"WHERE order_id={sql_quote(order_id)}"
    ).splitlines()
    order_db_status = scalar(f"SELECT status FROM billing_orders WHERE id={sql_quote(order_id)}")
    if invoice_rows != ["open\tUSD\t2100"] or order_db_status != "pending":
        return fail(payload, f"T021 post-D015 discovery order/invoice pre-callback state mismatch: order={order_db_status!r}, invoice={invoice_rows!r}")
    payload["details"].update({
        "pre_callback_order_status": order_db_status,
        "pre_callback_invoice_status": "open",
        "pre_callback_money_immutable": True,
    })

    before_callback = counts(workspace_id)
    callback_payload = {
        "event_id": f"p20-t021-event-{suffix}",
        "transaction_id": f"p20-t021-txn-{suffix}",
        "order_id": order_id,
        "event_type": "payment.paid",
        "outcome": "paid",
        "currency": "USD",
        "amount_minor": 2100,
        "received_at": "2026-09-07T18:00:00Z",
        "correlation_id": f"p20-t021-callback-{suffix}",
    }
    callback_status, _, callback_body = request_json(
        "POST",
        "/api/payments/callbacks/stripe",
        callback_payload,
        headers={
            "X-GoJet-Test-Callback-Signature": "discovery-not-production-authority",
            "X-Request-ID": f"p20-t021-callback-{suffix}",
        },
    )
    after_callback = counts(workspace_id)
    callback_delta = delta(before_callback, after_callback)
    callback_code = error_code(callback_body)
    post_order_status = scalar(f"SELECT status FROM billing_orders WHERE id={sql_quote(order_id)}")
    post_invoice_status = scalar(f"SELECT status FROM billing_invoices WHERE order_id={sql_quote(order_id)}")
    payload["details"].update({
        "callback_http_status": callback_status,
        "callback_error_code": callback_code,
        "callback_write_deltas": callback_delta,
        "post_callback_order_status": post_order_status,
        "post_callback_invoice_status": post_invoice_status,
        "candidate_severity": "P1",
        "candidate_boundary": "production Billing provider callback verifier is not composed into native platformapi",
    })

    zero_callback_writes = callback_delta == {
        "orders": 0,
        "invoices": 0,
        "transactions": 0,
        "billing_grants": 0,
        "callback_events": 0,
    }
    if callback_status == 503 and callback_code == "callback_verifier_unavailable" and zero_callback_writes and post_order_status == "pending" and post_invoice_status == "open":
        return fail(
            payload,
            "P20-T021 blocked after D015 remediation: real owner order/invoice succeeds, but production provider callback returns 503 callback_verifier_unavailable with zero settlement/entitlement writes",
        )

    return fail(
        payload,
        f"P20-T021 post-D015 discovery reached an unexpected callback boundary: HTTP {callback_status}, code={callback_code!r}, deltas={callback_delta}, order={post_order_status!r}, invoice={post_invoice_status!r}",
    )


if __name__ == "__main__":
    raise SystemExit(main())
