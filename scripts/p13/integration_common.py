#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import hashlib
import hmac
import http.client
import json
import os
from pathlib import Path
import subprocess
import time
from typing import Any
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
HEAD = os.environ.get("GITHUB_SHA") or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
BASE_URL = os.environ.get("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081").rstrip("/")
MYSQL_HOST = os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = os.environ.get("GOJET_TEST_MYSQL_PORT", "3306")
MYSQL_USER = os.environ.get("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
CALLBACK_SECRET = os.environ.get("GOJET_TEST_BILLING_CALLBACK_SECRET", "")
ADMIN_ACTOR = os.environ.get("GOJET_TEST_BILLING_ADMIN_ACTOR", "p13-admin")
ADMIN_EMAIL = os.environ.get("GOJET_TEST_BILLING_ADMIN_EMAIL", "p13-admin@example.test")

ROOT_EVIDENCE = ROOT / "artifacts" / "v10" / "P13"
API_DIR = ROOT_EVIDENCE / "api"
RBAC_DIR = ROOT_EVIDENCE / "rbac"
SECURITY_DIR = ROOT_EVIDENCE / "security"
ENTITLEMENT_DIR = ROOT_EVIDENCE / "entitlement"
AUDIT_DIR = ROOT_EVIDENCE / "audit"
RUNTIME_DIR = ROOT_EVIDENCE / "runtime"
RESULTS_DIR = ROOT_EVIDENCE / "results"
for directory in (API_DIR, RBAC_DIR, SECURITY_DIR, ENTITLEMENT_DIR, AUDIT_DIR, RUNTIME_DIR, RESULTS_DIR):
    directory.mkdir(parents=True, exist_ok=True)


def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def utc_now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def iso(value: dt.datetime | None = None) -> str:
    value = value or utc_now()
    return value.astimezone(dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def unique(prefix: str) -> str:
    value = f"{prefix}-{time.time_ns():x}-{os.getpid():x}".lower()
    return value[:96]


def safe_plan_code(prefix: str) -> str:
    value = unique(prefix).replace("-", "_")
    return value[:64]


def auth_headers(actor: str, email: str, *, correlation: str | None = None, extra: dict[str, str] | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Email": email,
        "X-GoJet-Test-Display-Name": actor,
    }
    if correlation:
        headers["X-Request-ID"] = correlation
    if extra:
        headers.update(extra)
    return headers


def admin_headers(*, correlation: str | None = None, extra: dict[str, str] | None = None) -> dict[str, str]:
    return auth_headers(ADMIN_ACTOR, ADMIN_EMAIL, correlation=correlation, extra=extra)


def domain_headers(workspace: str, actor: str, *, role: str = "owner", correlation: str | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Workspace": workspace,
        "X-GoJet-Test-Workspace-Role": role,
    }
    if correlation:
        headers["X-Request-ID"] = correlation
    return headers


def http_raw(method: str, path: str, *, raw_body: bytes | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes]:
    parsed = urlsplit(BASE_URL)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=30)
    merged = {"Accept": "application/json"}
    if headers:
        merged.update(headers)
    if raw_body is not None:
        merged.setdefault("Content-Type", "application/json")
    conn.request(method, path, body=raw_body, headers=merged)
    response = conn.getresponse()
    raw = response.read()
    response_headers = {k.lower(): v for k, v in response.getheaders()}
    status = response.status
    conn.close()
    return status, response_headers, raw


def request_json(method: str, path: str, *, body: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes, Any]:
    raw_body = None
    if body is not None:
        raw_body = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    status, response_headers, raw = http_raw(method, path, raw_body=raw_body, headers=headers)
    parsed = None
    if raw:
        try:
            parsed = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            pass
    return status, response_headers, raw, parsed


def p13(method: str, path: str, actor: str, email: str, *, body: Any | None = None, correlation: str | None = None, extra: dict[str, str] | None = None):
    return request_json(method, path, body=body, headers=auth_headers(actor, email, correlation=correlation, extra=extra))


def admin(method: str, path: str, *, body: Any | None = None, correlation: str | None = None, extra: dict[str, str] | None = None):
    return request_json(method, path, body=body, headers=admin_headers(correlation=correlation, extra=extra))


def domain(method: str, path: str, workspace: str, actor: str, *, role: str = "owner", body: Any | None = None, correlation: str | None = None):
    return request_json(method, path, body=body, headers=domain_headers(workspace, actor, role=role, correlation=correlation))


def err_code(data: Any) -> str | None:
    try:
        return data["error"]["code"]
    except Exception:
        return None


def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def mysql(query: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    cmd = [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", MYSQL_PORT,
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B",
        MYSQL_DATABASE, "-e", query,
    ]
    completed = subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, env=env)
    if completed.returncode != 0:
        raise AssertionError(f"mysql failed: {query}\nstdout={completed.stdout}\nstderr={completed.stderr}")
    return completed.stdout.strip()


def mysql_scalar(query: str) -> str:
    output = mysql(query)
    return output.splitlines()[0] if output else ""


def mysql_rows(query: str) -> list[list[str]]:
    output = mysql(query)
    if not output:
        return []
    return [line.split("\t") for line in output.splitlines()]


def create_workspace(case_id: str, *, suffix: str = "owner") -> tuple[dict[str, Any], str, str]:
    actor = f"{case_id.lower()}-{suffix}"
    email = f"{actor}@example.test"
    status, headers, raw, data = p13(
        "POST", "/api/workspaces", actor, email,
        body={"name": f"{case_id} {suffix}"}, correlation=unique(f"{case_id}-workspace"),
    )
    expect(status == 201 and isinstance(data, dict), f"create workspace status={status} body={raw[:300]!r}")
    expect(headers.get("cache-control") == "no-store", "workspace create missing no-store")
    return data["workspace"], actor, email


def seed_member(workspace: str, actor: str, email: str, role: str) -> int:
    mysql(
        "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES ("
        + ",".join([sql_quote(workspace), sql_quote(actor), sql_quote(email), sql_quote(actor), sql_quote(role)]) + ")"
    )
    return int(mysql_scalar(
        f"SELECT id FROM workspace_memberships WHERE workspace_id={sql_quote(workspace)} AND user_id={sql_quote(actor)}"
    ))


def create_plan(case_id: str, *, name: str = "Plan", status: str = "active", currency: str = "USD", amount_minor: int = 1000,
                period: str = "monthly", entitlements: dict[str, int] | None = None) -> dict[str, Any]:
    entitlements = entitlements or {"links": 100}
    body = {
        "code": safe_plan_code(case_id.lower()),
        "name": f"{case_id} {name}",
        "status": status,
        "currency": currency,
        "amount_minor": amount_minor,
        "billing_period": period,
        "entitlements": [
            {"capability": capability, "limit_value": limit, "unit": "count"}
            for capability, limit in sorted(entitlements.items())
        ],
    }
    status_code, headers, raw, data = admin("POST", "/api/admin/plans", body=body, correlation=unique(f"{case_id}-plan-create"))
    expect(status_code == 201 and isinstance(data, dict) and "plan" in data, f"create plan status={status_code} body={raw[:400]!r}")
    expect(headers.get("cache-control") == "no-store" and headers.get("x-robots-tag") == "noindex, nofollow", "admin plan missing private headers")
    return data["plan"]


def update_plan(plan: dict[str, Any], *, name: str | None = None, status: str | None = None, currency: str | None = None,
                amount_minor: int | None = None, period: str | None = None, entitlements: dict[str, int] | None = None,
                expected_version: int | None = None, correlation: str | None = None):
    current_entitlements = {
        item["capability"]: int(item["limit_value"])
        for item in plan.get("entitlements", [])
    }
    values = entitlements if entitlements is not None else current_entitlements
    body = {
        "name": name if name is not None else plan["name"],
        "status": status if status is not None else plan["status"],
        "currency": currency if currency is not None else plan["money"]["currency"],
        "amount_minor": amount_minor if amount_minor is not None else int(plan["money"]["amount_minor"]),
        "billing_period": period if period is not None else plan["billing_period"],
        "entitlements": [
            {"capability": capability, "limit_value": limit, "unit": "count"}
            for capability, limit in sorted(values.items())
        ],
        "expected_version": expected_version if expected_version is not None else int(plan["version"]),
    }
    return admin("PUT", f"/api/admin/plans/{plan['id']}", body=body, correlation=correlation or unique("plan-update"))


def create_order(workspace: str, actor: str, email: str, plan_id: int, *, kind: str = "new", key: str | None = None,
                 forged_role: str | None = None):
    key = key or unique("p13-idempotency-key")
    extra = {"Idempotency-Key": key}
    if forged_role:
        extra["X-GoJet-Test-Workspace-Role"] = forged_role
    return p13(
        "POST", f"/api/workspaces/{workspace}/orders", actor, email,
        body={"plan_id": int(plan_id), "kind": kind}, correlation=unique("order"), extra=extra,
    )


def callback_payload(order: dict[str, Any], *, event_id: str, transaction_id: str, outcome: str,
                     event_type: str | None = None, received_at: str | None = None, correlation: str | None = None) -> dict[str, Any]:
    return {
        "event_id": event_id,
        "transaction_id": transaction_id,
        "order_id": order["id"],
        "event_type": event_type or f"payment.{outcome}",
        "outcome": outcome,
        "currency": order["money"]["currency"],
        "amount_minor": int(order["money"]["amount_minor"]),
        "received_at": received_at or iso(),
        "correlation_id": correlation or unique("callback-correlation"),
    }


def callback_signature(provider: str, payload: dict[str, Any], raw: bytes) -> str:
    expect(len(CALLBACK_SECRET.encode("utf-8")) >= 16, "GOJET_TEST_BILLING_CALLBACK_SECRET must be at least 16 bytes")
    canonical = (
        provider.encode("utf-8") + b"\n" +
        str(payload["event_id"]).encode("utf-8") + b"\n" +
        str(payload["transaction_id"]).encode("utf-8") + b"\n" + raw
    )
    return hmac.new(CALLBACK_SECRET.encode("utf-8"), canonical, hashlib.sha256).hexdigest()


def send_callback(provider: str, payload: dict[str, Any], *, valid_signature: bool = True, signature: str | None = None):
    raw = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
    if signature is None:
        signature = callback_signature(provider, payload, raw) if valid_signature else "00" * 32
    headers = {"X-GoJet-Test-Callback-Signature": signature, "Content-Type": "application/json"}
    status, response_headers, response_raw = http_raw("POST", f"/api/payments/callbacks/{provider}", raw_body=raw, headers=headers)
    data = None
    if response_raw:
        try:
            data = json.loads(response_raw.decode("utf-8"))
        except json.JSONDecodeError:
            pass
    return status, response_headers, response_raw, data, raw, signature


def paid_subscription(case_id: str, workspace: str, actor: str, email: str, plan: dict[str, Any], *, kind: str = "new", provider: str = "stripe"):
    order_result = create_order(workspace, actor, email, int(plan["id"]), kind=kind)
    expect(order_result[0] == 201, f"order create status={order_result[0]} body={order_result[2][:300]!r}")
    order = order_result[3]["order"]
    payload = callback_payload(
        order,
        event_id=unique(f"{case_id}-paid-event"),
        transaction_id=unique(f"{case_id}-paid-txn"),
        outcome="paid",
        event_type="payment.paid",
    )
    callback_result = send_callback(provider, payload)
    expect(callback_result[0] == 200 and callback_result[3].get("ok") is True, f"paid callback status={callback_result[0]} body={callback_result[2][:300]!r}")
    return order, payload, callback_result


def create_domain(workspace: str, actor: str, hostname: str):
    return domain(
        "POST", f"/api/workspaces/{workspace}/domains", workspace, actor,
        body={"hostname": hostname, "change_reason": "P13 integration evidence"},
        correlation=unique("p13-domain"),
    )


def get_entitlement(workspace: str, actor: str, email: str, capability: str):
    return p13("GET", f"/api/workspaces/{workspace}/billing/entitlements/{capability}", actor, email)


def record(case_id: str, observations: dict[str, Any], errors: list[str], directory: Path) -> Path:
    path = directory / f"{case_id}.json"
    payload = {
        "case_id": case_id,
        "implementation_commit": HEAD,
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "observations": observations,
    }
    path.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
    return path
