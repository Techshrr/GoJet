#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import http.client
import json
import os
from pathlib import Path
import subprocess
from typing import Any
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[2]
HEAD = os.environ.get("GITHUB_SHA") or subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
BASE_URL = os.environ.get("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081").rstrip("/")
MYSQL_HOST = os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = os.environ.get("GOJET_TEST_MYSQL_PORT", "3306")
MYSQL_USER = os.environ.get("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test")

ROOT_EVIDENCE = ROOT / "artifacts" / "v10" / "P12"
API_DIR = ROOT_EVIDENCE / "api"
RBAC_DIR = ROOT_EVIDENCE / "rbac"
AUDIT_DIR = ROOT_EVIDENCE / "audit"
SECURITY_DIR = ROOT_EVIDENCE / "security"
RUNTIME_DIR = ROOT_EVIDENCE / "runtime"
for directory in (API_DIR, RBAC_DIR, AUDIT_DIR, SECURITY_DIR, RUNTIME_DIR):
    directory.mkdir(parents=True, exist_ok=True)

def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)

def iso_after(minutes: int) -> str:
    value = dt.datetime.now(dt.timezone.utc) + dt.timedelta(minutes=minutes)
    return value.isoformat(timespec="seconds").replace("+00:00", "Z")

def p12_headers(actor: str, email: str, *, display: str = "", forged_role: str | None = None, correlation: str | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Email": email,
        "X-GoJet-Test-Display-Name": display or actor,
    }
    if forged_role is not None:
        headers["X-GoJet-Test-Workspace-Role"] = forged_role
    if correlation:
        headers["X-Request-ID"] = correlation
    return headers

def legacy_headers(workspace: str, actor: str, *, role: str = "owner", analytics: bool = False, correlation: str | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Workspace": workspace,
        "X-GoJet-Test-Workspace-Role": role,
    }
    if analytics:
        headers["X-GoJet-Test-Analytics-Permission"] = "allow"
    if correlation:
        headers["X-Request-ID"] = correlation
    return headers

def http_request(method: str, path: str, *, json_body: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes]:
    parsed = urlsplit(BASE_URL)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=20)
    merged = {"Accept": "application/json"}
    if headers:
        merged.update(headers)
    body = None
    if json_body is not None:
        body = json.dumps(json_body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        merged["Content-Type"] = "application/json"
    conn.request(method, path, body=body, headers=merged)
    response = conn.getresponse()
    raw = response.read()
    response_headers = {k.lower(): v for k, v in response.getheaders()}
    status = response.status
    conn.close()
    return status, response_headers, raw

def request_json(method: str, path: str, *, body: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes, Any]:
    status, response_headers, raw = http_request(method, path, json_body=body, headers=headers)
    parsed = None
    if raw:
        try:
            parsed = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            pass
    return status, response_headers, raw, parsed

def p12(method: str, path: str, actor: str, email: str, *, body: Any | None = None, forged_role: str | None = None, correlation: str | None = None):
    return request_json(method, path, body=body, headers=p12_headers(actor, email, forged_role=forged_role, correlation=correlation))

def legacy(method: str, path: str, workspace: str, actor: str, *, role: str = "owner", body: Any | None = None, analytics: bool = False, correlation: str | None = None):
    return request_json(method, path, body=body, headers=legacy_headers(workspace, actor, role=role, analytics=analytics, correlation=correlation))

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

def seed_member(workspace: str, user: str, email: str, role: str, display: str | None = None) -> int:
    mysql(
        "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES ("
        + ",".join([
            sql_quote(workspace), sql_quote(user), sql_quote(email),
            sql_quote(display or user), sql_quote(role)
        ]) + ")"
    )
    return int(mysql(f"SELECT id FROM workspace_memberships WHERE workspace_id={sql_quote(workspace)} AND user_id={sql_quote(user)}"))

def create_workspace(actor: str, email: str, name: str, *, forged_role: str | None = None) -> tuple[dict[str, Any], dict[str, Any]]:
    status, _, raw, data = p12("POST", "/api/workspaces", actor, email, body={"name": name}, forged_role=forged_role, correlation=f"p12-create-{actor}")
    expect(status == 201 and isinstance(data, dict), f"create workspace status={status} body={raw[:300]!r}")
    return data["workspace"], data["membership"]

def create_link(workspace: str, actor: str, code: str, destination: str, *, role: str = "owner") -> dict[str, Any]:
    payload = {
        "hostname": "go.p12.test",
        "domain_kind": "official",
        "code": code,
        "title": f"P12 {code}",
        "primary_destination": destination,
        "redirect_status": 302,
        "routing": [],
        "ab": [],
        "utm": {},
        "access": {},
        "expires_at": None,
        "click_limit": None,
        "one_time": False,
        "change_reason": f"P12 create {code}",
    }
    status, _, raw, data = legacy("POST", f"/api/workspaces/{workspace}/links", workspace, actor, role=role, body=payload, correlation=f"p12-link-{code}")
    expect(status == 201 and isinstance(data, dict), f"create link status={status} body={raw[:300]!r}")
    return data

def producer(*args: str) -> dict[str, Any]:
    cmd = ["go", "run", "./scripts/p12/producer.go", *args]
    completed = subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, env=os.environ.copy())
    if completed.returncode != 0:
        raise AssertionError(f"producer failed {' '.join(cmd)}\nstdout={completed.stdout}\nstderr={completed.stderr}")
    try:
        return json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"producer non-JSON: {completed.stdout!r}") from exc

def produce_notification(workspace: str, recipient: str, dedupe: str, *, category: str = "resources", title: str = "P12 notice", summary: str = "", deep_link: str = "", resource_type: str = "", resource_id: str = "") -> dict[str, Any]:
    return producer(
        "--action", "notification", "--workspace", workspace, "--recipient", recipient,
        "--category", category, "--event-key", "p12.integration", "--dedupe-key", dedupe,
        "--title", title, "--summary", summary, "--deep-link", deep_link,
        "--resource-type", resource_type, "--resource-id", resource_id,
    )

def set_notification_state(workspace: str, state: str, reason: str) -> dict[str, Any]:
    return producer("--action", "notification-state", "--workspace", workspace, "--state", state, "--reason", reason)

def produce_analytics_event(workspace: str, link_id: int, campaign_id: str, sequence: int = 1) -> dict[str, Any]:
    return producer(
        "--action", "analytics-event", "--workspace", workspace, "--link", str(link_id),
        "--campaign", campaign_id, "--sequence", str(sequence), "--state", "complete", "--reason", "current",
    )

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

def query_string(**params: Any) -> str:
    return urlencode({k: v for k, v in params.items() if v is not None})
