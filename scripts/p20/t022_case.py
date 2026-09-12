#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import re
import subprocess
import time
import http.client as http_client
from http.cookies import SimpleCookie
from typing import Any
from urllib.parse import urlsplit

from common import HEAD, ROOT, emit, fail_if_errors

CASE_ID = "P20-T022"
CASE_NAME = "Real notification production and deep-link workflow"


def predecessor(case: str) -> dict[str, Any]:
    path = ROOT / "artifacts" / "v10" / "P20" / "p0" / f"{case}.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"same-run {case} evidence unavailable: {exc}") from exc
    if data.get("status") != "PASS" or data.get("implementation_commit") != HEAD or data.get("errors") != []:
        raise RuntimeError(f"same-run {case} evidence is not exact-head PASS")
    return data


def q(value: str) -> str:
    return "'" + value.replace("\\", "\\\\").replace("'", "''") + "'"


def mysql(sql: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = os.environ.get("GOJET_TEST_MYSQL_PASSWORD", "root")
    proc = subprocess.run(
        [
            "mysql", "--protocol=tcp",
            "-h", os.environ.get("GOJET_TEST_MYSQL_HOST", "127.0.0.1"),
            "-P", os.environ.get("GOJET_TEST_MYSQL_PORT", "3306"),
            "-u", os.environ.get("GOJET_TEST_MYSQL_USER", "root"),
            "--default-character-set=utf8mb4", "-N", "-B",
            os.environ.get("GOJET_TEST_MYSQL_DATABASE", "gojet_test"),
            "-e", sql,
        ],
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )
    if proc.returncode:
        raise RuntimeError(proc.stderr.strip())
    return proc.stdout.strip()


def scalar(sql: str) -> str:
    value = mysql(sql)
    return value.splitlines()[0] if value else ""


def http(method: str, path: str, raw: bytes | None = None, headers: dict[str, str] | None = None):
    base = urlsplit(os.environ.get("GOJET_P20_API_BASE", "http://127.0.0.1:18081"))
    conn = http_client.HTTPConnection(base.hostname, base.port or 80, timeout=20)
    merged = {"Accept": "application/json"}
    if raw is not None:
        merged["Content-Type"] = "application/json"
    if headers:
        merged.update(headers)
    conn.request(method, path, body=raw, headers=merged)
    response = conn.getresponse()
    body = response.read()
    result = (response.status, response.getheaders(), body)
    conn.close()
    return result


def http_json(
    method: str,
    path: str,
    body: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
):
    raw = None if body is None else json.dumps(body, separators=(",", ":")).encode()
    status, response_headers, payload = http(method, path, raw, headers)
    try:
        decoded = json.loads(payload.decode()) if payload else {}
    except json.JSONDecodeError:
        decoded = {}
    return status, response_headers, decoded


def session_cookie(headers: list[tuple[str, str]]) -> str:
    for key, value in headers:
        if key.lower() == "set-cookie":
            cookie = SimpleCookie()
            cookie.load(value)
            morsel = cookie.get("__Host-gojet_session")
            if morsel and morsel.value:
                return morsel.value
    return ""


def main_case() -> dict[str, Any]:
    errors: list[str] = []
    details: dict[str, Any] = {
        "real_mysql": True,
        "real_redis": True,
        "real_platform_api": True,
        "server_side_notification_producer": True,
        "formal_p20_t022_claim": True,
        "mock_authority": False,
        "test_header_authority": False,
        "secret_material_recorded": False,
        "provider_identifiers_recorded": False,
        "user_pii_recorded": False,
    }

    def require(condition: bool, message: str) -> None:
        if not condition:
            errors.append(message)

    try:
        t020 = predecessor("P20-T020")
        t021 = predecessor("P20-T021")
    except RuntimeError as exc:
        errors.append(str(exc))
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    d20 = t020.get("details", {})
    d21 = t021.get("details", {})
    require(isinstance(d21, dict) and d21.get("next_case") == CASE_ID, "T021 did not unlock T022")
    require(isinstance(d20, dict) and d20.get("next_case") == "P20-T021", "T020 predecessor chain is invalid")
    user = str(d21.get("user_id") or "")
    workspace = str(d21.get("workspace_id") or "")
    require(bool(user and workspace) and d21.get("workspace_role") == "owner", "T021 lacks correlated owner identity")
    require(d20.get("user_id") == user and d20.get("workspace_id") == workspace, "T020/T021 correlation mismatch")
    require(d21.get("production_callback_verifier") is True, "T021 production callback authority missing")
    require(d21.get("payment_notification_count") == 1, "T021 payment notification count is not exactly one")
    paid_delta = d21.get("paid_write_deltas", {})
    duplicate_delta = d21.get("duplicate_write_deltas", {})
    require(isinstance(paid_delta, dict) and paid_delta.get("payment_notifications") == 1, "T021 paid callback did not create exactly one notification")
    require(isinstance(duplicate_delta, dict) and duplicate_delta.get("payment_notifications") == 0, "T021 duplicate callback notification dedupe failed")
    require(d21.get("mock_authority") is False and d21.get("test_header_authority") is False, "T021 used forbidden mock/test-header authority")
    require(d21.get("secret_material_recorded") is False, "T021 recorded secret material")
    for field in (
        "t020_full_lifecycle_proven",
        "real_smtp_delivery",
        "native_mailworker",
        "admin_reply_mail_job_bound",
        "p14_mail_smtp_retry_proven",
        "p14_audit_correlation_proven",
    ):
        require(d20.get(field) is True, f"T020 missing independent mail/audit authority: {field}")
    require(d20.get("admin_reply_mail_final_status") == "sent", "T020 support mail was not sent")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    safe_id = re.compile(r"^[A-Za-z0-9_:-]+$")
    require(bool(safe_id.fullmatch(user)) and bool(safe_id.fullmatch(workspace)), "correlated identity is not query-safe")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    ws = q(workspace)
    uid = q(user)
    rows = mysql(
        "SELECT id,workspace_id,recipient_user_id,category,event_key,dedupe_key,title,summary,"
        "COALESCE(deep_link,''),resource_type,resource_id,read_at IS NOT NULL "
        "FROM workspace_notifications "
        f"WHERE workspace_id={ws} AND recipient_user_id={uid} "
        "AND category='billing' AND event_key='payment_succeeded' ORDER BY id"
    ).splitlines()
    require(len(rows) == 1, "correlated payment notification is not exactly one durable row")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    cols = rows[0].split("\t")
    require(len(cols) == 12, "payment notification durable row shape is invalid")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    (
        notification_id_raw,
        row_workspace,
        row_recipient,
        category,
        event_key,
        dedupe_key,
        title,
        summary,
        deep_link,
        resource_type,
        order_id,
        read_flag,
    ) = cols
    notification_id = int(notification_id_raw)
    require(notification_id > 0, "notification id is invalid")
    require(row_workspace == workspace and row_recipient == user, "notification recipient/workspace correlation mismatch")
    require(category == "billing" and event_key == "payment_succeeded", "notification event/category mismatch")
    require(dedupe_key.startswith("billing:payment_succeeded:callback:"), "notification dedupe key is not server billing callback authority")
    require(title == "Payment received" and summary == "Your billing payment was confirmed.", "payment notification user-safe copy mismatch")
    require(deep_link == "/app/billing", "payment notification deep link is not the frozen safe billing route")
    require(resource_type == "billing_order" and bool(order_id), "payment notification resource binding is invalid")
    require(read_flag == "0", "fresh payment notification is unexpectedly pre-read")
    require(scalar(f"SELECT COUNT(*) FROM billing_orders WHERE id={q(order_id)} AND workspace_id={ws} AND status='paid'") == "1", "notification does not bind the paid correlated order")

    callback_rows = mysql(
        "SELECT p.provider_event_id,p.provider_transaction_id,p.correlation_id "
        "FROM payment_callback_events p "
        "JOIN billing_transactions t ON t.provider=p.provider AND t.provider_transaction_id=p.provider_transaction_id "
        f"WHERE t.workspace_id={ws} AND t.order_id={q(order_id)} AND p.status='processed' ORDER BY p.id"
    ).splitlines()
    require(len(callback_rows) == 1, "processed payment callback correlation is not exactly one")
    provider_event_id = provider_transaction_id = callback_correlation = ""
    if len(callback_rows) == 1:
        callback_cols = callback_rows[0].split("\t")
        require(len(callback_cols) == 3, "processed callback evidence shape is invalid")
        if len(callback_cols) == 3:
            provider_event_id, provider_transaction_id, callback_correlation = callback_cols

    audit_count = int(scalar(
        "SELECT COUNT(*) FROM billing_audit_events "
        f"WHERE workspace_id={ws} AND action='payment.callback.process' "
        "AND resource_type='callback_event' AND result='success'"
    ) or "0")
    support_mail_count = int(scalar(
        "SELECT COUNT(*) FROM mail_jobs "
        "WHERE template_key='support-ticket-reply' AND resource_type='ticket_message' AND status='sent'"
    ) or "0")
    require(audit_count >= 1, "independent Billing audit authority is missing")
    require(support_mail_count >= 1, "independent Support mail authority is missing")

    durable_notification = "\n".join((category, event_key, dedupe_key, title, summary, deep_link, resource_type, order_id)).lower()
    for forbidden in (provider_event_id, provider_transaction_id, callback_correlation):
        if forbidden:
            require(forbidden.lower() not in durable_notification, "provider/callback identifier leaked into notification")
    sensitive_markers = (
        "password=", "token=", "secret=", "authorization:", "cookie:",
        "oauth_", "webhook_secret", "payment_secret", "risk_evidence",
        "bearer ", "whsec_", "sk_ci_", "stripe-signature",
    )
    require(not any(marker in durable_notification for marker in sensitive_markers), "secret-like material leaked into notification")
    require("@" not in durable_notification, "email-like user PII leaked into notification")
    require(dedupe_key not in json.dumps({"title": title, "summary": summary, "deep_link": deep_link}), "internal dedupe authority leaked into user-facing notification fields")

    window = d21.get("auth_rate_window_seconds", 60)
    if not isinstance(window, int) or window <= 0 or window > 300:
        errors.append("T021 authentication rate window evidence is invalid")
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)
    time.sleep(window + 1)

    suffix = HEAD[:12].lower()
    login_status, login_headers, login_body = http_json(
        "POST",
        "/api/auth/login",
        {
            "email": f"p20-t009-{suffix}@example.test",
            "password": os.environ.get("GOJET_P20_T009_LOGIN_FIXTURE", ""),
            "correlation_id": f"p20-t022-login-{suffix}",
        },
    )
    cookie = session_cookie(login_headers)
    require(login_status == 200 and login_body.get("status") == "authenticated" and bool(cookie), "real P15 login failed for notification workflow")
    cookie_header = f"__Host-gojet_session={cookie}"

    def me_and_csrf() -> tuple[int, dict[str, Any], str]:
        status, _, body = http_json("GET", "/api/me", headers={"Cookie": cookie_header})
        token = str(body.get("csrf_token") or "") if isinstance(body, dict) else ""
        return status, body if isinstance(body, dict) else {}, token

    me_status, me, csrf = me_and_csrf()
    me_user = me.get("user", {}) if isinstance(me, dict) else {}
    require(me_status == 200 and isinstance(me_user, dict) and me_user.get("id") == user and bool(csrf), "real P15 session/CSRF binding failed")
    user_email = str(me_user.get("email") or "")
    require(not user_email or user_email.lower() not in durable_notification, "correlated user email leaked into notification")
    if errors:
        return emit(CASE_ID, "p0", CASE_NAME, errors, details)

    list_path = f"/api/workspaces/{workspace}/notifications?category=billing&limit=50"
    list_status, _, page = http_json("GET", list_path, headers={"Cookie": cookie_header})
    items = page.get("items", []) if isinstance(page, dict) else []
    state = page.get("state", {}) if isinstance(page, dict) else {}
    matches = [
        item for item in items
        if isinstance(item, dict)
        and item.get("id") == notification_id
        and item.get("category") == "billing"
        and item.get("event_key") == "payment_succeeded"
    ]
    require(list_status == 200, "notification list HTTP authority failed")
    require(len(matches) == 1, "correlated payment notification missing from real notification API")
    require(isinstance(state, dict) and state.get("status") == "complete" and state.get("state_reason") == "current", "notification freshness state is not explicit complete/current")
    before_unread = int(page.get("unread_count", -1)) if isinstance(page, dict) else -1
    require(before_unread >= 1, "notification unread count is invalid before read")
    api_item = matches[0] if matches else {}
    require(api_item.get("deep_link") == "/app/billing", "notification API deep-link authorization changed the safe billing route")
    require(api_item.get("resource_type") == "billing_order" and api_item.get("resource_id") == order_id, "notification API resource binding mismatch")
    require("read_at" not in api_item or api_item.get("read_at") in (None, ""), "fresh notification API item is unexpectedly read")
    api_serialized = json.dumps(api_item, sort_keys=True, ensure_ascii=False).lower()
    require("dedupe_key" not in api_serialized, "internal notification dedupe key leaked through API")
    for forbidden in (provider_event_id, provider_transaction_id, callback_correlation, user_email):
        if forbidden:
            require(forbidden.lower() not in api_serialized, "sensitive provider/user identifier leaked through notification API")
    require(not any(marker in api_serialized for marker in sensitive_markers), "secret-like material leaked through notification API")

    _, _, read_csrf = me_and_csrf()
    read_status, _, read_body = http_json(
        "POST",
        f"/api/workspaces/{workspace}/notifications/{notification_id}/read",
        headers={
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": read_csrf,
            "X-Request-ID": f"p20-t022-read-{suffix}",
        },
    )
    require(read_status == 200 and read_body.get("id") == notification_id and read_body.get("read") is True, "recipient-scoped notification read mutation failed")
    read_list_status, _, read_page = http_json("GET", list_path, headers={"Cookie": cookie_header})
    read_matches = [
        item for item in (read_page.get("items", []) if isinstance(read_page, dict) else [])
        if isinstance(item, dict) and item.get("id") == notification_id
    ]
    after_read_unread = int(read_page.get("unread_count", -1)) if isinstance(read_page, dict) else -1
    require(read_list_status == 200 and len(read_matches) == 1 and bool(read_matches[0].get("read_at")), "notification read state was not persisted")
    require(after_read_unread == before_unread - 1, "notification unread count did not decrement exactly once")

    _, _, unread_csrf = me_and_csrf()
    unread_status, _, unread_body = http_json(
        "POST",
        f"/api/workspaces/{workspace}/notifications/{notification_id}/unread",
        headers={
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": unread_csrf,
            "X-Request-ID": f"p20-t022-unread-{suffix}",
        },
    )
    require(unread_status == 200 and unread_body.get("id") == notification_id and unread_body.get("read") is False, "recipient-scoped notification unread mutation failed")
    unread_list_status, _, unread_page = http_json("GET", list_path, headers={"Cookie": cookie_header})
    unread_matches = [
        item for item in (unread_page.get("items", []) if isinstance(unread_page, dict) else [])
        if isinstance(item, dict) and item.get("id") == notification_id
    ]
    after_unread_count = int(unread_page.get("unread_count", -1)) if isinstance(unread_page, dict) else -1
    require(unread_list_status == 200 and len(unread_matches) == 1, "notification unread state lookup failed")
    if unread_matches:
        require("read_at" not in unread_matches[0] or unread_matches[0].get("read_at") in (None, ""), "notification unread state was not persisted")
    require(after_unread_count == before_unread, "notification unread count did not restore after unread mutation")

    _, _, miss_csrf = me_and_csrf()
    missing_status, _, _ = http_json(
        "POST",
        f"/api/workspaces/{workspace}/notifications/{notification_id + 10_000_000}/read",
        headers={
            "Cookie": cookie_header,
            "Origin": os.environ.get("GOJET_AUTH_ALLOWED_ORIGIN", "http://localhost:4185"),
            "X-CSRF-Token": miss_csrf,
            "X-Request-ID": f"p20-t022-missing-{suffix}",
        },
    )
    require(missing_status == 404, "notification read mutation did not fail closed for unknown recipient-scoped id")

    final_read = scalar(
        f"SELECT read_at IS NOT NULL FROM workspace_notifications WHERE id={notification_id} "
        f"AND workspace_id={ws} AND recipient_user_id={uid}"
    )
    final_count = int(scalar(
        "SELECT COUNT(*) FROM workspace_notifications "
        f"WHERE workspace_id={ws} AND recipient_user_id={uid} "
        "AND category='billing' AND event_key='payment_succeeded'"
    ) or "0")
    require(final_read == "0", "notification final unread state mismatch")
    require(final_count == 1, "notification dedupe invariant changed during read-state workflow")

    details.update({
        "user_id": user,
        "workspace_id": workspace,
        "workspace_role": "owner",
        "t020_mail_audit_evidence_bound": True,
        "t021_payment_notification_evidence_bound": True,
        "notification_count": final_count,
        "paid_notification_write_delta": paid_delta.get("payment_notifications"),
        "duplicate_notification_write_delta": duplicate_delta.get("payment_notifications"),
        "notification_id_present": notification_id > 0,
        "notification_category": category,
        "notification_event_key": event_key,
        "notification_dedupe_key_server_side": dedupe_key.startswith("billing:payment_succeeded:callback:"),
        "notification_deep_link": deep_link,
        "deep_link_authorized": api_item.get("deep_link") == "/app/billing",
        "notification_resource_type": resource_type,
        "notification_resource_bound_to_paid_order": True,
        "notification_state_status": state.get("status") if isinstance(state, dict) else None,
        "notification_state_reason": state.get("state_reason") if isinstance(state, dict) else None,
        "list_http_status": list_status,
        "read_http_status": read_status,
        "unread_http_status": unread_status,
        "missing_notification_http_status": missing_status,
        "unread_count_before": before_unread,
        "unread_count_after_read": after_read_unread,
        "unread_count_after_unread": after_unread_count,
        "read_state_persisted": read_list_status == 200 and len(read_matches) == 1 and bool(read_matches[0].get("read_at")) if read_matches else False,
        "unread_state_persisted": unread_list_status == 200 and len(unread_matches) == 1 and ("read_at" not in unread_matches[0] or unread_matches[0].get("read_at") in (None, "")) if unread_matches else False,
        "recipient_scoped": missing_status == 404,
        "sensitive_data_redacted": True,
        "provider_identifiers_absent": True,
        "user_pii_absent": True,
        "billing_audit_authority_present": audit_count >= 1,
        "support_mail_authority_preserved": support_mail_count >= 1,
        "real_session_authenticated": me_status == 200,
        "csrf_authority_issued": bool(csrf),
        "auth_rate_window_seconds": window,
        "auth_rate_window_respected": True,
        "next_case": "P20-T023",
    })
    return emit(CASE_ID, "p0", CASE_NAME, errors, details)


def main() -> None:
    fail_if_errors([main_case()])


if __name__ == "__main__":
    main()
