#!/usr/bin/env python3
from __future__ import annotations

import datetime as dt
import json

from integration_common import *
from integration_case_common import *

def case_016():
    high = create_plan("P13-T016-high", amount_minor=3200, entitlements={"links": 1000, "custom_domains": 2})
    low = create_plan("P13-T016-low", amount_minor=1200, entitlements={"links": 100, "custom_domains": 1})
    ws, actor, email = create_workspace("P13-T016")
    _, _, _ = paid_subscription("P13-T016-high", ws["id"], actor, email, high)
    d1 = create_domain(ws["id"], actor, unique("one") + ".example.com")
    d2 = create_domain(ws["id"], actor, unique("two") + ".example.com")
    expect(d1[0] == d2[0] == 201, f"pre-downgrade domain allocation failed {d1[0]}/{d2[0]}")
    current = mysql_rows(
        f"SELECT id,version FROM workspace_subscriptions WHERE workspace_id={sql_quote(ws['id'])} AND status='active'"
    )[0]
    scheduled = p13(
        "POST", f"/api/workspaces/{ws['id']}/billing/downgrade", actor, email,
        body={"target_plan_id": int(low["id"]), "expected_version": int(current[1])},
        correlation=unique("p13-t016-downgrade"),
    )
    expect(scheduled[0] == 201, "downgrade schedule failed")
    schedule = scheduled[3]["schedule"]
    during = create_domain(ws["id"], actor, unique("during") + ".example.com")
    expect(during[0] == 409 and err_code(during[3]) == "entitlement_required",
           f"new domain mutation allowed during grace {during[0]} {during[3]}")
    existing_before = int(mysql_scalar(
        f"SELECT COUNT(*) FROM custom_domains WHERE workspace_id={sql_quote(ws['id'])} AND removed_at IS NULL"
    ))
    current_id = schedule["current"]["id"]
    target_id = schedule["target"]["id"]
    # Advance authoritative boundaries in the isolated test database. This changes time
    # fixtures only; no final business result is written directly.
    mysql(
        f"UPDATE entitlement_grants SET starts_at=UTC_TIMESTAMP(6)-INTERVAL 30 DAY,"
        f"ends_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND,updated_at=UTC_TIMESTAMP(6) "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing' AND source_id={sql_quote(current_id)}"
    )
    mysql(
        f"UPDATE entitlement_grants SET starts_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND,"
        f"ends_at=UTC_TIMESTAMP(6)+INTERVAL 30 DAY,updated_at=UTC_TIMESTAMP(6) "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND source_type='billing' AND source_id={sql_quote(target_id)}"
    )
    mysql(
        f"UPDATE custom_domain_entitlement_sources SET starts_at=UTC_TIMESTAMP(6)-INTERVAL 30 DAY,"
        f"degraded_at=UTC_TIMESTAMP(6)-INTERVAL 8 DAY,"
        f"grace_until=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND,expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND source='plan' AND source_key='p13:billing'"
    )
    mysql(
        f"UPDATE custom_domain_entitlement_sources SET starts_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND,"
        f"expires_at=UTC_TIMESTAMP(6)+INTERVAL 30 DAY,status='active',degraded_at=NULL,grace_until=NULL "
        f"WHERE workspace_id={sql_quote(ws['id'])} AND source='plan' AND source_key='p13:billing:target'"
    )
    after_ent = get_entitlement(ws["id"], actor, email, "custom_domains")
    expect(after_ent[0] == 200 and after_ent[3]["allowed"] is True and int(after_ent[3]["limit_value"]) == 1,
           f"post-boundary entitlement mismatch {after_ent[3]}")
    after_mutation = create_domain(ws["id"], actor, unique("after") + ".example.com")
    expect(after_mutation[0] == 409 and err_code(after_mutation[3]) == "domain_limit_reached",
           f"over-quota mutation not denied after expiry {after_mutation[0]} {after_mutation[3]}")
    existing_after = int(mysql_scalar(
        f"SELECT COUNT(*) FROM custom_domains WHERE workspace_id={sql_quote(ws['id'])} AND removed_at IS NULL"
    ))
    expect(existing_before == existing_after == 2, f"downgrade destroyed existing resources {existing_before}->{existing_after}")
    return {
        "existing_domains_before": existing_before, "during_grace_mutation_status": during[0],
        "during_grace_error": err_code(during[3]), "effective_limit_after_boundary": after_ent[3]["limit_value"],
        "post_boundary_mutation_status": after_mutation[0], "post_boundary_error": err_code(after_mutation[3]),
        "existing_domains_after": existing_after, "destructive_cleanup": False,
    }

def case_017():
    plan = create_plan("P13-T017", entitlements={"links": 170, "custom_domains": 2})
    ws, actor, email = create_workspace("P13-T017")
    _, _, _ = paid_subscription("P13-T017", ws["id"], actor, email, plan)
    ent = get_entitlement(ws["id"], actor, email, "custom_domains")
    expect(ent[3]["allowed"] is True and int(ent[3]["limit_value"]) == 2, "P13 domain entitlement missing")
    created = create_domain(ws["id"], actor, unique("p13-t017") + ".example.com")
    expect(created[0] == 201, f"P06 domain request blocked despite entitlement {created[0]}")
    domain_id = created[3]["domain"]["id"] if "domain" in created[3] else created[3].get("id")
    states = mysql_rows(
        f"SELECT routing_state,ownership_status,ingress_dns_status,https_status,risk_status "
        f"FROM custom_domains WHERE id={int(domain_id)}"
    )
    expect(states == [["pending", "pending", "pending", "pending", "missing"]],
           f"payment bypassed P06 safety axes {states}")
    no_ent_ws, no_ent_actor, _ = create_workspace("P13-T017", suffix="no-entitlement")
    denied = create_domain(no_ent_ws["id"], no_ent_actor, unique("p13-t017-denied") + ".example.com")
    expect(denied[0] == 409 and err_code(denied[3]) == "entitlement_required",
           f"P06 accepted custom domain without P13 plan authority {denied[0]} {denied[3]}")
    return {
        "entitlement_limit": ent[3]["limit_value"], "created_domain_id": domain_id,
        "post_payment_p06_axes": states[0], "routing_enabled_by_payment": False,
        "no_entitlement_mutation_status": denied[0], "no_entitlement_error": err_code(denied[3]),
    }

def case_018():
    plan = create_plan("P13-T018", entitlements={"links": 100})
    ws, actor, email = create_workspace("P13-T018")
    order, paid_payload, _ = paid_subscription("P13-T018", ws["id"], actor, email, plan)
    manual_id = seed_grant(ws["id"], "links", "manual", "manual-contract", 250,
                           provenance={"ticket": "SUP-P13-018", "approved_by": "support"})
    inherited_id = seed_grant(ws["id"], "links", "inherited", "legacy-contract", 200,
                              provenance={"source": "inherited-contract"})
    combined = get_entitlement(ws["id"], actor, email, "links")
    expect(int(combined[3]["limit_value"]) == 250, f"non-additive max/precedence mismatch {combined[3]}")
    _, _refund = refund_order("P13-T018", order, paid_payload)
    after_refund = get_entitlement(ws["id"], actor, email, "links")
    expect(int(after_refund[3]["limit_value"]) == 250, "refund removed manual/inherited contribution")
    hard_id = seed_grant(ws["id"], "links", "hard_deny", "security-contract", 0,
                         provenance={"reason": "security"})
    denied = get_entitlement(ws["id"], actor, email, "links")
    expect(denied[3]["allowed"] is False and denied[3]["reason"] == "hard_deny", "hard deny did not override manual")
    mysql(f"UPDATE entitlement_grants SET revoked_at=UTC_TIMESTAMP(6) WHERE id={hard_id}")
    restored = get_entitlement(ws["id"], actor, email, "links")
    manual_state = mysql_rows(
        f"SELECT source_type,source_id,limit_value,revoked_at IS NULL,JSON_VALID(provenance_json) "
        f"FROM entitlement_grants WHERE id IN ({manual_id},{inherited_id}) ORDER BY source_type"
    )
    expect(int(restored[3]["limit_value"]) == 250 and all(row[3] == "1" and row[4] == "1" for row in manual_state),
           f"manual/inherited provenance/state mismatch {manual_state}")
    return {
        "combined_limit": combined[3]["limit_value"], "after_refund_limit": after_refund[3]["limit_value"],
        "hard_deny_reason": denied[3]["reason"], "restored_limit": restored[3]["limit_value"],
        "manual_inherited_rows": manual_state,
    }
