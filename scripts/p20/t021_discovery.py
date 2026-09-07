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
OUT = ROOT / "artifacts" / "v10" / "P20" / "runtime" / "t021-discovery" / "discovery.json"


def mysql_scalar(query: str) -> str:
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
    return result.stdout.strip().splitlines()[0] if result.stdout.strip() else ""


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


def write(payload: dict) -> None:
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(payload, indent=2, sort_keys=True))


def main() -> int:
    evidence_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T011.json"
    payload = {
        "schema": "gojet.p20-t021-discovery.v1",
        "case": "P20-T021-DISCOVERY",
        "implementation_commit": HEAD,
        "status": "FAIL",
        "errors": [],
        "details": {
            "real_mysql": True,
            "real_platform_api": True,
            "test_header_authority": False,
            "mock_authority": False,
            "secret_material_recorded": False,
        },
    }
    try:
        t011 = json.loads(evidence_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        payload["errors"].append("T021 discovery requires same-run P20-T011 evidence")
        write(payload)
        return 1
    if t011.get("status") != "PASS" or t011.get("implementation_commit") != HEAD or t011.get("errors") != []:
        payload["errors"].append("T021 discovery predecessor P20-T011 is not exact-head PASS")
        write(payload)
        return 1

    details = t011.get("details") if isinstance(t011.get("details"), dict) else {}
    user_id = str(details.get("user_id") or "").strip()
    workspace_id = str(details.get("workspace_id") or "").strip()
    if not user_id or not workspace_id or details.get("workspace_role") != "owner":
        payload["errors"].append("T021 discovery lacks the correlated active owner identity")
        write(payload)
        return 1
    payload["details"].update({
        "user_id": user_id,
        "workspace_id": workspace_id,
        "workspace_role": details.get("workspace_role"),
        "t011_evidence_bound": True,
    })

    suffix = HEAD[:12].lower()
    login_status, login_headers, login_body = request_json(
        "POST",
        "/api/auth/login",
        {
            "email": f"p20-t009-{suffix}@example.test",
            "password": "P20-T009!Strong-Passphrase-2026",
            "correlation_id": f"p20-t021-discovery-login-{suffix}",
        },
    )
    cookie = session_cookie(login_headers)
    payload["details"]["login_http_status"] = login_status
    payload["details"]["real_session_authenticated"] = login_status == 200 and login_body.get("status") == "authenticated" and bool(cookie)
    if not payload["details"]["real_session_authenticated"]:
        payload["errors"].append("T021 discovery could not establish a real P15 customer session")
        write(payload)
        return 1

    me_status, _, me_body = request_json("GET", "/api/me", headers={"Cookie": f"__Host-gojet_session={cookie}"})
    payload["details"]["me_http_status"] = me_status
    payload["details"]["session_identity_preserved"] = (
        me_status == 200
        and isinstance(me_body.get("user"), dict)
        and me_body["user"].get("id") == user_id
    )
    if not payload["details"]["session_identity_preserved"]:
        payload["errors"].append("T021 discovery session does not preserve the correlated P20 identity")
        write(payload)
        return 1

    before = {
        "orders": int(mysql_scalar("SELECT COUNT(*) FROM billing_orders") or "0"),
        "invoices": int(mysql_scalar("SELECT COUNT(*) FROM billing_invoices") or "0"),
        "transactions": int(mysql_scalar("SELECT COUNT(*) FROM billing_transactions") or "0"),
        "billing_grants": int(mysql_scalar("SELECT COUNT(*) FROM entitlement_grants WHERE source_type='billing'") or "0"),
    }
    order_status, _, order_body = request_json(
        "POST",
        f"/api/workspaces/{workspace_id}/orders",
        {"plan_id": 1, "kind": "new"},
        headers={
            "Cookie": f"__Host-gojet_session={cookie}",
            "Idempotency-Key": f"p20-t021-discovery-order-{suffix}",
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-Request-ID": f"p20-t021-discovery-order-{suffix}",
        },
    )
    after = {
        "orders": int(mysql_scalar("SELECT COUNT(*) FROM billing_orders") or "0"),
        "invoices": int(mysql_scalar("SELECT COUNT(*) FROM billing_invoices") or "0"),
        "transactions": int(mysql_scalar("SELECT COUNT(*) FROM billing_transactions") or "0"),
        "billing_grants": int(mysql_scalar("SELECT COUNT(*) FROM entitlement_grants WHERE source_type='billing'") or "0"),
    }
    deltas = {key: after[key] - before[key] for key in before}
    code = error_code(order_body)
    payload["details"].update({
        "billing_order_http_status": order_status,
        "billing_order_error_code": code,
        "durable_write_deltas": deltas,
        "expected_first_failure": "auth_dependency_unavailable",
        "candidate_severity": "P1",
        "candidate_boundary": "P15 real customer session authority is not composed into the P13 Billing workspace API",
    })

    zero_writes = all(value == 0 for value in deltas.values())
    if order_status == 503 and code == "auth_dependency_unavailable" and zero_writes:
        payload["errors"].append(
            "P20-T021 blocked: real authenticated P15 owner session is rejected by Billing with 503 auth_dependency_unavailable and zero durable billing writes"
        )
    else:
        payload["errors"].append(
            f"P20-T021 discovery did not reproduce the expected Billing auth boundary failure: HTTP {order_status}, code={code!r}, deltas={deltas}"
        )
    write(payload)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
