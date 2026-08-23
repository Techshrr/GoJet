#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json

from integration_common import *

SUPPORTED = {f"P12-T{i:03d}" for i in range(1, 19)}

def err_code(data):
    try:
        return data["error"]["code"]
    except Exception:
        return None

def invite(workspace: str, actor: str, email: str, invited: str, role: str = "member", minutes: int = 30):
    status, _, raw, data = p12(
        "POST", f"/api/workspaces/{workspace}/invitations", actor, email,
        body={"email": invited, "role": role, "expires_at": iso_after(minutes)},
        correlation=f"p12-invite-{actor}-{role}",
    )
    expect(status == 201 and isinstance(data, dict), f"invite status={status} body={raw[:300]!r}")
    expect("token" in data and "invitation" in data, "created invitation missing one-time token")
    return data

def run_case(case_id: str):
    observations = {}
    errors: list[str] = []
    number = int(case_id[-3:])
    directory = API_DIR
    if number in {3, 4, 5, 8, 16}:
        directory = RBAC_DIR
    elif number == 13:
        directory = AUDIT_DIR
    elif number == 17:
        directory = SECURITY_DIR

    try:
        if case_id == "P12-T001":
            a, ae = "p12-t001-owner", "p12-t001-owner@example.test"
            b, be = "p12-t001-other", "p12-t001-other@example.test"
            ws, membership = create_workspace(a, ae, "T001 Primary")
            other, _ = create_workspace(b, be, "T001 Other")
            status, _, _, listed = p12("GET", "/api/workspaces", a, ae, forged_role="viewer")
            expect(status == 200, f"list status={status}")
            ids = [item["id"] for item in listed["items"]]
            expect(ws["id"] in ids and other["id"] not in ids, f"membership list leaked {ids}")
            status, _, _, context = p12("GET", f"/api/workspaces/{ws['id']}", a, ae, forged_role="viewer")
            expect(status == 200 and context["membership"]["role"] == "owner", "authoritative context mismatch")
            expect(membership["role"] == "owner" and ws["created_by"] == a, "create did not atomically grant owner")
            observations = {"workspace_id": ws["id"], "membership_role": context["membership"]["role"], "visible_workspace_ids": ids}

        elif case_id == "P12-T002":
            owner, oe = "p12-t002-owner", "p12-t002-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T002 v1")
            admin, ae = "p12-t002-admin", "p12-t002-admin@example.test"
            member, me = "p12-t002-member", "p12-t002-member@example.test"
            viewer, ve = "p12-t002-viewer", "p12-t002-viewer@example.test"
            seed_member(ws["id"], admin, ae, "admin")
            seed_member(ws["id"], member, me, "member")
            seed_member(ws["id"], viewer, ve, "viewer")
            first = p12("PATCH", f"/api/workspaces/{ws['id']}", owner, oe, body={"name":"T002 v2","expected_version":1,"reason":"owner"}, forged_role="viewer")
            expect(first[0] == 200 and first[3]["version"] == 2, f"owner update failed {first[0]}")
            second = p12("PATCH", f"/api/workspaces/{ws['id']}", admin, ae, body={"name":"T002 v3","expected_version":2,"reason":"admin"}, forged_role="viewer")
            expect(second[0] == 200 and second[3]["version"] == 3, f"admin update failed {second[0]}")
            denied_member = p12("PATCH", f"/api/workspaces/{ws['id']}", member, me, body={"name":"bad","expected_version":3}, forged_role="owner")
            denied_viewer = p12("PATCH", f"/api/workspaces/{ws['id']}", viewer, ve, body={"name":"bad","expected_version":3}, forged_role="owner")
            stale = p12("PATCH", f"/api/workspaces/{ws['id']}", owner, oe, body={"name":"stale","expected_version":2}, forged_role="owner")
            expect(denied_member[0] == 403 and denied_viewer[0] == 403, "member/viewer metadata mutation allowed")
            expect(stale[0] == 409, f"stale write status={stale[0]}")
            final = p12("GET", f"/api/workspaces/{ws['id']}", owner, oe)[3]["workspace"]
            expect(final["name"] == "T002 v3" and final["version"] == 3, "stale write changed authority")
            observations = {"final_version": final["version"], "member_status": denied_member[0], "viewer_status": denied_viewer[0], "stale_status": stale[0]}

        elif case_id == "P12-T003":
            owner, oe = "p12-t003-owner", "p12-t003-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T003")
            admin, ae = "p12-t003-admin", "p12-t003-admin@example.test"
            member, me = "p12-t003-member", "p12-t003-member@example.test"
            viewer, ve = "p12-t003-viewer", "p12-t003-viewer@example.test"
            seed_member(ws["id"], admin, ae, "admin")
            seed_member(ws["id"], member, me, "member")
            seed_member(ws["id"], viewer, ve, "viewer")
            owner_mut = p12("PATCH", f"/api/workspaces/{ws['id']}", owner, oe, body={"name":"T003 owner","expected_version":1}, forged_role="viewer")
            org = p12("GET", f"/api/workspaces/{ws['id']}/organization", admin, ae)[3]
            admin_mut = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", admin, ae, body={"name":"T003 Org","description":"admin","expected_version":org["version"]}, forged_role="viewer")
            member_mut = p12("POST", f"/api/workspaces/{ws['id']}/campaigns", member, me, body={"name":"member-campaign"}, forged_role="viewer")
            viewer_mut = p12("POST", f"/api/workspaces/{ws['id']}/campaigns", viewer, ve, body={"name":"viewer-campaign"}, forged_role="owner")
            expect(owner_mut[0] == 200, "owner denied due forged viewer header")
            expect(admin_mut[0] == 200, "admin mutation denied")
            expect(member_mut[0] == 201, "member resource mutation denied")
            expect(viewer_mut[0] == 403, "viewer escalated via forged owner header")
            observations = {
                "mysql_roles": {
                    "owner": mysql_scalar(f"SELECT role FROM workspace_memberships WHERE workspace_id={sql_quote(ws['id'])} AND user_id={sql_quote(owner)}"),
                    "admin": mysql_scalar(f"SELECT role FROM workspace_memberships WHERE workspace_id={sql_quote(ws['id'])} AND user_id={sql_quote(admin)}"),
                    "member": mysql_scalar(f"SELECT role FROM workspace_memberships WHERE workspace_id={sql_quote(ws['id'])} AND user_id={sql_quote(member)}"),
                    "viewer": mysql_scalar(f"SELECT role FROM workspace_memberships WHERE workspace_id={sql_quote(ws['id'])} AND user_id={sql_quote(viewer)}"),
                },
                "forged_viewer_owner_update": owner_mut[0],
                "forged_owner_viewer_update": viewer_mut[0],
            }

        elif case_id == "P12-T004":
            a, ae = "p12-t004-a", "p12-t004-a@example.test"
            b, be = "p12-t004-b", "p12-t004-b@example.test"
            wa, _ = create_workspace(a, ae, "T004 A")
            wb, _ = create_workspace(b, be, "T004 B")
            known = p12("GET", f"/api/workspaces/{wb['id']}", a, ae, forged_role="owner")
            random_id = "ws_nonexistent_p12_t004"
            unknown = p12("GET", f"/api/workspaces/{random_id}", a, ae, forged_role="owner")
            mutation = p12("PATCH", f"/api/workspaces/{wb['id']}", a, ae, body={"name":"leak","expected_version":1}, forged_role="owner")
            expect(known[0] == 403 and unknown[0] == 403 and mutation[0] == 403, "cross-workspace denial mismatch")
            expect(err_code(known[3]) == err_code(unknown[3]) == "forbidden", "known foreign ID leaked distinct denial")
            raw_known = known[2].decode("utf-8", "replace")
            expect(wb["name"] not in raw_known and b not in raw_known, "foreign workspace details leaked")
            observations = {"own_workspace": wa["id"], "foreign_status": known[0], "unknown_status": unknown[0], "mutation_status": mutation[0], "error_code": err_code(known[3])}

        elif case_id == "P12-T005":
            owner, oe = "p12-t005-owner", "p12-t005-owner@example.test"
            ws, owner_membership = create_workspace(owner, oe, "T005")
            owner2, o2e = "p12-t005-owner2", "p12-t005-owner2@example.test"
            admin, ae = "p12-t005-admin", "p12-t005-admin@example.test"
            member, me = "p12-t005-member", "p12-t005-member@example.test"
            viewer, ve = "p12-t005-viewer", "p12-t005-viewer@example.test"
            owner2_id = seed_member(ws["id"], owner2, o2e, "owner")
            seed_member(ws["id"], admin, ae, "admin")
            member_id = seed_member(ws["id"], member, me, "member")
            viewer_id = seed_member(ws["id"], viewer, ve, "viewer")
            admin_touch_owner = p12("PATCH", f"/api/workspaces/{ws['id']}/members/{owner2_id}", admin, ae, body={"role":"member"}, forged_role="owner")
            admin_grant_owner = p12("PATCH", f"/api/workspaces/{ws['id']}/members/{member_id}", admin, ae, body={"role":"owner"}, forged_role="owner")
            self_escalate = p12("PATCH", f"/api/workspaces/{ws['id']}/members/{member_id}", member, me, body={"role":"owner"}, forged_role="owner")
            viewer_manage = p12("DELETE", f"/api/workspaces/{ws['id']}/members/{member_id}", viewer, ve, forged_role="owner")
            admin_manage_viewer = p12("PATCH", f"/api/workspaces/{ws['id']}/members/{viewer_id}", admin, ae, body={"role":"member"}, forged_role="viewer")
            owner_promote = p12("PATCH", f"/api/workspaces/{ws['id']}/members/{member_id}", owner, oe, body={"role":"owner"}, forged_role="viewer")
            expect(admin_touch_owner[0] == admin_grant_owner[0] == self_escalate[0] == viewer_manage[0] == 403, "role boundary failed")
            expect(admin_manage_viewer[0] == 200 and admin_manage_viewer[3]["role"] == "member", "admin normal management failed")
            expect(owner_promote[0] == 200 and owner_promote[3]["role"] == "owner", "owner promotion failed")
            observations = {
                "owner_member_id": owner_membership["id"], "second_owner_id": owner2_id,
                "admin_touch_owner": admin_touch_owner[0], "admin_grant_owner": admin_grant_owner[0],
                "member_self_escalate": self_escalate[0], "viewer_manage": viewer_manage[0],
                "admin_normal_manage": admin_manage_viewer[0], "owner_promote": owner_promote[0],
            }

        elif case_id == "P12-T006":
            owner, oe = "p12-t006-owner", "p12-t006-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T006")
            created = invite(ws["id"], owner, oe, "Invitee@Example.Test", "member")
            token = created["token"]
            invitation = created["invitation"]
            listed = p12("GET", f"/api/workspaces/{ws['id']}/invitations", owner, oe)
            duplicate = p12("POST", f"/api/workspaces/{ws['id']}/invitations", owner, oe, body={"email":" invitee@example.test ","role":"member","expires_at":iso_after(30)})
            owner_role = p12("POST", f"/api/workspaces/{ws['id']}/invitations", owner, oe, body={"email":"badowner@example.test","role":"owner","expires_at":iso_after(30)})
            admin, ae = "p12-t006-admin", "p12-t006-admin@example.test"
            seed_member(ws["id"], admin, ae, "admin")
            admin_owner = p12("POST", f"/api/workspaces/{ws['id']}/invitations", admin, ae, body={"email":"badadmin@example.test","role":"owner","expires_at":iso_after(30)}, forged_role="owner")
            db_hash = mysql_scalar(f"SELECT token_hash FROM workspace_invitations WHERE id={int(invitation['id'])}")
            raw_token_match = int(mysql_scalar(f"SELECT COUNT(*) FROM workspace_invitations WHERE token_hash={sql_quote(token)}"))
            revoke = p12("DELETE", f"/api/workspaces/{ws['id']}/invitations/{invitation['id']}", owner, oe)
            after = p12("GET", f"/api/workspaces/{ws['id']}/invitations", owner, oe)[3]["items"]
            raw_list = json.dumps(listed[3], ensure_ascii=False)
            expect(listed[0] == 200 and token not in raw_list, "list leaked raw invitation token")
            expect(duplicate[0] == 409, f"normalized duplicate status={duplicate[0]}")
            expect(owner_role[0] == 400 and admin_owner[0] == 403, "owner invitation grant accepted")
            expect(len(db_hash) == 64 and db_hash != token and raw_token_match == 0, "raw token persisted")
            expect(revoke[0] == 204 and any(i["id"] == invitation["id"] and i["status"] == "revoked" for i in after), "revoke lifecycle failed")
            observations = {"invitation_id": invitation["id"], "token_hash_length": len(db_hash), "raw_token_persisted": False, "duplicate_status": duplicate[0], "owner_role_status": owner_role[0], "admin_owner_status": admin_owner[0], "revoke_status": revoke[0]}

        elif case_id == "P12-T007":
            owner, oe = "p12-t007-owner", "p12-t007-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T007")
            accepted = invite(ws["id"], owner, oe, "p12-t007-accept@example.test")
            token = accepted["token"]
            unsigned = request_json("GET", f"/api/invitations/{token}")
            inspect = p12("GET", f"/api/invitations/{token}", "p12-t007-accept", "p12-t007-accept@example.test")
            mismatch_inspect = p12("GET", f"/api/invitations/{token}", "p12-t007-wrong", "wrong@example.test")
            mismatch_accept = p12("POST", "/api/invitations/accept", "p12-t007-wrong", "wrong@example.test", body={"token":token})
            accepted_call = p12("POST", "/api/invitations/accept", "p12-t007-accept", "p12-t007-accept@example.test", body={"token":token})
            replay = p12("POST", "/api/invitations/accept", "p12-t007-accept", "p12-t007-accept@example.test", body={"token":token})
            rejected = invite(ws["id"], owner, oe, "p12-t007-reject@example.test")
            reject1 = p12("POST", "/api/invitations/reject", "p12-t007-reject", "p12-t007-reject@example.test", body={"token":rejected["token"]})
            reject2 = p12("POST", "/api/invitations/reject", "p12-t007-reject", "p12-t007-reject@example.test", body={"token":rejected["token"]})
            revoked = invite(ws["id"], owner, oe, "p12-t007-revoked@example.test")
            p12("DELETE", f"/api/workspaces/{ws['id']}/invitations/{revoked['invitation']['id']}", owner, oe)
            revoked_inspect = p12("GET", f"/api/invitations/{revoked['token']}", "p12-t007-revoked", "p12-t007-revoked@example.test")
            revoked_accept = p12("POST", "/api/invitations/accept", "p12-t007-revoked", "p12-t007-revoked@example.test", body={"token":revoked["token"]})
            expired_inv = invite(ws["id"], owner, oe, "p12-t007-expired@example.test")
            mysql(f"UPDATE workspace_invitations SET expires_at=DATE_SUB(CURRENT_TIMESTAMP(6), INTERVAL 1 SECOND) WHERE id={int(expired_inv['invitation']['id'])}")
            expired_inspect = p12("GET", f"/api/invitations/{expired_inv['token']}", "p12-t007-expired", "p12-t007-expired@example.test")
            expired_accept = p12("POST", "/api/invitations/accept", "p12-t007-expired", "p12-t007-expired@example.test", body={"token":expired_inv["token"]})
            unknown = p12("GET", "/api/invitations/p12-unknown-token", "p12-t007-x", "p12-t007-x@example.test")
            expect(unsigned[0] == 401, f"unauth inspection status={unsigned[0]}")
            expect(inspect[0] == 200 and inspect[3]["account_match"] is True and "email" not in inspect[3], "safe matching inspection failed")
            expect(mismatch_inspect[0] == 200 and mismatch_inspect[3]["account_match"] is False and mismatch_accept[0] == 403, "account mismatch not fail-closed")
            expect(accepted_call[0] == 200 and accepted_call[3]["role"] == "member" and replay[0] == 409, "accept single-use failed")
            expect(reject1[0] == 204 and reject2[0] == 409, "reject single-use failed")
            expect(revoked_inspect[0] == 200 and revoked_inspect[3]["status"] == "revoked" and revoked_accept[0] == 409, "revoked state failed")
            expect(expired_inspect[0] == 200 and expired_inspect[3]["status"] == "expired" and expired_accept[0] == 410, "expired state failed")
            expect(unknown[0] == 404, f"unknown token status={unknown[0]}")
            observations = {"unauthenticated":unsigned[0],"valid":inspect[3],"mismatch_accept":mismatch_accept[0],"accepted":accepted_call[0],"replay":replay[0],"rejected":[reject1[0],reject2[0]],"revoked":[revoked_inspect[3]["status"],revoked_accept[0]],"expired":[expired_inspect[3]["status"],expired_accept[0]],"unknown":unknown[0]}

        elif case_id == "P12-T008":
            owner, oe = "p12-t008-owner", "p12-t008-owner@example.test"
            ws, m1 = create_workspace(owner, oe, "T008")
            owner2_id = seed_member(ws["id"], "p12-t008-owner2", "p12-t008-owner2@example.test", "owner")
            result = producer("--action","last-owner-race","--workspace",ws["id"],"--member-a",str(m1["id"]),"--member-b",str(owner2_id))
            outcomes = result["outcomes"]
            successes = sum(1 for o in outcomes if not o.get("error"))
            protected = sum(1 for o in outcomes if "last workspace owner is protected" in o.get("error",""))
            remaining = int(mysql_scalar(f"SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id={sql_quote(ws['id'])} AND role='owner'"))
            expect(successes == 1 and protected == 1 and remaining == 1, f"concurrent last-owner invariant failed outcomes={outcomes} remaining={remaining}")
            observations = {"outcomes": outcomes, "remaining_owner_count": remaining}

        elif case_id == "P12-T009":
            owner, oe = "p12-t009-owner", "p12-t009-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T009")
            admin, ae = "p12-t009-admin", "p12-t009-admin@example.test"
            member, me = "p12-t009-member", "p12-t009-member@example.test"
            viewer, ve = "p12-t009-viewer", "p12-t009-viewer@example.test"
            seed_member(ws["id"], admin, ae, "admin")
            seed_member(ws["id"], member, me, "member")
            seed_member(ws["id"], viewer, ve, "viewer")
            initial = p12("GET", f"/api/workspaces/{ws['id']}/organization", owner, oe)[3]
            owner_update = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", owner, oe, body={"name":"组织一","description":"owner","expected_version":initial["version"]})
            admin_update = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", admin, ae, body={"name":"组织二","description":"admin","expected_version":owner_update[3]["version"]})
            member_update = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", member, me, body={"name":"bad","description":"","expected_version":admin_update[3]["version"]}, forged_role="owner")
            viewer_update = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", viewer, ve, body={"name":"bad","description":"","expected_version":admin_update[3]["version"]}, forged_role="owner")
            stale = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", owner, oe, body={"name":"stale","description":"","expected_version":initial["version"]})
            invalid = p12("PATCH", f"/api/workspaces/{ws['id']}/organization", owner, oe, body={"name":"","description":"","expected_version":admin_update[3]["version"]})
            expect(owner_update[0] == admin_update[0] == 200, "owner/admin org update failed")
            expect(member_update[0] == viewer_update[0] == 403, "read-only org roles mutated")
            expect(stale[0] == 409 and invalid[0] == 400, "org validation/conflict mismatch")
            observations = {"final_name":admin_update[3]["name"],"final_version":admin_update[3]["version"],"member":member_update[0],"viewer":viewer_update[0],"stale":stale[0],"invalid":invalid[0]}

        elif case_id == "P12-T010":
            owner, oe = "p12-t010-owner", "p12-t010-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T010")
            campaign_resp = p12("POST", f"/api/workspaces/{ws['id']}/campaigns", owner, oe, body={"name":"Campaign Continuity"})
            expect(campaign_resp[0] == 201, f"campaign create={campaign_resp[0]}")
            campaign = campaign_resp[3]
            link = create_link(ws["id"], owner, "t010", "https://example.com/t010")
            produced = produce_analytics_event(ws["id"], int(link["id"]), campaign["id"], 1)
            expect(produced.get("inserted") is True, f"analytics event not inserted {produced}")
            conversion = legacy("POST", f"/api/workspaces/{ws['id']}/analytics/conversions", ws["id"], owner, role="owner", analytics=True, body={"conversion_id":"p12-t010-conversion","campaign_id":campaign["id"],"link_id":int(link["id"])})
            expect(conversion[0] == 201, f"P07 conversion rejected P12 campaign: {conversion[0]} {conversion[3]}")
            qs = query_string(campaign=campaign["id"], timezone="UTC", granularity="day")
            report = legacy("GET", f"/api/workspaces/{ws['id']}/analytics/overview?{qs}", ws["id"], owner, role="owner", analytics=True)
            expect(report[0] == 200 and report[3]["total_clicks"] == 1 and report[3]["total_conversions"] == 1, f"P07 campaign report mismatch {report[3]}")
            expect(any(x["value"] == campaign["id"] and x["clicks"] == 1 for x in report[3]["dimensions"]["campaign"]), "P07 campaign dimension did not preserve P12 ID")
            observations = {"campaign_id":campaign["id"],"link_id":link["id"],"conversion_status":conversion[0],"report_state":report[3]["state"],"clicks":report[3]["total_clicks"],"conversions":report[3]["total_conversions"]}

        elif case_id == "P12-T011":
            owner, oe = "p12-t011-owner", "p12-t011-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T011")
            link1 = create_link(ws["id"], owner, "t011a", "https://example.com/t011a")
            link2 = create_link(ws["id"], owner, "t011b", "https://example.com/t011b")
            tag_resp = p12("POST", f"/api/workspaces/{ws['id']}/tags", owner, oe, body={"name":"重要标签"})
            expect(tag_resp[0] == 201, f"Unicode tag create={tag_resp[0]}")
            tag = tag_resp[3]
            duplicate = p12("POST", f"/api/workspaces/{ws['id']}/tags", owner, oe, body={"name":"  重要标签  "})
            associate = p12("PATCH", f"/api/workspaces/{ws['id']}/links/organization", owner, oe, body={"link_ids":[int(link1["id"])],"campaign_id":None,"folder_id":None,"tag_ids":[int(tag["id"])]})
            expect(associate[0] == 200, f"tag association={associate[0]} {associate[3]}")
            listed = legacy("GET", f"/api/workspaces/{ws['id']}/links?tag={tag['id']}", ws["id"], owner)
            ids = [int(x["id"]) for x in listed[3]["items"]]
            delete = p12("DELETE", f"/api/workspaces/{ws['id']}/tags/{tag['id']}", owner, oe)
            expect(duplicate[0] == 409, f"normalized duplicate tag={duplicate[0]}")
            expect(listed[0] == 200 and ids == [int(link1["id"])], f"tag filter mismatch ids={ids}, other={link2['id']}")
            expect(delete[0] == 409 and err_code(delete[3]) == "resource_in_use", f"in-use tag delete={delete[0]}")
            observations = {"tag_id":tag["id"],"tag_name":tag["name"],"normalized_name":tag["normalized_name"],"duplicate_status":duplicate[0],"filtered_link_ids":ids,"in_use_delete":delete[0]}

        elif case_id == "P12-T012":
            owner, oe = "p12-t012-owner", "p12-t012-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T012")
            link1 = create_link(ws["id"], owner, "t012a", "https://example.com/t012a")
            link2 = create_link(ws["id"], owner, "t012b", "https://example.com/t012b")
            folder_resp = p12("POST", f"/api/workspaces/{ws['id']}/folders", owner, oe, body={"name":"客户资料"})
            expect(folder_resp[0] == 201, f"Unicode folder create={folder_resp[0]}")
            folder = folder_resp[3]
            associate = p12("PATCH", f"/api/workspaces/{ws['id']}/links/organization", owner, oe, body={"link_ids":[int(link1["id"])],"campaign_id":None,"folder_id":int(folder["id"]),"tag_ids":[]})
            listed = legacy("GET", f"/api/workspaces/{ws['id']}/links?folder={folder['id']}", ws["id"], owner)
            ids = [int(x["id"]) for x in listed[3]["items"]]
            foreign_owner, foe = "p12-t012-foreign", "p12-t012-foreign@example.test"
            foreign_ws, _ = create_workspace(foreign_owner, foe, "T012 Foreign")
            foreign_folder = p12("POST", f"/api/workspaces/{foreign_ws['id']}/folders", foreign_owner, foe, body={"name":"Foreign"})[3]
            cross = p12("PATCH", f"/api/workspaces/{ws['id']}/links/organization", owner, oe, body={"link_ids":[int(link2["id"])],"campaign_id":None,"folder_id":int(foreign_folder["id"]),"tag_ids":[]})
            delete = p12("DELETE", f"/api/workspaces/{ws['id']}/folders/{folder['id']}", owner, oe)
            expect(associate[0] == 200, f"folder association={associate[0]}")
            expect(listed[0] == 200 and ids == [int(link1["id"])], f"folder filter/bulk explicit IDs mismatch {ids}")
            expect(cross[0] == 403, f"foreign folder association status={cross[0]}")
            expect(delete[0] == 409 and err_code(delete[3]) == "resource_in_use", "in-use folder deletion did not fail explicitly")
            observations = {"folder_id":folder["id"],"folder_name":folder["name"],"filtered_link_ids":ids,"unselected_link_id":link2["id"],"foreign_folder_status":cross[0],"in_use_delete":delete[0]}

        elif case_id == "P12-T013":
            owner, oe = "p12-t013-owner", "p12-t013-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T013")
            secret_reason = "token=p12-super-secret"
            update = p12("PATCH", f"/api/workspaces/{ws['id']}", owner, oe, body={"name":"T013 changed","expected_version":1,"reason":secret_reason}, correlation="p12-t013-success")
            viewer, ve = "p12-t013-viewer", "p12-t013-viewer@example.test"
            seed_member(ws["id"], viewer, ve, "viewer")
            denied = p12("PATCH", f"/api/workspaces/{ws['id']}", viewer, ve, body={"name":"forbidden","expected_version":2,"reason":"password=hunter2"}, forged_role="owner", correlation="p12-t013-denied")
            rows = mysql(
                "SELECT actor_id,action,resource_type,resource_id,COALESCE(reason,''),request_correlation_id,result,CAST(metadata_json AS CHAR) "
                f"FROM workspace_audit_events WHERE workspace_id={sql_quote(ws['id'])} AND request_correlation_id IN ('p12-t013-success','p12-t013-denied') ORDER BY id"
            )
            expect(update[0] == 200 and denied[0] == 403, "audit setup requests failed")
            expect("p12-super-secret" not in rows and "hunter2" not in rows, f"audit stored raw secret: {rows}")
            expect("p12-t013-success" in rows and "p12-t013-denied" in rows and "success" in rows and "denied" in rows, "audit authority/correlation missing")
            observations = {"success_status":update[0],"denied_status":denied[0],"raw_secret_absent":True,"audit_rows":rows.splitlines()}

        elif case_id == "P12-T014":
            owner, oe = "p12-t014-owner", "p12-t014-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T014")
            first = produce_notification(ws["id"], owner, "p12-t014-dedupe", title="First notice", summary="dedupe")
            second = produce_notification(ws["id"], owner, "p12-t014-dedupe", title="Replay notice", summary="replay")
            count = int(mysql_scalar(f"SELECT COUNT(*) FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} AND recipient_user_id={sql_quote(owner)}"))
            page = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            expect(first["inserted"] is True and second["inserted"] is False, f"producer dedupe flags wrong {first}/{second}")
            expect(first["notification"]["id"] == second["notification"]["id"] and count == 1, "dedupe replay created duplicate row")
            expect(page[0] == 200 and len(page[3]["items"]) == 1, "dedupe API view mismatch")
            observations = {"notification_id":first["notification"]["id"],"first_inserted":first["inserted"],"replay_inserted":second["inserted"],"db_count":count,"api_count":len(page[3]["items"])}

        elif case_id == "P12-T015":
            owner, oe = "p12-t015-owner", "p12-t015-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T015")
            other, other_e = "p12-t015-other", "p12-t015-other@example.test"
            seed_member(ws["id"], other, other_e, "member")
            n1 = produce_notification(ws["id"], owner, "p12-t015-a")
            n2 = produce_notification(ws["id"], owner, "p12-t015-b")
            n3 = produce_notification(ws["id"], other, "p12-t015-other")
            initial = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            read = p12("POST", f"/api/workspaces/{ws['id']}/notifications/{n1['notification']['id']}/read", owner, oe)
            after_read = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            unread = p12("POST", f"/api/workspaces/{ws['id']}/notifications/{n1['notification']['id']}/unread", owner, oe)
            after_unread = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            mark_all = p12("POST", f"/api/workspaces/{ws['id']}/notifications/read-all", owner, oe)
            final = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            other_page = p12("GET", f"/api/workspaces/{ws['id']}/notifications", other, other_e)
            other_try = p12("POST", f"/api/workspaces/{ws['id']}/notifications/{n2['notification']['id']}/read", other, other_e)
            expect(initial[3]["unread_count"] == 2 and after_read[3]["unread_count"] == 1 and after_unread[3]["unread_count"] == 2, "read/unread count mismatch")
            expect(read[0] == unread[0] == mark_all[0] == 200 and final[3]["unread_count"] == 0, "read-all lifecycle failed")
            expect(other_page[3]["unread_count"] == 1 and other_try[0] == 404, "recipient scope violated")
            observations = {"owner_counts":[initial[3]["unread_count"],after_read[3]["unread_count"],after_unread[3]["unread_count"],final[3]["unread_count"]],"mark_all_updated":mark_all[3]["updated"],"other_unread":other_page[3]["unread_count"],"cross_recipient_status":other_try[0],"other_notification_id":n3["notification"]["id"]}

        elif case_id == "P12-T016":
            owner, oe = "p12-t016-owner", "p12-t016-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T016")
            link = create_link(ws["id"], owner, "t016", "https://example.com/t016")
            foreign_owner, foe = "p12-t016-foreign", "p12-t016-foreign@example.test"
            fws, _ = create_workspace(foreign_owner, foe, "T016 Foreign")
            foreign_link = create_link(fws["id"], foreign_owner, "t016f", "https://example.com/t016f")
            good = produce_notification(ws["id"], owner, "p12-t016-good", deep_link=f"/app/links/{link['id']}", resource_type="link", resource_id=str(link["id"]))
            bad = produce_notification(ws["id"], owner, "p12-t016-bad", deep_link=f"/app/links/{foreign_link['id']}", resource_type="link", resource_id=str(foreign_link["id"]))
            static = produce_notification(ws["id"], owner, "p12-t016-static", deep_link="/app/settings/workspace")
            external = produce_notification(ws["id"], owner, "p12-t016-external", deep_link="https://evil.example/path")
            page = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)[3]
            links = [item.get("deep_link","") for item in page["items"]]
            expect(f"/app/links/{link['id']}" in links, "authorized resource deep link removed")
            expect(f"/app/links/{foreign_link['id']}" not in links and "/app/notifications" in links, "foreign resource deep link not reauthorized")
            expect("/app/settings/workspace" in links, "registered static deep link rejected")
            expect("https://evil.example/path" not in links, "external deep link survived normalization")
            observations = {"authorized":f"/app/links/{link['id']}","foreign_requested":f"/app/links/{foreign_link['id']}","rendered_links":links,"producer_ids":[good["notification"]["id"],bad["notification"]["id"],static["notification"]["id"],external["notification"]["id"]]}

        elif case_id == "P12-T017":
            owner, oe = "p12-t017-owner", "p12-t017-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T017")
            jwt = "eyJabcdefghijk.abcdefghijklmnop.abcdefghijklmnop"
            first = produce_notification(ws["id"], owner, "p12-t017-email", title="Contact victim@example.test", summary="safe")
            second = produce_notification(ws["id"], owner, "p12-t017-bearer", title="Credential", summary="Bearer abcdefghijklmnop")
            third = produce_notification(ws["id"], owner, "p12-t017-jwt", title="JWT", summary=jwt)
            fourth = produce_notification(ws["id"], owner, "p12-t017-marker", title="token=p12-secret-token", summary="risk_evidence=private")
            page = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)[3]
            raw_api = json.dumps(page, ensure_ascii=False)
            db_dump = mysql(f"SELECT title,summary FROM workspace_notifications WHERE workspace_id={sql_quote(ws['id'])} ORDER BY id")
            for secret in ("victim@example.test","abcdefghijklmnop","p12-secret-token","risk_evidence=private"):
                expect(secret not in raw_api and secret not in db_dump, f"secret/PII persisted or returned: {secret}")
            redacted = [(item["title"], item["summary"]) for item in page["items"]]
            expect(any("[redacted]" in pair for pair in redacted), "redaction marker missing")
            observations = {"producer_ids":[first["notification"]["id"],second["notification"]["id"],third["notification"]["id"],fourth["notification"]["id"]],"redacted_pairs":redacted,"raw_secrets_absent":True}

        elif case_id == "P12-T018":
            owner, oe = "p12-t018-owner", "p12-t018-owner@example.test"
            ws, _ = create_workspace(owner, oe, "T018")
            partial_state = set_notification_state(ws["id"], "partial", "dependency_lag")
            partial = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            stale_state = set_notification_state(ws["id"], "stale", "source_stale")
            stale = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
            offline_status = None
            renamed = False
            try:
                mysql("RENAME TABLE workspace_notifications TO workspace_notifications_p12_offline")
                renamed = True
                offline = p12("GET", f"/api/workspaces/{ws['id']}/notifications", owner, oe)
                offline_status = offline[0]
                expect(offline_status >= 500, f"offline dependency masqueraded as success status={offline_status}")
            finally:
                if renamed:
                    mysql("RENAME TABLE workspace_notifications_p12_offline TO workspace_notifications")
            expect(partial[0] == 200 and partial[3]["state"]["status"] == "partial" and partial[3]["state"].get("data_through_at"), f"partial state mismatch {partial[3]}")
            expect(stale[0] == 200 and stale[3]["state"]["status"] == "stale" and stale[3]["state"]["state_reason"] == "source_stale", f"stale state mismatch {stale[3]}")
            observations = {"partial_state":partial[3]["state"],"stale_state":stale[3]["state"],"offline_status":offline_status,"producer_partial":partial_state,"producer_stale":stale_state}

        else:
            raise AssertionError(f"unsupported case {case_id}")

    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")

    path = record(case_id, observations, errors, directory)
    print(path.read_text(encoding="utf-8"))
    if errors:
        raise SystemExit(1)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    args = parser.parse_args()
    if args.case not in SUPPORTED:
        raise SystemExit("case must be P12-T001..P12-T018")
    run_case(args.case)

if __name__ == "__main__":
    main()
