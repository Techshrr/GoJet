#!/usr/bin/env python3
from integration_common import *


def case_t001():
    ws = "p10-t001"
    created = create_share(ws, title="First Text", content="private baseline")
    expect(created["workspace_id"] == ws, "workspace mismatch")
    expect(created["visibility"] == "private", "default/private authority mismatch")
    expect(created["version"] == 1, "initial version must be 1")
    expect(len(created["public_slug"]) >= 16, "opaque public slug too short")
    status, _, _, listed = json_request("GET", f"/api/workspaces/{ws}/text-shares", workspace=ws)
    expect(status == 200 and listed["total"] == 1, "same-workspace list mismatch")
    other = "p10-t001-other"
    status2, _, _, listed2 = json_request("GET", f"/api/workspaces/{other}/text-shares", workspace=other)
    expect(status2 == 200 and listed2["total"] == 0, "cross-workspace list leaked")
    return {"id": created["id"], "slug_length": len(created["public_slug"]), "version": created["version"], "same_workspace_total": listed["total"], "other_workspace_total": listed2["total"]}


def case_t002():
    ws = "p10-t002"
    before = int(mysql_scalar(f"SELECT COUNT(*) FROM text_shares WHERE workspace_id='{ws}'"))
    bad = [
        {"title":"","content":"x","visibility":"private","change_reason":"bad"},
        {"title":"x","content":"","visibility":"private","change_reason":"bad"},
        {"title":"x","content":"x","visibility":"world","change_reason":"bad"},
    ]
    statuses = []
    for payload in bad:
        status, _, _, _ = json_request("POST", f"/api/workspaces/{ws}/text-shares", body=payload, workspace=ws)
        statuses.append(status)
        expect(status == 400, f"malformed create status={status}")
    created = create_share(ws)
    status, _, _, _ = update_share(ws, created["id"], created["version"], title="")
    expect(status == 400, f"invalid update status={status}")
    after = int(mysql_scalar(f"SELECT COUNT(*) FROM text_shares WHERE workspace_id='{ws}'"))
    expect(after == before + 1, f"partial malformed resource persisted before={before} after={after}")
    return {"malformed_create_statuses": statuses, "invalid_update_status": status, "durable_resource_delta": after-before}


def case_t003():
    ws, other = "p10-t003-a", "p10-t003-b"
    created = create_share(ws)
    signed_out, _, _, _ = json_request("GET", f"/api/workspaces/{ws}/text-shares/{created['id']}")
    expect(signed_out == 403, f"signed-out status={signed_out}")
    viewer_status, _, _, _ = json_request("PATCH", f"/api/workspaces/{ws}/text-shares/{created['id']}", body={"expected_version":created["version"],"title":"viewer write","change_reason":"denied"}, headers=auth_headers(ws, VIEWER))
    expect(viewer_status == 403, f"viewer mutation status={viewer_status}")
    cross_status, _, cross_raw, _ = json_request("GET", f"/api/workspaces/{other}/text-shares/{created['id']}", workspace=other)
    expect(cross_status == 404, f"cross-tenant status={cross_status}")
    expect(ws.encode() not in cross_raw, "cross-tenant response leaked workspace")
    manage_status, _, _, _ = update_share(ws, created["id"], created["version"], title="managed")
    expect(manage_status == 200, f"manage mutation status={manage_status}")
    return {"signed_out": signed_out, "viewer_mutation": viewer_status, "cross_tenant": cross_status, "manage_mutation": manage_status}


def case_t004():
    ws = "p10-t004"
    created = create_share(ws, content="v1")
    current_status, _, _, current = update_share(ws, created["id"], created["version"], content="v2")
    expect(current_status == 200, f"current update status={current_status}")
    stale_status, _, _, _ = update_share(ws, created["id"], created["version"], content="stale-overwrite")
    expect(stale_status == 409, f"stale update status={stale_status}")
    get_status, _, _, actual = json_request("GET", f"/api/workspaces/{ws}/text-shares/{created['id']}", workspace=ws)
    expect(get_status == 200 and actual["content"] == "v2", "stale update overwrote committed content")
    return {"committed_version": current["version"], "stale_status": stale_status, "content": actual["content"]}


def case_t005():
    ws = "p10-t005"
    created = create_share(ws, visibility="public", content="deleted-secret")
    status, _, _, _ = delete_share(ws, created["id"], created["version"])
    expect(status == 204, f"delete status={status}")
    public_status, _, public_raw = public_get(created["public_slug"])
    expect(public_status == 410 and b"deleted-secret" not in public_raw, f"deleted public status={public_status}")
    stale_status, _, _, _ = update_share(ws, created["id"], created["version"], content="resurrected")
    expect(stale_status == 410, f"stale mutation after delete status={stale_status}")
    second_status, _, _, _ = delete_share(ws, created["id"], created["version"])
    expect(second_status == 410, f"repeat delete status={second_status}")
    return {"delete_status": status, "public_after_delete": public_status, "stale_update": stale_status, "repeat_delete": second_status}


def case_t006():
    ws = "p10-t006"
    content = '<script>alert("p10")</script>& hello'
    created = create_share(ws, title="<b>Unsafe title</b>", content=content, visibility="public")
    status, headers, raw = public_get(created["public_slug"])
    html = body_text(raw)
    expect(status == 200, f"public status={status}")
    expect(content not in html, "active raw script content rendered")
    expect("&lt;script&gt;" in html and "&lt;b&gt;Unsafe title&lt;/b&gt;" in html, "UGC escaping missing")
    expect(ws not in html, "public HTML leaked workspace")
    return {"status": status, "content_type": headers_lower(headers).get("content-type"), "script_escaped": True, "workspace_leaked": False}


def case_t007():
    ws = "p10-t007"
    created = create_share(ws, title="private-title-secret", content="private-body-secret", visibility="private")
    page, _, page_raw = public_get(created["public_slug"])
    action, _, action_raw = public_action(created["public_slug"])
    download, _, download_raw = public_get(created["public_slug"], download=True)
    expect(page == 403 and action == 403 and download == 403, f"private statuses page/action/download={page}/{action}/{download}")
    for raw in (page_raw, action_raw, download_raw):
        expect(b"private-body-secret" not in raw and b"private-title-secret" not in raw, "private content/title leaked")
    return {"page": page, "action": action, "download": download}


def case_t008():
    ws = "p10-t008"
    password, content = "p10-Strong-Password-42", "password-protected-content"
    created = create_share(ws, content=content, visibility="public", password=password)
    unauth, _, unauth_raw = public_get(created["public_slug"])
    expect(unauth == 401 and content.encode() not in unauth_raw, f"unauthenticated status={unauth}")
    wrong, _, wrong_raw = password_post(created["public_slug"], "wrong-password")
    expect(wrong == 403 and content.encode() not in wrong_raw, f"wrong-password status={wrong}")
    correct, correct_headers, _ = password_post(created["public_slug"], password)
    expect(correct == 303, f"correct password action status={correct}")
    cookie = extract_cookie(correct_headers)
    set_cookie = correct_headers.get("Set-Cookie", "")
    expect("HttpOnly" in set_cookie, "authorization cookie is not HttpOnly")
    expect(password not in set_cookie, "plaintext password leaked into cookie")
    authorized, _, authorized_raw = public_get(created["public_slug"], cookie=cookie)
    expect(authorized == 200 and content.encode() in authorized_raw, "authorized page did not expose content")
    stored_hash = mysql_scalar(f"SELECT COALESCE(password_hash,'') FROM text_shares WHERE id={int(created['id'])}")
    expect(stored_hash and stored_hash != password and password not in stored_hash, "plaintext password persisted")
    return {"unauthenticated": unauth, "wrong_password": wrong, "password_post": correct, "authorized": authorized, "cookie_httponly": True, "stored_verifier_prefix": stored_hash.split("$",1)[0]}


def case_t009():
    ws, content = "p10-t009", "expired-secret"
    created = create_share(ws, content=content, visibility="public", expires_at=past_time())
    results = {}
    for name, outcome in (("page", public_get(created["public_slug"])), ("action", public_action(created["public_slug"])), ("download", public_get(created["public_slug"], download=True))):
        status, _, raw = outcome
        results[name] = status
        expect(status == 410, f"expired {name} status={status}")
        expect(content.encode() not in raw, f"expired {name} leaked shared content")
    return results


def case_t010():
    ws, content = "p10-t010", "one-time-secret"
    created = create_share(ws, content=content, visibility="public", one_time=True)
    page_status, _, page_raw = public_get(created["public_slug"])
    expect(page_status == 200 and content.encode() not in page_raw, "one-time GET leaked content before consume")
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        outcomes = list(pool.map(lambda _: public_action(created["public_slug"]), range(2)))
    statuses = sorted(item[0] for item in outcomes)
    success = [item for item in outcomes if item[0] == 200]
    gone = [item for item in outcomes if item[0] == 410]
    expect(len(success) == 1 and len(gone) == 1, f"atomic consume statuses={statuses}")
    expect(content.encode() in success[0][2] and content.encode() not in gone[0][2], "one-time content authority mismatch")
    after, _, after_raw = public_get(created["public_slug"])
    expect(after == 410 and content.encode() not in after_raw, "post-consume page not 410/no-content")
    consumed = mysql_scalar(f"SELECT IF(consumed_at IS NULL,'NULL','SET') FROM text_shares WHERE id={int(created['id'])}")
    expect(consumed == "SET", f"consumed_at not durable: {consumed}")
    return {"preconsume_page": page_status, "concurrent_statuses": statuses, "postconsume_page": after, "consumed_at": consumed}


def case_t011():
    status, _, raw = public_get("missing-p10-text-share")
    expect(status == 404, f"unknown status={status}")
    text = body_text(raw).lower()
    for forbidden in ("workspace_id", "stack trace", "gojet_test"):
        expect(forbidden not in text, f"unknown response leaked {forbidden}")
    malformed, _, malformed_raw = http_request("GET", "/t/%20")
    expect(malformed == 404 and b"gojet_test" not in malformed_raw, f"malformed slug status={malformed}")
    return {"unknown": status, "malformed": malformed}


def case_t012():
    public = create_share("p10-t012-public", title="download-title", content="download-body", visibility="public")
    action_status, _, action_raw = public_action(public["public_slug"])
    download_status, download_headers, download_raw = public_get(public["public_slug"], download=True)
    lower = headers_lower(download_headers)
    expect(action_status == 200 and action_raw == b"download-body", "public action mismatch")
    expect(download_status == 200 and download_raw == b"download-body", "download mismatch")
    expect(lower.get("content-type","").startswith("text/plain") and "attachment" in lower.get("content-disposition","").lower(), "download headers invalid")
    password = create_share("p10-t012-password", content="pw-secret", visibility="public", password="p10-password-123")
    private = create_share("p10-t012-private", content="private-secret", visibility="private")
    expired = create_share("p10-t012-expired", content="expired-secret", visibility="public", expires_at=past_time())
    consumed = create_share("p10-t012-consumed", content="consumed-secret", visibility="public", one_time=True)
    expect(public_action(consumed["public_slug"])[0] == 200, "failed to seed consumed fixture")
    removed = create_share("p10-t012-removed", content="removed-secret", visibility="public")
    expect(delete_share("p10-t012-removed", removed["id"], removed["version"])[0] == 204, "failed to seed removed fixture")
    denied = {}
    for name, item, expected, secret in (("password",password,403,b"pw-secret"),("private",private,403,b"private-secret"),("expired",expired,410,b"expired-secret"),("consumed",consumed,410,b"consumed-secret"),("removed",removed,410,b"removed-secret")):
        status, _, raw = public_get(item["public_slug"], download=True)
        denied[name] = status
        expect(status == expected and secret not in raw, f"{name} download authority mismatch status={status}")
    return {"action_status": action_status, "download_status": download_status, "content_type": lower.get("content-type"), "content_disposition": lower.get("content-disposition"), "denied": denied}


def case_t015():
    public = create_share("p10-t015-public", visibility="public", content="abuse public")
    gated = create_share("p10-t015-gated", visibility="public", content="abuse gated", password="p10-abuse-123")
    results = {}
    for name, item in (("available", public), ("gated", gated)):
        status, _, raw = public_get(item["public_slug"])
        html = body_text(raw)
        results[name] = status
        expect(status in (200,401), f"{name} status={status}")
        expect('href="/abuse/report"' in html, f"{name} canonical abuse entry missing")
        expect("/report-abuse" not in html and "/t/abuse" not in html, f"{name} alternate abuse route exposed")
    return {"statuses": results, "canonical_abuse_entry": "/abuse/report", "p16_completion_claimed": False}


CASES = {"P10-T001":case_t001,"P10-T002":case_t002,"P10-T003":case_t003,"P10-T004":case_t004,"P10-T005":case_t005,"P10-T006":case_t006,"P10-T007":case_t007,"P10-T008":case_t008,"P10-T009":case_t009,"P10-T010":case_t010,"P10-T011":case_t011,"P10-T012":case_t012,"P10-T015":case_t015}
HEADER_CASES = {f"P10-T{n:03d}" for n in range(6,12)}


def main():
    import argparse, sys
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    errors, observations = [], {}
    try:
        observations = CASES[args.case]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    directory = HEADER_DIR if args.case in HEADER_CASES else API_DIR
    path = record(args.case, observations, errors, directory)
    print(path)
    if errors:
        print("\n".join(errors), file=sys.stderr)
        return 1
    print(f"{args.case} PASS on {HEAD}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
