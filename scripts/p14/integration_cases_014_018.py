#!/usr/bin/env python3
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import time

from integration_common import (
    MAIL_ADMIN, PRODUCER, SMTP_MODE, admin_mail, admin_ticket, create_ticket, expect, mysql, mysql_rows,
    mysql_scalar, redis, reset_p14, seed_member, seed_workspace, set_smtp_mode, smtp_state, sql_quote,
    support, ticket_admin_headers, unique, wait_for,
)

ROOT = Path(__file__).resolve().parents[2]
MAILWORKER = os.environ.get("GOJET_TEST_P14_MAILWORKER", "/tmp/gojet-p14-mailworker")
TMP = Path(os.environ.get("GOJET_TEST_P14_TMP", "/tmp/gojet-p14"))
TMP.mkdir(parents=True, exist_ok=True)


def start_mailworker(label: str) -> tuple[subprocess.Popen, object]:
    log_path = TMP / f"mailworker-{label}.log"
    log = log_path.open("w", encoding="utf-8")
    proc = subprocess.Popen([MAILWORKER], cwd=ROOT, env=os.environ.copy(), stdout=log, stderr=subprocess.STDOUT, text=True)
    time.sleep(0.4)
    if proc.poll() is not None:
        log.flush()
        log.close()
        raise AssertionError(f"mailworker exited early for {label}; log={log_path}")
    return proc, log


def stop_mailworker(proc: subprocess.Popen, log) -> None:
    proc.terminate()
    try:
        proc.wait(timeout=5)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)
    log.flush()
    log.close()


def case_t014():
    reset_p14()
    valid_values = json.dumps({
        "ticket_id": "tkt_fixture",
        "display_name": "Integration User",
        "subject": "Support subject",
        "status": "open",
    }, separators=(",", ":"))
    valid = producer("render-template", "support-ticket-created", "en", valid_values)
    expect(valid[0] == 0 and valid[1].get("subject_nonempty") and valid[1].get("text_nonempty") and valid[1].get("html_nonempty"), f"valid template render failed {valid[1]}")

    missing_values = json.dumps({"ticket_id": "tkt_fixture", "display_name": "Integration User", "subject": "Support subject"}, separators=(",", ":"))
    missing = producer("render-template", "support-ticket-created", "en", missing_values, expect_success=False)
    expect(missing[0] != 0 and missing[1].get("error") == "template_rejected", "missing template variable was not rejected")
    unknown_values = json.dumps({
        "ticket_id": "tkt_fixture", "display_name": "Integration User", "subject": "Support subject", "status": "open", "unexpected": "x"
    }, separators=(",", ":"))
    unknown = producer("render-template", "support-ticket-created", "en", unknown_values, expect_success=False)
    expect(unknown[0] != 0 and unknown[1].get("error") == "template_rejected", "unknown template variable was not rejected")

    mysql("""
INSERT INTO mail_templates
(template_key,locale,version,subject_template,text_template,html_template,variable_allowlist_json,internal_only,enabled)
VALUES ('p14-sensitive-fixture','en',1,'{{smtp_password}}','{{smtp_password}}','<p>{{smtp_password}}</p>',JSON_ARRAY('smtp_password'),0,1)
ON DUPLICATE KEY UPDATE template_key=template_key
""")
    sensitive = producer("render-template", "p14-sensitive-fixture", "en", json.dumps({"smtp_password": "fixture-value"}), expect_success=False)
    expect(sensitive[0] != 0 and sensitive[1].get("error") == "template_rejected", "sensitive variable allowlist was not rejected")

    seeded = mysql_rows("SELECT template_key,locale,version,enabled FROM mail_templates WHERE template_key IN ('support-ticket-created','support-ticket-reply','public-contact-received','mail-test') ORDER BY template_key")
    expect(len(seeded) == 4 and all(row[2] == "1" and row[3] == "1" for row in seeded), f"seeded template versions mismatch {seeded}")
    internal_note_refs = int(mysql_scalar("SELECT COUNT(*) FROM mail_templates WHERE internal_only=0 AND (subject_template LIKE '%internal_note%' OR text_template LIKE '%internal_note%' OR html_template LIKE '%internal_note%')"))
    expect(internal_note_refs == 0, "requester templates contain internal-note marker")
    return {
        "versioned_seed_templates": 4,
        "valid_allowlisted_render": True,
        "missing_variable_rejected": True,
        "unknown_variable_rejected": True,
        "sensitive_variable_rejected": True,
        "requester_internal_note_template_refs": internal_note_refs,
    }


def case_t015():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T015")
    idem = unique("t015-ticket-idem")
    first = create_ticket("P14-T015", workspace, actor, email, idempotency=idem)
    expect(first[0] == 201, "ticket fixture failed")
    redis("FLUSHDB")
    replay = create_ticket("P14-T015", workspace, actor, email, idempotency=idem, reset_replay=False)
    expect(replay[0] == 200 and replay[3]["created"] is False, "ticket replay did not hit durable idempotency")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs")) == 1, "logical ticket replay duplicated mail job")
    queued = mysql_rows("SELECT status,attempt_count,LENGTH(idempotency_key_hash),claim_token_hash IS NULL FROM mail_jobs")
    expect(queued == [["queued", "0", "32", "1"]], f"queued mail state mismatch {queued}")

    env = os.environ.copy()
    one = subprocess.Popen([PRODUCER, "claim-mail"], cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    two = subprocess.Popen([PRODUCER, "claim-mail"], cwd=ROOT, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    out1, err1 = one.communicate(timeout=20)
    out2, err2 = two.communicate(timeout=20)
    expect(one.returncode == 0 and two.returncode == 0, f"claim helper failed rc1={one.returncode} rc2={two.returncode} err1={err1} err2={err2}")
    data1 = json.loads(out1.strip().splitlines()[-1])
    data2 = json.loads(out2.strip().splitlines()[-1])
    claimed = [item for item in (data1, data2) if item.get("claimed") is True]
    empty = [item for item in (data1, data2) if item.get("claimed") is False]
    expect(len(claimed) == 1 and len(empty) == 1, f"concurrent claim count mismatch {data1} {data2}")
    durable = mysql_rows("SELECT status,attempt_count,LENGTH(claim_token_hash),claim_expires_at IS NOT NULL FROM mail_jobs")
    attempts = mysql_rows("SELECT attempt_number,status,completed_at IS NULL FROM mail_attempts")
    expect(durable == [["sending", "1", "32", "1"]], f"durable claim mismatch {durable}")
    expect(attempts == [["1", "sending", "1"]], f"attempt claim mismatch {attempts}")
    return {
        "logical_mail_jobs": 1,
        "idempotency_hash_bytes": 32,
        "concurrent_claim_winners": 1,
        "concurrent_claim_empty": 1,
        "durable_status": "sending",
        "attempt_count": 1,
        "claim_hash_bytes": 32,
    }


def case_t016():
    reset_p14()
    set_smtp_mode("success")
    proc, log = start_mailworker("t016")
    try:
        recipient = "p14-t016@example.test"
        result = admin_mail("POST", "/api/admin/mail/test", body={"recipient": recipient}, idempotency=unique("t016-test-send"), correlation=unique("t016-correlation"))
        expect(result[0] == 201 and result[3]["created"] is True, f"test-send enqueue status={result[0]} body={result[2][:200]!r}")
        expect(recipient.encode() not in result[2], "Admin test-send response leaked recipient value")
        job_id = result[3]["job"]["id"]
        wait_for(lambda: mysql_scalar("SELECT status FROM mail_jobs WHERE id=" + sql_quote(job_id)) == "sent", timeout=20, message="mail sent")
        wait_for(lambda: int(smtp_state().get("deliveries", 0)) == 1, timeout=10, message="SMTP delivery")
    finally:
        stop_mailworker(proc, log)
    attempts = mysql_rows("SELECT attempt_number,status,error_code IS NULL FROM mail_attempts WHERE mail_job_id=" + sql_quote(job_id))
    expect(attempts == [["1", "sent", "1"]], f"SMTP success attempt mismatch {attempts}")
    state = smtp_state()
    expect(state.get("deliveries") == 1 and len(state.get("last_message_sha256", "")) == 64, f"SMTP sink state mismatch {state}")
    return {
        "native_mailworker": True,
        "real_smtp_transactions": int(state.get("connections", 0)),
        "smtp_deliveries": int(state.get("deliveries", 0)),
        "durable_job_status": "sent",
        "attempt_status": "sent",
        "recipient_exposed_in_api": False,
        "captured_message_sha256_length": 64,
    }


def case_t017():
    reset_p14()
    set_smtp_mode("transient")
    proc, log = start_mailworker("t017")
    recipient = "p14-t017@example.test"
    try:
        result = admin_mail("POST", "/api/admin/mail/test", body={"recipient": recipient}, idempotency=unique("t017-test-send"), correlation=unique("t017-correlation"))
        expect(result[0] == 201, f"test-send enqueue status={result[0]}")
        job_id = result[3]["job"]["id"]
        wait_for(lambda: mysql_scalar("SELECT status FROM mail_jobs WHERE id=" + sql_quote(job_id)) == "retrying", timeout=20, message="mail retrying")
        retry = mysql_rows("SELECT attempt_count,last_error_code,TIMESTAMPDIFF(SECOND,updated_at,next_attempt_at) FROM mail_jobs WHERE id=" + sql_quote(job_id))
        expect(len(retry) == 1 and retry[0][0] == "1" and retry[0][1] == "smtp_transient" and int(retry[0][2]) >= 29, f"retry/backoff mismatch {retry}")
        mysql("UPDATE mail_jobs SET next_attempt_at=UTC_TIMESTAMP(6) WHERE id=" + sql_quote(job_id) + " AND status='retrying'")
        set_smtp_mode("terminal")
        wait_for(lambda: mysql_scalar("SELECT status FROM mail_jobs WHERE id=" + sql_quote(job_id)) == "failed", timeout=20, message="mail terminal failure")
    finally:
        stop_mailworker(proc, log)
    durable = mysql_rows("SELECT status,attempt_count,last_error_code,next_attempt_at IS NULL,claim_token_hash IS NULL FROM mail_jobs WHERE id=" + sql_quote(job_id))
    attempts = mysql_rows("SELECT attempt_number,status,error_code FROM mail_attempts WHERE mail_job_id=" + sql_quote(job_id) + " ORDER BY attempt_number")
    expect(durable == [["failed", "2", "smtp_terminal", "1", "1"]], f"terminal mail state mismatch {durable}")
    expect(attempts == [["1", "transient_failure", "smtp_transient"], ["2", "terminal_failure", "smtp_terminal"]], f"attempt history mismatch {attempts}")
    state = smtp_state()
    expect(int(state.get("transient_rejections", 0)) >= 1 and int(state.get("terminal_rejections", 0)) >= 1 and int(state.get("deliveries", 0)) == 0, f"SMTP failure modes mismatch {state}")
    return {
        "transient_attempts": 1,
        "terminal_attempts": 1,
        "initial_backoff_seconds_min": 29,
        "durable_status": "failed",
        "attempt_count": 2,
        "smtp_deliveries": 0,
        "transient_protocol_rejections": int(state.get("transient_rejections", 0)),
        "terminal_protocol_rejections": int(state.get("terminal_rejections", 0)),
    }


def case_t018():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T018")
    mysql("INSERT INTO workspace_notification_state (workspace_id,status,state_reason) VALUES (" + sql_quote(workspace) + ",'complete','current')")
    set_smtp_mode("terminal")
    proc, log = start_mailworker("t018")
    try:
        created = create_ticket("P14-T018", workspace, actor, email)
        expect(created[0] == 201, "ticket create failed")
        ticket_id = created[3]["ticket"]["id"]
        wait_for(
            lambda: int(mysql_scalar("SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=" + sql_quote(workspace) + " AND event_key='mail_delivery_failed'")) == 1,
            timeout=20,
            message="mail_delivery_failed notification",
        )
    finally:
        stop_mailworker(proc, log)

    requester_reply = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                              body={"message": "requester update"}, idempotency=unique("t018-requester-reply"), correlation=unique("t018-requester-correlation"))
    expect(requester_reply[0] == 201, "requester reply failed")
    support_reply = admin_ticket("POST", f"/api/admin/support/tickets/{ticket_id}/replies",
                                 body={"kind": "support_reply", "message": "support update"}, idempotency=unique("t018-support-reply"), correlation=unique("t018-support-correlation"))
    expect(support_reply[0] == 201, "support reply failed")
    closed = support("POST", f"/api/support/tickets/{ticket_id}/close", actor, email, correlation=unique("t018-close"))
    expect(closed[0] == 200, "ticket close failed")

    events = mysql_rows("SELECT event_key,COUNT(*) FROM workspace_notifications WHERE workspace_id=" + sql_quote(workspace) + " AND recipient_user_id=" + sql_quote(actor) + " AND category='support' GROUP BY event_key ORDER BY event_key")
    expected = {
        "ticket_created": 1,
        "ticket_reply_received": 1,
        "ticket_reply_sent": 1,
        "ticket_closed": 1,
        "mail_delivery_failed": 1,
    }
    actual = {row[0]: int(row[1]) for row in events}
    expect(actual == expected, f"P12 support events mismatch {actual}")
    unsafe = int(mysql_scalar("SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id=" + sql_quote(workspace) + " AND category='support' AND (title LIKE '%@%' OR summary LIKE '%@%' OR title LIKE '%token%' OR summary LIKE '%token%' OR title LIKE '%password%' OR summary LIKE '%password%')"))
    expect(unsafe == 0, "notification text contains PII/secret marker")
    links = mysql_rows("SELECT DISTINCT deep_link FROM workspace_notifications WHERE workspace_id=" + sql_quote(workspace) + " AND category='support'")
    expect(links == [[f"/app/support/{ticket_id}"]], f"notification deep link mismatch {links}")

    page = support("GET", f"/api/workspaces/{workspace}/notifications?category=support", actor, email)
    expect(page[0] == 200 and len(page[3].get("items", [])) == 5, f"P12 notification API mismatch status={page[0]}")
    expect(all(item.get("deep_link") == f"/app/support/{ticket_id}" for item in page[3]["items"]), "authorized deep links not preserved")

    other_actor, _ = seed_member(workspace, "P14-T018", suffix="replacement")
    mysql("UPDATE support_tickets SET requester_user_id=" + sql_quote(other_actor) + " WHERE id=" + sql_quote(ticket_id))
    reauthorized = support("GET", f"/api/workspaces/{workspace}/notifications?category=support", actor, email)
    expect(reauthorized[0] == 200 and all(item.get("deep_link") == "/app/notifications" for item in reauthorized[3].get("items", [])), "P12 read-time deep-link reauthorization did not fail closed")
    return {
        "event_keys": sorted(expected.keys()),
        "event_counts_exact": True,
        "recipient_scoped": True,
        "unsafe_notification_rows": unsafe,
        "authorized_deep_link": True,
        "reauthorized_fallback": "/app/notifications",
        "mail_delivery_failed_from_terminal_smtp": True,
    }


CASES = {
    "P14-T014": case_t014,
    "P14-T015": case_t015,
    "P14-T016": case_t016,
    "P14-T017": case_t017,
    "P14-T018": case_t018,
}
