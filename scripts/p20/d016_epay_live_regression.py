#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import http.client
import json
import os
import subprocess
from http.cookies import SimpleCookie
from pathlib import Path
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[2]
HEAD = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
OUT = ROOT / "artifacts" / "v10" / "P20" / "runtime" / "d016-epay" / "regression.json"


def mysql_rows(query: str) -> list[list[str]]:
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
    if not result.stdout.strip():
        return []
    return [line.split("\t") for line in result.stdout.strip().splitlines()]


def mysql_scalar(query: str) -> str:
    rows = mysql_rows(query)
    return rows[0][0] if rows else ""


def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def request(method: str, path: str, body: dict | None = None, headers: dict[str, str] | None = None):
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
    result = response.status, response.getheaders(), payload
    conn.close()
    return result


def decode_json(raw: bytes) -> dict:
    try:
        value = json.loads(raw.decode("utf-8")) if raw else {}
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}


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


def epay_sign(params: dict[str, str], key: str) -> str:
    names = sorted(name for name, value in params.items() if name not in {"sign", "sign_type"} and value != "")
    canonical = "&".join(f"{name}={params[name]}" for name in names) + key
    return hashlib.md5(canonical.encode("utf-8"), usedforsecurity=False).hexdigest()


def signed_epay_query(order_id: str, money: str, trade_no: str) -> str:
    key = os.environ["GOJET_BILLING_EPAY_KEY"]
    params = {
        "pid": os.environ["GOJET_BILLING_EPAY_PID"],
        "trade_no": trade_no,
        "out_trade_no": order_id,
        "type": "alipay",
        "name": "GoJet P20 D016 Epay live regression",
        "money": money,
        "trade_status": "TRADE_SUCCESS",
        "param": "",
        "sign_type": "MD5",
    }
    params["sign"] = epay_sign(params, key)
    return urlencode(params)


def write(payload: dict) -> None:
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))


def main() -> int:
    evidence = {
        "schema": "gojet.p20-d016-epay-live-regression.v1",
        "case": "P20-D016-EPAY-REGRESSION",
        "implementation_commit": HEAD,
        "status": "FAIL",
        "errors": [],
        "details": {
            "real_mysql": True,
            "real_redis": True,
            "real_platform_api": True,
            "real_p15_session": True,
            "production_test_auth_enabled": False,
            "test_callback_verifier_enabled": False,
            "provider": "epay",
            "protocol": "Rainbow Epay V1 compatible GET notify / MD5",
            "formal_p20_t021_claim": False,
            "secret_material_recorded": False,
        },
    }

    try:
        t011_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T011.json"
        t011 = json.loads(t011_path.read_text(encoding="utf-8"))
        details = t011.get("details") if isinstance(t011.get("details"), dict) else {}
        user_id = str(details.get("user_id") or "").strip()
        workspace_id = str(details.get("workspace_id") or "").strip()
        if t011.get("status") != "PASS" or t011.get("implementation_commit") != HEAD or t011.get("errors") != []:
            raise RuntimeError("same-run P20-T011 is not exact-head PASS")
        if not user_id or not workspace_id or details.get("workspace_role") != "owner":
            raise RuntimeError("same-run P20-T011 lacks correlated owner identity")

        suffix = HEAD[:12].lower()
        plan_code = f"p20-d016-epay-{suffix}"
        mysql_rows(
            "INSERT INTO billing_plans (code,name,status,currency,amount_minor,billing_period,version) VALUES ("
            f"{sql_quote(plan_code)},'P20 D016 Epay Plan','active','CNY',1234,'monthly',1)"
        )
        plan_id = int(mysql_scalar(f"SELECT id FROM billing_plans WHERE code={sql_quote(plan_code)}"))
        mysql_rows(
            "INSERT INTO billing_plan_entitlements (plan_id,capability,limit_value,unit,source_version) VALUES ("
            f"{plan_id},'links',16,'count',1)"
        )

        login_status, login_headers, login_raw = request(
            "POST",
            "/api/auth/login",
            {
                "email": f"p20-t009-{suffix}@example.test",
                "password": "P20-T009!Strong-Passphrase-2026",
                "correlation_id": f"p20-d016-epay-login-{suffix}",
            },
        )
        login_body = decode_json(login_raw)
        cookie = session_cookie(login_headers)
        if login_status != 200 or login_body.get("status") != "authenticated" or not cookie:
            raise RuntimeError(f"could not establish real P15 session: HTTP {login_status}")

        cookie_header = f"__Host-gojet_session={cookie}"
        me_status, _, me_raw = request("GET", "/api/me", headers={"Cookie": cookie_header})
        me_body = decode_json(me_raw)
        csrf = str(me_body.get("csrf_token") or "").strip()
        if me_status != 200 or not csrf or not isinstance(me_body.get("user"), dict) or me_body["user"].get("id") != user_id:
            raise RuntimeError("real P15 session/CSRF authority did not correlate to T011")

        order_status, _, order_raw = request(
            "POST",
            f"/api/workspaces/{workspace_id}/orders",
            {"plan_id": plan_id, "kind": "new"},
            headers={
                "Cookie": cookie_header,
                "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
                "X-CSRF-Token": csrf,
                "Idempotency-Key": f"p20-d016-epay-order-{suffix}",
                "X-Request-ID": f"p20-d016-epay-order-{suffix}",
            },
        )
        order_body = decode_json(order_raw)
        order = order_body.get("order") if isinstance(order_body.get("order"), dict) else {}
        order_id = str(order.get("id") or "").strip()
        if order_status != 201 or not order_id:
            raise RuntimeError(f"real Billing order creation failed: HTTP {order_status}")

        before = {
            "events": int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events") or "0"),
            "transactions": int(mysql_scalar("SELECT COUNT(*) FROM billing_transactions") or "0"),
            "billing_grants": int(mysql_scalar("SELECT COUNT(*) FROM entitlement_grants WHERE source_type='billing'") or "0"),
        }
        trade_no = f"20260908{suffix[:8]}"
        valid_query = signed_epay_query(order_id, "12.34", trade_no)

        tampered = valid_query.replace("money=12.34", "money=99.99", 1)
        bad_status, _, bad_raw = request("GET", "/api/payments/callbacks/epay?" + tampered)
        after_bad = {
            "events": int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events") or "0"),
            "transactions": int(mysql_scalar("SELECT COUNT(*) FROM billing_transactions") or "0"),
            "billing_grants": int(mysql_scalar("SELECT COUNT(*) FROM entitlement_grants WHERE source_type='billing'") or "0"),
        }
        bad_zero_write = before == after_bad
        order_after_bad = mysql_scalar(f"SELECT status FROM billing_orders WHERE id={sql_quote(order_id)}")
        invoice_after_bad = mysql_scalar(f"SELECT status FROM billing_invoices WHERE order_id={sql_quote(order_id)}")
        if bad_status != 401 or bad_raw != b"fail" or not bad_zero_write or order_after_bad != "pending" or invoice_after_bad != "open":
            raise RuntimeError(
                f"tampered Epay callback did not fail closed: HTTP {bad_status}, body={bad_raw!r}, before={before}, after={after_bad}, order={order_after_bad}, invoice={invoice_after_bad}"
            )

        first_status, first_headers, first_raw = request("GET", "/api/payments/callbacks/epay?" + valid_query)
        first_content_type = next((value for key, value in first_headers if key.lower() == "content-type"), "")
        if first_status != 200 or first_raw != b"success" or first_content_type != "text/plain; charset=utf-8":
            raise RuntimeError(f"valid Epay ACK mismatch: HTTP {first_status}, body={first_raw!r}, content_type={first_content_type!r}")

        first_state = mysql_rows(
            "SELECT o.status,i.status,t.status,t.provider,t.provider_transaction_id,t.currency,t.amount_minor "
            "FROM billing_orders o JOIN billing_invoices i ON i.order_id=o.id "
            "JOIN billing_transactions t ON t.order_id=o.id "
            f"WHERE o.id={sql_quote(order_id)}"
        )
        event_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM payment_callback_events WHERE provider='epay' AND provider_event_id={sql_quote(trade_no)}"
        ) or "0")
        active_subscription_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id={sql_quote(workspace_id)} AND status='active'"
        ) or "0")
        active_grant_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(workspace_id)} AND source_type='billing' AND capability='links' AND revoked_at IS NULL"
        ) or "0")
        if first_state != [["paid", "paid", "paid", "epay", trade_no, "CNY", "1234"]] or event_count != 1 or active_subscription_count != 1 or active_grant_count != 1:
            raise RuntimeError(
                f"valid Epay settlement mismatch: state={first_state}, events={event_count}, subscriptions={active_subscription_count}, grants={active_grant_count}"
            )

        replay_status, _, replay_raw = request("GET", "/api/payments/callbacks/epay?" + valid_query)
        replay_event_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM payment_callback_events WHERE provider='epay' AND provider_event_id={sql_quote(trade_no)}"
        ) or "0")
        replay_tx_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM billing_transactions WHERE provider='epay' AND provider_transaction_id={sql_quote(trade_no)}"
        ) or "0")
        replay_grant_count = int(mysql_scalar(
            f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(workspace_id)} AND source_type='billing' AND capability='links' AND revoked_at IS NULL"
        ) or "0")
        if replay_status != 200 or replay_raw != b"success" or (replay_event_count, replay_tx_count, replay_grant_count) != (1, 1, 1):
            raise RuntimeError(
                f"Epay replay was not idempotent: HTTP {replay_status}, body={replay_raw!r}, events={replay_event_count}, tx={replay_tx_count}, grants={replay_grant_count}"
            )

        evidence["details"].update({
            "t011_evidence_bound": True,
            "user_id": user_id,
            "workspace_id": workspace_id,
            "workspace_role": "owner",
            "order_http_status": order_status,
            "order_id": order_id,
            "order_currency": "CNY",
            "order_amount_minor": 1234,
            "tampered_callback_http_status": bad_status,
            "tampered_callback_ack": "fail",
            "tampered_callback_zero_durable_writes": bad_zero_write,
            "valid_callback_http_status": first_status,
            "valid_callback_ack": "success",
            "valid_callback_content_type": first_content_type,
            "paid_state": first_state[0][:4],
            "callback_event_count": event_count,
            "active_subscription_count": active_subscription_count,
            "active_billing_grant_count": active_grant_count,
            "replay_http_status": replay_status,
            "replay_ack": "success",
            "replay_event_count": replay_event_count,
            "replay_transaction_count": replay_tx_count,
            "replay_billing_grant_count": replay_grant_count,
            "idempotent_replay": True,
        })
        evidence["status"] = "PASS"
    except Exception as exc:
        evidence["errors"].append(f"{type(exc).__name__}: {exc}")

    write(evidence)
    return 0 if not evidence["errors"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
