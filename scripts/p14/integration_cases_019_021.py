#!/usr/bin/env python3
from __future__ import annotations

import os
from pathlib import Path
import subprocess

from integration_common import (
    admin_mail, admin_ticket, create_ticket, expect, initial_message_id, mysql, mysql_rows,
    mysql_scalar, producer, reset_p14, seed_workspace, set_smtp_mode, smtp_state, sql_quote, support,
    unique, wait_for,
)

ROOT = Path(__file__).resolve().parents[2]
MAILWORKER = os.environ.get("GOJET_TEST_P14_MAILWORKER", "/tmp/gojet-p14/mailworker")
TMP = Path(os.environ.get("GOJET_TEST_P14_TMP", "/tmp/gojet-p14"))
TMP.mkdir(parents=True, exist_ok=True)


def run_mailworker_until(predicate, label: str, timeout: float = 20.0) -> None:
    log_path = TMP / f"mailworker-{label}.log"
    with log_path.open("w", encoding="utf-8") as log:
        proc = subprocess.Popen([MAILWORKER], cwd=ROOT, env=os.environ.copy(), stdout=log, stderr=subprocess.STDOUT, text=True)
        try:
            wait_for(predicate, timeout=timeout, message=label)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)


def case_t019():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T019")
    created = create_ticket("P14-T019", workspace, actor, email)
    expect(created[0] == 201, "ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    denied_actor = unique("t019-denied-admin")
    denied = admin_ticket("GET", "/api/admin/support/tickets", actor=denied_actor)
    expect(denied[0] == 403, f"missing tickets.manage status={denied[0]}")

    before_mail = int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs"))
    before_notifications = int(mysql_scalar("SELECT COUNT(*) FROM workspace_notifications WHERE category='support'"))
    note_body = "private internal diagnostic note"
    note = admin_ticket(
        "POST", f"/api/admin/support/tickets/{ticket_id}/replies",
        body={"kind": "internal_note", "message": note_body},
        idempotency=unique("t019-note"), correlation=unique("t019-note-correlation"),
    )
    expect(note[0] == 201 and note[3]["message"]["kind"] == "internal_note", f"internal note failed status={note[0]}")
    after_mail = int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs"))
    after_notifications = int(mysql_scalar("SELECT COUNT(*) FROM workspace_notifications WHERE category='support'"))
    expect(after_mail == before_mail, "internal note queued requester mail")
    expect(after_notifications == before_notifications, "internal note emitted requester notification")

    requester_detail = support("GET", f"/api/support/tickets/{ticket_id}", actor, email)
    requester_list = support("GET", f"/api/support/tickets?workspace_id={workspace}", actor, email)
    expect(requester_detail[0] == 200 and requester_list[0] == 200, "requester read failed")
    expect(note_body.encode() not in requester_detail[2] and note_body.encode() not in requester_list[2], "internal note leaked to requester API")
    admin_detail = admin_ticket("GET", f"/api/admin/support/tickets/{ticket_id}")
    expect(admin_detail[0] == 200 and any(item.get("kind") == "internal_note" for item in admin_detail[3].get("messages", [])), "authorized Admin cannot inspect internal note")
    audit = mysql_rows("SELECT action,JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.message_kind')) FROM support_audit_events WHERE resource_type='ticket_message' AND action='admin_ticket_internal_note'")
    expect(audit == [["admin_ticket_internal_note", "internal_note"]], f"internal-note audit mismatch {audit}")
    return {
        "missing_permission_status": denied[0],
        "authorized_internal_note": True,
        "requester_internal_note_leak": False,
        "internal_note_mail_delta": after_mail - before_mail,
        "internal_note_notification_delta": after_notifications - before_notifications,
        "admin_note_visible": True,
        "audit_body_exposed": False,
    }


def case_t020():
    reset_p14()
    denied_actor = unique("t020-denied-admin")
    surfaces = [
        ("GET", "/api/admin/mail/queue", None),
        ("GET", "/api/admin/mail/templates", None),
        ("GET", "/api/admin/mail/settings", None),
        ("PATCH", "/api/admin/mail/settings", {"enabled": False, "expected_version": 1}),
        ("POST", "/api/admin/mail/test", {"recipient": "denied@example.test"}),
    ]
    denied_statuses = []
    for method, path, body in surfaces:
        response = admin_mail(method, path, body=body, actor=denied_actor, idempotency=unique("t020-denied") if method == "POST" else None)
        denied_statuses.append(response[0])
    expect(denied_statuses == [403, 403, 403, 403, 403], f"mail.manage gating mismatch {denied_statuses}")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs")) == 0, "denied test-send mutated queue")
    expect(mysql_scalar("SELECT CONCAT(enabled,':',version) FROM mail_settings WHERE settings_key='primary'") == "1:1", "denied settings mutation changed state")

    queue = admin_mail("GET", "/api/admin/mail/queue")
    templates = admin_mail("GET", "/api/admin/mail/templates")
    settings = admin_mail("GET", "/api/admin/mail/settings")
    expect(queue[0] == 200 and templates[0] == 200 and settings[0] == 200, "authorized Admin Mail read surface failed")
    expect(settings[3].get("credentials_masked") is True and settings[3].get("credential_source") == "runtime", "credential masking contract missing")
    lower = settings[2].decode("utf-8", "replace").lower()
    expect(all(marker not in lower for marker in ("smtp_password", "smtp_username", "smtp_addr", "smtp_from")), "settings response exposed SMTP credential field")
    expect(any(item.get("key") == "mail-test" and item.get("version") == 1 for item in templates[3].get("items", [])), "mail-test template missing")

    disabled = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": False, "expected_version": 1}, correlation=unique("t020-settings"))
    expect(disabled[0] == 200 and disabled[3]["settings"]["enabled"] is False and disabled[3]["settings"]["version"] == 2, "versioned settings update failed")
    replay = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": False, "expected_version": 1}, correlation=unique("t020-settings-replay"))
    expect(replay[0] == 200 and replay[3]["settings"]["version"] == 2, "same-setting one-version-behind replay was not repairable")
    stale = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": True, "expected_version": 1}, correlation=unique("t020-settings-stale"))
    expect(stale[0] == 409, f"stale conflicting settings update status={stale[0]}")
    enabled = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": True, "expected_version": 2}, correlation=unique("t020-settings-enable"))
    expect(enabled[0] == 200 and enabled[3]["settings"]["enabled"] is True and enabled[3]["settings"]["version"] == 3, "mail dispatch re-enable failed")

    recipient = "p14-t020@example.test"
    idem = unique("t020-test-send")
    test1 = admin_mail("POST", "/api/admin/mail/test", body={"recipient": recipient}, idempotency=idem, correlation=unique("t020-test"))
    test2 = admin_mail("POST", "/api/admin/mail/test", body={"recipient": recipient}, idempotency=idem, correlation=unique("t020-test-replay"))
    expect(test1[0] == 201 and test1[3]["created"] is True and test2[0] == 200 and test2[3]["created"] is False, "test-send idempotency failed")
    expect(recipient.encode() not in test1[2] and recipient.encode() not in test2[2], "test-send API exposed recipient")
    job_id = test1[3]["job"]["id"]
    queued = mysql_rows("SELECT template_key,template_version,recipient_kind,resource_type,status FROM mail_jobs WHERE id=" + sql_quote(job_id))
    expect(queued == [["mail-test", "1", "admin_test", "mail_test", "queued"]], f"safe test mail queue mismatch {queued}")

    set_smtp_mode("success")
    run_mailworker_until(lambda: mysql_scalar("SELECT status FROM mail_jobs WHERE id=" + sql_quote(job_id)) == "sent", "t020-test-delivery")
    state = smtp_state()
    expect(int(state.get("deliveries", 0)) == 1 and len(state.get("last_message_sha256", "")) == 64, f"safe test SMTP delivery mismatch {state}")
    queue_after = admin_mail("GET", "/api/admin/mail/queue")
    expect(queue_after[0] == 200 and len(queue_after[3].get("items", [])) == 1 and "recipient_value" not in queue_after[3]["items"][0], "Admin queue exposed recipient value")
    expect(queue_after[3]["items"][0].get("status") == "sent", "Admin queue did not surface durable sent state")
    return {
        "denied_surface_statuses": denied_statuses,
        "authorized_queue": True,
        "authorized_templates": True,
        "credentials_masked": True,
        "settings_version_after_disable": 2,
        "settings_version_after_reenable": 3,
        "same_setting_replay_repaired": True,
        "stale_conflict_status": stale[0],
        "test_send_idempotent": True,
        "test_send_template": "mail-test",
        "test_send_delivered": True,
        "smtp_deliveries": 1,
        "recipient_exposed": False,
        "credential_exposed": False,
    }


def case_t021():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T021")
    create_correlation = unique("t021-create-corr")
    created = create_ticket("P14-T021", workspace, actor, email, category="custom-domain-access", correlation=create_correlation)
    expect(created[0] == 201, "custom-domain ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    message_id = initial_message_id(ticket_id)
    pair = mysql_rows("SELECT action,request_correlation_id FROM support_audit_events WHERE action IN ('ticket_created','domain_request_linked') ORDER BY action")
    expect(pair == [["domain_request_linked", create_correlation], ["ticket_created", create_correlation]], f"ticket/domain request correlation mismatch {pair}")

    attachment_path = TMP / "t021-attachment.txt"
    attachment_path.write_text("T021 audit attachment\n", encoding="utf-8")
    intake = producer("attachment-intake", ticket_id, message_id, str(attachment_path), "audit.txt", "text/plain")
    attachment_id = intake[1]["attachment"]["id"]
    scanned = producer("attachment-scan", attachment_id)
    expect(scanned[0] == 0 and scanned[1]["attachment"]["scan_status"] == "clean", "attachment audit fixture scan failed")

    requester_correlation = unique("t021-requester-corr")
    requester_reply = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                              body={"message": "audit requester reply"}, idempotency=unique("t021-requester"), correlation=requester_correlation)
    expect(requester_reply[0] == 201, "requester audit mutation failed")
    admin_correlation = unique("t021-admin-corr")
    admin_note = admin_ticket("POST", f"/api/admin/support/tickets/{ticket_id}/replies",
                              body={"kind": "internal_note", "message": "audit internal note"}, idempotency=unique("t021-admin-note"), correlation=admin_correlation)
    expect(admin_note[0] == 201, "Admin audit mutation failed")
    settings_correlation = unique("t021-settings-corr")
    settings = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": False, "expected_version": 1}, correlation=settings_correlation)
    expect(settings[0] == 200, "Admin mail settings audit mutation failed")
    enabled = admin_mail("PATCH", "/api/admin/mail/settings", body={"enabled": True, "expected_version": 2}, correlation=unique("t021-settings-enable"))
    expect(enabled[0] == 200, "mail dispatch re-enable failed")

    set_smtp_mode("success")
    run_mailworker_until(
        lambda: int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs WHERE status='sent'")) >= 1,
        "t021-mail-attempt",
    )
    expect(int(smtp_state().get("deliveries", 0)) >= 1, "mail audit fixture did not traverse SMTP")

    attachment_audit = mysql_rows("SELECT action,request_correlation_id FROM support_audit_events WHERE resource_type='attachment' AND resource_id=" + sql_quote(attachment_id) + " ORDER BY id")
    expect(len(attachment_audit) >= 2 and all(row[1] == create_correlation for row in attachment_audit), f"attachment audit correlation mismatch {attachment_audit}")
    requester_audit = mysql_rows("SELECT action,request_correlation_id FROM support_audit_events WHERE action='ticket_requester_reply'")
    expect(requester_audit == [["ticket_requester_reply", requester_correlation]], f"requester audit mismatch {requester_audit}")
    admin_audit = mysql_rows("SELECT action,request_correlation_id FROM support_audit_events WHERE action='admin_ticket_internal_note'")
    expect(admin_audit == [["admin_ticket_internal_note", admin_correlation]], f"Admin ticket audit mismatch {admin_audit}")
    settings_audit = mysql_rows("SELECT request_correlation_id FROM support_audit_events WHERE action='admin_mail_settings_updated' AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.enabled'))='false'")
    expect(settings_audit == [[settings_correlation]], f"Admin mail audit mismatch {settings_audit}")
    mail_audits = mysql_rows("SELECT action,request_correlation_id FROM support_audit_events WHERE action LIKE 'mail_attempt_%' ORDER BY id")
    expect(len(mail_audits) >= 1 and all(row[1].startswith("mail:") for row in mail_audits), f"mail attempt audit correlation mismatch {mail_audits}")

    unsafe = int(mysql_scalar("""
SELECT COUNT(*) FROM support_audit_events
WHERE CAST(metadata_json AS CHAR) LIKE '%@%'
   OR LOWER(CAST(metadata_json AS CHAR)) LIKE '%password%'
   OR LOWER(CAST(metadata_json AS CHAR)) LIKE '%turnstile%'
   OR LOWER(CAST(metadata_json AS CHAR)) LIKE '%smtp_%'
   OR LOWER(CAST(metadata_json AS CHAR)) LIKE '%authorization%'
"""))
    expect(unsafe == 0, "audit metadata contains PII/secret marker")
    invalid_metadata_keys = int(mysql_scalar("""
SELECT COUNT(*) FROM support_audit_events e
JOIN JSON_TABLE(JSON_KEYS(e.metadata_json), '$[*]' COLUMNS(k VARCHAR(64) PATH '$')) j
WHERE j.k NOT IN ('status','previous_status','message_kind','scan_status','template_key','attempt_number','error_code','domain_status','domain_source','grant_authority','enabled','version','created')
"""))
    expect(invalid_metadata_keys == 0, "audit metadata contains non-allowlisted key")
    empty_correlation = int(mysql_scalar("SELECT COUNT(*) FROM support_audit_events WHERE CHAR_LENGTH(TRIM(request_correlation_id))=0"))
    expect(empty_correlation == 0, "audit event missing correlation")
    return {
        "ticket_domain_same_correlation": True,
        "attachment_scan_correlated": True,
        "requester_mutation_correlated": True,
        "admin_ticket_correlated": True,
        "admin_mail_correlated": True,
        "mail_attempt_correlated": True,
        "audit_rows": int(mysql_scalar("SELECT COUNT(*) FROM support_audit_events")),
        "unsafe_metadata_rows": unsafe,
        "non_allowlisted_metadata_keys": invalid_metadata_keys,
        "empty_correlation_rows": empty_correlation,
    }


CASES = {
    "P14-T019": case_t019,
    "P14-T020": case_t020,
    "P14-T021": case_t021,
}
