#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import subprocess
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

from common import HEAD, emit, fail_if_errors


@dataclass
class HTTPResult:
    status: int
    body: dict[str, Any]
    headers: dict[str, str]


def sql_quote(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def mysql(query: str) -> list[list[str]]:
    host = os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
    port = os.environ.get("GOJET_TEST_MYSQL_PORT", "3306")
    user = os.environ.get("GOJET_TEST_MYSQL_USER", "root")
    password = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "")
    database = os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
    env = os.environ.copy()
    env["MYSQL_PWD"] = password
    output = subprocess.check_output(
        [
            "mysql",
            "--protocol=tcp",
            "-h",
            host,
            "-P",
            port,
            "-u",
            user,
            "--batch",
            "--skip-column-names",
            "--raw",
            database,
            "-e",
            query,
        ],
        text=True,
        env=env,
    )
    rows: list[list[str]] = []
    for line in output.splitlines():
        if line.strip():
            rows.append(line.split("\t"))
    return rows


def http_json(method: str, path: str, payload: dict[str, Any] | None = None) -> HTTPResult:
    base = os.environ.get("GOJET_P20_API_BASE", "http://127.0.0.1:18081").rstrip("/")
    body = None if payload is None else json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        base + path,
        data=body,
        method=method,
        headers={
            "Accept": "application/json",
            "Content-Type": "application/json",
            "X-Request-ID": f"p20-{HEAD[:12]}-{path.rsplit('/', 1)[-1] or 'root'}",
        },
    )
    try:
        response = urllib.request.urlopen(request, timeout=10)
        raw = response.read().decode("utf-8")
        status = response.status
        headers = {key.lower(): value for key, value in response.headers.items()}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8")
        status = exc.code
        headers = {key.lower(): value for key, value in exc.headers.items()}
    try:
        decoded = json.loads(raw) if raw else {}
    except json.JSONDecodeError:
        decoded = {"_non_json": True}
    return HTTPResult(status=status, body=decoded, headers=headers)


def scalar(query: str) -> str:
    rows = mysql(query)
    if not rows or not rows[0]:
        return ""
    return rows[0][0]


def count(query: str) -> int:
    value = scalar(query)
    return int(value or "0")


def t009() -> dict[str, Any]:
    errors: list[str] = []
    suffix = HEAD[:12].lower()
    email = f"p20-t009-{suffix}@example.test"
    invalid_email = f"p20-t009-invalid-{suffix}"
    display_name = "P20 Registration Candidate"
    password = "P20-T009!Strong-Passphrase-2026"
    correlation = f"p20-t009-{suffix}"

    # The case runs against a fresh CI database. Fail closed if stale candidate data exists.
    if count(f"SELECT COUNT(*) FROM auth_users WHERE email_normalized={sql_quote(email)}") != 0:
        errors.append("fresh T009 database already contains the candidate user")

    invalid = http_json(
        "POST",
        "/api/auth/register",
        {
            "email": invalid_email,
            "display_name": display_name,
            "password": password,
            "correlation_id": correlation + "-invalid",
        },
    )
    invalid_rows = count(f"SELECT COUNT(*) FROM auth_users WHERE email={sql_quote(invalid_email)}")
    if invalid.status != 400:
        errors.append(f"invalid registration did not fail validation: HTTP {invalid.status}")
    if invalid_rows != 0:
        errors.append("invalid registration produced a durable auth user")

    registered = http_json(
        "POST",
        "/api/auth/register",
        {
            "email": email,
            "display_name": display_name,
            "password": password,
            "correlation_id": correlation,
        },
    )
    if registered.status != 202 or registered.body.get("status") != "verification_required":
        errors.append(f"real registration API did not accept the candidate: HTTP {registered.status}")
    if registered.headers.get("cache-control") != "no-store":
        errors.append("registration response is missing Cache-Control: no-store")

    user_rows = mysql(
        "SELECT id,status FROM auth_users "
        f"WHERE email_normalized={sql_quote(email)} ORDER BY created_at"
    )
    user_id = user_rows[0][0] if len(user_rows) == 1 else ""
    user_status = user_rows[0][1] if len(user_rows) == 1 and len(user_rows[0]) > 1 else ""
    if len(user_rows) != 1:
        errors.append(f"registration persistence expected one auth user, got {len(user_rows)}")
    if user_status != "pending_verification":
        errors.append(f"new auth user has unexpected state {user_status!r}")

    grant_rows: list[list[str]] = []
    if user_id:
        grant_rows = mysql(
            "SELECT id,correlation_id FROM auth_one_time_grants "
            f"WHERE user_id={sql_quote(user_id)} AND purpose='email_verification' "
            "AND consumed_at IS NULL AND invalidated_at IS NULL ORDER BY created_at"
        )
    grant_id = grant_rows[0][0] if len(grant_rows) == 1 else ""
    grant_correlation = grant_rows[0][1] if len(grant_rows) == 1 and len(grant_rows[0]) > 1 else ""
    if len(grant_rows) != 1:
        errors.append(f"registration expected one live verification grant, got {len(grant_rows)}")
    if grant_correlation != correlation:
        errors.append("verification grant lost the T009 request correlation")

    mail_jobs = 0
    audit_rows = 0
    if user_id and grant_id:
        mail_jobs = count(
            "SELECT COUNT(*) FROM mail_jobs "
            "WHERE template_key='auth-email-verification' "
            "AND resource_type='auth_one_time_grant' "
            f"AND resource_id={sql_quote(grant_id)}"
        )
        audit_rows = count(
            "SELECT COUNT(*) FROM auth_audit_events "
            f"WHERE user_id={sql_quote(user_id)} "
            "AND action='auth.registration.created' "
            f"AND resource_id={sql_quote(grant_id)} "
            f"AND request_correlation_id={sql_quote(correlation)} AND result='success'"
        )
    if mail_jobs != 1:
        errors.append(f"registration expected one durable verification mail job, got {mail_jobs}")
    if audit_rows != 1:
        errors.append(f"registration expected one correlated audit record, got {audit_rows}")

    duplicate = http_json(
        "POST",
        "/api/auth/register",
        {
            "email": email,
            "display_name": display_name,
            "password": password,
            "correlation_id": correlation + "-duplicate",
        },
    )
    if duplicate.status != 409:
        errors.append(f"duplicate registration did not fail closed: HTTP {duplicate.status}")

    post_user_count = count(f"SELECT COUNT(*) FROM auth_users WHERE email_normalized={sql_quote(email)}")
    post_grant_count = count(
        "SELECT COUNT(*) FROM auth_one_time_grants "
        f"WHERE user_id={sql_quote(user_id)} AND purpose='email_verification'"
    ) if user_id else 0
    post_mail_count = count(
        "SELECT COUNT(*) FROM mail_jobs "
        f"WHERE resource_type='auth_one_time_grant' AND resource_id={sql_quote(grant_id)}"
    ) if grant_id else 0
    if (post_user_count, post_grant_count, post_mail_count) != (1, 1, 1):
        errors.append(
            "duplicate registration changed durable registration state: "
            f"users/grants/mail={post_user_count}/{post_grant_count}/{post_mail_count}"
        )

    workspace_rows = mysql(
        "SELECT w.id,m.role FROM workspace_memberships m "
        "JOIN workspaces w ON w.id=m.workspace_id "
        f"WHERE m.user_id={sql_quote(user_id)} ORDER BY w.created_at,w.id"
    ) if user_id else []
    workspace_id = workspace_rows[0][0] if len(workspace_rows) == 1 else ""
    workspace_role = workspace_rows[0][1] if len(workspace_rows) == 1 and len(workspace_rows[0]) > 1 else ""
    if len(workspace_rows) != 1 or workspace_role != "owner":
        errors.append(
            "registration does not establish exactly one real owner Workspace correlation; "
            f"found {len(workspace_rows)} membership(s)"
        )

    details = {
        "real_platform_api": True,
        "mock_authority": False,
        "registration_http_status": registered.status,
        "invalid_registration_http_status": invalid.status,
        "duplicate_registration_http_status": duplicate.status,
        "private_no_store": registered.headers.get("cache-control") == "no-store",
        "user_id": user_id or None,
        "user_status": user_status or None,
        "workspace_id": workspace_id or None,
        "workspace_role": workspace_role or None,
        "verification_grant_count": len(grant_rows),
        "verification_mail_job_count": mail_jobs,
        "registration_audit_count": audit_rows,
        "request_correlation_id": correlation,
        "invalid_write_count": invalid_rows,
        "duplicate_state_unchanged": (post_user_count, post_grant_count, post_mail_count) == (1, 1, 1),
        "secret_material_recorded": False,
        "next_case": "P20-T010" if not errors else None,
    }
    return emit("P20-T009", "p0", "Real registration workflow", errors, details)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    args = parser.parse_args()
    if args.case != "P20-T009":
        raise SystemExit(f"unsupported P20 P0 tranche case: {args.case}")
    payload = t009()
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
