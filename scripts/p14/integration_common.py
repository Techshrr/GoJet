#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import http.client
import json
import os
from pathlib import Path
import subprocess
import time
from typing import Any, Callable
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
HEAD = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
BASE_URL = os.environ.get("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081").rstrip("/")
MYSQL_HOST = os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = os.environ.get("GOJET_TEST_MYSQL_PORT", "3306")
MYSQL_USER = os.environ.get("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
REDIS_ADDR = os.environ.get("GOJET_REDIS_ADDR", "127.0.0.1:6379")
TURNSTILE_TOKEN = os.environ.get("GOJET_TEST_SUPPORT_TURNSTILE_TOKEN", "")
TICKET_ADMIN = os.environ.get("GOJET_TEST_SUPPORT_TICKETS_ADMIN_ACTOR", "p14-ticket-admin")
MAIL_ADMIN = os.environ.get("GOJET_TEST_SUPPORT_MAIL_ADMIN_ACTOR", "p14-mail-admin")
PRODUCER = os.environ.get("GOJET_TEST_P14_PRODUCER", "/tmp/gojet-p14-producer")
STORAGE_ROOT = os.environ.get("GOJET_TEST_P14_ATTACHMENT_ROOT", "/tmp/gojet-p14/attachments")
CLAMAV_ADDRESS = os.environ.get("GOJET_TEST_P14_CLAMAV_ADDRESS", "127.0.0.1:3310")
SMTP_STATE = Path(os.environ.get("GOJET_TEST_P14_SMTP_STATE", "/tmp/gojet-p14/smtp-state.json"))
SMTP_MODE = Path(os.environ.get("GOJET_TEST_P14_SMTP_MODE", "/tmp/gojet-p14/smtp-mode"))

ROOT_EVIDENCE = ROOT / "artifacts" / "v10" / "P14"
CASE_DIRS = {
    1: "api", 2: "rbac", 3: "api", 4: "security", 5: "entitlement", 6: "entitlement", 7: "entitlement",
    8: "security", 9: "security", 10: "security", 11: "security", 12: "security", 13: "api",
    14: "mail", 15: "mail", 16: "mail", 17: "mail", 18: "notification", 19: "rbac", 20: "api", 21: "audit",
}
for name in set(CASE_DIRS.values()) | {"runtime", "results"}:
    (ROOT_EVIDENCE / name).mkdir(parents=True, exist_ok=True)


def expect(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def unique(prefix: str) -> str:
    return f"{prefix}-{time.time_ns():x}-{os.getpid():x}"[:120]


def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def mysql(query: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    cmd = [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", MYSQL_PORT,
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE, "-e", query,
    ]
    completed = subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, env=env)
    if completed.returncode != 0:
        raise AssertionError(f"mysql failed: {query}\nstdout={completed.stdout}\nstderr={completed.stderr}")
    return completed.stdout.strip()


def mysql_scalar(query: str) -> str:
    out = mysql(query)
    return out.splitlines()[0] if out else ""


def mysql_rows(query: str) -> list[list[str]]:
    out = mysql(query)
    return [line.split("\t") for line in out.splitlines()] if out else []


def redis(*args: str) -> str:
    host, port = REDIS_ADDR.rsplit(":", 1)
    completed = subprocess.run(["redis-cli", "-h", host, "-p", port, *args], cwd=ROOT, text=True, capture_output=True)
    if completed.returncode != 0:
        raise AssertionError(f"redis-cli failed: {completed.stderr}")
    return completed.stdout.strip()


def reset_p14() -> None:
    redis("FLUSHDB")
    mysql("SET FOREIGN_KEY_CHECKS=0; "
          "DELETE FROM mail_attempts; DELETE FROM mail_jobs; DELETE FROM support_ticket_attachments; "
          "DELETE FROM support_ticket_messages; DELETE FROM support_tickets; DELETE FROM support_public_contacts; "
          "DELETE FROM support_audit_events; DELETE FROM custom_domain_entitlement_requests; "
          "DELETE FROM custom_domain_entitlement_sources; DELETE FROM custom_domains; DELETE FROM custom_domain_usage; "
          "DELETE FROM workspace_notifications WHERE category='support'; SET FOREIGN_KEY_CHECKS=1;")
    mysql("UPDATE mail_settings SET enabled=1,version=1 WHERE settings_key='primary'")
    SMTP_MODE.parent.mkdir(parents=True, exist_ok=True)
    SMTP_MODE.write_text("success\n", encoding="utf-8")
    if SMTP_STATE.exists():
        SMTP_STATE.unlink()


def seed_workspace(case_id: str, *, suffix: str = "owner", role: str = "owner") -> tuple[str, str, str]:
    workspace = unique(f"ws-{case_id.lower()}-{suffix}")[:64]
    actor = unique(f"actor-{case_id.lower()}-{suffix}")[:128]
    email = f"{actor[:80]}@example.test"
    mysql(
        "INSERT INTO workspaces (id,name,status,version,created_by) VALUES ("
        + ",".join([sql_quote(workspace), sql_quote(f"{case_id} workspace"), "'active'", "1", sql_quote(actor)]) + ");"
        + "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES ("
        + ",".join([sql_quote(workspace), sql_quote(actor), sql_quote(email), sql_quote(actor), sql_quote(role)]) + ")"
    )
    return workspace, actor, email


def seed_member(workspace: str, case_id: str, *, suffix: str, role: str = "member") -> tuple[str, str]:
    actor = unique(f"actor-{case_id.lower()}-{suffix}")[:128]
    email = f"{actor[:80]}@example.test"
    mysql(
        "INSERT INTO workspace_memberships (workspace_id,user_id,email,display_name,role) VALUES ("
        + ",".join([sql_quote(workspace), sql_quote(actor), sql_quote(email), sql_quote(actor), sql_quote(role)]) + ")"
    )
    return actor, email


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


def ticket_admin_headers(*, correlation: str | None = None, extra: dict[str, str] | None = None, actor: str | None = None) -> dict[str, str]:
    actor = actor or TICKET_ADMIN
    return auth_headers(actor, f"{actor}@example.test", correlation=correlation, extra=extra)


def mail_admin_headers(*, correlation: str | None = None, extra: dict[str, str] | None = None, actor: str | None = None) -> dict[str, str]:
    actor = actor or MAIL_ADMIN
    return auth_headers(actor, f"{actor}@example.test", correlation=correlation, extra=extra)


def http_raw(method: str, path: str, *, body: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes, Any]:
    parsed = urlsplit(BASE_URL)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=30)
    merged = {"Accept": "application/json"}
    raw_body = None
    if body is not None:
        raw_body = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        merged["Content-Type"] = "application/json"
    if headers:
        merged.update(headers)
    conn.request(method, path, body=raw_body, headers=merged)
    response = conn.getresponse()
    raw = response.read()
    response_headers = {key.lower(): value for key, value in response.getheaders()}
    status = response.status
    conn.close()
    data = None
    if raw:
        try:
            data = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            pass
    return status, response_headers, raw, data


def support(method: str, path: str, actor: str, email: str, *, body: Any | None = None,
            correlation: str | None = None, idempotency: str | None = None, extra: dict[str, str] | None = None):
    headers = auth_headers(actor, email, correlation=correlation, extra=extra)
    if idempotency:
        headers["Idempotency-Key"] = idempotency
    return http_raw(method, path, body=body, headers=headers)


def admin_ticket(method: str, path: str, *, body: Any | None = None, correlation: str | None = None,
                 idempotency: str | None = None, actor: str | None = None):
    extra = {"Idempotency-Key": idempotency} if idempotency else None
    return http_raw(method, path, body=body, headers=ticket_admin_headers(correlation=correlation, extra=extra, actor=actor))


def admin_mail(method: str, path: str, *, body: Any | None = None, correlation: str | None = None,
               idempotency: str | None = None, actor: str | None = None):
    extra = {"Idempotency-Key": idempotency} if idempotency else None
    return http_raw(method, path, body=body, headers=mail_admin_headers(correlation=correlation, extra=extra, actor=actor))


def create_ticket(case_id: str, workspace: str, actor: str, email: str, *, category: str = "general",
                  subject: str | None = None, message: str | None = None, idempotency: str | None = None,
                  correlation: str | None = None, reset_replay: bool = True):
    if reset_replay:
        redis("DEL", "support:turnstile:replay:" + "0" * 64)  # harmless; FLUSHDB is used by cases.
    expect(TURNSTILE_TOKEN != "", "GOJET_TEST_SUPPORT_TURNSTILE_TOKEN is required")
    correlation = correlation or unique(f"{case_id}-ticket")
    idempotency = idempotency or unique(f"{case_id}-idem")
    return support(
        "POST", "/api/support/tickets", actor, email,
        body={
            "workspace_id": workspace,
            "category": category,
            "subject": subject or f"{case_id} support subject",
            "message": message or f"{case_id} support body",
            "turnstile_token": TURNSTILE_TOKEN,
        },
        correlation=correlation,
        idempotency=idempotency,
    )


def create_public_contact(case_id: str, *, idempotency: str | None = None, correlation: str | None = None,
                          token: str | None = None):
    correlation = correlation or unique(f"{case_id}-contact")
    idempotency = idempotency or unique(f"{case_id}-contact-idem")
    headers = {"Idempotency-Key": idempotency, "X-Request-ID": correlation}
    return http_raw(
        "POST", "/api/public/contact",
        body={
            "email": f"{case_id.lower()}@example.test",
            "name": "Integration User",
            "subject": f"{case_id} contact",
            "message": "Integration contact body",
            "turnstile_token": TURNSTILE_TOKEN if token is None else token,
        },
        headers=headers,
    )


def initial_message_id(ticket_id: str) -> str:
    return mysql_scalar(
        "SELECT id FROM support_ticket_messages WHERE ticket_id=" + sql_quote(ticket_id) + " ORDER BY created_at,id LIMIT 1"
    )


def producer(command: str, *args: str, expect_success: bool = True, env: dict[str, str] | None = None) -> tuple[int, Any, str]:
    proc_env = os.environ.copy()
    proc_env.setdefault("GOJET_TEST_P14_ATTACHMENT_ROOT", STORAGE_ROOT)
    proc_env.setdefault("GOJET_TEST_P14_CLAMAV_ADDRESS", CLAMAV_ADDRESS)
    if env:
        proc_env.update(env)
    completed = subprocess.run([PRODUCER, command, *args], cwd=ROOT, text=True, capture_output=True, env=proc_env)
    data: Any = None
    if completed.stdout.strip():
        try:
            data = json.loads(completed.stdout.strip().splitlines()[-1])
        except json.JSONDecodeError:
            data = None
    if expect_success and completed.returncode != 0:
        raise AssertionError(f"producer {command} failed rc={completed.returncode} stdout={completed.stdout} stderr={completed.stderr}")
    return completed.returncode, data, completed.stderr.strip()


def wait_for(predicate: Callable[[], bool], *, timeout: float = 20.0, interval: float = 0.2, message: str = "condition") -> None:
    deadline = time.monotonic() + timeout
    last_error: Exception | None = None
    while time.monotonic() < deadline:
        try:
            if predicate():
                return
        except Exception as exc:  # transient DB/process readiness only
            last_error = exc
        time.sleep(interval)
    if last_error:
        raise AssertionError(f"timed out waiting for {message}: {last_error}")
    raise AssertionError(f"timed out waiting for {message}")


def set_smtp_mode(mode: str) -> None:
    expect(mode in {"success", "transient", "terminal"}, f"invalid SMTP mode {mode}")
    SMTP_MODE.parent.mkdir(parents=True, exist_ok=True)
    SMTP_MODE.write_text(mode + "\n", encoding="utf-8")


def smtp_state() -> dict[str, Any]:
    if not SMTP_STATE.is_file():
        return {"connections": 0, "deliveries": 0, "transient_rejections": 0, "terminal_rejections": 0}
    return json.loads(SMTP_STATE.read_text(encoding="utf-8"))


def case_path(case_id: str) -> Path:
    number = int(case_id.rsplit("T", 1)[1])
    return ROOT_EVIDENCE / CASE_DIRS[number] / f"{case_id}.json"


def record(case_id: str, checks: dict[str, Any], *, status: str = "PASS", errors: list[str] | None = None) -> Path:
    errors = errors or []
    payload = {
        "case_id": case_id,
        "implementation_commit": HEAD,
        "status": status,
        "checks": checks,
        "errors": errors,
        "recorded_at": dt.datetime.now(dt.timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z"),
    }
    path = case_path(case_id)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return path


def run_case(case_id: str, function: Callable[[], dict[str, Any]]) -> None:
    try:
        checks = function()
        path = record(case_id, checks)
        print(json.dumps({"case_id": case_id, "status": "PASS", "evidence": str(path.relative_to(ROOT))}, sort_keys=True))
    except Exception as exc:
        path = record(case_id, {}, status="FAIL", errors=[f"{type(exc).__name__}: {exc}"])
        print(json.dumps({"case_id": case_id, "status": "FAIL", "evidence": str(path.relative_to(ROOT)), "error": str(exc)}, sort_keys=True))
        raise
