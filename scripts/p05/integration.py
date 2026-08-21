#!/usr/bin/env python3
"""GoJet V10 P05 real-dependency integration driver for P05-T001..P05-T016."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import http.client
import json
import os
import subprocess
import sys
from pathlib import Path
from typing import Any
from urllib.parse import urlencode, urlsplit, urlparse, parse_qs

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "artifacts" / "v10" / "P05" / "results"
MIGRATION = ROOT / "migrations" / "000001_links_vertical_slice.sql"

MYSQL_HOST = os.getenv("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = int(os.getenv("GOJET_TEST_MYSQL_PORT", "3306"))
MYSQL_USER = os.getenv("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.getenv("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.getenv("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
REDIS_HOST = os.getenv("GOJET_TEST_REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.getenv("GOJET_TEST_REDIS_PORT", "6379"))
PLATFORM_URL = os.getenv("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081")
REDIRECT_URL = os.getenv("GOJET_TEST_REDIRECT_URL", "http://127.0.0.1:18080")
WORKSPACE = "ws-p05"
ACTOR = "p05-test-actor"


def now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def iso(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def commit_sha() -> str:
    value = os.getenv("GITHUB_SHA", "").strip()
    if value:
        return value
    p = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True)
    return p.stdout.strip()


def run(argv: list[str], *, input_text: str | None = None, expect: int | None = 0) -> subprocess.CompletedProcess[str]:
    p = subprocess.run(argv, cwd=ROOT, text=True, input=input_text, capture_output=True, env=os.environ.copy())
    if expect is not None and p.returncode != expect:
        raise AssertionError(f"command failed {p.returncode}: {' '.join(argv)}\nstdout={p.stdout}\nstderr={p.stderr}")
    return p


def mysql_args() -> list[str]:
    return [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT),
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE,
    ]


def mysql_env_run(*, query: str | None = None, file_text: str | None = None, expect: int | None = 0) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    argv = mysql_args()
    if query is not None:
        argv += ["-e", query]
    p = subprocess.run(argv, cwd=ROOT, text=True, input=file_text, capture_output=True, env=env)
    if expect is not None and p.returncode != expect:
        raise AssertionError(f"mysql failed {p.returncode}: {query or '<stdin>'}\nstdout={p.stdout}\nstderr={p.stderr}")
    return p


def mysql_scalar(query: str) -> str:
    return mysql_env_run(query=query).stdout.strip()


def reset_schema() -> None:
    mysql_env_run(query="SET FOREIGN_KEY_CHECKS=0; DROP TABLE IF EXISTS link_audit_events; DROP TABLE IF EXISTS link_versions; DROP TABLE IF EXISTS links; SET FOREIGN_KEY_CHECKS=1;")
    redis_cli("FLUSHDB")


def apply_migration() -> None:
    mysql_env_run(file_text=MIGRATION.read_text(encoding="utf-8"))


def redis_cli(*args: str, expect: int = 0) -> str:
    argv = ["redis-cli", "-h", REDIS_HOST, "-p", str(REDIS_PORT), "--raw", *args]
    p = run(argv, expect=expect)
    return p.stdout.strip()


def risk_key(link: dict[str, Any]) -> str:
    return f"risk:link:{link['id']}:{link['risk_fingerprint']}"


def set_risk(link: dict[str, Any], state: str, *, stale: bool = False, malformed: bool = False) -> None:
    key = risk_key(link)
    redis_cli("DEL", key)
    if malformed:
        redis_cli("SET", key, "{not-json", "EX", "300")
        return
    checked = now() - (dt.timedelta(minutes=10) if stale else dt.timedelta(seconds=1))
    valid_until = now() - dt.timedelta(seconds=1) if stale else now() + dt.timedelta(minutes=5)
    payload = json.dumps({
        "schema_version": 1,
        "decision": state,
        "fingerprint": link["risk_fingerprint"],
        "checked_at": iso(checked),
        "valid_until": iso(valid_until),
        "policy_version": "p05-integration-v1",
    }, separators=(",", ":"))
    redis_cli("SET", key, payload, "EX", "300")


def http_request(base: str, method: str, path: str, *, payload: Any | None = None, headers: dict[str, str] | None = None) -> tuple[int, dict[str, str], bytes]:
    parsed = urlsplit(base)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=10)
    body: bytes | None = None
    merged = {"Accept": "application/json"}
    if headers:
        merged.update(headers)
    if payload is not None:
        body = json.dumps(payload, separators=(",", ":")).encode()
        merged["Content-Type"] = "application/json"
    conn.request(method, path, body=body, headers=merged)
    response = conn.getresponse()
    data = response.read()
    result_headers = {k.lower(): v for k, v in response.getheaders()}
    status = response.status
    conn.close()
    return status, result_headers, data


def auth_headers(workspace: str = WORKSPACE, role: str = "owner", actor: str = ACTOR, correlation: str | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": actor,
        "X-GoJet-Test-Workspace-Role": role,
        "X-GoJet-Test-Workspace": workspace,
    }
    if correlation:
        headers["X-Request-ID"] = correlation
    return headers


def api(method: str, path: str, *, payload: Any | None = None, workspace: str = WORKSPACE, role: str = "owner", header_workspace: str | None = None, correlation: str | None = None) -> tuple[int, dict[str, str], Any]:
    headers = auth_headers(header_workspace or workspace, role=role, correlation=correlation)
    status, response_headers, raw = http_request(PLATFORM_URL, method, path, payload=payload, headers=headers)
    content_type = response_headers.get("content-type", "")
    if "application/json" in content_type and raw:
        return status, response_headers, json.loads(raw)
    return status, response_headers, raw.decode(errors="replace")


def redirect(hostname: str, code: str, *, country: str | None = None) -> tuple[int, dict[str, str], str]:
    headers = {"Host": hostname, "User-Agent": "GoJet-P05-Integration/1.0", "Accept-Language": "en-US,en;q=0.9"}
    if country:
        headers["X-GoJet-Test-Country"] = country
    status, response_headers, raw = http_request(REDIRECT_URL, "GET", f"/{code}", headers=headers)
    return status, response_headers, raw.decode(errors="replace")


def create_payload(code: str, destination: str, **overrides: Any) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "hostname": "go.p05.test",
        "domain_kind": "official",
        "code": code,
        "title": f"P05 {code}",
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
    payload.update(overrides)
    return payload


def create_link(code: str, destination: str, *, workspace: str = WORKSPACE, **overrides: Any) -> dict[str, Any]:
    status, _, value = api("POST", f"/api/workspaces/{workspace}/links", payload=create_payload(code, destination, **overrides), workspace=workspace, correlation=f"create-{code}")
    assert status == 201, (status, value)
    assert isinstance(value, dict)
    return value


def update_payload(link: dict[str, Any], **overrides: Any) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "expected_version": link["version"],
        "hostname": link["hostname"],
        "domain_kind": link["domain_kind"],
        "code": link["code"],
        "title": link["title"],
        "primary_destination": link["primary_destination"],
        "redirect_status": link["redirect_status"],
        "status": link["status"],
        "routing": link.get("routing") or [],
        "ab": link.get("ab") or [],
        "utm": link.get("utm") or {},
        "access": {},
        "expires_at": link.get("expires_at"),
        "click_limit": link.get("click_limit"),
        "one_time": bool(link.get("one_time")),
        "change_reason": "integration update",
    }
    payload.update(overrides)
    return payload


def patch_link(link: dict[str, Any], *, workspace: str = WORKSPACE, correlation: str = "p05-update", **overrides: Any) -> tuple[int, Any]:
    status, _, value = api("PATCH", f"/api/workspaces/{workspace}/links/{link['id']}", payload=update_payload(link, **overrides), workspace=workspace, correlation=correlation)
    return status, value


def prepare_case() -> None:
    reset_schema()
    apply_migration()


def assert_no_destination(response_headers: dict[str, str], body: str, destinations: list[str]) -> None:
    assert "location" not in response_headers, response_headers
    for destination in destinations:
        assert destination not in body, (destination, body)


def case_t001() -> dict[str, Any]:
    reset_schema()
    apply_migration()
    tables = int(mysql_scalar("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('links','link_versions','link_audit_events');"))
    indexes = int(mysql_scalar("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name='links' AND index_name IN ('uq_links_hostname_code','idx_links_risk_fingerprint');"))
    assert tables == 3
    assert indexes >= 2
    second = mysql_env_run(file_text=MIGRATION.read_text(encoding="utf-8"), expect=None)
    assert second.returncode != 0, "second immutable migration application unexpectedly succeeded"
    return {"tables": tables, "required_indexes": indexes, "second_apply_exit": second.returncode}


def case_t002() -> dict[str, Any]:
    prepare_case()
    link = create_link("crud", "https://example.com/one")
    status, _, fetched = api("GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}")
    assert status == 200 and fetched["id"] == link["id"]
    status, updated = patch_link(link, title="Updated CRUD", primary_destination="https://example.com/two", change_reason="update CRUD")
    assert status == 200 and updated["version"] == 2
    status, _, _ = api("DELETE", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", payload={"expected_version": updated["version"], "change_reason": "delete CRUD"}, correlation="delete-crud")
    assert status == 204
    row = mysql_scalar(f"SELECT CONCAT(status,':',version) FROM links WHERE id={link['id']};")
    assert row == "deleted:3", row
    return {"link_id": link["id"], "persisted_state": row}


def case_t003() -> dict[str, Any]:
    prepare_case()
    status, _, value = api("POST", f"/api/workspaces/{WORKSPACE}/links", payload=create_payload("bad code", "https://example.com"))
    assert status == 400, (status, value)
    link = create_link("unique", "https://example.com")
    status, _, value = api("POST", f"/api/workspaces/{WORKSPACE}/links", payload=create_payload("unique", "https://example.net"))
    assert status == 409, (status, value)
    return {"existing_id": link["id"], "invalid_code_status": 400, "duplicate_status": 409}


def case_t004() -> dict[str, Any]:
    p = run(["go", "test", "./internal/links", "-run", "^TestRiskFingerprintTargetSet$", "-count=1", "-v"])
    return {"driver": "go test", "output_sha256": hashlib.sha256(p.stdout.encode()).hexdigest()}


def case_t005() -> dict[str, Any]:
    prepare_case()
    link = create_link("mutate", "https://example.com/original")
    set_risk(link, "allow")
    old_fp = link["risk_fingerprint"]
    status, updated = patch_link(link, primary_destination="https://example.com/new", change_reason="mutate destination")
    assert status == 200 and updated["risk_fingerprint"] != old_fp
    r_status, headers, body = redirect(updated["hostname"], updated["code"])
    assert r_status == 200
    assert_no_destination(headers, body, ["https://example.com/original", "https://example.com/new"])
    return {"old_fingerprint": old_fp, "new_fingerprint": updated["risk_fingerprint"], "redirect_status": r_status}


def case_t006() -> dict[str, Any]:
    prepare_case()
    link = create_link("allow", "https://example.com/allowed", redirect_status=307)
    set_risk(link, "allow")
    status, headers, _ = redirect(link["hostname"], link["code"])
    assert status == 307 and headers.get("location") == "https://example.com/allowed"
    return {"status": status, "location": headers.get("location")}


def case_t007() -> dict[str, Any]:
    prepare_case()
    link = create_link("review", "https://unsafe.example/review")
    set_risk(link, "review")
    status, headers, body = redirect(link["hostname"], link["code"])
    assert status == 200 and "under review" in body.lower()
    assert_no_destination(headers, body, [link["primary_destination"]])
    assert headers.get("x-robots-tag") == "noindex, nofollow"
    return {"status": status, "safety": "review"}


def case_t008() -> dict[str, Any]:
    prepare_case()
    link = create_link("blocked", "https://unsafe.example/blocked")
    set_risk(link, "block")
    status, headers, body = redirect(link["hostname"], link["code"])
    assert status == 200 and "link blocked" in body.lower()
    assert_no_destination(headers, body, [link["primary_destination"]])
    return {"status": status, "safety": "blocked"}


def case_t009() -> dict[str, Any]:
    prepare_case()
    link = create_link("riskstates", "https://unsafe.example/states")
    outcomes: dict[str, int] = {}
    redis_cli("DEL", risk_key(link))
    status, headers, body = redirect(link["hostname"], link["code"])
    assert status == 200
    assert_no_destination(headers, body, [link["primary_destination"]])
    outcomes["missing"] = status
    set_risk(link, "allow", malformed=True)
    status, headers, body = redirect(link["hostname"], link["code"])
    assert status == 200
    assert_no_destination(headers, body, [link["primary_destination"]])
    outcomes["malformed"] = status
    set_risk(link, "allow", stale=True)
    status, headers, body = redirect(link["hostname"], link["code"])
    assert status == 200
    assert_no_destination(headers, body, [link["primary_destination"]])
    outcomes["stale"] = status
    return outcomes


def case_t010() -> dict[str, Any]:
    prepare_case()
    link = create_link(
        "ordering", "https://primary.example/path",
        routing=[{"id": "us", "match_type": "country", "match_value": "US", "destination": "https://route.example/us", "enabled": True}],
        ab=[
            {"id": "a", "destination": "https://a.example/path", "weight": 50, "enabled": True},
            {"id": "b", "destination": "https://b.example/path", "weight": 50, "enabled": True},
        ],
        utm={"source": "gojet", "campaign": "p05"},
        access={"password": "P05-Ordering-Password-2026!"},
        click_limit=1,
        one_time=True,
    )
    redis_cli("DEL", risk_key(link))
    status, headers, body = redirect(link["hostname"], link["code"], country="US")
    assert status == 200
    assert_no_destination(headers, body, ["https://primary.example/path", "https://route.example/us", "https://a.example/path", "https://b.example/path"])
    click_count = int(mysql_scalar(f"SELECT click_count FROM links WHERE id={link['id']};"))
    assert click_count == 0, click_count
    return {"risk_status": status, "click_count_before_allow": click_count}


def case_t011() -> dict[str, Any]:
    prepare_case()
    link = create_link(
        "routeab", "https://primary.example/root",
        routing=[{"id": "us", "match_type": "country", "match_value": "US", "destination": "https://route.example/us?existing=1", "enabled": True}],
        ab=[
            {"id": "a", "destination": "https://a.example/path", "weight": 50, "enabled": True},
            {"id": "b", "destination": "https://b.example/path", "weight": 50, "enabled": True},
        ],
        utm={"source": "gojet", "medium": "short", "campaign": "p05"},
    )
    set_risk(link, "allow")
    status, headers, _ = redirect(link["hostname"], link["code"], country="US")
    assert status == 302
    routed = urlparse(headers["location"])
    assert routed.hostname == "route.example"
    query = parse_qs(routed.query)
    assert query.get("utm_source") == ["gojet"] and query.get("utm_campaign") == ["p05"] and query.get("existing") == ["1"]

    link2 = create_link(
        "abonly", "https://primary.example/root",
        ab=[
            {"id": "a", "destination": "https://a.example/path", "weight": 50, "enabled": True},
            {"id": "b", "destination": "https://b.example/path", "weight": 50, "enabled": True},
        ],
        utm={"source": "gojet"},
    )
    set_risk(link2, "allow")
    status2, headers2, _ = redirect(link2["hostname"], link2["code"])
    assert status2 == 302
    ab_url = urlparse(headers2["location"])
    assert ab_url.hostname in {"a.example", "b.example"}
    assert parse_qs(ab_url.query).get("utm_source") == ["gojet"]
    return {"routing_location": headers["location"], "ab_location": headers2["location"]}


def case_t012() -> dict[str, Any]:
    prepare_case()
    payload = create_payload("viewer", "https://example.com")
    status, _, _ = api("POST", f"/api/workspaces/{WORKSPACE}/links", payload=payload, role="viewer")
    assert status == 403
    link = create_link("scope", "https://example.com")
    status2, _, _ = api("GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", header_workspace="other-workspace")
    assert status2 == 403
    return {"viewer_mutation": status, "cross_workspace": status2}


def case_t013() -> dict[str, Any]:
    prepare_case()
    link = create_link("conflict", "https://example.com/a")
    stale_payload = update_payload(link, primary_destination="https://example.com/stale", change_reason="stale writer")
    status1, first = patch_link(link, primary_destination="https://example.com/first", change_reason="first writer")
    assert status1 == 200 and first["version"] == 2
    status2, _, _ = api("PATCH", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", payload=stale_payload, correlation="stale-writer")
    assert status2 == 409
    current_status, _, current = api("GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}")
    assert current_status == 200 and current["primary_destination"] == "https://example.com/first"
    return {"first_version": first["version"], "stale_status": status2}


def case_t014() -> dict[str, Any]:
    prepare_case()
    link = create_link("history", "https://example.com/v1")
    status, updated = patch_link(link, primary_destination="https://example.com/v2", change_reason="move to v2", correlation="corr-update-v2")
    assert status == 200
    status, _, history = api("GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}/history")
    assert status == 200 and len(history["items"]) >= 2
    status, _, restored = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links/{link['id']}/restore",
        payload={"expected_version": updated["version"], "restore_version": 1, "change_reason": "restore v1"},
        correlation="corr-restore-v1",
    )
    assert status == 200 and restored["version"] == 3 and restored["primary_destination"] == "https://example.com/v1"
    actions = mysql_env_run(query=f"SELECT CONCAT(action,':',request_correlation_id) FROM link_audit_events WHERE link_id={link['id']} ORDER BY id;").stdout.strip().splitlines()
    assert any(x == "link.update:corr-update-v2" for x in actions)
    assert any(x == "link.restore:corr-restore-v1" for x in actions)
    return {"history_count": len(history["items"]), "restored_version": restored["version"], "audit": actions}


def case_t015() -> dict[str, Any]:
    prepare_case()
    alpha = create_link("alpha", "https://alpha.example")
    beta = create_link("beta", "https://beta.example")
    gamma = create_link("gamma", "https://gamma.example")
    status, _, bulk = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links/bulk",
        payload={"action": "pause", "items": [{"id": beta["id"], "version": beta["version"]}], "change_reason": "pause beta"},
        correlation="bulk-pause",
    )
    assert status == 200 and bulk["results"][0]["status"] == "success"
    beta_version = bulk["results"][0]["version"]
    status, _, search = api("GET", f"/api/workspaces/{WORKSPACE}/links?{urlencode({'q':'alpha'})}")
    assert status == 200 and search["total"] == 1 and search["items"][0]["id"] == alpha["id"]
    status, _, paused = api("GET", f"/api/workspaces/{WORKSPACE}/links?status=paused")
    assert status == 200 and paused["total"] == 1 and paused["items"][0]["id"] == beta["id"]
    status, _, activate = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links/bulk",
        payload={"action": "activate", "items": [{"id": beta["id"], "version": beta_version}], "change_reason": "activate beta"},
        correlation="bulk-activate",
    )
    assert status == 200 and activate["results"][0]["status"] == "success"
    status, _, delete = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links/bulk",
        payload={"action": "delete", "items": [{"id": gamma["id"], "version": gamma["version"]}], "change_reason": "delete gamma"},
        correlation="bulk-delete",
    )
    assert status == 200 and delete["results"][0]["status"] == "success"
    export_status, headers, export_body = api("GET", f"/api/workspaces/{WORKSPACE}/links/export")
    assert export_status == 200 and "text/csv" in headers.get("content-type", "") and "alpha" in export_body and "beta" in export_body and "gamma" not in export_body
    return {"search_total": search["total"], "paused_total": paused["total"], "bulk": [bulk["results"][0]["status"], activate["results"][0]["status"], delete["results"][0]["status"]]}


def case_t016() -> dict[str, Any]:
    prepare_case()
    statuses: dict[str, int] = {}
    for code, configured in (("r301", 301), ("r302", 302), ("r307", 307), ("r308", 308)):
        link = create_link(code, f"https://example.com/{code}", redirect_status=configured)
        set_risk(link, "allow")
        status, headers, _ = redirect(link["hostname"], link["code"])
        assert status == configured and headers.get("location") == f"https://example.com/{code}"
        statuses[code] = status

    paused = create_link("paused", "https://example.com/paused")
    set_risk(paused, "allow")
    status, paused_value = patch_link(paused, status="paused", change_reason="pause")
    assert status == 200
    status, headers, body = redirect(paused_value["hostname"], paused_value["code"])
    assert status == 200
    assert_no_destination(headers, body, [paused_value["primary_destination"]])
    statuses["paused"] = status

    expired_time = iso(now() - dt.timedelta(minutes=1))
    expired = create_link("expired", "https://example.com/expired", expires_at=expired_time)
    set_risk(expired, "allow")
    status, headers, body = redirect(expired["hostname"], expired["code"])
    assert status == 410
    assert_no_destination(headers, body, [expired["primary_destination"]])
    statuses["expired"] = status

    limited = create_link("limited", "https://example.com/limited", click_limit=1)
    set_risk(limited, "allow")
    first, _, _ = redirect(limited["hostname"], limited["code"])
    second, headers, body = redirect(limited["hostname"], limited["code"])
    assert first == 302 and second == 410
    assert_no_destination(headers, body, [limited["primary_destination"]])
    statuses["limited_first"] = first
    statuses["limited_second"] = second

    deleted = create_link("deleted", "https://example.com/deleted")
    set_risk(deleted, "allow")
    status, _, _ = api("DELETE", f"/api/workspaces/{WORKSPACE}/links/{deleted['id']}", payload={"expected_version": deleted["version"], "change_reason": "delete"}, correlation="delete-disabled")
    assert status == 204
    status, headers, body = redirect(deleted["hostname"], deleted["code"])
    assert status == 410
    assert_no_destination(headers, body, [deleted["primary_destination"]])
    statuses["deleted"] = status
    return statuses


CASES = {
    "P05-T001": case_t001,
    "P05-T002": case_t002,
    "P05-T003": case_t003,
    "P05-T004": case_t004,
    "P05-T005": case_t005,
    "P05-T006": case_t006,
    "P05-T007": case_t007,
    "P05-T008": case_t008,
    "P05-T009": case_t009,
    "P05-T010": case_t010,
    "P05-T011": case_t011,
    "P05-T012": case_t012,
    "P05-T013": case_t013,
    "P05-T014": case_t014,
    "P05-T015": case_t015,
    "P05-T016": case_t016,
}


def write_result(case_id: str, status: str, details: dict[str, Any], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    payload = {
        "case_id": case_id,
        "status": status,
        "generated_at": iso(now()),
        "implementation_commit": commit_sha(),
        "environment": {
            "mysql": f"{MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DATABASE}",
            "redis": f"{REDIS_HOST}:{REDIS_PORT}",
            "platformapi": PLATFORM_URL,
            "redirectengine": REDIRECT_URL,
        },
        "details": details,
        "errors": errors,
    }
    (RESULTS / f"{case_id}.json").write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def execute(case_id: str) -> bool:
    try:
        details = CASES[case_id]()
        write_result(case_id, "PASS", details, [])
        print(f"{case_id}: PASS")
        return True
    except Exception as exc:
        message = f"{type(exc).__name__}: {exc}"
        write_result(case_id, "FAIL", {}, [message])
        print(f"{case_id}: FAIL\n  - {message}", file=sys.stderr)
        return False


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", choices=[*CASES.keys(), "all"], required=True)
    args = parser.parse_args()
    selected = list(CASES) if args.case == "all" else [args.case]
    ok = True
    for case_id in selected:
        ok = execute(case_id) and ok
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
