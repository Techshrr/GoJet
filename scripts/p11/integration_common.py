#!/usr/bin/env python3
from __future__ import annotations

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
ROOT = Path("artifacts/v10/P11")
API_DIR, HEADER_DIR, SITEMAP_DIR, BROWSER_DIR, CAPTURE_DIR, RUNTIME_DIR, RESULT_DIR = (
    ROOT / "api", ROOT / "headers", ROOT / "sitemap", ROOT / "browser", ROOT / "captures", ROOT / "runtime", ROOT / "results"
)
for directory in (API_DIR, HEADER_DIR, SITEMAP_DIR, BROWSER_DIR, CAPTURE_DIR, RUNTIME_DIR, RESULT_DIR):
    directory.mkdir(parents=True, exist_ok=True)

MANAGE = {"X-GoJet-Test-Actor": "p11-owner", "X-GoJet-Test-Workspace-Role": "owner"}
VIEWER = {"X-GoJet-Test-Actor": "p11-viewer", "X-GoJet-Test-Workspace-Role": "viewer"}

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)

def auth_headers(workspace: str, base=None):
    headers = dict(base or MANAGE)
    headers["X-GoJet-Test-Workspace"] = workspace
    headers.setdefault("X-Request-ID", f"p11-{workspace}")
    return headers

def http_request(method: str, path: str, *, json_body=None, headers=None):
    body = None
    request_headers = dict(headers or {})
    if json_body is not None:
        body = json.dumps(json_body, ensure_ascii=False).encode("utf-8")
        request_headers["Content-Type"] = "application/json"
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

def child(label: str, url: str, position: int, child_id: int | None = None):
    value = {"position": position, "label": label, "destination_url": url}
    if child_id:
        value["id"] = child_id
    return value

def create_page(workspace: str, *, title="P11 Bio", bio="P11 profile", links=None, extra=None):
    payload = {"title": title, "bio": bio, "links": links or [], "change_reason": "P11 exact-head evidence"}
    if extra:
        payload.update(extra)
    status, _, raw, parsed = json_request("POST", f"/api/workspaces/{workspace}/bio-pages", body=payload, workspace=workspace)
    expect(status == 201, f"create status={status} body={raw[:400]!r}")
    expect(isinstance(parsed, dict), "create response is not JSON")
    return parsed

def get_page(workspace: str, page_id: int, *, headers=None):
    return json_request("GET", f"/api/workspaces/{workspace}/bio-pages/{page_id}", workspace=workspace, headers=headers)

def list_pages(workspace: str, *, headers=None):
    return json_request("GET", f"/api/workspaces/{workspace}/bio-pages?limit=100&offset=0", workspace=workspace, headers=headers)

def update_page(workspace: str, page_id: int, version: int, **changes):
    payload = {"expected_version": version, "change_reason": "P11 exact-head update"}
    payload.update(changes)
    return json_request("PATCH", f"/api/workspaces/{workspace}/bio-pages/{page_id}", body=payload, workspace=workspace)

def transition_page(workspace: str, page_id: int, version: int, action: str):
    payload = {"expected_version": version, "change_reason": f"P11 {action} evidence"}
    return json_request("POST", f"/api/workspaces/{workspace}/bio-pages/{page_id}/{action}", body=payload, workspace=workspace)

def delete_page(workspace: str, page_id: int, version: int):
    payload = {"expected_version": version, "change_reason": "P11 delete evidence"}
    return json_request("DELETE", f"/api/workspaces/{workspace}/bio-pages/{page_id}", body=payload, workspace=workspace)

def public_page(slug: str):
    return http_request("GET", f"/p/{urllib.parse.quote(slug, safe='')}")

def public_api(slug: str):
    return http_request("GET", f"/api/public/bio/{urllib.parse.quote(slug, safe='')}")

def body_text(raw: bytes) -> str:
    return raw.decode("utf-8", "replace")

def headers_lower(headers):
    return {k.lower(): v for k, v in headers.items()}

def seed_risk(child_record: dict, state: str, *, ttl_seconds=1800, policy="p11-evidence-v1") -> dict:
    expect(state in {"allow", "review", "block"}, f"invalid risk state {state}")
    now = dt.datetime.now(dt.timezone.utc)
    decision = {
        "schema_version": 1,
        "decision": state,
        "fingerprint": child_record["destination_fingerprint"],
        "checked_at": now.isoformat().replace("+00:00", "Z"),
        "valid_until": (now + dt.timedelta(seconds=ttl_seconds)).isoformat().replace("+00:00", "Z"),
        "policy_version": policy,
    }
    key = f"risk:bio-child:{child_record['id']}:{child_record['destination_fingerprint']}"
    cmd = ["redis-cli", "-h", os.environ.get("GOJET_TEST_REDIS_HOST", "127.0.0.1"), "-p", os.environ.get("GOJET_TEST_REDIS_PORT", "6379"), "SET", key, json.dumps(decision, separators=(",", ":")), "EX", str(ttl_seconds)]
    output = subprocess.check_output(cmd, text=True).strip()
    expect(output == "OK", f"redis seed failed: {output}")
    return {"key": key, "decision": decision}

def delete_risk(child_record: dict) -> None:
    key = f"risk:bio-child:{child_record['id']}:{child_record['destination_fingerprint']}"
    subprocess.check_call(["redis-cli", "-h", os.environ.get("GOJET_TEST_REDIS_HOST", "127.0.0.1"), "-p", os.environ.get("GOJET_TEST_REDIS_PORT", "6379"), "DEL", key], stdout=subprocess.DEVNULL)

def mysql_scalar(sql: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
    cmd = ["mysql", "--protocol=tcp", "-h", os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1"), "-P", os.environ.get("GOJET_TEST_MYSQL_PORT", "3306"), "-u", os.environ.get("GOJET_TEST_MYSQL_USER", "root"), "-N", "-B", os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test"), "-e", sql]
    return subprocess.check_output(cmd, env=env, text=True).strip()

def record(case_id: str, observations: dict, errors: list[str], directory: Path) -> Path:
    path = directory / f"{case_id}.json"
    payload = {"case_id": case_id, "implementation_commit": HEAD, "status": "PASS" if not errors else "FAIL", "errors": errors, "observations": observations}
    path.write_text(json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
    return path
