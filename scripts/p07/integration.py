#!/usr/bin/env python3
"""GoJet V10 P07 real Redis/MySQL analytics integration driver."""

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
from urllib.parse import urlsplit

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
WORKSPACE = "ws-p07"
ACTOR = "p07-test-actor"
STREAM = "gojet:analytics:clicks:v1"
GROUP = "gojet-analytics-workers-v1"


def commit_sha() -> str:
    value = os.getenv("GITHUB_SHA", "").strip()
    if value:
        return value
    proc = subprocess.run(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True)
    return proc.stdout.strip()


def run(argv: list[str], *, env: dict[str, str] | None = None, timeout: int = 30, expect: int = 0) -> subprocess.CompletedProcess[str]:
    merged = os.environ.copy()
    if env:
        merged.update(env)
    proc = subprocess.run(argv, cwd=ROOT, text=True, capture_output=True, env=merged, timeout=timeout)
    if proc.returncode != expect:
        raise AssertionError(
            f"command failed {proc.returncode}: {' '.join(argv)}\nstdout={proc.stdout}\nstderr={proc.stderr}"
        )
    return proc


def mysql_args() -> list[str]:
    return [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT),
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE,
    ]


def mysql_query(query: str) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    proc = subprocess.run(mysql_args() + ["-e", query], cwd=ROOT, text=True, capture_output=True, env=env)
    if proc.returncode != 0:
        raise AssertionError(f"mysql failed: {query}\nstdout={proc.stdout}\nstderr={proc.stderr}")
    return proc.stdout.strip()


def mysql_scalar(query: str) -> str:
    return mysql_query(query).splitlines()[0].strip() if mysql_query(query).strip() else ""


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


def api_create_link(code: str, destination: str) -> dict[str, Any]:
    payload = {
        "hostname": "go.p07.test",
        "domain_kind": "official",
        "code": code,
        "title": f"P07 {code}",
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
    headers = {
        "X-GoJet-Test-Actor": ACTOR,
        "X-GoJet-Test-Workspace-Role": "owner",
        "X-GoJet-Test-Workspace": WORKSPACE,
        "X-Request-ID": f"p07-create-{code}",
    }
    status, _, raw = http_request(PLATFORM_URL, "POST", f"/api/workspaces/{WORKSPACE}/links", payload=payload, headers=headers)
    if status != 201:
        raise AssertionError(f"create link status={status} body={raw.decode(errors='replace')}")
    return json.loads(raw)


def set_risk_allow(link: dict[str, Any]) -> None:
    now = dt.datetime.now(dt.timezone.utc)
    payload = json.dumps({
        "schema_version": 1,
        "decision": "allow",
        "fingerprint": link["risk_fingerprint"],
        "checked_at": (now - dt.timedelta(seconds=1)).isoformat().replace("+00:00", "Z"),
        "valid_until": (now + dt.timedelta(minutes=5)).isoformat().replace("+00:00", "Z"),
        "policy_version": "p07-integration-v1",
    }, separators=(",", ":"))
    redis_cli("SET", f"risk:link:{link['id']}:{link['risk_fingerprint']}", payload, "EX", "300")


def redirect(link: dict[str, Any], *, country: str = "sg", user_agent: str = "GoJet-P07-Mobile/1.0 Mobile") -> tuple[int, dict[str, str], str]:
    headers = {
        "Host": link["hostname"],
        "User-Agent": user_agent,
        "Accept-Language": "en-SG,en;q=0.9",
        "Referer": "https://source.p07.test/article",
        "X-GoJet-Test-Country": country,
    }
    status, response_headers, raw = http_request(REDIRECT_URL, "GET", f"/{link['code']}", headers=headers)
    return status, response_headers, raw.decode(errors="replace")


def deterministic_event_id(link_id: int, click_sequence: int) -> str:
    identity = f"gojet.analytics.click.v1\n{WORKSPACE}\n{link_id}\n{click_sequence}".encode()
    return hashlib.sha256(identity).hexdigest()


def link_by_code(code: str) -> dict[str, Any] | None:
    raw = mysql_query(
        "SELECT id,workspace_id,hostname,code,risk_fingerprint,click_count "
        f"FROM links WHERE workspace_id='{WORKSPACE}' AND code='{code}' LIMIT 1"
    )
    if not raw:
        return None
    row = raw.split("\t")
    return {
        "id": int(row[0]), "workspace_id": row[1], "hostname": row[2], "code": row[3],
        "risk_fingerprint": row[4], "click_count": int(row[5]),
    }


def ensure_redirect_event(code: str, destination: str) -> tuple[dict[str, Any], str]:
    link = link_by_code(code)
    if link is None:
        link = api_create_link(code, destination)
    expected_id = deterministic_event_id(int(link["id"]), 1)
    exists = mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE event_id='{expected_id}'")
    if exists == "0":
        set_risk_allow(link)
        status, headers, _ = redirect(link)
        assert status == 302, (status, headers)
        assert headers.get("location") == destination, headers
    return link, expected_id


def worker_once(consumer: str, count: int = 1) -> str:
    env = {
        "GOJET_ANALYTICS_WORKER_CONSUMER": consumer,
        "GOJET_ANALYTICS_WORKER_MAX_MESSAGES": str(count),
    }
    proc = run([WORKER_BIN], env=env, timeout=30)
    return proc.stdout + proc.stderr


def evidence(case_id: str, observed: dict[str, Any]) -> dict[str, Any]:
    return {
        "case_id": case_id,
        "status": "PASS",
        "implementation_commit": commit_sha(),
        "environment": {
            "mysql": f"{MYSQL_HOST}:{MYSQL_PORT}/{MYSQL_DATABASE}",
            "redis": f"{REDIS_HOST}:{REDIS_PORT}",
            "stream": STREAM,
            "worker_binary": WORKER_BIN,
        },
        "observed": observed,
        "errors": [],
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
    assert len(row) == 8, row
    assert row[0] == event_id and row[1] == "1"
    assert row[2] == "sg" and row[3] == "mobile" and row[4] == "en-sg" and row[5] == "source.p07.test"
    assert row[6] == "1"
    payload = json.loads(row[7])
    assert payload["event_id"] == event_id
    assert payload["link_id"] == link["id"]
    assert "destination" not in row[7].lower()
    assert "example.com" not in row[7].lower()
    stream_dump = redis_cli("XRANGE", STREAM, "-", "+")
    assert event_id in stream_dump
    assert destination not in stream_dump
    click_count = mysql_scalar(f"SELECT click_count FROM links WHERE id={link['id']}")
    assert click_count == "1"
    return evidence("P07-T001", {
        "link_id": link["id"],
        "event_id": event_id,
        "click_sequence": 1,
        "redirect_status": 302,
        "redirect_location": destination,
        "outbox_published": True,
        "stream_contains_event": True,
        "destination_absent_from_event": True,
        "dimensions": {"country_code": row[2], "device": row[3], "language": row[4], "source_hostname": row[5]},
    })


def case_t002() -> dict[str, Any]:
    _, event_id = ensure_redirect_event("p07t001", "https://example.com/p07/t001?kept=1")
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'") == "0":
        logs = worker_once("p07-worker-t002", 1)
    else:
        logs = "already-consumed"
    event_count = mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'")
    aggregate = mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'")
    assert event_count == "1"
    assert aggregate == "1"
    stream_id = mysql_scalar(f"SELECT stream_id FROM analytics_events WHERE event_id='{event_id}'")
    assert stream_id
    return evidence("P07-T002", {
        "event_id": event_id,
        "event_rows": int(event_count),
        "aggregate_clicks": int(aggregate),
        "stream_id": stream_id,
        "worker_log_has_event": event_id in logs or logs == "already-consumed",
    })


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
    assert after_events == before_events
    assert after_aggregate == before_aggregate
    return evidence("P07-T003", {
        "event_id": event_id,
        "duplicate_stream_id": duplicate_stream_id,
        "event_rows_before": before_events,
        "event_rows_after": after_events,
        "aggregate_before": before_aggregate,
        "aggregate_after": after_aggregate,
        "logical_once": True,
        "worker_acknowledged_duplicate": event_id in logs,
    })


def case_t004() -> dict[str, Any]:
    case_t002()
    destination = "https://example.com/p07/t004"
    link = link_by_code("p07t004")
    if link is None:
        link = api_create_link("p07t004", destination)
    event_id = deterministic_event_id(int(link["id"]), 1)
    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_outbox WHERE event_id='{event_id}'") == "0":
        set_risk_allow(link)
        status, headers, _ = redirect(link, country="us", user_agent="GoJet-P07-Restart/1.0")
        assert status == 302 and headers.get("location") == destination

    if mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'") == "0":
        pending = redis_cli("XREADGROUP", "GROUP", GROUP, "p07-restart", "COUNT", "1", "STREAMS", STREAM, ">")
        assert event_id in pending, pending
        pending_before = redis_cli("XPENDING", STREAM, GROUP).splitlines()[0]
        assert int(pending_before) >= 1
        logs = worker_once("p07-restart", 1)
    else:
        pending_before = "0"
        logs = "already-recovered"
    event_rows = int(mysql_scalar(f"SELECT COUNT(*) FROM analytics_events WHERE event_id='{event_id}'"))
    click_sequence = int(mysql_scalar(f"SELECT click_sequence FROM analytics_events WHERE event_id='{event_id}'"))
    pending_after = int(redis_cli("XPENDING", STREAM, GROUP).splitlines()[0])
    aggregate = int(mysql_scalar("SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates WHERE workspace_id='ws-p07'"))
    source_events = int(mysql_scalar("SELECT COUNT(*) FROM analytics_events WHERE workspace_id='ws-p07'"))
    assert event_rows == 1 and click_sequence == 1
    assert pending_after == 0
    assert aggregate == source_events
    return evidence("P07-T004", {
        "event_id": event_id,
        "pending_before_restart": int(pending_before),
        "pending_after_restart": pending_after,
        "event_rows": event_rows,
        "aggregate_clicks": aggregate,
        "authoritative_event_rows": source_events,
        "no_loss": True,
        "no_double_count": True,
        "worker_recovered_pending": event_id in logs or logs == "already-recovered",
    })


CASES: dict[str, Callable[[], dict[str, Any]]] = {
    "P07-T001": case_t001,
    "P07-T002": case_t002,
    "P07-T003": case_t003,
    "P07-T004": case_t004,
}


def run_case(case_id: str) -> None:
    try:
        data = CASES[case_id]()
    except Exception as exc:
        data = {
            "case_id": case_id,
            "status": "FAIL",
            "implementation_commit": commit_sha(),
            "observed": {},
            "errors": [f"{type(exc).__name__}: {exc}"],
        }
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
