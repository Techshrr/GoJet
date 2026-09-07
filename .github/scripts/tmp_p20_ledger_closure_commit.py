import base64
import hashlib
import json
import os
import sys
import urllib.error
import urllib.request

REPO = os.environ["GITHUB_REPOSITORY"]
TOKEN = os.environ["GITHUB_TOKEN"]
BRANCH = "develop/p20-whole-product-verification"
LEDGER_PATH = "artifacts/v10/P20/defect-ledger.json"
EXPECTED_HEAD = "5695af3b804442acaed4bed23c80dc0f5b19a4d1"
EXPECTED_BLOB_SHA = "383339fceef6afba3662578231d17b6ae662896d"
EXPECTED_OUTPUT_SHA256 = "34ace1722d8b326d93d7ad30fdd37610bd0c3fef04605b5d2aa9943f14123d14"
CLOSED_AT = "2026-09-07T14:57:54Z"

COMMON = {
    "verification_head": EXPECTED_HEAD,
    "verification_workflow_run": 34132693845,
    "verification_workflow_job": 101776366942,
    "verification_artifact_id": 10023033952,
    "verification_artifact_digest": "sha256:ec7b224f04cdabbe8463a5efa2ba0391aef127563c267d38a59dc11dd1c78b46",
    "supporting_contract_workflow_run": 34132693734,
    "supporting_contract_workflow_job": 101776367039,
    "supporting_contract_artifact_id": 10022799666,
    "supporting_contract_artifact_digest": "sha256:73882283d680e3060bc06df9908a13e0fcbc3d2a60175731586ceae918338c60",
    "supporting_candidate_preclosure_workflow_run": 34132693719,
    "supporting_candidate_preclosure_workflow_job": 101776366384,
    "supporting_candidate_preclosure_p0_open": 0,
    "supporting_candidate_preclosure_p1_open": 3,
    "supporting_candidate_preclosure_decision_required": 0,
    "supporting_candidate_preclosure_blockers": ["P20-D012", "P20-D013", "P20-D014"],
}

CLOSURES = {
    "P20-D012": {
        "remediation_pr": 94,
        "remediation_merge_commit": "60cac306440255c55388b11d92aebd997eec4a82",
        "resolution": "Native platformapi composes the real P15 server-side session plus Origin and one-time CSRF authority into the P14 Support requester principal boundary, rejects production test-header substitution, and preserves the predecessor P14 Support test adapter and ticket lifecycle semantics.",
        "verified_outcome": {
            "t020_status": "PASS",
            "login_http_status": 200,
            "real_session_authenticated": True,
            "csrf_authority_issued": True,
            "auth_rate_window_respected": True,
            "ticket_create_http_status": 201,
            "ticket_create_row_delta": 1,
            "ticket_create_identity_bound": True,
            "requester_reply_http_status": 201,
            "requester_reply_row_delta": 1,
            "requester_reply_persisted": True,
            "workspace_role": "owner",
            "mock_authority": False,
            "test_header_authority": False,
            "secret_material_recorded": False,
        },
    },
    "P20-D013": {
        "remediation_pr": 99,
        "remediation_merge_commit": "96eba621573e2773bb535026c41e64a38927cce1",
        "resolution": "Native platformapi composes the real P17 Admin server-side session and MFA-backed PermissionCatalog authority into the P14 Support Admin boundary, requiring tickets.manage and mail.manage server-side while preserving the separate customer requester P15 boundary and predecessor P14 adapter semantics.",
        "verified_outcome": {
            "t020_status": "PASS",
            "p17_admin_session_authenticated": True,
            "p17_tickets_manage": True,
            "p17_mail_manage": True,
            "p17_admin_id_present": True,
            "admin_reply_http_status": 201,
            "admin_reply_row_delta": 1,
            "admin_reply_persisted": True,
            "admin_reply_message_id_present": True,
            "admin_reply_mail_job_bound": True,
            "admin_reply_mail_initial_status": "queued",
            "admin_reply_mail_final_status": "sent",
            "attachment_scan_status": "infected",
            "infected_attachment_download_blocked": True,
            "mock_authority": False,
            "test_header_authority": False,
            "secret_material_recorded": False,
        },
    },
    "P20-D014": {
        "remediation_pr": 103,
        "remediation_merge_commit": "d70b72477884eb2d624ab7aa5424d49cab1e501d",
        "resolution": "The Support ticket-message mail persistence path now emits only the immutable support-ticket-reply v1 delivery-value allowlist (ticket_id, display_name, subject, message_body), removing the unsupported status key without changing the seeded template contract or weakening validation.",
        "verified_outcome": {
            "t020_status": "PASS",
            "admin_reply_mail_initial_status": "queued",
            "native_mailworker": True,
            "admin_reply_mail_attempt_status": "sent",
            "admin_reply_mail_final_status": "sent",
            "real_smtp_delivery": True,
            "smtp_deliveries": 2,
            "smtp_message_sha256_length": 64,
            "attachment_bound_to_admin_reply": True,
            "attachment_scan_status": "infected",
            "real_clamav_infected": True,
            "infected_attachment_download_blocked": True,
            "t020_full_lifecycle_proven": True,
            "next_case": "P20-T021",
            "mock_authority": False,
            "test_header_authority": False,
            "secret_material_recorded": False,
        },
    },
}

NOTE = (
    "P20-D001 through P20-D014 are closed in the ledger using exact-head formal "
    "P20-T020 and same-head P20 Contract authority at "
    "5695af3b804442acaed4bed23c80dc0f5b19a4d1. P20-T021 remains locked until "
    "a ledger-closure exact head proves formal P20-T020, P20 Contract and "
    "Candidate Freeze coherence with P0/P1/decision_required = 0/0/0."
)

def api(method, path, payload=None):
    url = "https://api.github.com" + path
    data = None if payload is None else json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "User-Agent": "gojet-p20-ledger-closure",
        },
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read()
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8", "replace")
        raise RuntimeError(f"{method} {path} failed: HTTP {e.code}: {body}") from e

def build_closure(raw):
    ledger = json.loads(raw)
    if ledger.get("schema") != "gojet.p20-defect-ledger.v1":
        raise RuntimeError("unexpected ledger schema")
    if ledger.get("status") != "ACTIVE":
        raise RuntimeError("unexpected ledger status")
    if [d["id"] for d in ledger["closed"]] != [f"P20-D{i:03d}" for i in range(1, 12)]:
        raise RuntimeError("historical closed ledger is not D001-D011 in order")
    if [d["id"] for d in ledger["open"]] != ["P20-D012", "P20-D013", "P20-D014"]:
        raise RuntimeError("open defects are not exactly D012-D014")
    if ledger.get("decision_required") != []:
        raise RuntimeError("decision_required is not empty")

    historical = json.loads(json.dumps(ledger["closed"]))
    opened = {d["id"]: d for d in ledger["open"]}
    for defect_id in ["P20-D012", "P20-D013", "P20-D014"]:
        d = opened[defect_id]
        closure = CLOSURES[defect_id]
        d["remediation_pr"] = closure["remediation_pr"]
        d["remediation_merge_commit"] = closure["remediation_merge_commit"]
        d.update(COMMON)
        d["resolution"] = closure["resolution"]
        d["verified_outcome"] = closure["verified_outcome"]
        d["closed_at"] = CLOSED_AT
        ledger["closed"].append(d)

    ledger["open"] = []
    ledger["note"] = NOTE

    if ledger["closed"][:11] != historical:
        raise RuntimeError("D001-D011 history changed")
    ids = [d["id"] for d in ledger["closed"]]
    if ids != [f"P20-D{i:03d}" for i in range(1, 15)] or len(ids) != len(set(ids)):
        raise RuntimeError("closed IDs are not exactly unique D001-D014")
    if ledger["open"] != [] or ledger["decision_required"] != []:
        raise RuntimeError("closure counters are not zero")
    if ledger["rules"] != {
        "p0_open_max": 0,
        "p1_open_max": 0,
        "decision_required_max": 0,
        "lower_severity_requires_disposition": True,
        "gate_hard_failure_cannot_be_downgraded": True,
    }:
        raise RuntimeError("ledger rules changed")

    out = (json.dumps(ledger, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
    actual = hashlib.sha256(out).hexdigest()
    if actual != EXPECTED_OUTPUT_SHA256:
        raise RuntimeError(f"closure bytes differ: {actual}")
    return out

def main():
    ref = api("GET", f"/repos/{REPO}/git/ref/heads/{BRANCH}")
    current_head = ref["object"]["sha"]
    if current_head != EXPECTED_HEAD:
        raise RuntimeError(f"integration drift: {current_head}")

    from urllib.parse import quote
    content_obj = api("GET", f"/repos/{REPO}/contents/{LEDGER_PATH}?ref={quote(BRANCH, safe='')}")
    if content_obj["sha"] != EXPECTED_BLOB_SHA:
        raise RuntimeError(f"ledger blob drift: {content_obj['sha']}")
    raw = base64.b64decode(content_obj["content"])
    closure = build_closure(raw)

    blob = api("POST", f"/repos/{REPO}/git/blobs", {
        "content": base64.b64encode(closure).decode("ascii"),
        "encoding": "base64",
    })
    base_commit = api("GET", f"/repos/{REPO}/git/commits/{EXPECTED_HEAD}")
    tree = api("POST", f"/repos/{REPO}/git/trees", {
        "base_tree": base_commit["tree"]["sha"],
        "tree": [{
            "path": LEDGER_PATH,
            "mode": "100644",
            "type": "blob",
            "sha": blob["sha"],
        }],
    })
    commit = api("POST", f"/repos/{REPO}/git/commits", {
        "message": "P20 T020: close D012-D014 after formal authority",
        "tree": tree["sha"],
        "parents": [EXPECTED_HEAD],
    })
    api("PATCH", f"/repos/{REPO}/git/refs/heads/{BRANCH}", {
        "sha": commit["sha"],
        "force": False,
    })
    print(json.dumps({
        "closure_commit": commit["sha"],
        "closure_blob": blob["sha"],
        "closure_sha256": EXPECTED_OUTPUT_SHA256,
    }))

if __name__ == "__main__":
    main()
