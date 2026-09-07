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
OUT = ROOT / "artifacts" / "v10" / "P20" / "runtime" / "d015-billing-session" / "regression.json"


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
    payload = {
        "schema": "gojet.p20-d015-billing-session-regression.v1",
        "case": "P20-D015-REGRESSION",
        "implementation_commit": HEAD,
        "status": "FAIL",
        "errors": [],
        "details": {
            "real_mysql": True,
            "real_platform_api": True,
            "real_p15_session": True,
            "real_workspace_membership": True,
            "mock_authority": False,
            "test_header_authority": False,
            "formal_p20_t021_claim": False,
            "secret_material_recorded": False,
        },
    }

    evidence_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T011.json"
    try:
        t011 = json.loads(evidence_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        payload["errors"].append("D015 regression requires same-run P20-T011 evidence")
        write(payload)
        return 1
    if t011.get("status") != "PASS" or t011.get("implementation_commit") != HEAD or t011.get("errors") != []:
        payload["errors"].append("D015 regression predecessor P20-T011 is not exact-head PASS")
        write(payload)
        return 1

    details = t011.get("details") if isinstance(t011.get("details"), dict) else {}
    user_id = str(details.get("user_id") or "").strip()
    workspace_id = str(details.get("workspace_id") or "").strip()
    if not user_id or not workspace_id or details.get("workspace_role") != "owner":
        payload["errors"].append("D015 regression lacks the correlated active owner identity")
        write(payload)
        return 1
    payload["details"].update({
        "user_id": user_id,
        "workspace_id": workspace_id,
        "workspace_role": "owner",
        "t011_evidence_bound": True,
    })

    suffix = HEAD[:12].lower()
    login_status, login_headers, login_body = request_json(
        "POST",
        "/api/auth/login",
        {
            "email": f"p20-t009-{suffix}@example.test",
            "password": "P20-T009!Strong-Passphrase-2026",
            "correlation_id": f"p20-d015-regression-login-{suffix}",
        },
    )
    cookie = session_cookie(login_headers)
    payload["details"]["login_http_status"] = login_status
    payload["details"]["real_session_authenticated"] = login_status == 200 and login_body.get("status") == "authenticated" and bool(cookie)
    if not payload["details"]["real_session_authenticated"]:
        payload["errors"].append("D015 regression could not establish a real P15 customer session")
        write(payload)
        return 1

    cookie_header = f"__Host-gojet_session={cookie}"
    me_status, _, me_body = request_json("GET", "/api/me", headers={"Cookie": cookie_header})
    csrf = str(me_body.get("csrf_token") or "").strip()
    session = me_body.get("session") if isinstance(me_body.get("session"), dict) else {}
    session_id = str(session.get("id") or "").strip()
    payload["details"].update({
        "me_http_status": me_status,
        "session_identity_preserved": me_status == 200 and isinstance(me_body.get("user"), dict) and me_body["user"].get("id") == user_id,
        "csrf_authority_issued": bool(csrf),
        "session_id_present": bool(session_id),
    })
    if not payload["details"]["session_identity_preserved"] or not csrf or not session_id:
        payload["errors"].append("D015 regression could not bind real P15 session/CSRF authority")
        write(payload)
        return 1

    before = {
        "orders": int(mysql_scalar("SELECT COUNT(*) FROM billing_orders") or "0"),
        "invoices": int(mysql_scalar("SELECT COUNT(*) FROM billing_invoices") or "0"),
        "transactions": int(mysql_scalar("SELECT COUNT(*) FROM billing_transactions") or "0"),
        "billing_grants": int(mysql_scalar("SELECT COUNT(*) FROM entitlement_grants WHERE source_type='billing'") or "0"),
    }
    max_plan = int(mysql_scalar("SELECT COALESCE(MAX(id),0) FROM billing_plans") or "0")
    missing_plan_id = max_plan + 1000000
    order_status, _, order_body = request_json(
        "POST",
        f"/api/workspaces/{workspace_id}/orders",
        {"plan_id": missing_plan_id, "kind": "new"},
        headers={
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": csrf,
            "Idempotency-Key": f"p20-d015-regression-order-{suffix}",
            "X-Request-ID": f"p20-d015-regression-order-{suffix}",
            "X-GoJet-Test-Actor": "spoofed-d015-actor",
            "X-GoJet-Test-Email": "spoofed-d015@example.test",
            "X-GoJet-Test-Workspace-Role": "viewer",
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
    zero_writes = all(value == 0 for value in deltas.values())
    boundary_passed = order_status == 404 and code == "not_found" and zero_writes
    payload["details"].update({
        "billing_order_http_status": order_status,
        "billing_order_error_code": code,
        "missing_plan_id": missing_plan_id,
        "durable_write_deltas": deltas,
        "billing_session_authority_composed": boundary_passed,
        "owner_rbac_passed_to_store": boundary_passed,
        "spoofed_test_headers_ignored": boundary_passed,
        "invalid_plan_failed_without_write": zero_writes,
    })

    if not boundary_passed:
        payload["errors"].append(
            f"D015 regression expected authenticated owner request to reach Billing store and fail only on missing plan: HTTP {order_status}, code={code!r}, deltas={deltas}"
        )
    else:
        payload["status"] = "PASS"
    write(payload)
    return 0 if not payload["errors"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
