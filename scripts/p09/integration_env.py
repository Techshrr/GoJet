#!/usr/bin/env python3
from __future__ import annotations

import argparse
import concurrent.futures
import contextlib
import http.client
import json
import os
from pathlib import Path
import shutil
import socket
import stat
import subprocess
import sys
import time
import urllib.parse
import uuid

CASE_IDS = {f"P09-T{i:03d}" for i in range(1, 19)}
PLATFORM = os.environ.get("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081").rstrip("/")
MYSQL_HOST = os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = os.environ.get("GOJET_TEST_MYSQL_PORT", "3306")
MYSQL_USER = os.environ.get("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
STORAGE_ROOT = Path(os.environ.get("GOJET_FILE_STORAGE_ROOT", "/tmp/gojet-p09/storage"))
WORKER = Path(os.environ.get("GOJET_P09_FILEWORKER", "/tmp/gojet-p09/fileworker"))
REAL_CLAMD = os.environ.get("GOJET_P09_REAL_CLAMD_ADDRESS", "127.0.0.1:3310")
FAULT_SERVER = Path(os.environ.get("GOJET_P09_FAULT_SERVER", "scripts/p09/clamd_fault_server.py"))
HEAD = os.environ.get("GITHUB_SHA") or subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
RESULT_ROOT = Path("artifacts/v10/P09/results")
CLAM_ROOT = Path("artifacts/v10/P09/clamav")
RUNTIME_ROOT = Path("artifacts/v10/P09/runtime")
EICAR = b"X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
BENIGN = b"GoJet P09 clean integration fixture.\n"

class Failure(RuntimeError):
    pass

def expect(condition: bool, message: str) -> None:
    if not condition:
        raise Failure(message)

def mysql(sql: str, *, check: bool = True) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    cmd = [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT),
        "-u", MYSQL_USER, "-N", "-B", MYSQL_DATABASE, "-e", sql,
    ]
    completed = subprocess.run(cmd, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if check and completed.returncode != 0:
        raise Failure(f"mysql failed rc={completed.returncode}: {completed.stderr.strip()}")
    return completed.stdout.strip()

def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"

def reset_case() -> None:
    mysql("""SET FOREIGN_KEY_CHECKS=0;
TRUNCATE TABLE file_audit_events;
TRUNCATE TABLE file_scan_attempts;
TRUNCATE TABLE files;
TRUNCATE TABLE file_workspace_counters;
SET FOREIGN_KEY_CHECKS=1;""")
    for dirname in ("quarantine", "published"):
        directory = STORAGE_ROOT / dirname
        directory.mkdir(parents=True, exist_ok=True)
        os.chmod(directory, 0o700)
        for child in directory.iterdir():
            if child.is_dir() and not child.is_symlink():
                shutil.rmtree(child)
            else:
                child.unlink(missing_ok=True)

def conn_target():
    parsed = urllib.parse.urlsplit(PLATFORM)
    expect(parsed.scheme == "http", "P09 integration expects native local HTTP test endpoint")
    return parsed.hostname or "127.0.0.1", parsed.port or 80

def request(method: str, path: str, body: bytes | None = None, headers: dict[str, str] | None = None):
    host, port = conn_target()
    conn = http.client.HTTPConnection(host, port, timeout=10)
    request_headers = dict(headers or {})
    try:
        conn.request(method, path, body=body, headers=request_headers)
        response = conn.getresponse()
        payload = response.read()
        return response.status, dict(response.getheaders()), payload
    finally:
        conn.close()

def ws_headers(workspace: str, role: str = "admin", actor: str = "p09-integration") -> dict[str, str]:
    return {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Workspace-Role": role,
        "X-GoJet-Test-Workspace": workspace,
        "X-Request-ID": f"p09-{uuid.uuid4().hex}",
    }

def json_request(method: str, path: str, workspace: str, payload=None, role: str = "admin", actor: str = "p09-integration"):
    headers = ws_headers(workspace, role, actor)
    body = None
    if payload is not None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        headers["Content-Type"] = "application/json"
    status, response_headers, raw = request(method, path, body, headers)
    decoded = None
    if raw:
        try:
            decoded = json.loads(raw)
        except json.JSONDecodeError:
            decoded = raw.decode("utf-8", "replace")
    return status, response_headers, raw, decoded

def upload(workspace: str, filename: str, content_type: str, payload: bytes, role: str = "admin", actor: str = "p09-integration", reason: str = "P09 integration upload"):
    boundary = "----gojetp09" + uuid.uuid4().hex
    parts = [
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"change_reason\"\r\n\r\n{reason}\r\n".encode(),
        f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\nContent-Type: {content_type}\r\n\r\n".encode() + payload + b"\r\n",
        f"--{boundary}--\r\n".encode(),
    ]
    headers = ws_headers(workspace, role, actor)
    headers["Content-Type"] = f"multipart/form-data; boundary={boundary}"
    status, response_headers, raw = request("POST", f"/api/workspaces/{workspace}/files", b"".join(parts), headers)
    decoded = None
    if raw:
        try:
            decoded = json.loads(raw)
        except json.JSONDecodeError:
            decoded = raw.decode("utf-8", "replace")
    return status, response_headers, raw, decoded

def get_resource(workspace: str, file_id: int, role: str = "admin"):
    return json_request("GET", f"/api/workspaces/{workspace}/files/{file_id}", workspace, role=role)

def action(workspace: str, file_id: int, name: str, role: str = "admin", reason: str | None = None):
    return json_request(
        "POST", f"/api/workspaces/{workspace}/files/{file_id}/{name}", workspace,
        {"change_reason": reason or f"P09 integration {name}"}, role=role,
    )

def patch_policy(workspace: str, file_id: int, payload: dict, role: str = "admin"):
    body = dict(payload)
    body.setdefault("change_reason", "P09 integration policy update")
    return json_request("PATCH", f"/api/workspaces/{workspace}/files/{file_id}", workspace, body, role=role)

def delete_resource(workspace: str, file_id: int, role: str = "admin"):
    return json_request(
        "DELETE", f"/api/workspaces/{workspace}/files/{file_id}", workspace,
        {"change_reason": "P09 integration delete"}, role=role,
    )

