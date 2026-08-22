#!/usr/bin/env python3
"""GoJet V10 P07 real Redis/MySQL/API/worker/reconciler integration driver."""

from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import http.client
import json
import os
import subprocess
from pathlib import Path
from typing import Any, Callable
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[2]
RESULTS = ROOT / "artifacts" / "v10" / "P07" / "results"
MYSQL_HOST = os.getenv("GOJET_TEST_MYSQL_HOST", "127.0.0.1")
MYSQL_PORT = int(os.getenv("GOJET_TEST_MYSQL_PORT", "3306"))
MYSQL_USER = os.getenv("GOJET_TEST_MYSQL_USER", "root")
MYSQL_PASSWORD = os.getenv("GOJET_TEST_MYSQL_PASSWORD", "root")
MYSQL_DATABASE = os.getenv("GOJET_TEST_MYSQL_DATABASE", "gojet_test")
REDIS_HOST = os.getenv("GOJET_TEST_REDIS_HOST", "127.0.0.1")
REDIS_PORT = int(os.getenv("GOJET_TEST_REDIS_PORT", "6379"))
PLATFORM_URL = os.getenv("GOJET_TEST_PLATFORM_URL", "http://127.0.0.1:18081")
REDIRECT_URL = os.getenv("GOJET_TEST_REDIRECT_URL", "http://127.0.0.1:18080")
WORKER_BIN = os.getenv("GOJET_TEST_ANALYTICS_WORKER_BIN", "/tmp/gojet-p07/analyticsworker")
RECONCILER_BIN = os.getenv("GOJET_TEST_ANALYTICS_RECONCILER_BIN", "/tmp/gojet-p07/analyticsreconciler")
FIXTURE_BIN = os.getenv("GOJET_TEST_ANALYTICS_FIXTURE_BIN", "/tmp/gojet-p07/analyticsfixture")
WORKSPACE = "ws-p07"
OTHER_WORKSPACE = "ws-p07-other"
ACTOR = "p07-test-actor"
STREAM = "gojet:analytics:clicks:v1"
GROUP = "gojet-analytics-workers-v1"


def utcnow() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


def iso(value: dt.datetime) -> str:
    return value.astimezone(dt.timezone.utc).isoformat(timespec="microseconds").replace("+00:00", "Z")


def commit_sha() -> str:
    value = os.getenv("GITHUB_SHA", "").strip()
    if value:
        return value
    return subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True).stdout.strip()


def run(argv: list[str], *, env: dict[str, str] | None = None, timeout: int = 45, expect: int = 0) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    proc = subprocess.run(argv, cwd=ROOT, text=True, capture_output=True, env=merged, timeout=timeout)
    if proc.returncode != expect:
        raise AssertionError(f"command failed {proc.returncode}: {' '.join(argv)}\nstdout={proc.stdout}\nstderr={proc.stderr}")
    return proc


def mysql_args() -> list[str]:
    return ["mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT), "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE]


def mysql_query(query: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    proc = subprocess.run(mysql_args() + ["-e", query], cwd=ROOT, text=True, capture_output=True, env=env)
    if proc.returncode != 0:
        raise AssertionError(f"mysql failed: {query}\nstdout={proc.stdout}\nstderr={proc.stderr}")
    return proc.stdout.strip()


def mysql_scalar(query: str) -> str:
    output = mysql_query(query)
    return output.splitlines()[0].strip() if output else ""


def redis_cli(*args: str) -> str:
    return run(["redis-cli", "-h", REDIS_HOST, "-p", str(REDIS_PORT), "--raw", *args]).stdout.strip()


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
    response_headers = {key.lower(): value for key, value in response.getheaders()}
    status = response.status
    conn.close()
    return status, response_headers, data


def workspace_headers(workspace: str, role: str = "owner", *, analytics_permission: str | None = None) -> dict[str, str]:
    headers = {
        "X-GoJet-Test-Actor": ACTOR,
        "X-GoJet-Test-Workspace-Role": role,
        "X-GoJet-Test-Workspace": workspace,
        "X-Request-ID": f"p07-{workspace}-{role}",
    }
    if analytics_permission is not None:
        headers["X-GoJet-Test-Analytics-Permission"] = analytics_permission
    return headers


def api_create_link(code: str, destination: str, *, workspace: str = WORKSPACE, campaign: str = "") -> dict[str, Any]:
    payload = {
        "hostname": "go.p07.test" if workspace == WORKSPACE else "go.other-p07.test",
        "domain_kind": "official",
        "code": code,
        "title": f"P07 {code}",
        "primary_destination": destination,
        "redirect_status": 302,
        "routing": [], "ab": [],
        "utm": {"campaign": campaign} if campaign else {},
        "access": {}, "expires_at": None, "click_limit": None, "one_time": False,
        "change_reason": f"create {code}",
    }
    status, _, raw = http_request(PLATFORM_URL, "POST", f"/api/workspaces/{workspace}/links", payload=payload, headers=workspace_headers(workspace))
    if status != 201:
        raise AssertionError(f"create link status={status} body={raw.decode(errors='replace')}")
    return json.loads(raw)


def link_by_code(code: str, workspace: str = WORKSPACE) -> dict[str, Any] | None:
    safe_code = code.replace("'", "''")
    safe_ws = workspace.replace("'", "''")
    raw = mysql_query(
        "SELECT id,workspace_id,hostname,code,risk_fingerprint,click_count "
        f"FROM links WHERE workspace_id='{safe_ws}' AND code='{safe_code}' LIMIT 1"
    )
    if not raw:
        return None
    row = raw.split("\t")
    return {"id": int(row[0]), "workspace_id": row[1], "hostname": row[2], "code": row[3], "risk_fingerprint": row[4], "click_count": int(row[5])}


def ensure_link(code: str, destination: str, *, workspace: str = WORKSPACE, campaign: str = "") -> dict[str, Any]:
    return link_by_code(code, workspace) or api_create_link(code, destination, workspace=workspace, campaign=campaign)


def set_risk_allow(link: dict[str, Any]) -> None:
    now = utcnow()
    payload = json.dumps({
        "schema_version": 1, "decision": "allow", "fingerprint": link["risk_fingerprint"],
        "checked_at": iso(now - dt.timedelta(seconds=1)), "valid_until": iso(now + dt.timedelta(minutes=5)),
        "policy_version": "p07-integration-v1",
    }, separators=(",", ":"))
    redis_cli("SET", f"risk:link:{link['id']}:{link['risk_fingerprint']}", payload, "EX", "300")


def redirect(link: dict[str, Any], *, country: str = "sg", user_agent: str = "GoJet-P07-Mobile/1.0 Mobile") -> tuple[int, dict[str, str], str]:
    headers = {
        "Host": link["hostname"], "User-Agent": user_agent,
        "Accept-Language": "en-SG,en;q=0.9", "Referer": "https://source.p07.test/article",
        "X-GoJet-Test-Country": country,
    }
    status, response_headers, raw = http_request(REDIRECT_URL, "GET", f"/{link['code']}", headers=headers)
    return status, response_headers, raw.decode(errors="replace")


def deterministic_event_id(workspace: str, link_id: int, click_sequence: int) -> str:
    return hashlib.sha256(f"gojet.analytics.click.v1\n{workspace}\n{link_id}\n{click_sequence}".encode()).hexdigest()


def ensure_redirect_event(code: str, destination: str) -> tuple[dict[str, Any], str]:
    link = ensure_link(code, destination)
    event_id = deterministic_event_id(WORKSPACE, int(link["id"]), 1)
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE event_id='{event_id}'") == "0":
        set_risk_allow(link)
        status, headers, _ = redirect(link)
        assert status == 302 and headers.get("location") == destination, (status, headers)
    return link, event_id


def fixture_claim(link: dict[str, Any], when: dt.datetime, *, country: str = "", device: str = "desktop", language: str = "en", source: str = "", campaign: str = "", publish: bool = True) -> dict[str, Any]:
    argv = [
        FIXTURE_BIN, "-workspace", link["workspace_id"], "-link-id", str(link["id"]), "-at", iso(when),
        "-country", country, "-device", device, "-language", language, "-source", source,
        "-campaign", campaign, f"-publish={'true' if publish else 'false'}",
    ]
    proc = run(argv)
    return json.loads(proc.stdout)


def worker_once(consumer: str, count: int = 1) -> str:
    proc = run([WORKER_BIN], env={"GOJET_ANALYTICS_WORKER_CONSUMER": consumer, "GOJET_ANALYTICS_WORKER_MAX_MESSAGES": str(count)}, timeout=45)
    return proc.stdout + proc.stderr


def reconciler_once(*, repair: bool) -> str:
    proc = run([RECONCILER_BIN], env={
        "GOJET_ANALYTICS_RECONCILER_ONCE": "1",
        "GOJET_ANALYTICS_RECONCILE_REPAIR": "1" if repair else "0",
    }, timeout=45)
    return proc.stdout + proc.stderr


def set_workspace_state(workspace: str, status: str = "complete", retention_days: int = 3660, reason: str = "integration") -> None:
    safe_ws = workspace.replace("'", "''")
    safe_reason = reason.replace("'", "''")
    mysql_query(
        "INSERT INTO analytics_workspace_state(workspace_id,status,data_through_at,retention_days,state_reason) "
        f"VALUES('{safe_ws}','{status}',NOW(6),{retention_days},'{safe_reason}') "
        "ON DUPLICATE KEY UPDATE status=VALUES(status),data_through_at=VALUES(data_through_at),retention_days=VALUES(retention_days),state_reason=VALUES(state_reason)"
    )


def analytics_api(method: str, path: str, *, workspace: str = WORKSPACE, role: str = "owner", permission: str | None = "allow", payload: Any | None = None) -> tuple[int, Any]:
    status, _, raw = http_request(PLATFORM_URL, method, path, payload=payload, headers=workspace_headers(workspace, role, analytics_permission=permission))
    try:
        body: Any = json.loads(raw) if raw else None
    except json.JSONDecodeError:
        body = raw.decode(errors="replace")
    return status, body


def report_path(*, workspace: str = WORKSPACE, link_id: int | None = None, start: dt.datetime, end: dt.datetime, timezone: str = "UTC", granularity: str = "day", **filters: str) -> str:
    params = {"from": iso(start), "to": iso(end), "timezone": timezone, "granularity": granularity}
    for key, value in filters.items():
        if value:
            params[key] = value
    base = f"/api/workspaces/{workspace}/analytics/overview" if link_id is None else f"/api/workspaces/{workspace}/analytics/links/{link_id}"
    return base + "?" + urlencode(params)


def query_report(*, workspace: str = WORKSPACE, link_id: int | None = None, start: dt.datetime, end: dt.datetime, timezone: str = "UTC", granularity: str = "day", role: str = "owner", permission: str | None = "allow", **filters: str) -> tuple[int, Any]:
    path = report_path(workspace=workspace, link_id=link_id, start=start, end=end, timezone=timezone, granularity=granularity, **filters)
    return analytics_api("GET", path, workspace=workspace, role=role, permission=permission)


def dimension_map(report: dict[str, Any], name: str) -> dict[str, int]:
    return {item["value"]: int(item["clicks"]) for item in report["dimensions"][name]}


def evidence(case_id: str, observed: dict[str, Any]) -> dict[str, Any]:
    return {
        "case_id": case_id, "status": "PASS", "implementation_commit": commit_sha(),
        "environment": {"mysql": f"{MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DATABASE}", "redis": f"{REDIS_HOST}:{REDIS_PORT}", "stream": STREAM},
        "observed": observed, "errors": [],
    }


def write_result(case_id: str, data: dict[str, Any]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    (RESULTS / f"{case_id}.json").write_text(json.dumps(data, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def case_t001() -> dict[str, Any]:
    destination = "https://example.com/p07/t001?kept=1"
    link, event_id = ensure_redirect_event("p07t001", destination)
    row = mysql_query(
        "SELECT event_id,click_sequence,country_code,device,language,source_hostname,published_at IS NOT NULL,payload_json "
        f"FROM analytics_outbox WHERE event_id='{event_id}'"
    ).split("\t", 7)
    assert len(row) == 8 and row[0] == event_id and row[1] == "1"
    assert row[2:6] == ["sg", "mobile", "en-sg", "source.p07.test"]
    assert row[6] == "1"
    assert "destination" not in row[7].lower() and "example.com" not in row[7].lower()
    stream_dump = redis_cli("XRANGE", STREAM, "-", "+")
    assert event_id in stream_dump and destination not in stream_dump
    assert mysql_scalar(f"SELECT click_count FROM links WHERE id={link['id']}") == "1"
    return evidence("P07-T001", {"link_id": link["id"], "event_id": event_id, "redirect_status": 302, "redirect_location": destination, "outbox_published": True, "stream_contains_event": True, "destination_absent_from_event": True, "dimensions": {"country_code": row[2], "device": row[3], "language": row[4], "source_hostname": row[5]}})


def case_t002() -> dict[str, Any]:
    _, event_id = ensure_redirect_event("p07t001", "https://example.com/p07/t001?kept=1")
    logs = "already-consumed"
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'") == "0":
        logs = worker_once("p07-worker-t002", 1)
    event_count = int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'"))
    aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'"))
    assert event_count == 1 and aggregate >= 1
    return evidence("P07-T002", {"event_id": event_id, "event_rows": event_count, "workspace_aggregate_clicks": aggregate, "worker_log_has_event": event_id in logs or logs == "already-consumed"})


def case_t003() -> dict[str, Any]:
    case_t002()
    event_id = mysql_scalar("SELECT event_id FROM analytics_events WHERE workspace_id='ws-p07' ORDER BY consumed_at,event_id LIMIT 1")
    payload = mysql_scalar(f"SELECT payload_json FROM analytics_outbox WHERE event_id='{event_id}'")
    before_events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events WHERE workspace_id='ws-p07'"))
    before_aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'"))
    duplicate_stream_id = redis_cli("XADD", STREAM, "*", "event_id", event_id, "payload", payload)
    logs = worker_once("p07-worker-t003", 1)
    after_events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events WHERE workspace_id='ws-p07'"))
    after_aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'"))
    assert (after_events, after_aggregate) == (before_events, before_aggregate)
    return evidence("P07-T003", {"event_id": event_id, "duplicate_stream_id": duplicate_stream_id, "event_rows_before": before_events, "event_rows_after": after_events, "aggregate_before": before_aggregate, "aggregate_after": after_aggregate, "logical_once": True, "worker_acknowledged_duplicate": event_id in logs})


def case_t004() -> dict[str, Any]:
    destination = "https://example.com/p07/t004"
    link = ensure_link("p07t004", destination)
    event_id = deterministic_event_id(WORKSPACE, int(link["id"]), 1)
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE event_id='{event_id}'") == "0":
        set_risk_allow(link)
        status, headers, _ = redirect(link, country="us", user_agent="GoJet-P07-Restart/1.0")
        assert status == 302 and headers.get("location") == destination
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'") == "0":
        pending = redis_cli("XREADGROUP", "GROUP", GROUP, "p07-restart", "COUNT", "1", "STREAMS", STREAM, ">")
        assert event_id in pending
        pending_before = int(redis_cli("XPENDING", STREAM, GROUP).splitlines()[0])
        logs = worker_once("p07-restart", 1)
    else:
        pending_before, logs = 0, "already-recovered"
    pending_after = int(redis_cli("XPENDING", STREAM, GROUP).splitlines()[0])
    event_rows = int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'"))
    aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'"))
    source_events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events WHERE workspace_id='ws-p07'"))
    assert event_rows == 1 and pending_after == 0 and aggregate == source_events
    return evidence("P07-T004", {"event_id": event_id, "pending_before_restart": pending_before, "pending_after_restart": pending_after, "event_rows": event_rows, "aggregate_clicks": aggregate, "authoritative_event_rows": source_events, "no_loss": True, "no_double_count": True, "worker_recovered_pending": event_id in logs or logs == "already-recovered"})


def case_t005() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    link = ensure_link("p07t005", "https://example.com/p07/t005")
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, dt.datetime(2026, 1, 1, 23, 30, tzinfo=dt.timezone.utc), country="sg", device="desktop")
        fixture_claim(link, dt.datetime(2026, 1, 2, 0, 30, tzinfo=dt.timezone.utc), country="sg", device="desktop")
        fixture_claim(link, dt.datetime(2026, 1, 2, 0, 45, tzinfo=dt.timezone.utc), country="sg", device="desktop")
        worker_once("p07-worker-t005", 3)
    status, report = query_report(link_id=link["id"], start=dt.datetime(2026,1,1,23,0,tzinfo=dt.timezone.utc), end=dt.datetime(2026,1,2,2,0,tzinfo=dt.timezone.utc), timezone="UTC", granularity="hour")
    assert status == 200 and report["total_clicks"] == 3
    buckets = {item["key"]: int(item["clicks"]) for item in report["buckets"]}
    assert buckets == {"2026-01-01T23:00+00:00": 1, "2026-01-02T00:00+00:00": 2}, buckets
    persisted = int(mysql_scalar(f"SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE link_id={link['id']}"))
    assert persisted == 3
    return evidence("P07-T005", {"link_id": link["id"], "api_total": 3, "persisted_hourly_total": persisted, "buckets": buckets})


def case_t006() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    link = ensure_link("p07t006", "https://example.com/p07/t006", campaign="campaign-alpha")
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, dt.datetime(2026,1,3,10,0,tzinfo=dt.timezone.utc), country="sg", device="mobile", language="en-sg", source="news.example")
        fixture_claim(link, dt.datetime(2026,1,3,11,0,tzinfo=dt.timezone.utc), country="us", device="desktop", language="en-us", source="search.example")
        worker_once("p07-worker-t006", 2)
    start, end = dt.datetime(2026,1,3,0,0,tzinfo=dt.timezone.utc), dt.datetime(2026,1,4,0,0,tzinfo=dt.timezone.utc)
    status, report = query_report(link_id=link["id"], start=start, end=end)
    assert status == 200 and report["total_clicks"] == 2
    assert dimension_map(report, "country") == {"sg": 1, "us": 1}
    assert dimension_map(report, "device") == {"desktop": 1, "mobile": 1}
    assert dimension_map(report, "campaign") == {"campaign-alpha": 2}
    status, filtered = query_report(link_id=link["id"], start=start, end=end, country="sg", device="mobile")
    assert status == 200 and filtered["total_clicks"] == 1
    return evidence("P07-T006", {"link_id": link["id"], "country": dimension_map(report,"country"), "device": dimension_map(report,"device"), "language": dimension_map(report,"language"), "source": dimension_map(report,"source"), "campaign": dimension_map(report,"campaign"), "filtered_total": filtered["total_clicks"]})


def case_t007() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    set_workspace_state(OTHER_WORKSPACE)
    start, end = dt.datetime(2025,1,1,tzinfo=dt.timezone.utc), dt.datetime(2027,1,1,tzinfo=dt.timezone.utc)
    before_status, before = query_report(start=start, end=end)
    assert before_status == 200
    other = ensure_link("p07other", "https://example.com/p07/other", workspace=OTHER_WORKSPACE)
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={other['id']}")) == 0:
        fixture_claim(other, dt.datetime(2026,1,5,12,0,tzinfo=dt.timezone.utc), country="ca")
        worker_once("p07-worker-t007", 1)
    after_status, after = query_report(start=start, end=end)
    assert after_status == 200 and after["total_clicks"] == before["total_clicks"]
    status, body = query_report(workspace=WORKSPACE, link_id=other["id"], start=start, end=end)
    assert status == 404 and body["error"]["code"] == "not_found"
    path = report_path(workspace=WORKSPACE, start=start, end=end)
    status, _, _ = http_request(PLATFORM_URL, "GET", path, headers=workspace_headers(OTHER_WORKSPACE, analytics_permission="allow"))
    assert status == 403
    return evidence("P07-T007", {"other_workspace_link_id": other["id"], "workspace_total_before": before["total_clicks"], "workspace_total_after": after["total_clicks"], "cross_workspace_link_status": 404, "header_workspace_mismatch_status": 403})


def case_t008() -> dict[str, Any]:
    start, end = utcnow()-dt.timedelta(days=1), utcnow()+dt.timedelta(days=1)
    path = report_path(start=start, end=end)
    denied_status, denied = analytics_api("GET", path, permission="deny")
    missing_status, missing = analytics_api("GET", path, permission=None)
    viewer_status, viewer = analytics_api("GET", path, role="viewer", permission="allow")
    mutation_status, mutation = analytics_api("POST", f"/api/workspaces/{WORKSPACE}/analytics/conversions", role="viewer", permission="allow", payload={"conversion_id":"viewer-denied","campaign_id":"none","link_id":1,"occurred_at":iso(utcnow())})
    assert denied_status == 403 and missing_status == 403 and viewer_status == 200 and mutation_status == 403
    return evidence("P07-T008", {"permission_deny_status": denied_status, "permission_missing_status": missing_status, "viewer_read_status": viewer_status, "viewer_mutation_status": mutation_status, "viewer_mutation_code": mutation["error"]["code"]})


def case_t009() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    link = ensure_link("p07t009", "https://example.com/p07/t009")
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, dt.datetime(2026,1,4,18,20,tzinfo=dt.timezone.utc), country="in")
        fixture_claim(link, dt.datetime(2026,1,4,18,40,tzinfo=dt.timezone.utc), country="in")
        worker_once("p07-worker-t009", 2)
    start, end = dt.datetime(2026,1,4,18,0,tzinfo=dt.timezone.utc), dt.datetime(2026,1,4,19,0,tzinfo=dt.timezone.utc)
    status, local = query_report(link_id=link["id"], start=start, end=end, timezone="Asia/Kolkata", granularity="day")
    assert status == 200 and local["total_clicks"] == 2
    local_buckets = {b["key"]: int(b["clicks"]) for b in local["buckets"]}
    assert local_buckets == {"2026-01-04": 1, "2026-01-05": 1}, local_buckets
    status, utc = query_report(link_id=link["id"], start=start, end=end, timezone="UTC", granularity="day")
    utc_buckets = {b["key"]: int(b["clicks"]) for b in utc["buckets"]}
    assert status == 200 and utc_buckets == {"2026-01-04": 2}
    return evidence("P07-T009", {"link_id": link["id"], "asia_kolkata": local_buckets, "utc": utc_buckets, "half_open_range": "[from,to)"})


def case_t010() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    link = ensure_link("p07t010", "https://example.com/p07/t010", campaign="camp-t010")
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, dt.datetime(2026,1,6,10,0,tzinfo=dt.timezone.utc), device="mobile")
        fixture_claim(link, dt.datetime(2026,1,6,11,0,tzinfo=dt.timezone.utc), device="desktop")
        worker_once("p07-worker-t010", 2)
    start, end = dt.datetime(2026,1,6,0,0,tzinfo=dt.timezone.utc), dt.datetime(2026,1,7,0,0,tzinfo=dt.timezone.utc)
    filters = {"campaign":"camp-t010", "device":"mobile"}
    status1, overview = query_report(start=start, end=end, **filters)
    status2, detail = query_report(link_id=link["id"], start=start, end=end, **filters)
    assert status1 == status2 == 200 and overview["total_clicks"] == detail["total_clicks"] == 1
    assert overview["buckets"] == detail["buckets"] and overview["dimensions"] == detail["dimensions"]
    return evidence("P07-T010", {"link_id": link["id"], "overview_total": overview["total_clicks"], "detail_total": detail["total_clicks"], "same_buckets": True, "same_dimensions": True, "filters": filters})


def case_t011() -> dict[str, Any]:
    set_workspace_state(WORKSPACE)
    link = ensure_link("p07t011", "https://example.com/p07/t011", campaign="camp-t011")
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, dt.datetime(2026,1,7,10,0,tzinfo=dt.timezone.utc), device="mobile")
        worker_once("p07-worker-t011", 1)
    payload = {"conversion_id":"conv-t011-1","campaign_id":"camp-t011","link_id":link["id"],"occurred_at":"2026-01-07T10:05:00Z"}
    created_status, created = analytics_api("POST", f"/api/workspaces/{WORKSPACE}/analytics/conversions", payload=payload)
    duplicate_status, duplicate = analytics_api("POST", f"/api/workspaces/{WORKSPACE}/analytics/conversions", payload=payload)
    wrong_status, wrong = analytics_api("POST", f"/api/workspaces/{WORKSPACE}/analytics/conversions", payload={**payload,"conversion_id":"conv-wrong","campaign_id":"camp-not-measured"})
    other = ensure_link("p07other", "https://example.com/p07/other", workspace=OTHER_WORKSPACE)
    cross_status, cross = analytics_api("POST", f"/api/workspaces/{WORKSPACE}/analytics/conversions", payload={**payload,"conversion_id":"conv-cross","link_id":other["id"]})
    assert created_status == 201 and created["recorded"] is True
    assert duplicate_status == 200 and duplicate["idempotent_duplicate"] is True
    assert wrong_status == 409 and wrong["error"]["code"] == "campaign_not_measured"
    assert cross_status == 404
    status, report = query_report(link_id=link["id"], start=dt.datetime(2026,1,7,0,0,tzinfo=dt.timezone.utc), end=dt.datetime(2026,1,8,0,0,tzinfo=dt.timezone.utc), campaign="camp-t011")
    assert status == 200 and report["total_clicks"] == 1 and report["total_conversions"] == 1
    return evidence("P07-T011", {"link_id":link["id"],"created_status":created_status,"duplicate_status":duplicate_status,"wrong_campaign_status":wrong_status,"cross_workspace_status":cross_status,"campaign_clicks":report["total_clicks"],"campaign_conversions":report["total_conversions"]})


def case_t012() -> dict[str, Any]:
    link = ensure_link("p07t012", "https://example.com/p07/t012")
    now = utcnow()
    if int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE link_id={link['id']}")) == 0:
        fixture_claim(link, now-dt.timedelta(days=45), device="desktop")
        fixture_claim(link, now-dt.timedelta(days=5), device="mobile")
        worker_once("p07-worker-t012", 2)
    set_workspace_state(WORKSPACE, retention_days=30, reason="retention-test")
    status, report = query_report(link_id=link["id"], start=now-dt.timedelta(days=60), end=now+dt.timedelta(days=1))
    assert status == 200 and report["state"] == "retention-limited" and report["retention_limited"] is True
    assert report["total_clicks"] == 1 and report["effective_from"] != report["requested_from"]
    set_workspace_state(WORKSPACE, retention_days=3660)
    return evidence("P07-T012", {"link_id":link["id"],"state":report["state"],"retention_limited":report["retention_limited"],"visible_clicks":report["total_clicks"],"requested_from":report["requested_from"],"effective_from":report["effective_from"]})


def case_t013() -> dict[str, Any]:
    link = ensure_link("p07t013", "https://example.com/p07/t013")
    future_start = utcnow()+dt.timedelta(days=20)
    future_end = future_start+dt.timedelta(days=1)
    states: dict[str, str] = {}
    for persisted in ("complete", "partial", "stale"):
        set_workspace_state(WORKSPACE, status=persisted, retention_days=3660, reason=f"state-{persisted}")
        status, report = query_report(link_id=link["id"], start=future_start, end=future_end)
        assert status == 200
        states[persisted] = report["state"]
    assert states == {"complete":"empty","partial":"partial","stale":"stale"}, states
    set_workspace_state(WORKSPACE)
    return evidence("P07-T013", {"persisted_to_api_state":states,"complete_zero_distinct_from_unavailable":True})


def case_t014() -> dict[str, Any]:
    before_source = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events"))
    before_aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates"))
    assert before_source == before_aggregate and before_source > 0
    mysql_query("UPDATE analytics_hourly_aggregates SET clicks = clicks + 7 ORDER BY bucket_start,link_id LIMIT 1")
    corrupted = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates"))
    assert corrupted == before_source + 7
    logs = reconciler_once(repair=True)
    row = mysql_query("SELECT source_event_total,aggregate_total_before,aggregate_total_after,repaired FROM analytics_reconciliation_runs ORDER BY id DESC LIMIT 1").split("\t")
    assert row == [str(before_source), str(corrupted), str(before_source), "1"], row
    return evidence("P07-T014", {"source_total":before_source,"corrupted_aggregate":corrupted,"repaired_aggregate":int(row[2]),"repaired":True,"native_reconciler_logged_cycle":"analytics reconciliation cycle" in logs})


def case_t015() -> dict[str, Any]:
    stable_before = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates"))
    logs = reconciler_once(repair=True)
    row = mysql_query("SELECT source_event_total,aggregate_total_before,aggregate_total_after,repaired FROM analytics_reconciliation_runs ORDER BY id DESC LIMIT 1").split("\t")
    assert row[0] == row[1] == row[2] == str(stable_before) and row[3] == "0", row
    return evidence("P07-T015", {"source_total":int(row[0]),"aggregate_before":int(row[1]),"aggregate_after":int(row[2]),"second_run_repaired":False,"idempotent":True,"native_reconciler_logged_cycle":"analytics reconciliation cycle" in logs})


def case_t016() -> dict[str, Any]:
    outbox = int(mysql_scalar("SELECT COUNT(*) FROM analytics_outbox"))
    events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events"))
    aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates"))
    unpublished = int(mysql_scalar("SELECT COUNT(*) FROM analytics_outbox WHERE published_at IS NULL"))
    assert outbox == events == aggregate and unpublished == 0
    return evidence("P07-T016", {"accepted_outbox_total":outbox,"consumed_event_total":events,"aggregate_click_total":aggregate,"unpublished_outbox":unpublished,"known_total_closure":True})


def case_t017() -> dict[str, Any]:
    link = ensure_link("p07t017", "https://example.com/p07/t017")
    event_id = deterministic_event_id(WORKSPACE, int(link["id"]), 1)
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE event_id='{event_id}'") == "0":
        fixture_claim(link, utcnow()-dt.timedelta(minutes=2), device="desktop", publish=False)
    assert mysql_scalar(f"SELECT published_at IS NULL FROM analytics_outbox WHERE event_id='{event_id}'") == "1"
    recovery_logs = reconciler_once(repair=False)
    assert mysql_scalar(f"SELECT published_at IS NOT NULL FROM analytics_outbox WHERE event_id='{event_id}'") == "1"
    assert event_id in redis_cli("XRANGE", STREAM, "-", "+")
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'") == "0":
        worker_logs = worker_once("p07-worker-t017", 1)
    else:
        worker_logs = "already-consumed"
    closure_logs = reconciler_once(repair=True)
    outbox = int(mysql_scalar("SELECT COUNT(*) FROM analytics_outbox"))
    events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events"))
    aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates"))
    state = mysql_query(f"SELECT status,state_reason FROM analytics_workspace_state WHERE workspace_id='{WORKSPACE}'").split("\t")
    runbook = ROOT / "docs" / "operations" / "analytics-recovery.md"
    assert outbox == events == aggregate and state[0] == "complete"
    assert runbook.is_file() and "analyticsreconciler" in runbook.read_text(encoding="utf-8")
    combined_logs = recovery_logs + worker_logs + closure_logs
    assert "example.com" not in combined_logs and "destination" not in combined_logs.lower()
    return evidence("P07-T017", {"recovered_event_id":event_id,"outbox_republished":True,"worker_consumed":event_id in worker_logs or worker_logs == "already-consumed","final_outbox":outbox,"final_events":events,"final_aggregate":aggregate,"workspace_state":state[0],"workspace_state_reason":state[1],"runbook_present":True,"logs_destination_free":True})


CASES: dict[str, Callable[[], dict[str, Any]]] = {
    "P07-T001": case_t001, "P07-T002": case_t002, "P07-T003": case_t003, "P07-T004": case_t004,
    "P07-T005": case_t005, "P07-T006": case_t006, "P07-T007": case_t007, "P07-T008": case_t008,
    "P07-T009": case_t009, "P07-T010": case_t010, "P07-T011": case_t011, "P07-T012": case_t012,
    "P07-T013": case_t013, "P07-T014": case_t014, "P07-T015": case_t015, "P07-T016": case_t016,
    "P07-T017": case_t017,
}


def run_case(case_id: str) -> None:
    try:
        data = CASES[case_id]()
    except Exception as exc:
        data = {"case_id":case_id,"status":"FAIL","implementation_commit":commit_sha(),"observed":{},"errors":[f"{type(exc).__name__}: {exc}"]}
        write_result(case_id, data)
        raise
    write_result(case_id, data)
    print(f"{case_id}: PASS")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["all", *CASES])
    args = parser.parse_args()
    selected = list(CASES) if args.case == "all" else [args.case]
    for case_id in selected:
        run_case(case_id)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
