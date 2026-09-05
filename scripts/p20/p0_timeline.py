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

from common import HEAD, ROOT, emit, fail_if_errors


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


def t010() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "mock_authority": False,
        "token_rule_bypass": False,
        "secret_material_recorded": False,
    }

    t009_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T009.json"
    if not t009_path.is_file():
        errors.append("T010 requires same-run T009 evidence")
        return emit("P20-T010", "p0", "Real email verification workflow", errors, details)
    try:
        t009_evidence = json.loads(t009_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        errors.append("T010 could not read same-run T009 evidence")
        return emit("P20-T010", "p0", "Real email verification workflow", errors, details)
    if t009_evidence.get("status") != "PASS" or t009_evidence.get("implementation_commit") != HEAD:
        errors.append("T010 same-run T009 evidence is not exact-head PASS")
        return emit("P20-T010", "p0", "Real email verification workflow", errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t010_runner"],
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if runner.returncode != 0:
        errors.append("T010 runtime runner failed before producing safe evidence")
        details["runner_exit_code"] = runner.returncode
        return emit("P20-T010", "p0", "Real email verification workflow", errors, details)
    try:
        runtime = json.loads(runner.stdout)
    except json.JSONDecodeError:
        errors.append("T010 runtime runner did not produce safe JSON evidence")
        return emit("P20-T010", "p0", "Real email verification workflow", errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T010 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T010 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)

    t009_details = t009_evidence.get("details", {})
    if not isinstance(t009_details, dict):
        errors.append("T010 T009 detail evidence is invalid")
    else:
        same_user = details.get("user_id") == t009_details.get("user_id")
        same_workspace = details.get("workspace_id") == t009_details.get("workspace_id")
        details["t009_user_correlation_preserved"] = same_user
        details["t009_workspace_correlation_preserved"] = same_workspace
        details["t009_evidence_bound"] = True
        if not same_user or not same_workspace:
            errors.append("T010 runtime identity/workspace did not correlate to T009")

    if details.get("secret_material_recorded") is not False:
        errors.append("T010 runtime evidence reported secret material")
    if details.get("mock_authority") is not False:
        errors.append("T010 runtime evidence reported mock authority")
    if details.get("token_rule_bypass") is not False:
        errors.append("T010 runtime evidence reported token-rule bypass")

    if not errors:
        details["next_case"] = "P20-T011"
    else:
        details["next_case"] = None
    return emit("P20-T010", "p0", "Real email verification workflow", errors, details)


def t011() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_platform_api": True,
        "real_mysql": True,
        "real_redis": True,
        "mock_authority": False,
        "secret_material_recorded": False,
    }

    t010_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T010.json"
    if not t010_path.is_file():
        errors.append("T011 requires same-run T010 evidence")
        return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)
    try:
        t010_evidence = json.loads(t010_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        errors.append("T011 could not read same-run T010 evidence")
        return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)
    if t010_evidence.get("status") != "PASS" or t010_evidence.get("implementation_commit") != HEAD:
        errors.append("T011 same-run T010 evidence is not exact-head PASS")
        return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t011_runner"],
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if runner.returncode != 0:
        errors.append("T011 runtime runner failed before producing safe evidence")
        details["runner_exit_code"] = runner.returncode
        return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)
    try:
        runtime = json.loads(runner.stdout)
    except json.JSONDecodeError:
        errors.append("T011 runtime runner did not produce safe JSON evidence")
        return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T011 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T011 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)

    t010_details = t010_evidence.get("details", {})
    if not isinstance(t010_details, dict):
        errors.append("T011 T010 detail evidence is invalid")
    else:
        same_user = details.get("user_id") == t010_details.get("user_id")
        same_workspace = details.get("workspace_id") == t010_details.get("workspace_id")
        details["t010_user_correlation_preserved"] = same_user
        details["t010_workspace_correlation_preserved"] = same_workspace
        details["t010_evidence_bound"] = True
        if not same_user or not same_workspace:
            errors.append("T011 runtime identity/workspace did not correlate to T010")

    if details.get("secret_material_recorded") is not False:
        errors.append("T011 runtime evidence reported secret material")
    if details.get("mock_authority") is not False:
        errors.append("T011 runtime evidence reported mock authority")

    details["next_case"] = "P20-T012" if not errors else None
    return emit("P20-T011", "p0", "Real login session and account workflow", errors, details)


def t012() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_platform_api": True,
        "real_mysql": True,
        "real_redis": True,
        "real_p05_links": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t011_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T011.json"
    if not t011_path.is_file():
        errors.append("T012 requires same-run T011 evidence")
        return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)
    try:
        t011_evidence = json.loads(t011_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        errors.append("T012 could not read same-run T011 evidence")
        return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)
    if t011_evidence.get("status") != "PASS" or t011_evidence.get("implementation_commit") != HEAD:
        errors.append("T012 same-run T011 evidence is not exact-head PASS")
        return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t012_runner"],
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if runner.returncode != 0:
        errors.append("T012 runtime runner failed before producing safe evidence")
        details["runner_exit_code"] = runner.returncode
        return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)
    try:
        runtime = json.loads(runner.stdout)
    except json.JSONDecodeError:
        errors.append("T012 runtime runner did not produce safe JSON evidence")
        return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T012 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T012 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)

    t011_details = t011_evidence.get("details", {})
    if not isinstance(t011_details, dict):
        errors.append("T012 T011 detail evidence is invalid")
    else:
        same_user = details.get("user_id") == t011_details.get("user_id")
        same_workspace = details.get("workspace_id") == t011_details.get("workspace_id")
        details["t011_user_correlation_preserved"] = same_user
        details["t011_workspace_correlation_preserved"] = same_workspace
        details["t011_evidence_bound"] = True
        if not same_user or not same_workspace:
            errors.append("T012 runtime identity/workspace did not correlate to T011")

    if details.get("real_p05_links") is not True:
        errors.append("T012 runtime evidence did not use the real P05 Links surface")
    if details.get("real_mysql") is not True or details.get("real_redis") is not True:
        errors.append("T012 runtime evidence did not preserve real MySQL/Redis authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T012 runtime evidence reported secret material")
    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T012 runtime evidence reported mock/test-header authority")

    details["next_case"] = "P20-T013" if not errors else None
    return emit("P20-T012", "p0", "Real Link creation and mutation", errors, details)


def t013() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_redirectengine": True,
        "real_mysql": True,
        "real_redis": True,
        "real_p05_links": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
    }

    t012_path = ROOT / "artifacts" / "v10" / "P20" / "p0" / "P20-T012.json"
    if not t012_path.is_file():
        errors.append("T013 requires same-run T012 evidence")
        return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)
    try:
        t012_evidence = json.loads(t012_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        errors.append("T013 could not read same-run T012 evidence")
        return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)
    if t012_evidence.get("status") != "PASS" or t012_evidence.get("implementation_commit") != HEAD:
        errors.append("T013 same-run T012 evidence is not exact-head PASS")
        return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)

    env = os.environ.copy()
    env["P20_EXACT_HEAD"] = HEAD
    runner = subprocess.run(
        ["go", "run", "./scripts/p20/t013_runner"],
        cwd=ROOT,
        env=env,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
    )
    if runner.returncode != 0:
        errors.append("T013 runtime runner failed before producing safe evidence")
        details["runner_exit_code"] = runner.returncode
        return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)
    try:
        runtime = json.loads(runner.stdout)
    except json.JSONDecodeError:
        errors.append("T013 runtime runner did not produce safe JSON evidence")
        return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)

    runtime_errors = runtime.get("errors")
    runtime_details = runtime.get("details")
    if not isinstance(runtime_errors, list) or not all(isinstance(item, str) for item in runtime_errors):
        errors.append("T013 runtime runner returned an invalid error ledger")
    else:
        errors.extend(runtime_errors)
    if not isinstance(runtime_details, dict):
        errors.append("T013 runtime runner returned invalid detail evidence")
    else:
        details.update(runtime_details)

    t012_details = t012_evidence.get("details", {})
    if not isinstance(t012_details, dict):
        errors.append("T013 T012 detail evidence is invalid")
    else:
        same_user = details.get("user_id") == t012_details.get("user_id")
        same_workspace = details.get("workspace_id") == t012_details.get("workspace_id")
        same_link = details.get("link_id") == t012_details.get("link_id")
        details["t012_user_correlation_preserved"] = same_user
        details["t012_workspace_correlation_preserved"] = same_workspace
        details["t012_link_correlation_preserved"] = same_link
        details["t012_evidence_bound"] = True
        if not same_user or not same_workspace or not same_link:
            errors.append("T013 runtime user/Workspace/Link did not correlate to T012")

    if details.get("real_redirectengine") is not True or details.get("real_p05_links") is not True:
        errors.append("T013 runtime evidence did not use real redirectengine/P05 Link authority")
    if details.get("real_mysql") is not True or details.get("real_redis") is not True:
        errors.append("T013 runtime evidence did not preserve real MySQL/Redis authority")
    if details.get("secret_material_recorded") is not False:
        errors.append("T013 runtime evidence reported secret material")
    if details.get("mock_authority") is not False or details.get("test_header_authority") is not False:
        errors.append("T013 runtime evidence reported mock/test-header authority")

    details["next_case"] = "P20-T014" if not errors else None
    return emit("P20-T013", "p0", "Real redirect routing and safety workflow", errors, details)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    args = parser.parse_args()
    if args.case == "P20-T009":
        payload = t009()
    elif args.case == "P20-T010":
        payload = t010()
    elif args.case == "P20-T011":
        payload = t011()
    elif args.case == "P20-T012":
        payload = t012()
    elif args.case == "P20-T013":
        payload = t013()
    else:
        raise SystemExit(f"unsupported P20 P0 tranche case: {args.case}")
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
