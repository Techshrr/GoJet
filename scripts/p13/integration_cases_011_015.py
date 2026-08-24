#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json

from integration_common import *
from integration_case_common import *

def case_011():
    plan = create_plan("P13-T011", entitlements={"links": 110})
    ws, actor, email = create_workspace("P13-T011")
    seed_grant(ws["id"], "links", "manual", "support-exception", 7)
    before = get_entitlement(ws["id"], actor, email, "links")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t011-key"))
    order = order_result[3]["order"]
    payload = callback_payload(order, event_id=unique("p13-t011-event"),
                               transaction_id=unique("p13-t011-txn"), outcome="failed", event_type="payment.failed")
    failed = send_callback("stripe", payload)
    expect(failed[0] == 200, f"failed payment callback failed {failed[0]}")
    after = get_entitlement(ws["id"], actor, email, "links")
    order_status = mysql_scalar(f"SELECT status FROM billing_orders WHERE id={sql_quote(order['id'])}")
    tx_status = mysql_scalar(
        f"SELECT status FROM billing_transactions WHERE provider='stripe' AND provider_transaction_id={sql_quote(payload['transaction_id'])}"
    )
    billing_grants = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing'"
    ))
    expect(before[3]["allowed"] is True and int(before[3]["limit_value"]) == 7, "manual fixture missing")
    expect(after[3]["allowed"] is True and int(after[3]["limit_value"]) == 7, "failed payment altered valid manual authority")
    expect(order_status == tx_status == "failed" and billing_grants == 0,
           f"failed payment fabricated entitlement order={order_status} tx={tx_status} grants={billing_grants}")
    return {
        "order_status": order_status, "transaction_status": tx_status,
        "manual_limit_before": before[3]["limit_value"], "manual_limit_after": after[3]["limit_value"],
        "billing_grants": billing_grants,
    }

def case_012():
    plan = create_plan("P13-T012", entitlements={"links": 120})
    ws, actor, email = create_workspace("P13-T012")
    manual_id = seed_grant(ws["id"], "links", "manual", "manual-survives-refund", 250)
    order, paid_payload, paid = paid_subscription("P13-T012", ws["id"], actor, email, plan)
    before_refund = get_entitlement(ws["id"], actor, email, "links")
    refund_payload, refund = refund_order("P13-T012", order, paid_payload)
    replay = send_callback("stripe", refund_payload)
    expect(replay[0] == 200 and replay[3].get("duplicate") is True, "refund replay not idempotent")
    after = get_entitlement(ws["id"], actor, email, "links")
    manual_revoked = mysql_scalar(f"SELECT IFNULL(DATE_FORMAT(revoked_at,'%Y-%m-%d'),'') FROM entitlement_grants WHERE id={manual_id}")
    billing_revoked = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND source_type='billing' AND revoked_at IS NOT NULL"
    ))
    states = mysql_rows(
        f"SELECT o.status,i.status,t.status FROM billing_orders o "
        f"JOIN billing_invoices i ON i.order_id=o.id JOIN billing_transactions t ON t.order_id=o.id "
        f"WHERE o.id={sql_quote(order['id'])}"
    )
    expect(int(before_refund[3]["limit_value"]) == 250 and int(after[3]["limit_value"]) == 250,
           "refund removed unrelated manual authority")
    expect(manual_revoked == "" and billing_revoked == 1, f"refund source isolation mismatch manual={manual_revoked} billing={billing_revoked}")
    expect(states == [["refunded", "refunded", "refunded"]], f"refund states mismatch {states}")
    return {
        "order_id": order["id"], "manual_limit_before": before_refund[3]["limit_value"],
        "manual_limit_after": after[3]["limit_value"], "manual_revoked": False,
        "revoked_billing_grants": billing_revoked, "financial_states": states[0],
        "refund_replay_duplicate": replay[3]["duplicate"],
    }

def case_013():
    plan = create_plan("P13-T013", currency="EUR", amount_minor=1300, entitlements={"links": 13})
    ws, actor, email = create_workspace("P13-T013")
    order_result = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t013-key"))
    expect(order_result[0] == 201 and isinstance(order_result[3], dict), f"EUR order create failed {order_result[0]}")
    order = order_result[3]["order"]
    expect(order["money"] == {"currency": "EUR", "amount_minor": 1300}, f"order currency mismatch {order['money']}")
    current = admin("PUT", "/api/admin/fx/USD/SGD", body={
        "rate": "1.345600000000", "source": "ecb", "as_of": iso(), "status": "current", "override_reason": ""
    }, correlation=unique("p13-t013-current"))
    expect(current[0] == 200 and current[3]["fx"]["status"] == "current", f"current FX failed {current[0]}")
    current_rate = current[3]["fx"]["rate"]
    stale = admin("PUT", "/api/admin/fx/USD/SGD", body={
        "rate": "1.344400000000", "source": "ecb", "as_of": iso(utc_now() - dt.timedelta(hours=12)),
        "status": "stale", "override_reason": ""
    }, correlation=unique("p13-t013-stale"))
    expect(stale[0] == 200 and stale[3]["fx"]["status"] == "stale", "stale FX not explicit")
    stale_rate = stale[3]["fx"]["rate"]
    provider_error = admin("POST", "/api/admin/fx/USD/SGD/provider-error", body={
        "source": "ecb", "as_of": iso()
    }, correlation=unique("p13-t013-provider-error"))
    expect(provider_error[0] == 200 and provider_error[3]["fx"]["status"] == "provider-error",
           "provider error state failed")
    expect(provider_error[3]["fx"]["rate"] == stale_rate, "provider error destroyed last valid rate")
    bad_override = admin("PUT", "/api/admin/fx/USD/SGD", body={
        "rate": "1.350000000000", "source": "manual", "as_of": iso(), "status": "override", "override_reason": ""
    }, correlation=unique("p13-t013-bad-override"))
    expect(bad_override[0] == 400, f"override without reason accepted {bad_override[0]}")
    override = admin("PUT", "/api/admin/fx/USD/SGD", body={
        "rate": "1.350000000000", "source": "manual", "as_of": iso(),
        "status": "override", "override_reason": "Treasury approved temporary rate"
    }, correlation=unique("p13-t013-override"))
    expect(override[0] == 200 and override[3]["fx"]["status"] == "override", "audited override failed")
    audits = mysql_rows(
        "SELECT action,result FROM billing_audit_events "
        "WHERE resource_type='billing_fx_rate' AND resource_id='USD/SGD' ORDER BY id"
    )
    actions = [row[0] for row in audits]
    expect("billing.fx.provider_error" in actions and "billing.fx.override" in actions, f"FX audits incomplete {actions}")
    return {
        "order_currency": order["money"]["currency"], "order_amount_minor": order["money"]["amount_minor"],
        "current_rate": current_rate, "stale_status": stale[3]["fx"]["status"],
        "provider_error_status": provider_error[3]["fx"]["status"],
        "provider_error_preserved_rate": provider_error[3]["fx"]["rate"],
        "bad_override_status": bad_override[0], "override_status": override[3]["fx"]["status"],
        "audit_actions": actions,
    }

def case_014():
    basic = create_plan("P13-T014-basic", amount_minor=1400, entitlements={"links": 100})
    pro = create_plan("P13-T014-pro", amount_minor=2800, entitlements={"links": 500})
    ws, actor, email = create_workspace("P13-T014")
    first_order, first_payload, _ = paid_subscription("P13-T014-basic", ws["id"], actor, email, basic)
    first_ent = get_entitlement(ws["id"], actor, email, "links")
    expect(int(first_ent[3]["limit_value"]) == 100, "basic entitlement mismatch")
    upgrade_order, upgrade_payload, _ = paid_subscription(
        "P13-T014-upgrade", ws["id"], actor, email, pro, kind="upgrade"
    )
    upgraded = get_entitlement(ws["id"], actor, email, "links")
    expect(int(upgraded[3]["limit_value"]) == 500, f"upgrade limit mismatch {upgraded[3]}")
    active_subs = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} AND status='active'"
    ))
    old_expired = mysql_scalar(
        f"SELECT status FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND plan_id={int(basic['id'])} ORDER BY created_at LIMIT 1"
    )
    active_grants = int(mysql_scalar(
        f"SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND capability='links' AND source_type='billing' AND revoked_at IS NULL"
    ))
    upgrade_notes = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND event_key='plan_upgraded'"
    ))
    expect(active_subs == 1 and old_expired == "expired" and active_grants == 1,
           f"upgrade authority mismatch active_subs={active_subs} old={old_expired} grants={active_grants}")
    expect(upgrade_notes == 1, "upgrade notification missing")
    return {
        "basic_order": first_order["id"], "upgrade_order": upgrade_order["id"],
        "basic_limit": first_ent[3]["limit_value"], "upgraded_limit": upgraded[3]["limit_value"],
        "active_subscriptions": active_subs, "old_subscription_status": old_expired,
        "active_billing_grants": active_grants, "upgrade_notifications": upgrade_notes,
    }

def case_015():
    high = create_plan("P13-T015-high", amount_minor=3000, entitlements={"links": 1000, "custom_domains": 4})
    low = create_plan("P13-T015-low", amount_minor=1500, entitlements={"links": 100, "custom_domains": 2})
    ws, actor, email = create_workspace("P13-T015")
    _, _, _ = paid_subscription("P13-T015-high", ws["id"], actor, email, high)
    current = mysql_rows(
        f"SELECT id,version FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} AND status='active'"
    )
    expect(len(current) == 1, f"current subscription missing {current}")
    current_id, current_version = current[0][0], int(current[0][1])
    scheduled = p13(
        "POST", f"/api/workspaces/{ws['id']}/billing/downgrade", actor, email,
        body={"target_plan_id": int(low["id"]), "expected_version": current_version},
        correlation=unique("p13-t015-downgrade"),
    )
    expect(scheduled[0] == 201 and scheduled[3]["created"] is True, f"downgrade schedule failed {scheduled[0]} {scheduled[2][:300]!r}")
    schedule = scheduled[3]["schedule"]
    expect(schedule["current"]["status"] == "grace" and schedule["target"]["status"] == "pending",
           f"downgrade states mismatch {schedule}")
    effective = dt.datetime.fromisoformat(schedule["effective_at"].replace("Z", "+00:00"))
    starts = dt.datetime.fromisoformat(schedule["grace_starts_at"].replace("Z", "+00:00"))
    seconds = int((effective - starts).total_seconds())
    expect(seconds == 7 * 24 * 3600, f"grace duration mismatch {seconds}")
    boundary = mysql_rows(
        f"SELECT "
        f"(SELECT DATE_FORMAT(ends_at,'%Y-%m-%dT%H:%i:%s.%fZ') FROM entitlement_grants "
        f" WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing' AND source_id={sql_quote(current_id)} AND capability='links'),"
        f"(SELECT DATE_FORMAT(starts_at,'%Y-%m-%dT%H:%i:%s.%fZ') FROM entitlement_grants "
        f" WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing' AND source_id={sql_quote(schedule['target']['id'])} AND capability='links')"
    )
    expect(boundary and boundary[0][0] == boundary[0][1], f"entitlement handoff boundary mismatch {boundary}")
    note = int(mysql_scalar(
        f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND event_key='downgrade_scheduled'"
    ))
    expect(note == 1, "downgrade notification missing")
    return {
        "current_subscription": current_id, "target_subscription": schedule["target"]["id"],
        "current_status": schedule["current"]["status"], "target_status": schedule["target"]["status"],
        "grace_seconds": seconds, "effective_at": schedule["effective_at"],
        "entitlement_boundary_equal": True, "downgrade_notifications": note,
    }
