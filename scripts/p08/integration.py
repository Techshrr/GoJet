#!/usr/bin/env python3
"""GoJet V10 P08 real-dependency integration driver for P08-T001..P08-T010 (except T003 scanner)."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import http.client
import json
import os
import subprocess
import sys
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "artifacts" / "v10" / "P08" / "results"
SCANNER = ROOT / "artifacts" / "v10" / "P08" / "scanner"
MIGRATIONS = [ROOT / "migrations" / f"00000{n}_{name}.sql" for n, name in [
    (1, "links_vertical_slice"),
    (2, "custom_domains"),
    (3, "analytics"),
    (4, "qr"),
]]

MYSQL_HOST = os.getenv("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = int(os.getenv("GOJET_TEST_MYSQL_PORT", "3306"))
MYSQL_USER = os.getenv("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.getenv("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.getenv("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
REDIS_HOST = os.getenv("GOJET_TEST_REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.getenv("GOJET_TEST_REDIS_PORT", "6379"))
PLATFORM_URL = os.getenv("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081")
WORKSPACE = "ws-p08"
ACTOR = "p08-owner"
VIEWER = "p08-viewer"
SUPPORTED = ("P08-T001", "P08-T002", "P08-T004", "P08-T005", "P08-T006", "P08-T007", "P08-T008", "P08-T009", "P08-T010")


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def iso(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def exact_head() -> str:
    value = os.getenv("GITHUB_SHA", "").strip()
    if value:
        return value
    return subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()


def run(argv: list[str], *, input_text: str | None = None, expect: int | None = 0) -> subprocess.CompletedProcess[str]:
    completed = subprocess.run(argv, cwd=ROOT, text=True, input=input_text, capture_output=True, env=os.environ.copy())
    if expect is not None and completed.returncode != expect:
        raise AssertionError(
            f"command failed {completed.returncode}: {' '.join(argv)}\nstdout={completed.stdout}\nstderr={completed.stderr}"
        )
    return completed


def mysql_args() -> list[str]:
    return [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT),
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE,
    ]


def mysql_run(*, query: str | None = None, file_text: str | None = None, expect: int | None = 0) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    argv = mysql_args()
    if query is not None:
        argv += ["-e", query]
    completed = subprocess.run(argv, cwd=ROOT, text=True, input=file_text, capture_output=True, env=env)
    if expect is not None and completed.returncode != expect:
        raise AssertionError(
            f"mysql failed {completed.returncode}: {query or '<stdin>'}\nstdout={completed.stdout}\nstderr={completed.stderr}"
        )
    return completed


def mysql_scalar(query: str) -> str:
    return mysql_run(query=query).stdout.strip()


def redis_cli(*args: str, expect: int | None = 0) -> str:
    completed = run(["redis-cli", "-h", REDIS_HOST, "-p", str(REDIS_PORT), "--raw", *args], expect=expect)
    return completed.stdout.strip()


def reset_data() -> None:
    tables = [
        "qr_audit_events", "qr_codes", "qr_workspace_counters",
        "analytics_reconciliation_runs", "analytics_conversions", "analytics_workspace_state",
        "analytics_hourly_aggregates", "analytics_events", "analytics_outbox",
        "custom_domain_audit_events", "custom_domain_revalidations", "custom_domains", "custom_domain_usage",
        "custom_domain_entitlement_requests", "custom_domain_entitlement_sources",
        "link_audit_events", "link_versions", "links",
    ]
    statements = ["SET FOREIGN_KEY_CHECKS=0"] + [f"TRUNCATE TABLE {table}" for table in tables] + ["SET FOREIGN_KEY_CHECKS=1"]
    mysql_run(query="; ".join(statements) + ";")
    redis_cli("FLUSHDB")


def http_request(base: str, method: str, path: str, *, payload: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes]:
    parsed = urlsplit(base)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=20)
    merged = {"Accept": "application/json"}
    if headers:
        merged.update(headers)
    body: bytes | None = None
    if payload is not None:
        body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
        merged["Content-Type"] = "application/json"
    conn.request(method, path, body=body, headers=merged)
    response = conn.getresponse()
    data = response.read()
    response_headers = {key.lower(): value for key, value in response.getheaders()}
    status = response.status
    conn.close()
    return status, response_headers, data


def auth_headers(workspace: str = WORKSPACE, *, role: str = "owner", actor: str = ACTOR, correlation: str | None = None) -> dict[str, str]:
    result = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Workspace": workspace,
        "X-GoJet-Test-Workspace-Role": role,
    }
    if correlation:
        result["X-Request-ID"] = correlation
    return result


def api(method: str, path: str, *, payload: Any | None = None, header_workspace: str = WORKSPACE, role: str = "owner", actor: str = ACTOR, correlation: str | None = None) -> tuple[int, dict[str, str], Any]:
    status, headers, raw = http_request(
        PLATFORM_URL, method, path, payload=payload,
        headers=auth_headers(header_workspace, role=role, actor=actor, correlation=correlation),
    )
    if "application/json" in headers.get("content-type", "") and raw:
        return status, headers, json.loads(raw)
    return status, headers, raw


def link_payload(code: str, destination: str, *, hostname: str = "go.p08.test", domain_kind: str = "official") -> dict[str, Any]:
    return {
        "hostname": hostname,
        "domain_kind": domain_kind,
        "code": code,
        "title": f"P08 {code}",
        "primary_destination": destination,
        "redirect_status": 302,
        "routing": [],
        "ab": [],
        "utm": {},
        "access": {},
        "expires_at": None,
        "click_limit": None,
        "one_time": False,
        "change_reason": f"create {code}",
    }


def create_link(code: str, destination: str, *, workspace: str = WORKSPACE, hostname: str = "go.p08.test", domain_kind: str = "official") -> dict[str, Any]:
    status, _, value = api(
        "POST", f"/api/workspaces/{workspace}/links",
        payload=link_payload(code, destination, hostname=hostname, domain_kind=domain_kind),
        header_workspace=workspace,
        correlation=f"p08-link-{workspace}-{code}",
    )
    assert status == 201, (status, value)
    assert isinstance(value, dict)
    return value


def update_link(link: dict[str, Any], destination: str, *, workspace: str = WORKSPACE) -> dict[str, Any]:
    payload = {
        "expected_version": link["version"],
        "hostname": link["hostname"],
        "domain_kind": link["domain_kind"],
        "code": link["code"],
        "title": link["title"],
        "primary_destination": destination,
        "redirect_status": link["redirect_status"],
        "status": link["status"],
        "routing": link.get("routing") or [],
        "ab": link.get("ab") or [],
        "utm": link.get("utm") or {},
        "access": {},
        "expires_at": link.get("expires_at"),
        "click_limit": link.get("click_limit"),
        "one_time": bool(link.get("one_time")),
        "change_reason": "P08 reachable-target update",
    }
    status, _, value = api(
        "PATCH", f"/api/workspaces/{workspace}/links/{link['id']}", payload=payload,
        header_workspace=workspace, correlation=f"p08-update-{link['id']}",
    )
    assert status == 200, (status, value)
    assert isinstance(value, dict)
    return value


def risk_key(link: dict[str, Any]) -> str:
    return f"risk:link:{link['id']}:{link['risk_fingerprint']}"


def set_risk(link: dict[str, Any], state: str, *, stale: bool = False, malformed: bool = False) -> None:
    key = risk_key(link)
    redis_cli("DEL", key)
    if malformed:
        redis_cli("SET", key, "{not-json", "EX", "300")
        return
    checked = utcnow() - (dt.timedelta(minutes=10) if stale else dt.timedelta(seconds=1))
    valid_until = utcnow() - dt.timedelta(seconds=1) if stale else utcnow() + dt.timedelta(minutes=5)
    payload = json.dumps({
        "schema_version": 1,
        "decision": state,
        "fingerprint": link["risk_fingerprint"],
        "checked_at": iso(checked),
        "valid_until": iso(valid_until),
        "policy_version": "p08-integration-v1",
    }, separators=(",", ":"))
    redis_cli("SET", key, payload, "EX", "300")


def create_qr(link: dict[str, Any], *, workspace: str = WORKSPACE, label: str = "P08 QR", role: str = "owner", actor: str = ACTOR) -> tuple[int, dict[str, str], Any]:
    return api(
        "POST", f"/api/workspaces/{workspace}/qr-codes",
        payload={"source_link_id": link["id"], "label": label, "change_reason": "P08 integration create"},
        header_workspace=workspace, role=role, actor=actor, correlation=f"p08-qr-create-{workspace}-{link['id']}",
    )


def artifact(qr_id: int, fmt: str, *, workspace: str = WORKSPACE, preview: bool = False, role: str = "owner", actor: str = ACTOR) -> tuple[int, dict[str, str], bytes]:
    action = "preview" if preview else "download"
    status, headers, raw = http_request(
        PLATFORM_URL, "GET", f"/api/workspaces/{workspace}/qr-codes/{qr_id}/{action}?format={fmt}",
        headers=auth_headers(workspace, role=role, actor=actor),
    )
    return status, headers, raw


def error_code(value: Any) -> str:
    if isinstance(value, dict):
        return str(value.get("error", {}).get("code", ""))
    if isinstance(value, (bytes, bytearray)):
        try:
            parsed = json.loads(bytes(value))
            return str(parsed.get("error", {}).get("code", ""))
        except Exception:
            return ""
    return ""


def sha256(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


def assert_no_leak(raw: bytes | str, needles: list[str]) -> None:
    text = raw.decode("utf-8", errors="replace") if isinstance(raw, (bytes, bytearray)) else str(raw)
    for needle in needles:
        assert needle not in text, (needle, text)


def case_t001() -> dict[str, Any]:
    reset_data()
    destination = "https://customer.example/p08-t001"
    link = create_link("qr-create", destination)
    set_risk(link, "allow")

    bad_status, _, bad = api(
        "POST", f"/api/workspaces/{WORKSPACE}/qr-codes",
        payload={
            "source_link_id": link["id"], "label": "bad", "change_reason": "reject alternate target",
            "target": "https://attacker.example/bypass",
        },
        correlation="p08-t001-bad-target",
    )
    assert bad_status == 400 and error_code(bad) == "invalid_json", (bad_status, bad)
    assert mysql_scalar("SELECT COUNT(*) FROM qr_codes") == "0"

    status, _, created = create_qr(link, label="Authoritative QR")
    assert status == 201 and isinstance(created, dict), (status, created)
    expected_url = f"https://{link['hostname']}/{link['code']}"
    assert created["workspace_id"] == WORKSPACE
    assert created["source_link_id"] == link["id"]
    assert created["source"]["public_url"] == expected_url
    assert destination not in json.dumps(created)
    row = mysql_run(query=f"SELECT workspace_id,source_link_id,label,deleted_at IS NULL FROM qr_codes WHERE id={int(created['id'])}").stdout.strip().split("\t")
    assert row == [WORKSPACE, str(link["id"]), "Authoritative QR", "1"], row

    SCANNER.mkdir(parents=True, exist_ok=True)
    scanner_meta: dict[str, Any] = {
        "implementation_commit": exact_head(),
        "qr_id": created["id"],
        "source_link_id": link["id"],
        "public_url": expected_url,
        "destination": destination,
        "hostname": link["hostname"],
        "code": link["code"],
    }
    for fmt in ("png", "svg"):
        art_status, art_headers, raw = artifact(created["id"], fmt)
        assert art_status == 200, (fmt, art_status, raw[:200])
        path = SCANNER / f"P08-T003-source.{fmt}"
        path.write_bytes(raw)
        digest = sha256(raw)
        assert art_headers.get("x-gojet-artifact-sha256") == digest
        scanner_meta[f"{fmt}_sha256"] = digest
    (SCANNER / "P08-T003-expected.json").write_text(json.dumps(scanner_meta, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return {"qr": created, "source_link": {"id": link["id"], "public_url": expected_url}, "alternate_target_rejected": True, "scanner_fixture": scanner_meta}


def case_t002() -> dict[str, Any]:
    reset_data()
    ws_a, ws_b = "ws-p08-a", "ws-p08-b"
    link_a = create_link("tenant-a", "https://a.example/", workspace=ws_a)
    link_b = create_link("tenant-b", "https://b.example/", workspace=ws_b)
    set_risk(link_a, "allow")
    set_risk(link_b, "allow")
    status_a, _, qr_a = create_qr(link_a, workspace=ws_a, label="A")
    status_b, _, qr_b = create_qr(link_b, workspace=ws_b, label="B")
    assert status_a == 201 and status_b == 201

    list_status, _, listing = api("GET", f"/api/workspaces/{ws_a}/qr-codes", header_workspace=ws_a, actor="actor-a")
    assert list_status == 200
    assert [item["id"] for item in listing["items"]] == [qr_a["id"]]
    cross_status, _, cross = api("GET", f"/api/workspaces/{ws_b}/qr-codes/{qr_b['id']}", header_workspace=ws_a, actor="actor-a")
    assert cross_status == 403 and error_code(cross) == "forbidden"
    same_path_status, _, same_path = api("GET", f"/api/workspaces/{ws_a}/qr-codes/{qr_b['id']}", header_workspace=ws_a, actor="actor-a")
    assert same_path_status == 404 and error_code(same_path) == "not_found"
    assert_no_leak(json.dumps(cross).encode(), ["tenant-b", "https://b.example/"])
    assert_no_leak(json.dumps(same_path).encode(), ["tenant-b", "https://b.example/"])
    return {"workspace_a_qr": qr_a["id"], "workspace_b_qr": qr_b["id"], "list_ids": [item["id"] for item in listing["items"]], "cross_workspace_status": cross_status, "same_path_unknown_status": same_path_status}


def case_t004() -> dict[str, Any]:
    reset_data()
    link = create_link("deterministic", "https://customer.example/deterministic")
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    details: dict[str, Any] = {"qr_id": created["id"], "formats": {}}
    for fmt, expected_type in (("png", "image/png"), ("svg", "image/svg+xml; charset=utf-8")):
        first_status, first_headers, first = artifact(created["id"], fmt)
        second_status, second_headers, second = artifact(created["id"], fmt)
        assert first_status == second_status == 200
        digest = sha256(first)
        assert first == second
        assert digest == sha256(second)
        assert first_headers.get("x-gojet-artifact-sha256") == second_headers.get("x-gojet-artifact-sha256") == digest
        assert first_headers.get("content-type") == expected_type
        assert first_headers.get("cache-control") == "no-store"
        assert first_headers.get("x-robots-tag") == "noindex, nofollow"
        disposition = first_headers.get("content-disposition", "")
        assert disposition.startswith("attachment;") and f"gojet-qr-{created['id']}.{fmt}" in disposition
        if fmt == "png":
            assert first.startswith(b"\x89PNG\r\n\x1a\n")
        else:
            root = ET.fromstring(first)
            assert root.tag.endswith("svg")
            lowered = first.lower()
            for forbidden in (b"<script", b"javascript:", b"xlink:href=", b"<foreignobject", b" href="):
                assert forbidden not in lowered, forbidden
            assert first_headers.get("content-security-policy") == "default-src 'none'; sandbox"
        details["formats"][fmt] = {"sha256": digest, "bytes": len(first), "content_type": expected_type, "content_disposition": disposition}
    return details


def case_t005() -> dict[str, Any]:
    reset_data()
    link = create_link("rbac", "https://customer.example/rbac")
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    qr_id = created["id"]

    read_status, _, _ = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}", role="viewer", actor=VIEWER)
    preview_status, _, preview_raw = artifact(qr_id, "png", preview=True, role="viewer", actor=VIEWER)
    download_status, _, download_raw = artifact(qr_id, "png", role="viewer", actor=VIEWER)
    assert read_status == preview_status == download_status == 200
    assert preview_raw.startswith(b"\x89PNG") and download_raw.startswith(b"\x89PNG")

    viewer_create_status, _, viewer_create = create_qr(link, role="viewer", actor=VIEWER)
    assert viewer_create_status == 403 and error_code(viewer_create) == "read_only"
    viewer_delete_status, _, viewer_delete = api(
        "DELETE", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}",
        payload={"change_reason": "viewer must not delete"}, role="viewer", actor=VIEWER,
    )
    assert viewer_delete_status == 403 and error_code(viewer_delete) == "read_only"

    unauth_status, _, unauth = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}", header_workspace="wrong-workspace", actor="outsider")
    assert unauth_status == 403 and error_code(unauth) == "forbidden"
    assert mysql_scalar(f"SELECT deleted_at IS NULL FROM qr_codes WHERE id={int(qr_id)}") == "1"
    assert mysql_scalar("SELECT COUNT(*) FROM qr_codes") == "1"
    return {"viewer_read": read_status, "viewer_preview": preview_status, "viewer_download": download_status, "viewer_create": viewer_create_status, "viewer_delete": viewer_delete_status, "unauthorized_read": unauth_status, "row_count": 1}


def case_t006() -> dict[str, Any]:
    reset_data()
    link = create_link("quota", "https://customer.example/quota")
    set_risk(link, "allow")
    created_ids: list[int] = []
    for label in ("one", "two"):
        status, _, value = create_qr(link, label=label)
        assert status == 201, (status, value)
        created_ids.append(value["id"])
    denied_status, _, denied = create_qr(link, label="three")
    assert denied_status == 429 and error_code(denied) == "quota_reached", (denied_status, denied)
    assert mysql_scalar("SELECT COUNT(*) FROM qr_codes WHERE deleted_at IS NULL") == "2"
    assert mysql_scalar(f"SELECT active_count FROM qr_workspace_counters WHERE workspace_id='{WORKSPACE}'") == "2"
    denied_audits = mysql_scalar("SELECT COUNT(*) FROM qr_audit_events WHERE action='qr.create' AND result='denied'")
    assert denied_audits == "1"
    return {"created_ids": created_ids, "denied_status": denied_status, "active_rows": 2, "counter": 2, "denied_audits": 1}


def case_t007() -> dict[str, Any]:
    reset_data()
    destination = "https://customer.example/review"
    link = create_link("review", destination)
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    set_risk(link, "review")
    detail_status, _, detail = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/{created['id']}")
    assert detail_status == 200 and detail["state"] == "source-link-review" and detail["source"]["risk_state"] == "review"
    for preview in (True, False):
        art_status, art_headers, raw = artifact(created["id"], "png", preview=preview)
        assert art_status == 409, (art_status, raw)
        assert "x-gojet-artifact-sha256" not in art_headers
        assert error_code(raw) == "source_link_review"
        assert_no_leak(raw, [destination, "continue-anyway"])
    return {"qr_id": created["id"], "detail_state": detail["state"], "risk_state": detail["source"]["risk_state"], "preview_status": 409, "download_status": 409}


def install_ready_custom_domain_fixture(workspace: str, hostname: str) -> None:
    mysql_run(query=f"""
        INSERT INTO custom_domain_entitlement_sources
          (workspace_id,source,source_key,status,domain_limit,starts_at,expires_at)
        VALUES ('{workspace}','plan','p08-plan','active',5,UTC_TIMESTAMP(6)-INTERVAL 1 DAY,UTC_TIMESTAMP(6)+INTERVAL 30 DAY);
        INSERT INTO custom_domain_usage (workspace_id,allocated_count,version) VALUES ('{workspace}',1,1);
        INSERT INTO custom_domains
          (workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,
           ownership_token_version,ownership_secret_hash,ownership_secret_issued_at,ownership_verified_at,ingress_dns_checked_at,https_checked_at,
           risk_checked_at,risk_policy_version,risk_evidence_ref)
        VALUES ('{workspace}','{hostname}','{hostname}','enabled','verified','valid','active','allow',
                1,UNHEX(REPEAT('00',32)),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),'p08-fixture-v1','p08-fixture');
    """)


def case_t008() -> dict[str, Any]:
    reset_data()
    unsafe_destination = "https://unsafe-customer.example/private-target"
    link = create_link("risk-matrix", unsafe_destination)
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    qr_id = created["id"]
    matrix: dict[str, Any] = {}

    set_risk(link, "block")
    block_status, _, block_raw = artifact(qr_id, "png")
    assert block_status == 403 and error_code(block_raw) == "source_link_block"
    assert_no_leak(block_raw, [unsafe_destination])
    matrix["block"] = block_status

    redis_cli("DEL", risk_key(link))
    missing_status, _, missing_raw = artifact(qr_id, "png")
    assert missing_status == 409 and error_code(missing_raw) == "source_link_unavailable"
    assert_no_leak(missing_raw, [unsafe_destination])
    matrix["missing"] = missing_status
    matrix["pending_semantic"] = "missing exact-current decision"

    set_risk(link, "allow", malformed=True)
    malformed_status, _, malformed_raw = artifact(qr_id, "png")
    assert malformed_status == 409 and error_code(malformed_raw) == "source_link_unavailable"
    matrix["malformed"] = malformed_status

    set_risk(link, "allow", stale=True)
    stale_status, _, stale_raw = artifact(qr_id, "png")
    assert stale_status == 409 and error_code(stale_raw) == "source_link_unavailable"
    matrix["stale"] = stale_status

    set_risk(link, "allow")
    old_fingerprint = link["risk_fingerprint"]
    changed = update_link(link, "https://unsafe-customer.example/new-target")
    assert changed["risk_fingerprint"] != old_fingerprint
    changed_status, _, changed_raw = artifact(qr_id, "png")
    assert changed_status == 409 and error_code(changed_raw) == "source_link_unavailable"
    assert redis_cli("EXISTS", f"risk:link:{link['id']}:{old_fingerprint}") == "1"
    assert redis_cli("EXISTS", f"risk:link:{changed['id']}:{changed['risk_fingerprint']}") == "0"
    assert_no_leak(changed_raw, ["https://unsafe-customer.example/new-target"])
    matrix["changed_fingerprint"] = {"status": changed_status, "old_allow_still_present": True, "new_allow_present": False}

    custom_ws = "ws-p08-custom"
    custom_host = "qr-custom-p08.example.com"
    install_ready_custom_domain_fixture(custom_ws, custom_host)
    custom_link = create_link("custom", "https://customer.example/custom", workspace=custom_ws, hostname=custom_host, domain_kind="custom")
    set_risk(custom_link, "allow")
    custom_status, _, custom_qr = create_qr(custom_link, workspace=custom_ws)
    assert custom_status == 201, (custom_status, custom_qr)
    mysql_run(query=f"UPDATE custom_domains SET https_status='error', updated_at=CURRENT_TIMESTAMP(6) WHERE workspace_id='{custom_ws}' AND hostname_ascii='{custom_host}'")
    denied_status, _, denied_raw = artifact(custom_qr["id"], "png", workspace=custom_ws)
    assert denied_status == 409 and error_code(denied_raw) == "source_link_unavailable", (denied_status, denied_raw)
    assert_no_leak(denied_raw, ["https://customer.example/custom"])
    matrix["custom_domain_denied"] = denied_status
    return {"qr_id": qr_id, "risk_matrix": matrix}


def case_t009() -> dict[str, Any]:
    reset_data()
    link = create_link("delete", "https://customer.example/delete")
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    qr_id = created["id"]
    download_status, _, downloaded = artifact(qr_id, "png")
    assert download_status == 200
    before_digest = sha256(downloaded)
    delete_status, _, delete_raw = api(
        "DELETE", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}",
        payload={"change_reason": "P08 integration delete"}, correlation=f"p08-delete-{qr_id}",
    )
    assert delete_status == 204 and delete_raw == b""
    statuses: dict[str, int] = {}
    for action, path in {
        "detail": f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}",
        "preview": f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}/preview?format=png",
        "download": f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}/download?format=png",
    }.items():
        response_status, _, raw = http_request(PLATFORM_URL, "GET", path, headers=auth_headers())
        assert response_status == 410 and error_code(raw) == "deleted", (action, response_status, raw)
        statuses[action] = response_status
    assert mysql_scalar(f"SELECT deleted_at IS NOT NULL FROM qr_codes WHERE id={int(qr_id)}") == "1"
    assert mysql_scalar(f"SELECT active_count FROM qr_workspace_counters WHERE workspace_id='{WORKSPACE}'") == "0"
    assert sha256(downloaded) == before_digest
    return {"qr_id": qr_id, "downloaded_before_delete_sha256": before_digest, "after_delete": statuses, "counter": 0, "previous_bytes_still_hashable": True}


def case_t010() -> dict[str, Any]:
    reset_data()
    destination = "https://customer.example/error-stability"
    link = create_link("errors", destination)
    set_risk(link, "allow")
    status, _, created = create_qr(link)
    assert status == 201
    qr_id = created["id"]

    malformed_status, _, malformed = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/not-a-number")
    assert malformed_status == 400 and error_code(malformed) == "invalid_qr_id"
    unknown_status, _, unknown = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/999999999")
    assert unknown_status == 404 and error_code(unknown) == "not_found"
    delete_status, _, _ = api("DELETE", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}", payload={"change_reason": "prepare deleted error"})
    assert delete_status == 204
    deleted_status, _, deleted = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/{qr_id}")
    assert deleted_status == 410 and error_code(deleted) == "deleted"

    reset_data()
    link = create_link("dependency", destination)
    set_risk(link, "allow")
    create_status, _, ready_qr = create_qr(link)
    assert create_status == 201
    redis_cli("SHUTDOWN", "NOSAVE", expect=None)
    dependency_status, _, dependency = api("GET", f"/api/workspaces/{WORKSPACE}/qr-codes/{ready_qr['id']}")
    assert dependency_status == 503 and error_code(dependency) == "source_authority_unavailable", (dependency_status, dependency)
    for value in (malformed, unknown, deleted, dependency):
        serialized = json.dumps(value)
        assert "stack" not in serialized.lower()
        assert destination not in serialized
    return {
        "malformed": {"status": malformed_status, "code": error_code(malformed)},
        "unknown": {"status": unknown_status, "code": error_code(unknown)},
        "deleted": {"status": deleted_status, "code": error_code(deleted)},
        "dependency": {"status": dependency_status, "code": error_code(dependency)},
        "destination_leaked": False,
    }


CASES = {
    "P08-T001": case_t001,
    "P08-T002": case_t002,
    "P08-T004": case_t004,
    "P08-T005": case_t005,
    "P08-T006": case_t006,
    "P08-T007": case_t007,
    "P08-T008": case_t008,
    "P08-T009": case_t009,
    "P08-T010": case_t010,
}


def write_evidence(case_id: str, status: str, details: dict[str, Any], errors: list[str]) -> None:
    evidence = {
        "node": "P08",
        "case_id": case_id,
        "implementation_commit": exact_head(),
        "status": status,
        "driver": f"scripts/p08/integration.py --case {case_id}",
        "environment": {
            "mysql": "real MySQL 8.x service",
            "redis": "real Redis service for exact-current source-Link destination-risk authority",
            "platformapi": "native Go services/platformapi/cmd/server",
            "redirectengine": "native Go services/redirectengine/cmd/server is started in the P08 integration workflow; T003 follows the decoded short URL through it",
            "migrations": [str(path.relative_to(ROOT)) for path in MIGRATIONS],
            "fixture_policy": "fixtures arrange source links, risk decisions, quota and custom-domain downgrade state in real MySQL/Redis; no in-memory service substitutes are used",
        },
        "details": details,
        "errors": errors,
    }
    RESULTS.mkdir(parents=True, exist_ok=True)
    (RESULTS / f"{case_id}.json").write_text(json.dumps(evidence, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=SUPPORTED)
    args = parser.parse_args()
    case_id = args.case
    try:
        details = CASES[case_id]()
        write_evidence(case_id, "PASS", details, [])
        print(f"{case_id}: PASS")
        return 0
    except Exception as exc:
        write_evidence(case_id, "FAIL", {}, [f"{type(exc).__name__}: {exc}"])
        print(f"{case_id}: FAIL: {type(exc).__name__}: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
