#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json

from integration_common import *
from integration_case_common import *

def case_006():
    plan = create_plan("P13-T006", amount_minor=3300, entitlements={"links": 330})
    ws, actor, email = create_workspace("P13-T006")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t006-key"))
    expect(order_result[0] == 201, "order create failed")
    order = order_result[3]["order"]
    before_invoice = mysql_rows(
        f"SELECT status,currency,amount_minor FROM billing_invoices WHERE order_id={sql_quote(order['id'])}"
    )
    expect(before_invoice == [["open", "USD", "3300"]], f"initial invoice mismatch {before_invoice}")
    expect(int(mysql_scalar(f"SELECT COUNT(*) FROM billing_transactions WHERE order_id={sql_quote(order['id'])}")) == 0,
           "transaction exists before callback")
    paid_payload = callback_payload(order, event_id=unique("p13-t006-paid-event"),
                                    transaction_id=unique("p13-t006-txn"), outcome="paid")
    paid = send_callback("stripe", paid_payload)
    expect(paid[0] == 200, f"paid callback failed {paid[0]}")
    paid_state = mysql_rows(
        f"SELECT o.status,i.status,t.status,o.currency,o.amount_minor,i.currency,i.amount_minor,t.currency,t.amount_minor "
        f"FROM billing_orders o JOIN billing_invoices i ON i.order_id=o.id "
        f"JOIN billing_transactions t ON t.order_id=o.id WHERE o.id={sql_quote(order['id'])}"
    )
    expect(paid_state == [["paid", "paid", "paid", "USD", "3300", "USD", "3300", "USD", "3300"]],
           f"paid financial state mismatch {paid_state}")
    invalid_transition = callback_payload(
        order, event_id=unique("p13-t006-late-failed"), transaction_id=paid_payload["transaction_id"],
        outcome="failed", event_type="payment.failed"
    )
    failed_after_paid = send_callback("stripe", invalid_transition)
    expect(failed_after_paid[0] == 409, f"paid->failed transition accepted {failed_after_paid[0]}")
    refund_payload, refund = refund_order("P13-T006", order, paid_payload)
    final_state = mysql_rows(
        f"SELECT o.status,i.status,t.status,o.currency,o.amount_minor,i.currency,i.amount_minor,t.currency,t.amount_minor "
        f"FROM billing_orders o JOIN billing_invoices i ON i.order_id=o.id "
        f"JOIN billing_transactions t ON t.order_id=o.id WHERE o.id={sql_quote(order['id'])}"
    )
    expect(final_state == [["refunded", "refunded", "refunded", "USD", "3300", "USD", "3300", "USD", "3300"]],
           f"refund financial state mismatch {final_state}")
    return {
        "order_id": order["id"], "initial_invoice_status": "open", "paid_state": paid_state[0][:3],
        "paid_to_failed_status": failed_after_paid[0], "final_state": final_state[0][:3],
        "money_immutable": True, "refund_event": refund_payload["event_id"],
    }

def case_007():
    ws, actor, email = create_workspace("P13-T007")
    plan = create_plan("P13-T007", entitlements={"links": 70})
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t007-key"))
    order = order_result[3]["order"]
    before = int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events"))
    known_statuses = {}
    for provider in ["alipay", "wechat", "epay", "paypal", "stripe", "crypto"]:
        payload = callback_payload(order, event_id=unique(f"p13-t007-{provider}-event"),
                                   transaction_id=unique(f"p13-t007-{provider}-txn"), outcome="paid")
        result = send_callback(provider, payload, valid_signature=False)
        known_statuses[provider] = result[0]
        expect(result[0] == 401, f"known provider {provider} did not fail invalid credential closed: {result[0]}")
    unknown_payload = callback_payload(order, event_id=unique("p13-t007-unknown-event"),
                                       transaction_id=unique("p13-t007-unknown-txn"), outcome="paid")
    unknown = send_callback("not-a-provider", unknown_payload, valid_signature=False)
    expect(unknown[0] == 404, f"unknown provider accepted {unknown[0]}")
    after = int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events"))
    expect(before == after, f"invalid/unknown provider callbacks mutated durable events {before}->{after}")
    columns = {row[0] for row in mysql_rows("SHOW COLUMNS FROM payment_callback_events")}
    forbidden_columns = {"raw_body", "signature", "secret", "credential", "payer_identity"}
    expect(not columns.intersection(forbidden_columns), f"callback secret columns present {columns.intersection(forbidden_columns)}")
    return {
        "frozen_providers": sorted(known_statuses), "known_invalid_signature_statuses": known_statuses,
        "unknown_provider_status": unknown[0], "durable_mutation_count": after - before,
        "raw_callback_secret_columns": [],
    }

def case_008():
    plan = create_plan("P13-T008", entitlements={"links": 80})
    ws, actor, email = create_workspace("P13-T008")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t008-key"))
    order = order_result[3]["order"]
    payload = callback_payload(order, event_id=unique("p13-t008-event"),
                               transaction_id=unique("p13-t008-txn"), outcome="paid")
    events_before = int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events"))
    grants_before = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing'"
    ))
    bad = send_callback("stripe", payload, valid_signature=False)
    expect(bad[0] == 401, f"invalid signature status={bad[0]}")
    missing_raw = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    missing_status = http_raw(
        "POST", "/api/payments/callbacks/stripe", raw_body=missing_raw, headers={"Content-Type": "application/json"}
    )[0]
    expect(missing_status == 401, f"missing signature status={missing_status}")
    events_after = int(mysql_scalar("SELECT COUNT(*) FROM payment_callback_events"))
    grants_after = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing'"
    ))
    order_status = mysql_scalar(f"SELECT status FROM billing_orders WHERE id={sql_quote(order['id'])}")
    expect(events_before == events_after and grants_before == grants_after and order_status == "pending",
           "unauthenticated callback mutated durable authority")
    return {
        "invalid_signature_status": bad[0], "missing_signature_status": missing_status,
        "callback_events_delta": events_after - events_before, "billing_grants_delta": grants_after - grants_before,
        "order_status": order_status,
    }

def case_009():
    plan = create_plan("P13-T009", entitlements={"links": 90})
    ws, actor, email = create_workspace("P13-T009")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t009-key"))
    order = order_result[3]["order"]
    payload = callback_payload(order, event_id=unique("p13-t009-event"),
                               transaction_id=unique("p13-t009-txn"), outcome="paid")
    first = send_callback("stripe", payload)
    second = send_callback("stripe", payload)
    expect(first[0] == second[0] == 200, f"callback replay statuses {first[0]}/{second[0]}")
    expect(first[3].get("duplicate") is False and second[3].get("duplicate") is True,
           f"duplicate ack mismatch first={first[3]} second={second[3]}")
    event_count = int(mysql_scalar(
        f"SELECT COUNT(*) FROM payment_callback_events WHERE provider='stripe' AND provider_event_id={sql_quote(payload['event_id'])}"
    ))
    tx_count = int(mysql_scalar(
        f"SELECT COUNT(*) FROM billing_transactions WHERE provider='stripe' AND provider_transaction_id={sql_quote(payload['transaction_id'])}"
    ))
    grant_count = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND source_type='billing' AND revoked_at IS NULL"
    ))
    note_count = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND category='billing' AND event_key='payment_succeeded'"
    ))
    expect((event_count, tx_count, grant_count, note_count) == (1, 1, 1, 1),
           f"replay duplicated authority event={event_count} tx={tx_count} grant={grant_count} note={note_count}")
    return {
        "event_id": payload["event_id"], "first_duplicate": first[3]["duplicate"],
        "replay_duplicate": second[3]["duplicate"], "callback_rows": event_count,
        "transaction_rows": tx_count, "active_billing_grants": grant_count, "payment_notifications": note_count,
    }

def case_010():
    plan = create_plan("P13-T010", amount_minor=4100, entitlements={"links": 410, "custom_domains": 2})
    ws, actor, email = create_workspace("P13-T010")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t010-key"))
    order = order_result[3]["order"]
    before = get_entitlement(ws["id"], actor, email, "links")
    expect(before[0] == 200 and before[3]["allowed"] is False, "optimistic order state granted entitlement")
    payload = callback_payload(order, event_id=unique("p13-t010-event"),
                               transaction_id=unique("p13-t010-txn"), outcome="paid")
    paid = send_callback("stripe", payload)
    expect(paid[0] == 200, "paid callback failed")
    links = get_entitlement(ws["id"], actor, email, "links")
    domains_ent = get_entitlement(ws["id"], actor, email, "custom_domains")
    expect(links[3]["allowed"] is True and int(links[3]["limit_value"]) == 410, f"links grant mismatch {links[3]}")
    expect(domains_ent[3]["allowed"] is True and int(domains_ent[3]["limit_value"]) == 2,
           f"domain grant mismatch {domains_ent[3]}")
    sub = mysql_rows(
        f"SELECT id,status,plan_id FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} AND status='active'"
    )
    p06 = mysql_rows(
        f"SELECT source,source_key,status,domain_limit FROM custom_domain_entitlement_sources "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND source='plan' AND source_key='p13:billing'"
    )
    provenance = mysql_scalar(
        f"SELECT JSON_UNQUOTE(JSON_EXTRACT(provenance_json,'$.source')) FROM entitlement_grants "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND capability='links' AND source_type='billing' AND revoked_at IS NULL"
    )
    expect(len(sub) == 1 and sub[0][1] == "active", f"active subscription mismatch {sub}")
    expect(p06 == [["plan", "p13:billing", "active", "2"]], f"P06 plan projection mismatch {p06}")
    expect(provenance == "billing", f"billing provenance mismatch {provenance}")
    return {
        "order_id": order["id"], "pre_callback_allowed": before[3]["allowed"],
        "links_limit": links[3]["limit_value"], "custom_domains_limit": domains_ent[3]["limit_value"],
        "active_subscription_id": sub[0][0], "p06_source": p06[0][:3], "provenance_source": provenance,
    }
