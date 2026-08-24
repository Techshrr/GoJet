#!/usr/bin/env python3
from __future__ import annotations

from integration_common import (
    admin_ticket, create_ticket, expect, initial_message_id, mysql, mysql_rows, mysql_scalar,
    redis, reset_p14, seed_member, seed_workspace, sql_quote, support, unique,
)


def case_t001():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T001")
    correlation = unique("p14-t001-correlation")
    status, headers, raw, data = create_ticket("P14-T001", workspace, actor, email, correlation=correlation)
    expect(status == 201 and isinstance(data, dict), f"create status={status} body={raw[:200]!r}")
    ticket = data["ticket"]
    expect(ticket["id"].startswith("tkt_"), "ticket id is not opaque P14 id")
    expect(ticket["workspace_id"] == workspace and ticket["requester_user_id"] == actor, "ticket scope mismatch")
    expect(ticket["status"] == "awaiting_support" and int(ticket["version"]) == 2, "initial ticket state mismatch")
    expect(ticket["correlation_id"] == correlation, "ticket correlation mismatch")
    expect(headers.get("cache-control") == "no-store" and headers.get("x-robots-tag") == "noindex, nofollow", "private headers missing")
    row = mysql_rows(
        "SELECT status,version,correlation_id FROM support_tickets WHERE id=" + sql_quote(ticket["id"])
    )
    expect(row == [["awaiting_support", "2", correlation]], f"durable ticket row mismatch {row}")
    messages = mysql_rows(
        "SELECT actor_type,kind,correlation_id FROM support_ticket_messages WHERE ticket_id=" + sql_quote(ticket["id"])
    )
    expect(messages == [["requester", "requester_reply", correlation]], f"initial message mismatch {messages}")
    return {
        "ticket_id_opaque": True,
        "durable_status": "awaiting_support",
        "durable_version": 2,
        "correlation_stable": True,
        "message_count": 1,
        "mail_jobs": int(mysql_scalar("SELECT COUNT(*) FROM mail_jobs")),
        "support_notifications": int(mysql_scalar("SELECT COUNT(*) FROM workspace_notifications WHERE category='support'")),
        "audit_rows": int(mysql_scalar("SELECT COUNT(*) FROM support_audit_events")),
        "private_headers": True,
    }


def case_t002():
    reset_p14()
    workspace, owner, owner_email = seed_workspace("P14-T002", suffix="owner")
    member, member_email = seed_member(workspace, "P14-T002", suffix="other-member")
    foreign_workspace, foreign_owner, foreign_email = seed_workspace("P14-T002", suffix="foreign")
    status, _, raw, data = create_ticket("P14-T002", workspace, owner, owner_email)
    expect(status == 201, f"create status={status} body={raw[:200]!r}")
    ticket_id = data["ticket"]["id"]

    same_ws = support("GET", f"/api/support/tickets/{ticket_id}", member, member_email)
    expect(same_ws[0] == 404, f"same-workspace non-requester disclosed ticket status={same_ws[0]}")
    foreign = support("GET", f"/api/support/tickets/{ticket_id}", foreign_owner, foreign_email)
    expect(foreign[0] == 404, f"foreign workspace disclosed ticket status={foreign[0]}")
    own_list = support("GET", f"/api/support/tickets?workspace_id={workspace}", member, member_email)
    expect(own_list[0] == 200 and own_list[3].get("items") in ([], None), "member list leaked another requester's ticket")

    mysql("DELETE FROM workspace_memberships WHERE workspace_id=" + sql_quote(workspace) + " AND user_id=" + sql_quote(owner))
    former = support("GET", f"/api/support/tickets/{ticket_id}", owner, owner_email)
    expect(former[0] == 404, f"former member retained ticket access status={former[0]}")
    return {
        "same_workspace_non_requester_status": same_ws[0],
        "foreign_workspace_status": foreign[0],
        "former_member_status": former[0],
        "foreign_workspace_fixture_distinct": foreign_workspace != workspace,
        "list_is_requester_scoped": True,
    }


def case_t003():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T003")
    created = create_ticket("P14-T003", workspace, actor, email)
    expect(created[0] == 201, f"create status={created[0]}")
    ticket = created[3]["ticket"]
    ticket_id = ticket["id"]

    listed = support("GET", f"/api/support/tickets?workspace_id={workspace}", actor, email)
    expect(listed[0] == 200 and len(listed[3].get("items", [])) == 1, "list did not return own ticket")
    detail = support("GET", f"/api/support/tickets/{ticket_id}", actor, email)
    expect(detail[0] == 200 and detail[3]["ticket"]["id"] == ticket_id, "detail mismatch")

    reply_key = unique("p14-t003-reply")
    reply1 = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                     body={"message": "requester follow-up"}, idempotency=reply_key, correlation=unique("t003-reply"))
    reply2 = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                     body={"message": "requester follow-up"}, idempotency=reply_key, correlation=unique("t003-reply-replay"))
    expect(reply1[0] == 201 and reply1[3]["created"] is True, f"first reply status={reply1[0]}")
    expect(reply2[0] == 200 and reply2[3]["created"] is False, f"reply replay status={reply2[0]}")
    expect(reply1[3]["message_id"] == reply2[3]["message_id"], "reply idempotency changed message id")

    admin_key = unique("p14-t003-support-reply")
    admin1 = admin_ticket("POST", f"/api/admin/support/tickets/{ticket_id}/replies",
                          body={"kind": "support_reply", "message": "support answer"}, idempotency=admin_key,
                          correlation=unique("t003-admin-reply"))
    admin2 = admin_ticket("POST", f"/api/admin/support/tickets/{ticket_id}/replies",
                          body={"kind": "support_reply", "message": "support answer"}, idempotency=admin_key,
                          correlation=unique("t003-admin-replay"))
    expect(admin1[0] == 201 and admin1[3]["created"] is True, f"admin reply status={admin1[0]} body={admin1[2][:200]!r}")
    expect(admin2[0] == 200 and admin2[3]["created"] is False, f"admin replay status={admin2[0]}")
    expect(admin1[3]["message"]["id"] == admin2[3]["message"]["id"], "admin replay changed message id")
    expect(admin1[3]["ticket"]["status"] == "awaiting_user", "support reply did not transition awaiting_user")

    closed = support("POST", f"/api/support/tickets/{ticket_id}/close", actor, email, correlation=unique("t003-close"))
    replay_close = support("POST", f"/api/support/tickets/{ticket_id}/close", actor, email, correlation=unique("t003-close-replay"))
    expect(closed[0] == 200 and closed[3]["changed"] is True and closed[3]["ticket"]["status"] == "closed", "close failed")
    expect(replay_close[0] == 200 and replay_close[3]["changed"] is False, "close replay was not idempotent")
    after_close = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                          body={"message": "must fail"}, idempotency=unique("closed-reply"))
    expect(after_close[0] == 409, f"closed ticket accepted reply status={after_close[0]}")
    return {
        "create_list_detail": True,
        "requester_reply_idempotent": True,
        "support_reply_idempotent": True,
        "support_reply_status": "awaiting_user",
        "close_idempotent": True,
        "closed_reply_status": after_close[0],
        "message_count": int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_messages WHERE ticket_id=" + sql_quote(ticket_id))),
    }


def case_t004():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T004")
    created = create_ticket("P14-T004", workspace, actor, email)
    expect(created[0] == 201, "fixture ticket create failed")
    before = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_messages"))
    forged = "tkt_" + "0" * 32
    direct = support("GET", f"/api/support/tickets/{forged}", actor, email)
    mutate = support("POST", f"/api/support/tickets/{forged}/replies", actor, email,
                     body={"message": "forged"}, idempotency=unique("forged"))
    admin = admin_ticket("GET", f"/api/admin/support/tickets/{forged}")
    expect(direct[0] == 404 and mutate[0] == 404 and admin[0] == 404, "forged ticket identifier did not fail closed")
    after = int(mysql_scalar("SELECT COUNT(*) FROM support_ticket_messages"))
    expect(after == before, "forged mutation changed durable messages")
    return {
        "requester_forged_get_status": direct[0],
        "requester_forged_reply_status": mutate[0],
        "admin_forged_get_status": admin[0],
        "durable_message_delta": after - before,
    }


def case_t005():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T005")
    created = create_ticket("P14-T005", workspace, actor, email, category="custom-domain-access")
    expect(created[0] == 201, f"custom-domain ticket create status={created[0]}")
    ticket_id = created[3]["ticket"]["id"]
    request = mysql_rows(
        "SELECT workspace_id,support_ticket_id,status FROM custom_domain_entitlement_requests WHERE support_ticket_id=" + sql_quote(ticket_id)
    )
    expect(request == [[workspace, ticket_id, "requested"]], f"request projection mismatch {request}")
    source_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=" + sql_quote(workspace)))
    domain_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domains WHERE workspace_id=" + sql_quote(workspace)))
    expect(source_count == 0 and domain_count == 0, "request projection created grant/domain authority")
    return {
        "request_rows": 1,
        "request_status": "requested",
        "resolved_source_expected": "none",
        "grant_authority": "NONE",
        "entitlement_source_rows": source_count,
        "custom_domain_rows": domain_count,
    }


def case_t006():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T006")
    mysql(
        "INSERT INTO custom_domain_entitlement_sources "
        "(workspace_id,source,source_key,status,domain_limit,starts_at) VALUES ("
        + ",".join([sql_quote(workspace), "'plan'", sql_quote(unique("p14-t006-plan")), "'active'", "3", "UTC_TIMESTAMP(6)-INTERVAL 1 DAY"]) + ")"
    )
    before_source = int(mysql_scalar("SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=" + sql_quote(workspace)))
    created = create_ticket("P14-T006", workspace, actor, email, category="custom-domain-access")
    expect(created[0] == 201, f"ticket create with active plan status={created[0]}")
    request_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domain_entitlement_requests WHERE workspace_id=" + sql_quote(workspace)))
    source_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=" + sql_quote(workspace)))
    domain_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domains WHERE workspace_id=" + sql_quote(workspace)))
    expect(request_count == 0, "active plan was duplicated by support access request")
    expect(source_count == before_source == 1 and domain_count == 0, "ticket creation changed entitlement/domain authority")
    source = mysql_rows("SELECT source,status,domain_limit FROM custom_domain_entitlement_sources WHERE workspace_id=" + sql_quote(workspace))
    expect(source == [["plan", "active", "3"]], f"plan authority mutated {source}")
    return {
        "existing_plan_preserved": True,
        "request_rows_created": request_count,
        "entitlement_source_delta": source_count - before_source,
        "custom_domain_rows": domain_count,
    }


def case_t007():
    reset_p14()
    workspace, actor, email = seed_workspace("P14-T007")
    created = create_ticket("P14-T007", workspace, actor, email, category="custom-domain-access")
    expect(created[0] == 201, "custom-domain ticket fixture failed")
    ticket_id = created[3]["ticket"]["id"]
    baseline_request = mysql_rows(
        "SELECT status,support_ticket_id FROM custom_domain_entitlement_requests WHERE workspace_id=" + sql_quote(workspace)
    )
    expect(baseline_request == [["requested", ticket_id]], f"baseline request mismatch {baseline_request}")
    reply = support("POST", f"/api/support/tickets/{ticket_id}/replies", actor, email,
                    body={"message": "additional context"}, idempotency=unique("p14-t007-reply"), correlation=unique("t007-reply"))
    expect(reply[0] == 201, f"reply status={reply[0]}")
    closed = support("POST", f"/api/support/tickets/{ticket_id}/close", actor, email, correlation=unique("t007-close"))
    expect(closed[0] == 200 and closed[3]["ticket"]["status"] == "closed", "close failed")
    after_request = mysql_rows(
        "SELECT status,support_ticket_id FROM custom_domain_entitlement_requests WHERE workspace_id=" + sql_quote(workspace)
    )
    source_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domain_entitlement_sources WHERE workspace_id=" + sql_quote(workspace)))
    domain_count = int(mysql_scalar("SELECT COUNT(*) FROM custom_domains WHERE workspace_id=" + sql_quote(workspace)))
    expect(after_request == baseline_request and source_count == 0 and domain_count == 0, "reply/close altered entitlement authority")
    return {
        "request_linkage_unchanged": True,
        "request_status": "requested",
        "entitlement_source_rows": source_count,
        "custom_domain_rows": domain_count,
        "ticket_closed": True,
    }


CASES = {
    "P14-T001": case_t001,
    "P14-T002": case_t002,
    "P14-T003": case_t003,
    "P14-T004": case_t004,
    "P14-T005": case_t005,
    "P14-T006": case_t006,
    "P14-T007": case_t007,
}
