#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json

from integration_common import *
from integration_case_common import *

def case_001():
    plan = create_plan("P13-T001", entitlements={"links": 123, "custom_domains": 2})
    status, headers, raw, data = request_json("GET", "/api/public/plans")
    expect(status == 200 and isinstance(data, dict), f"public plans status={status}")
    item = next((p for p in data.get("items", []) if int(p.get("id", 0)) == int(plan["id"])), None)
    expect(item is not None, "created active plan absent from public plans")
    expect("features" not in item, "generic features metadata leaked into public plan authority")
    ent = {e["capability"]: int(e["limit_value"]) for e in item.get("entitlements", [])}
    expect(ent == {"custom_domains": 2, "links": 123}, f"structured entitlements mismatch {ent}")
    expect("idempotency" not in raw.decode("utf-8", "replace").lower(), "private order material leaked")
    return {
        "plan_id": plan["id"], "capabilities": sorted(ent), "links_limit": ent["links"],
        "features_present": False, "public_status": status, "content_type": headers.get("content-type", ""),
    }

def case_002():
    plan = create_plan("P13-T002", status="draft", amount_minor=1500, entitlements={"links": 100})
    first = update_plan(plan, status="active", amount_minor=1600, entitlements={"links": 120}, expected_version=1)
    expect(first[0] == 200, f"activate plan status={first[0]} body={first[2][:300]!r}")
    active = first[3]["plan"]
    expect(int(active["version"]) == 2 and active["status"] == "active", "plan version/activation mismatch")
    stale = update_plan(plan, status="active", amount_minor=1700, expected_version=1)
    expect(stale[0] == 409, f"stale plan write status={stale[0]}")
    archived_result = update_plan(active, status="archived", expected_version=2)
    expect(archived_result[0] == 200, f"archive status={archived_result[0]}")
    archived = archived_result[3]["plan"]
    expect(archived["status"] == "archived" and int(archived["version"]) == 3, "archive result mismatch")
    terminal = update_plan(archived, status="archived", name="should-not-change", expected_version=3)
    expect(terminal[0] == 409, f"archived terminal write status={terminal[0]}")
    ws, actor, email = create_workspace("P13-T002")
    order = create_order(ws["id"], actor, email, int(plan["id"]), key=unique("p13-t002-idempotency"))
    expect(order[0] == 409, f"archived plan newly purchasable status={order[0]}")
    forged = request_json(
        "GET", "/api/admin/plans",
        headers=auth_headers("p13-t002-not-admin", "p13-t002-not-admin@example.test",
                             extra={"X-GoJet-Test-Billing-Permission": "billing.manage"}),
    )
    expect(forged[0] == 403, f"forged admin permission accepted status={forged[0]}")
    return {
        "plan_id": plan["id"], "active_version": active["version"], "archived_version": archived["version"],
        "stale_write_status": stale[0], "terminal_write_status": terminal[0],
        "archived_purchase_status": order[0], "forged_permission_status": forged[0],
    }

def case_003():
    ws, actor, email = create_workspace("P13-T003")
    capability = "links"
    seed_grant(ws["id"], capability, "baseline", "baseline", 10)
    seed_grant(ws["id"], capability, "billing", "billing-fixture", 20)
    seed_grant(ws["id"], capability, "inherited", "inherited-fixture", 25)
    seed_grant(ws["id"], capability, "manual", "manual-fixture", 30)
    deny_id = seed_grant(ws["id"], capability, "hard_deny", "security-suspension", 0)
    denied = get_entitlement(ws["id"], actor, email, capability)
    expect(denied[0] == 200 and denied[3]["allowed"] is False and denied[3]["reason"] == "hard_deny",
           f"hard deny did not win {denied[3]}")
    mysql(f"UPDATE entitlement_grants SET revoked_at=UTC_TIMESTAMP(6) WHERE id={deny_id}")
    allowed = get_entitlement(ws["id"], actor, email, capability)
    expect(allowed[0] == 200 and allowed[3]["allowed"] is True, "entitlement should become allowed")
    expect(int(allowed[3]["limit_value"]) == 30, f"limits added or wrong precedence {allowed[3]}")
    expect(int(allowed[3]["limit_value"]) != 85, "active grants were implicitly added")
    return {
        "workspace_id": ws["id"], "hard_deny_result": denied[3]["reason"],
        "effective_limit_after_revoke": allowed[3]["limit_value"], "non_additive": True,
    }

def case_004():
    plan = create_plan("P13-T004", entitlements={"links": 50})
    wa, owner, oe = create_workspace("P13-T004", suffix="owner-a")
    wb, foreign_owner, fe = create_workspace("P13-T004", suffix="owner-b")
    admin_actor, admin_email = "p13-t004-admin", "p13-t004-admin@example.test"
    member_actor, member_email = "p13-t004-member", "p13-t004-member@example.test"
    viewer_actor, viewer_email = "p13-t004-viewer", "p13-t004-viewer@example.test"
    seed_member(wa["id"], admin_actor, admin_email, "admin")
    seed_member(wa["id"], member_actor, member_email, "member")
    seed_member(wa["id"], viewer_actor, viewer_email, "viewer")
    owner_order = create_order(wa["id"], owner, oe, int(plan["id"]), key=unique("p13-t004-owner-key"))
    expect(owner_order[0] == 201, f"owner order denied {owner_order[0]}")
    forged_admin = create_order(
        wa["id"], admin_actor, admin_email, int(plan["id"]), key=unique("p13-t004-admin-key"), forged_role="owner"
    )
    expect(forged_admin[0] == 403, f"admin escalated via forged owner role {forged_admin[0]}")
    admin_ent = get_entitlement(wa["id"], admin_actor, admin_email, "links")
    expect(admin_ent[0] == 200, f"admin safe entitlement read denied {admin_ent[0]}")
    member_ledger = list_invoices(wa["id"], member_actor, member_email)
    viewer_ledger = list_payments(wa["id"], viewer_actor, viewer_email)
    expect(member_ledger[0] == 403 and viewer_ledger[0] == 403, "member/viewer financial ledger leaked")
    foreign = list_invoices(wa["id"], foreign_owner, fe)
    unknown = list_invoices("ws_p13_t004_unknown", foreign_owner, fe)
    expect(foreign[0] == 403 and unknown[0] == 403, f"foreign/unknown tenant denial mismatch {foreign[0]}/{unknown[0]}")
    return {
        "workspace_a": wa["id"], "workspace_b": wb["id"], "owner_order_status": owner_order[0],
        "admin_forged_owner_status": forged_admin[0], "admin_safe_read_status": admin_ent[0],
        "member_ledger_status": member_ledger[0], "viewer_ledger_status": viewer_ledger[0],
        "foreign_status": foreign[0], "unknown_status": unknown[0],
    }

def case_005():
    plan = create_plan("P13-T005", amount_minor=2500, entitlements={"links": 250})
    ws, actor, email = create_workspace("P13-T005")
    key = unique("p13-t005-idempotency-material")
    first = create_order(ws["id"], actor, email, int(plan["id"]), key=key)
    second = create_order(ws["id"], actor, email, int(plan["id"]), key=key)
    expect(first[0] == 201 and second[0] == 200, f"idempotent order statuses {first[0]}/{second[0]}")
    one, two = first[3]["order"], second[3]["order"]
    expect(one["id"] == two["id"], "retry created a second payable order")
    count = int(mysql_scalar(f"SELECT COUNT(*) FROM billing_orders WHERE workspace_id={sql_quote(ws['id'])}"))
    raw_match = int(mysql_scalar(
        f"SELECT COUNT(*) FROM billing_orders WHERE workspace_id={sql_quote(ws['id'])} "
        f"AND HEX(idempotency_key_hash)={sql_quote(key.upper())}"
    ))
    expect(count == 1 and raw_match == 0, f"idempotency persistence mismatch count={count} raw_match={raw_match}")
    invoice_count = int(mysql_scalar(f"SELECT COUNT(*) FROM billing_invoices WHERE order_id={sql_quote(one['id'])}"))
    expect(invoice_count == 1, "idempotent retry duplicated invoice")
    return {
        "order_id": one["id"], "first_status": first[0], "retry_status": second[0],
        "durable_order_count": count, "invoice_count": invoice_count, "raw_key_persisted": False,
    }
