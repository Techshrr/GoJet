#!/usr/bin/env python3
from __future__ import annotations

import concurrent.futures
import datetime as dt
import json
import os
from pathlib import Path
import subprocess
import urllib.error
import urllib.parse
import urllib.request

HEAD = os.environ.get("GITHUB_SHA") or subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
BASE_URL = os.environ.get("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081").rstrip("/")
ROOT = Path("artifacts/v10/P10")
API_DIR, HEADER_DIR, RUNTIME_DIR = ROOT / "api", ROOT / "headers", ROOT / "runtime"
for directory in (API_DIR, HEADER_DIR, RUNTIME_DIR):
    directory.mkdir(parents=True, exist_ok=True)

MANAGE = {"X-GoJet-Test-Actor": "p10-owner", "X-GoJet-Test-Workspace-Role": "owner"}
VIEWER = {"X-GoJet-Test-Actor": "p10-viewer", "X-GoJet-Test-Workspace-Role": "viewer"}

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)

def auth_headers(workspace: str, base=None):
    headers = dict(base or MANAGE)
    headers["X-GoJet-Test-Workspace"] = workspace
    headers.setdefault("X-Request-ID", f"p10-{workspace}")
    return headers

def http_request(method: str, path: str, *, json_body=None, form=None, headers=None, cookie=None):
    body = None
    request_headers = dict(headers or {})
    if json_body is not None:
        body = json.dumps(json_body, ensure_ascii=False).encode()
        request_headers["Content-Type"] = "application/json"
    elif form is not None:
        body = urllib.parse.urlencode(form).encode()
        request_headers["Content-Type"] = "application/x-www-form-urlencoded"
    if cookie:
        request_headers["Cookie"] = cookie
    request = urllib.request.Request(BASE_URL + path, data=body, headers=request_headers, method=method)
    opener = urllib.request.build_opener(NoRedirect)
    try:
        response = opener.open(request, timeout=10)
        return response.status, dict(response.headers.items()), response.read()
    except urllib.error.HTTPError as exc:
        return exc.code, dict(exc.headers.items()), exc.read()

def json_request(method: str, path: str, *, body=None, workspace=None, headers=None):
    merged = dict(headers or {})
    if workspace is not None:
        merged = auth_headers(workspace, merged or MANAGE)
    status, response_headers, raw = http_request(method, path, json_body=body, headers=merged)
    parsed = None
    if raw:
        try:
            parsed = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            pass
    return status, response_headers, raw, parsed

def create_share(workspace: str, *, title="P10 text", content="P10 content", visibility="private", password="", expires_at="", one_time=False):
    payload = {"title": title, "content": content, "visibility": visibility, "password": password, "expires_at": expires_at, "one_time": one_time, "change_reason": "P10 exact-head evidence"}
    status, _, raw, parsed = json_request("POST", f"/api/workspaces/{workspace}/text-shares", body=payload, workspace=workspace)
    expect(status == 201, f"create status={status} body={raw[:400]!r}")
    expect(isinstance(parsed, dict), "create response is not JSON")
    return parsed

def update_share(workspace: str, share_id: int, version: int, **changes):
    payload = {"expected_version": version, "change_reason": "P10 exact-head update"}
    payload.update(changes)
    return json_request("PATCH", f"/api/workspaces/{workspace}/text-shares/{share_id}", body=payload, workspace=workspace)

def delete_share(workspace: str, share_id: int, version: int):
    payload = {"expected_version": version, "change_reason": "P10 exact-head delete"}
    return json_request("DELETE", f"/api/workspaces/{workspace}/text-shares/{share_id}", body=payload, workspace=workspace)

def mysql_scalar(sql: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
    cmd = ["mysql", "--protocol=tcp", "-h", os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1"), "-P", os.environ.get("GOJET_TEST_MYSQL_PORT", "3306"), "-u", os.environ.get("GOJET_TEST_MYSQL_USER", "root"), "-N", "-B", os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test"), "-e", sql]
    return subprocess.check_output(cmd, env=env, text=True).strip()

def extract_cookie(headers):
    raw = headers.get("Set-Cookie", "")
    expect(raw and "=" in raw, "Set-Cookie missing/malformed")
    return raw.split(";", 1)[0]

def public_get(slug: str, *, cookie=None, download=False):
    suffix = "?download=1" if download else ""
    return http_request("GET", f"/t/{urllib.parse.quote(slug, safe='')}{suffix}", cookie=cookie)

def public_action(slug: str, *, cookie=None):
    return http_request("POST", f"/api/public/text/{urllib.parse.quote(slug, safe='')}", cookie=cookie)

def password_post(slug: str, password: str):
    return http_request("POST", f"/t/{urllib.parse.quote(slug, safe='')}", form={"password": password})

def body_text(raw: bytes) -> str:
    return raw.decode("utf-8", "replace")

def headers_lower(headers):
    return {k.lower(): v for k, v in headers.items()}

def record(case_id: str, observations: dict, errors: list[str], directory: Path) -> Path:
    path = directory / f"{case_id}.json"
    payload = {"case_id": case_id, "implementation_commit": HEAD, "status": "PASS" if not errors else "FAIL", "errors": errors, "observations": observations}
    path.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
    return path

def past_time(minutes=5) -> str:
    return (dt.datetime.now(dt.timezone.utc) - dt.timedelta(minutes=minutes)).isoformat().replace("+00:00", "Z")
