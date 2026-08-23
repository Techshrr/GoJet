#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys

from integration_common import *


def run_case(case_id: str):
    observations = {}
    errors = []
    directory = HEADER_DIR if case_id in {"P11-T005","P11-T006","P11-T007","P11-T008","P11-T009","P11-T011"} else API_DIR
    try:
        number = int(case_id[-3:])
        workspace = f"ws-p11-{number:03d}"

        if case_id == "P11-T001":
            page = create_page(workspace, title="Alpha Bio", bio="Profile α", links=[child("Site", "HTTPS://Example.COM:443/a/../b?z=2&a=1#fragment", 0)])
            expect(page["status"] == "draft" and page["version"] == 1, "create lifecycle mismatch")
            slug = page["slug"]
            expect(len(slug) >= 16 and re.fullmatch(r"[A-Za-z0-9_-]+", slug) is not None, "slug is not an opaque URL-safe token")
            expect(slug not in {str(page["id"]), "Alpha Bio", "alpha-bio", workspace}, "slug exposes predictable internal or title authority")
            expect(page["links"][0]["destination_url"] == "https://example.com/b?a=1&z=2", "destination normalization mismatch")
            status, _, _, listed = list_pages(workspace)
            expect(status == 200 and listed["total"] == 1 and listed["items"][0]["id"] == page["id"], "same-workspace list mismatch")
            status2, _, _, listed2 = list_pages("ws-p11-001-other")
            expect(status2 == 200 and listed2["total"] == 0, "cross-workspace list leaked page")
            observations = {"page_id": page["id"], "slug": slug, "normalized_destination": page["links"][0]["destination_url"], "quota": listed["quota"]}

        elif case_id == "P11-T002":
            before = int(mysql_scalar("SELECT COUNT(*) FROM bio_pages"))
            bad = [
                {"title":"Bad","bio":"x","links":[child("JS","javascript:alert(1)",0)],"change_reason":"bad"},
                {"title":"Bad","bio":"x","links":[child("Data","data:text/html,boom",0)],"change_reason":"bad"},
                {"title":"Bad","bio":"x","links":[child("Creds","https://user:pass@example.com/",0)],"change_reason":"bad"},
                {"title":"Bad","bio":"x","links":[child("Gap","https://example.com/",1)],"change_reason":"bad"},
            ]
            statuses=[]
            for payload in bad:
                status, _, _, _ = json_request("POST", f"/api/workspaces/{workspace}/bio-pages", body=payload, workspace=workspace)
                statuses.append(status); expect(status == 400, f"invalid create accepted: {status}")
            after = int(mysql_scalar("SELECT COUNT(*) FROM bio_pages"))
            expect(before == after, "invalid creates partially persisted")
            observations={"statuses":statuses,"before":before,"after":after}

        elif case_id == "P11-T003":
            page=create_page(workspace)
            path=f"/api/workspaces/{workspace}/bio-pages/{page['id']}"
            unsigned=json_request("GET", path)
            viewer=json_request("PATCH", path, body={"expected_version":1,"title":"x","change_reason":"viewer"}, workspace=workspace, headers=VIEWER)
            cross=json_request("GET", f"/api/workspaces/other-workspace/bio-pages/{page['id']}", workspace="other-workspace")
            publish_viewer=json_request("POST", path+"/publish", body={"expected_version":1,"change_reason":"viewer"}, workspace=workspace, headers=VIEWER)
            expect(unsigned[0] in (403,503), f"signed-out not denied: {unsigned[0]}")
            expect(viewer[0] == 403 and publish_viewer[0] == 403, "viewer mutation not denied")
            expect(cross[0] == 404, f"cross-workspace inference status={cross[0]}")
            observations={"signed_out":unsigned[0],"viewer_patch":viewer[0],"viewer_publish":publish_viewer[0],"cross_workspace":cross[0]}

        elif case_id == "P11-T004":
            page=create_page(workspace,title="Version 1")
            first=update_page(workspace,page["id"],1,title="Version 2")
            expect(first[0] == 200 and first[3]["version"] == 2, "current update failed")
            stale=update_page(workspace,page["id"],1,title="Stale overwrite")
            expect(stale[0] == 409, f"stale update status={stale[0]}")
            current=get_page(workspace,page["id"])[3]
            expect(current["title"] == "Version 2" and current["version"] == 2, "stale write changed authority")
            observations={"current_version":current["version"],"stale_status":stale[0],"title":current["title"]}

        elif case_id == "P11-T005":
            page=create_page(workspace,links=[])
            published=transition_page(workspace,page["id"],page["version"],"publish")
            expect(published[0] == 200, "publish before delete failed")
            version=published[3]["version"]
            removed=delete_page(workspace,page["id"],version)
            expect(removed[0] == 204, "delete failed")
            hp=public_page(page["slug"]); ap=public_api(page["slug"])
            expect(hp[0] == 410 and ap[0] == 410, f"removed public statuses {hp[0]}/{ap[0]}")
            stale=update_page(workspace,page["id"],version,title="resurrect")
            expect(stale[0] in (409,410), f"stale delete resurrection status={stale[0]}")
            observations={"html_status":hp[0],"api_status":ap[0],"stale_status":stale[0]}

        elif case_id == "P11-T006":
            page=create_page(workspace,links=[child("Allowed","https://example.com/allowed",0)])
            seed=seed_risk(page["links"][0],"allow")
            published=transition_page(workspace,page["id"],page["version"],"publish")
            expect(published[0] == 200 and published[3]["status"] == "published" and published[3]["published_at"], "publish authority failed")
            hp=public_page(page["slug"]); ap=public_api(page["slug"])
            html=body_text(hp[2]); api=json.loads(ap[2])
            expect(hp[0] == 200 and ap[0] == 200, "published public status mismatch")
            expect('href="https://example.com/allowed"' in html and api["links"][0]["url"] == "https://example.com/allowed", "allowed navigation missing")
            observations={"published_version":published[3]["version"],"html_status":hp[0],"api_status":ap[0],"risk_key":seed["key"]}

        elif case_id == "P11-T007":
            page=create_page(workspace,links=[child("Allowed","https://example.com/pause",0)])
            draft=public_page(page["slug"]); expect(draft[0] == 404, "draft public status not 404")
            seed_risk(page["links"][0],"allow")
            published=transition_page(workspace,page["id"],1,"publish")[3]
            paused=transition_page(workspace,page["id"],published["version"],"pause")
            expect(paused[0] == 200 and paused[3]["status"] == "paused", "pause failed")
            hp=public_page(page["slug"]); ap=public_api(page["slug"])
            html=body_text(hp[2]); api=json.loads(ap[2])
            expect(hp[0] == 200 and ap[0] == 200, "paused public status mismatch")
            expect('href="https://example.com/pause"' not in html and api["links"][0].get("url") is None, "paused child remained navigable")
            observations={"draft_status":draft[0],"paused_html":hp[0],"paused_api":ap[0]}

        elif case_id == "P11-T008":
            page=create_page(workspace,links=[child("First","https://example.com/one",0),child("Second","https://example.net/two",1)])
            for link in page["links"]: seed_risk(link,"allow")
            published=transition_page(workspace,page["id"],1,"publish"); expect(published[0] == 200,"publish failed")
            hp=public_page(page["slug"]); html=body_text(hp[2])
            expect(html.count('rel="ugc nofollow"') == 2, "required rel missing")
            expect(html.index("First") < html.index("Second"), "owner order not preserved")
            expect("workspace_id" not in html and "policy_version" not in html, "internal evidence leaked")
            observations={"rel_count":html.count('rel="ugc nofollow"'),"ordered":True}

        elif case_id == "P11-T009":
            page=create_page(workspace,links=[child("Allow","https://example.com/a",0),child("Review","https://example.com/r",1),child("Block","https://example.com/b",2)])
            for link in page["links"]: seed_risk(link,"allow")
            published=transition_page(workspace,page["id"],1,"publish"); expect(published[0] == 200,"publish setup failed")
            seed_risk(page["links"][1],"review"); seed_risk(page["links"][2],"block")
            hp=public_page(page["slug"]); ap=public_api(page["slug"]); html=body_text(hp[2]); api=json.loads(ap[2])
            expect(hp[0] == 200 and ap[0] == 200, "risk-blocked published page did not remain 200")
            expect('href="https://example.com/a"' in html and 'href="https://example.com/r"' not in html and 'href="https://example.com/b"' not in html, "risk navigation fail-close mismatch")
            urls=[item.get("url") for item in api["links"]]
            expect(urls == ["https://example.com/a",None,None], f"public API unsafe URLs {urls}")
            expect("policy_version" not in body_text(ap[2]) and "workspace_id" not in body_text(ap[2]), "risk/tenant internals leaked")
            observations={"html_status":hp[0],"api_urls":urls}

        elif case_id == "P11-T010":
            page=create_page(workspace,links=[child("Mutable","https://example.com/old",0)])
            old=page["links"][0]; seed_risk(old,"allow")
            published=transition_page(workspace,page["id"],1,"publish")[3]
            update=update_page(workspace,page["id"],published["version"],links=[child("Mutable","https://example.com/new",0,old["id"])])
            expect(update[0] == 200, "destination update failed")
            changed=update[3]["links"][0]
            expect(changed["destination_fingerprint"] != old["destination_fingerprint"] and changed["risk_status"] == "review", "destination change did not invalidate allow")
            hp=public_page(page["slug"]); html=body_text(hp[2])
            expect(hp[0] == 200 and 'href="https://example.com/new"' not in html and 'href="https://example.com/old"' not in html, "stale allow created public href window")
            observations={"old_fingerprint":old["destination_fingerprint"],"new_fingerprint":changed["destination_fingerprint"],"public_status":hp[0]}

        elif case_id == "P11-T011":
            published=create_page(workspace+"-pub",links=[]); transition_page(workspace+"-pub",published["id"],1,"publish")
            paused=create_page(workspace+"-pause",links=[]); pub2=transition_page(workspace+"-pause",paused["id"],1,"publish")[3]; transition_page(workspace+"-pause",paused["id"],pub2["version"],"pause")
            draft=create_page(workspace+"-draft",links=[])
            removed=create_page(workspace+"-gone",links=[]); delete_page(workspace+"-gone",removed["id"],1)
            statuses={
                "published":public_api(published["slug"])[0],"paused":public_api(paused["slug"])[0],"draft":public_api(draft["slug"])[0],"removed":public_api(removed["slug"])[0],"unknown":public_api("unknown-p11-slug-123456789")[0]
            }
            expect(statuses=={"published":200,"paused":200,"draft":404,"removed":410,"unknown":404},f"public API lifecycle mismatch {statuses}")
            raw=body_text(public_api(published["slug"])[2]); expect("workspace_id" not in raw and "destination_fingerprint" not in raw, "public API leaked internals")
            observations=statuses

        elif case_id == "P11-T012":
            quota_ws=workspace+"-quota"; limit=int(os.environ.get("GOJET_BIO_WORKSPACE_QUOTA","3"))
            for i in range(limit): create_page(quota_ws,title=f"Quota {i}")
            status,_,raw,_=json_request("POST",f"/api/workspaces/{quota_ws}/bio-pages",body={"title":"Over","bio":"","links":[],"change_reason":"quota"},workspace=quota_ws)
            expect(status == 429, f"over quota status={status} {raw[:200]!r}")
            count=mysql_scalar(f"SELECT COUNT(*) FROM bio_pages WHERE workspace_id='{quota_ws}' AND deleted_at IS NULL")
            expect(int(count)==limit,"over-quota create partially persisted")
            risk_ws=workspace+"-risk"; page=create_page(risk_ws,links=[child("Needs review","https://example.com/review",0)])
            publish=transition_page(risk_ws,page["id"],1,"publish")
            expect(publish[0] == 409 and publish[3]["error"]["code"] == "child_link_risk_unresolved", f"unresolved publish status={publish[0]}")
            current=get_page(risk_ws,page["id"])[3]; expect(current["status"] == "draft" and current["version"] == 1,"failed publish changed lifecycle")
            expect(public_page(page["slug"])[0] == 404,"failed publish exposed public page")
            observations={"quota_limit":limit,"over_quota_status":status,"persisted_count":int(count),"unresolved_publish_status":publish[0],"authoritative_status":current["status"]}
        else:
            raise AssertionError(f"unsupported integration case {case_id}")
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    path=record(case_id,observations,errors,directory)
    print(path.read_text())
    if errors: raise SystemExit(1)


def main():
    parser=argparse.ArgumentParser(); parser.add_argument("--case",required=True); args=parser.parse_args()
    if args.case not in {f"P11-T{i:03d}" for i in range(1,13)}: raise SystemExit("case must be P11-T001..P11-T012")
    run_case(args.case)

if __name__ == "__main__": main()
