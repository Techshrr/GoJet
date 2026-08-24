#!/usr/bin/env python3
from __future__ import annotations

import os
from pathlib import Path
import subprocess
import time

from integration_common import (
    TURNSTILE_TOKEN, create_public_contact, create_ticket, expect, initial_message_id, mysql_rows, mysql_scalar,
    producer, redis, reset_p14, seed_workspace, sql_quote, support, unique,
)

TMP = Path(os.environ.get("GOJET_TEST_P14_TMP", "/tmp/gojet-p14"))
TMP.mkdir(parents=True, exist_ok=True)


def make_file(name: str, content: bytes) -> Path:
    path = TMP / name
    path.write_bytes(content)
    return path


def intake_text(ticket_id: str, message_id: str, suffix: str, content: bytes = b"GoJet P14 integration attachment\n") -> dict:
    path = make_file(f"{suffix}.txt", content)
    rc, data, _ = producer("attachment-intake", ticket_id, message_id, str(path), f"{suffix}.txt", "text/plain")
    expect(rc == 0 and isinstance(data, dict) and data.get("created") is True, f"attachment intake failed rc={rc} data={data}")
    return data["attachment"]


def start_fault_server(mode: str, port: int, *, hold: float = 2.0) -> subprocess.Popen:
    proc = subprocess.Popen(
        ["python3", "scripts/p09/clamd_fault_server.py", "--mode", mode, "--port", str(port), "--hold-seconds", str(hold)],
        cwd=Path(__file__).resolve().parents[2], text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
    )
    assert proc.stdout is not None
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        line = proc.stdout.readline().strip()
        if "READY" in line:
            return proc
        if proc.poll() is not None:
            break
    stderr = proc.stderr.read() if proc.stderr else ""
    proc.terminate()
    raise AssertionError(f"fault server {mode} failed to start: {stderr}")


def stop_process(proc: subprocess.Popen) -> None:
    proc.terminate()
    try:
        proc.wait(timeout=3)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=3)


def case_t008():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T008")
    created = create_ticket("P14-T008", workspace, actor, email)
    expect(created[0] == 201, "ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    message_id = initial_message_id(ticket_id)
    expect(message_id != "", "initial message missing")

    attachment = intake_text(ticket_id, message_id, "t008-clean-input")
    expect(attachment["scan_status"] == "quarantined" and attachment["size_bytes"] > 0, "accepted attachment not quarantined")
    blocked = producer("attachment-download-check", attachment["id"], expect_success=False)
    expect(blocked[0] != 0 and blocked[1].get("allowed") is False, "quarantined attachment was downloadable")

    safe_path = make_file("t008-invalid.txt", b"safe text\n")
    before = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_attachments"))
    bad_name = producer("attachment-intake", ticket_id, message_id, str(safe_path), "../evil.txt", "text/plain", expect_success=False)
    bad_mime = producer("attachment-intake", ticket_id, message_id, str(safe_path), "safe.txt", "application/octet-stream", expect_success=False)
    oversize_path = make_file("t008-oversize.txt", b"A" * 70000)
    oversize = producer("attachment-intake", ticket_id, message_id, str(oversize_path), "large.txt", "text/plain", expect_success=False)
    after = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_attachments"))
    expect(bad_name[0] != 0 and bad_mime[0] != 0 and oversize[0] != 0, "invalid attachment policy did not reject")
    expect(after == before, "rejected attachments created durable rows")
    return {
        "accepted_state": "quarantined",
        "pre_scan_download_allowed": False,
        "unsafe_name_rejected": True,
        "mime_mismatch_rejected": True,
        "oversize_rejected": True,
        "rejected_durable_delta": after - before,
    }


def case_t009():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T009")
    created = create_ticket("P14-T009", workspace, actor, email)
    expect(created[0] == 201, "ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    attachment = intake_text(ticket_id, initial_message_id(ticket_id), "t009-clean")
    rc, scanned, _ = producer("attachment-scan", attachment["id"])
    expect(rc == 0 and scanned.get("verdict") == "clean", f"real ClamAV clean scan failed rc={rc} data={scanned}")
    expect(scanned["attachment"]["scan_status"] == "clean" and scanned.get("download_allowed") is True, "clean verdict did not release attachment")
    download = producer("attachment-download-check", attachment["id"])
    expect(download[0] == 0 and download[1].get("allowed") is True, "clean published attachment unavailable")
    durable = mysql_rows("SELECT scan_status,sha256,size_bytes FROM support_ticket_attachments WHERE id=" + sql_quote(attachment["id"]))
    expect(len(durable) == 1 and durable[0][0] == "clean" and len(durable[0][1]) == 64, f"durable clean state mismatch {durable}")
    audits = int(mysql_scalar("SELECT COUNT(*) FROM support_audit_events WHERE resource_type='attachment' AND resource_id=" + sql_quote(attachment["id"])))
    expect(audits >= 2, "attachment scan audit chain missing")
    return {
        "real_clamav_verdict": "clean",
        "durable_state": "clean",
        "download_allowed": True,
        "sha256_length": 64,
        "scan_audit_rows": audits,
    }


def case_t010():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T010")
    created = create_ticket("P14-T010", workspace, actor, email)
    expect(created[0] == 201, "ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    message_id = initial_message_id(ticket_id)

    eicar = (b"X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*\n")
    infected = intake_text(ticket_id, message_id, "t010-eicar", eicar)
    inf_rc, inf_data, _ = producer("attachment-scan", infected["id"])
    expect(inf_rc == 0 and inf_data["attachment"]["scan_status"] == "infected", f"EICAR was not infected: {inf_data}")

    unavailable = intake_text(ticket_id, message_id, "t010-unavailable")
    un_rc, un_data, _ = producer(
        "attachment-scan", unavailable["id"], expect_success=False,
        env={"GOJET_TEST_P14_CLAMAV_ADDRESS": "127.0.0.1:39999", "GOJET_TEST_P14_CLAMAV_DIAL_TIMEOUT": "200ms"},
    )
    expect(un_rc != 0 and un_data["attachment"]["scan_status"] == "scan-error", f"unavailable scanner did not fail closed {un_data}")

    timeout_attachment = intake_text(ticket_id, message_id, "t010-timeout")
    timeout_server = start_fault_server("timeout", 33311, hold=2.0)
    try:
        to_rc, to_data, _ = producer(
            "attachment-scan", timeout_attachment["id"], expect_success=False,
            env={"GOJET_TEST_P14_CLAMAV_ADDRESS": "127.0.0.1:33311", "GOJET_TEST_P14_CLAMAV_SCAN_TIMEOUT": "300ms"},
        )
    finally:
        stop_process(timeout_server)
    expect(to_rc != 0 and to_data["attachment"]["scan_status"] == "scan-error", f"timeout did not fail closed {to_data}")

    stale_attachment = intake_text(ticket_id, message_id, "t010-stale")
    stale_server = start_fault_server("stale", 33312)
    try:
        stale_rc, stale_data, _ = producer(
            "attachment-scan", stale_attachment["id"], expect_success=False,
            env={"GOJET_TEST_P14_CLAMAV_ADDRESS": "127.0.0.1:33312", "GOJET_TEST_P14_CLAMAV_MAX_SIGNATURE_AGE": "1h"},
        )
    finally:
        stop_process(stale_server)
    expect(stale_rc != 0 and stale_data["attachment"]["scan_status"] == "scan-error", f"stale signatures did not fail closed {stale_data}")

    indeterminate_attachment = intake_text(ticket_id, message_id, "t010-indeterminate")
    ind_server = start_fault_server("indeterminate", 33313)
    try:
        ind_rc, ind_data, _ = producer(
            "attachment-scan", indeterminate_attachment["id"], expect_success=False,
            env={"GOJET_TEST_P14_CLAMAV_ADDRESS": "127.0.0.1:33313"},
        )
    finally:
        stop_process(ind_server)
    expect(ind_rc != 0 and ind_data["attachment"]["scan_status"] == "scan-error", f"indeterminate scan did not fail closed {ind_data}")

    states = mysql_rows("SELECT scan_status,COUNT(*) FROM support_ticket_attachments GROUP BY scan_status ORDER BY scan_status")
    expect(["infected", "1"] in states and ["scan-error", "4"] in states, f"durable failure state counts mismatch {states}")
    for attachment in (infected, unavailable, timeout_attachment, stale_attachment, indeterminate_attachment):
        check = producer("attachment-download-check", attachment["id"], expect_success=False)
        expect(check[0] != 0 and check[1].get("allowed") is False, f"blocked scan state downloadable {attachment['id']}")
    return {
        "infected_state": "infected",
        "unavailable_state": "scan-error",
        "timeout_state": "scan-error",
        "stale_signature_state": "scan-error",
        "indeterminate_state": "scan-error",
        "blocked_downloads": 5,
    }


def case_t011():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T011")
    expect(TURNSTILE_TOKEN != "", "deterministic Turnstile token missing")
    invalid = support(
        "POST", "/api/support/tickets", actor, email,
        body={"workspace_id": workspace, "category": "general", "subject": "invalid", "message": "invalid", "turnstile_token": "definitely-invalid"},
        idempotency=unique("t011-invalid"), correlation=unique("t011-invalid-correlation"),
    )
    expect(invalid[0] == 400 and int(mysql_scalar("SELECT COUNT(*) FROM support_tickets")) == 0, "invalid Turnstile mutated ticket state")
    valid_key = unique("t011-valid-idem")
    valid = create_ticket("P14-T011", workspace, actor, email, idempotency=valid_key)
    expect(valid[0] == 201, f"valid Turnstile status={valid[0]}")
    replay = create_ticket("P14-T011", workspace, actor, email, idempotency=valid_key, reset_replay=False)
    expect(replay[0] == 400 and int(mysql_scalar("SELECT COUNT(*) FROM support_tickets")) == 1, "replayed Turnstile did not fail before mutation")
    keys = redis("KEYS", "support:turnstile:replay:*").splitlines()
    expect(len(keys) == 1 and all(TURNSTILE_TOKEN not in key for key in keys), "raw Turnstile token leaked into Redis key")
    return {
        "invalid_status": invalid[0],
        "valid_status": valid[0],
        "replay_status": replay[0],
        "ticket_rows_after_replay": 1,
        "redis_replay_keys": len(keys),
        "raw_token_in_redis_key": False,
    }


def case_t012():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T012")
    ticket_key = unique("t012-ticket-idem")
    first = create_ticket("P14-T012", workspace, actor, email, idempotency=ticket_key)
    expect(first[0] == 201, "initial ticket create failed")
    ticket_id = first[3]["ticket"]["id"]
    redis("FLUSHDB")  # independent deterministic verification fixture; durable idempotency remains in MySQL.
    second = create_ticket("P14-T012", workspace, actor, email, idempotency=ticket_key, reset_replay=False)
    expect(second[0] == 200 and second[3]["created"] is False and second[3]["ticket"]["id"] == ticket_id, "ticket idempotency replay duplicated or changed identity")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM support_tickets")) == 1 and int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs")) == 1, "ticket idempotency duplicated durable ticket/mail")

    reply_key = unique("t012-reply-idem")
    reply1 = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                     body={"message": "idempotent reply"}, idempotency=reply_key, correlation=unique("t012-reply"))
    reply2 = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                     body={"message": "idempotent reply"}, idempotency=reply_key, correlation=unique("t012-reply-replay"))
    expect(reply1[0] == 201 and reply2[0] == 200 and reply2[3]["created"] is False, "reply idempotency failed")
    expect(reply1[3]["message_id"] == reply2[3]["message_id"], "reply idempotency changed message identity")

    rejected_without_mutation = False
    for index in range(30):
        before = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=" + sql_quote(ticket_id)))
        result = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                         body={"message": f"rate probe {index}"}, idempotency=unique(f"t012-rate-{index}"))
        after = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=" + sql_quote(ticket_id)))
        if result[0] == 429:
            expect(after == before, "rate-limited request mutated message state")
            rejected_without_mutation = True
            break
        expect(result[0] in {200, 201}, f"unexpected rate probe status={result[0]}")
    expect(rejected_without_mutation, "rate limiter did not reject within bounded probes")
    return {
        "ticket_idempotent": True,
        "ticket_rows": 1,
        "mail_jobs_after_ticket_replay": 1,
        "reply_idempotent": True,
        "rate_limit_reached": True,
        "rate_limited_mutation_delta": 0,
    }


def case_t013():
    reset_p14()
    key = unique("t013-contact-idem")
    first = create_public_contact("P14-T013", idempotency=key)
    expect(first[0] == 201 and first[3]["status"] == "received" and first[3]["created"] is True, f"contact create status={first[0]} body={first[2][:200]!r}")
    contact_email = "p14-t013@example.test"
    expect(contact_email.encode() not in first[2], "public contact response exposed email")
    ticket_id = first[3]["ticket_id"]
    durable = mysql_rows(
        "SELECT pc.status,t.status,t.public_contact_id IS NOT NULL,t.workspace_id IS NULL FROM support_public_contacts pc JOIN support_tickets t ON t.public_contact_id=pc.id WHERE t.id=" + sql_quote(ticket_id)
    )
    expect(durable == [["new", "awaiting_support", "1", "1"]], f"public contact durable state mismatch {durable}")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs WHERE resource_type='public_contact'")) == 1, "public contact mail not queued once")
    redis("FLUSHDB")
    second = create_public_contact("P14-T013", idempotency=key)
    expect(second[0] == 200 and second[3]["created"] is False and second[3]["ticket_id"] == ticket_id, "public contact idempotency failed")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM support_public_contacts")) == 1, "contact replay duplicated PII record")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM support_tickets")) == 1, "contact replay duplicated ticket")
    expect(int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs")) == 1, "contact replay duplicated mail")
    return {
        "persistent_success": True,
        "public_contact_rows": 1,
        "public_ticket_rows": 1,
        "mail_jobs": 1,
        "workspace_membership_granted": False,
        "response_contains_email": False,
        "idempotent_replay_created": False,
    }


CASES = {
    "P14-T008": case_t008,
    "P14-T009": case_t009,
    "P14-T010": case_t010,
    "P14-T011": case_t011,
    "P14-T012": case_t012,
    "P14-T013": case_t013,
}
