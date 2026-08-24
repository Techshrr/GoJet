#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json

from integration_common import *
from integration_case_common import *

def case_019():
    plan = create_plan("P13-T019", entitlements={"links": 190})
    ws, actor, email = create_workspace("P13-T019")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t019-key"))
    order = order_result[3]["order"]
    payload = callback_payload(
        order, event_id=unique("p13-t019-event"), transaction_id=unique("p13-t019-txn"),
        outcome="paid", correlation=unique("p13-t019-callback")
    )
    callback = send_callback("stripe", payload)
    expect(callback[0] == 200, "audit fixture callback failed")
    audit_rows = mysql_rows(
        f"SELECT actor_id,action,resource_type,resource_id,request_correlation_id,result,CAST(metadata_json AS CHAR) "
        f"FROM billing_audit_events WHERE workspace_id={sql_quote(ws['id'])} ORDER BY id"
    )
    expect(audit_rows, "billing audit rows missing")
    flattened = "\n".join("\t".join(row) for row in audit_rows)
    secret = CALLBACK_SECRET
    expect(secret and secret not in flattened, "callback secret leaked into billing audit")
    expect(email not in flattened, "unnecessary payer/workspace email leaked into billing audit")
    expect("X-GoJet-Test-Callback-Signature" not in flattened, "callback signature header leaked into audit")
    expect(all(row[4].strip() and row[5] == "success" for row in audit_rows), f"audit correlation/result incomplete {audit_rows}")
    event_columns = {row[0] for row in mysql_rows("SHOW COLUMNS FROM payment_callback_events")}
    expect("raw_body" not in event_columns and "signature" not in event_columns and "secret" not in event_columns,
           "callback schema persists forbidden raw evidence")
    return {
        "audit_count": len(audit_rows), "actions": [row[1] for row in audit_rows],
        "all_have_correlation": True, "all_results_success": True,
        "callback_secret_present": False, "email_present": False, "raw_payload_columns_present": False,
    }

def case_020():
    high = create_plan("P13-T020-high", amount_minor=2200, entitlements={"links": 220, "custom_domains": 2})
    low = create_plan("P13-T020-low", amount_minor=1100, entitlements={"links": 110, "custom_domains": 1})
    ws, actor, email = create_workspace("P13-T020")
    order, paid_payload, _ = paid_subscription("P13-T020-paid", ws["id"], actor, email, high)
    paid_replay = send_callback("stripe", paid_payload)
    expect(paid_replay[0] == 200 and paid_replay[3].get("duplicate") is True, "paid notification replay fixture failed")
    current = mysql_rows(
        f"SELECT id,version FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} AND status='active'"
    )[0]
    scheduled = p13(
        "POST", f"/api/workspaces/{ws['id']}/billing/downgrade", actor, email,
        body={"target_plan_id": int(low["id"]), "expected_version": int(current[1])},
        correlation=unique("p13-t020-downgrade"),
    )
    expect(scheduled[0] == 201, "downgrade fixture failed")
    expiry_before = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND category='billing' AND event_key='entitlement_expiring'"
    ))
    sweep_first = run_internal_expiry_sweep(192)
    expiry_after_first = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND category='billing' AND event_key='entitlement_expiring'"
    ))
    sweep_second = run_internal_expiry_sweep(192)
    expiry_after_second = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND category='billing' AND event_key='entitlement_expiring'"
    ))
    expect(int(sweep_first.get("candidates", 0)) >= 1 and expiry_after_first > expiry_before,
           f"entitlement_expiring not produced sweep={sweep_first} counts={expiry_before}->{expiry_after_first}")
    expect(expiry_after_second == expiry_after_first,
           f"expiry notification not deduped {expiry_after_first}->{expiry_after_second} sweep={sweep_second}")
    rows = mysql_rows(
        f"SELECT event_key,dedupe_key,title,summary,COALESCE(deep_link,''),resource_type,resource_id "
        f"FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} AND category='billing' ORDER BY id"
    )
    events = [row[0] for row in rows]
    expect("payment_succeeded" in events and "downgrade_scheduled" in events and "entitlement_expiring" in events,
           f"billing events incomplete {events}")
    expect(events.count("payment_succeeded") == 1, f"payment notification replay duplicated {events}")
    expect(all(row[4] == "/app/billing" for row in rows), f"billing deep link not allowlisted {rows}")
    safe_text = "\n".join(row[2] + "\n" + row[3] for row in rows)
    expect(CALLBACK_SECRET not in safe_text and email not in safe_text, "billing notification leaked secret/PII")
    public_emit = p13(
        "POST", f"/api/workspaces/{ws['id']}/notifications", actor, email,
        body={"event_key": "forged-billing-event"}
    )
    expect(public_emit[0] >= 400, f"public notification emit endpoint unexpectedly succeeded status={public_emit[0]}")
    return {
        "billing_events": events, "payment_succeeded_count": events.count("payment_succeeded"),
        "expiry_candidates": sweep_first.get("candidates"),
        "expiry_count_after_first": expiry_after_first, "expiry_count_after_second": expiry_after_second,
        "deep_links": sorted(set(row[4] for row in rows)), "safe_summary": True,
        "public_emit_status": public_emit[0],
    }
