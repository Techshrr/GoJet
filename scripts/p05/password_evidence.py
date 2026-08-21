#!/usr/bin/env python3
"""P05 password-access evidence extension for P05-T010.

Runs only against the real MySQL/Redis/platformapi/redirectengine test stack.
It augments P05-T010 and marks that case FAIL if any password contract fails.
"""

from __future__ import annotations

import datetime as dt
import http.client
import json
import os
import subprocess
from pathlib import Path
from typing import Any
from urllib.parse import urlencode, urlsplit

ROOT = Path(__file__).resolve().parents[2]
RESULT = ROOT / "artifacts" / "v10" / "P05" / "results" / "P05-T010.json"
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
WORKSPACE = "ws-p05-password"
ACTOR = "p05-password-owner"
PASSWORD = "P05-Password-Contract-2026!"
WRONG_PASSWORD = "Wrong-P05-Password-2026!"
HOSTNAME = "password.p05.test"
CODE = "password-gate"
DESTINATION = "https://destination.p05.test/secret"


def exact_head() -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"], cwd=ROOT, text=True, capture_output=True, check=True
    )
    return result.stdout.strip()


def run(argv: list[str], *, input_text: str | None = None) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        argv, cwd=ROOT, text=True, input=input_text, capture_output=True, env=os.environ.copy()
    )
    if result.returncode != 0:
        raise AssertionError(
            f"command failed {result.returncode}: {' '.join(argv)}\nstdout={result.stdout}\nstderr={result.stderr}"
        )
    return result


def mysql(query: str | None = None, *, stdin: str | None = None) -> str:
    env = os.environ.copy()
    env["MYSQL_PWD"] = MYSQL_PASSWORD
    argv = [
        "mysql", "--protocol=tcp", "-h", MYSQL_HOST, "-P", str(MYSQL_PORT),
        "-u", MYSQL_USER, "--default-character-set=utf8mb4", "-N", "-B", MYSQL_DATABASE,
    ]
    if query is not None:
        argv += ["-e", query]
    result = subprocess.run(argv, cwd=ROOT, text=True, input=stdin, capture_output=True, env=env)
    if result.returncode != 0:
        raise AssertionError(f"mysql failed: {result.stderr}")
    return result.stdout.strip()


def redis(*args: str) -> str:
    return run([
        "redis-cli", "-h", REDIS_HOST, "-p", str(REDIS_PORT), "--raw", *args
    ]).stdout.strip()


def reset_schema() -> None:
    mysql(
        "SET FOREIGN_KEY_CHECKS=0; "
        "DROP TABLE IF EXISTS link_audit_events; "
        "DROP TABLE IF EXISTS link_versions; "
        "DROP TABLE IF EXISTS links; "
        "SET FOREIGN_KEY_CHECKS=1;"
    )
    redis("FLUSHDB")
    mysql(stdin=MIGRATION.read_text(encoding="utf-8"))


def request(
    base: str,
    method: str,
    path: str,
    *,
    headers: dict[str, str] | None = None,
    body: bytes | None = None,
) -> tuple[int, dict[str, str], bytes]:
    parsed = urlsplit(base)
    conn = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=15)
    conn.request(method, path, body=body, headers=headers or {})
    response = conn.getresponse()
    data = response.read()
    response_headers = {key.lower(): value for key, value in response.getheaders()}
    status = response.status
    conn.close()
    return status, response_headers, data


def auth_headers(correlation: str) -> dict[str, str]:
    return {
        "Accept": "application/json",
        "Content-Type": "application/json",
        "X-GoJet-Test-Actor": ACTOR,
        "X-GoJet-Test-Workspace-Role": "owner",
        "X-GoJet-Test-Workspace": WORKSPACE,
        "X-Request-ID": correlation,
    }


def api(method: str, path: str, payload: Any | None = None, *, correlation: str = "password-evidence") -> tuple[int, dict[str, str], Any, bytes]:
    body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
    status, headers, raw = request(
        PLATFORM_URL, method, path, headers=auth_headers(correlation), body=body
    )
    value: Any = raw.decode(errors="replace")
    if raw and "application/json" in headers.get("content-type", ""):
        value = json.loads(raw)
    return status, headers, value, raw


def redirect(method: str, *, password: str | None = None) -> tuple[int, dict[str, str], str]:
    headers = {
        "Host": HOSTNAME,
        "User-Agent": "GoJet-P05-Password-Evidence/1.0",
        "Accept-Language": "en-US,en;q=0.9",
    }
    body = None
    if password is not None:
        body = urlencode({"password": password}).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    status, response_headers, raw = request(
        REDIRECT_URL, method, f"/{CODE}", headers=headers, body=body
    )
    return status, response_headers, raw.decode(errors="replace")


def set_risk(link: dict[str, Any], decision: str) -> None:
    now = dt.datetime.now(dt.timezone.utc)
    payload = json.dumps(
        {
            "schema_version": 1,
            "decision": decision,
            "fingerprint": link["risk_fingerprint"],
            "checked_at": (now - dt.timedelta(seconds=1)).isoformat().replace("+00:00", "Z"),
            "valid_until": (now + dt.timedelta(minutes=5)).isoformat().replace("+00:00", "Z"),
            "policy_version": "p05-password-evidence-v1",
        },
        separators=(",", ":"),
    )
    redis("SET", f"risk:link:{link['id']}:{link['risk_fingerprint']}", payload, "EX", "300")


def base_update(link: dict[str, Any], access: dict[str, Any], reason: str) -> dict[str, Any]:
    return {
        "expected_version": link["version"],
        "hostname": link["hostname"],
        "domain_kind": link["domain_kind"],
        "code": link["code"],
        "title": link["title"],
        "primary_destination": link["primary_destination"],
        "redirect_status": link["redirect_status"],
        "status": "paused" if link["status"] == "paused" else "active",
        "routing": link.get("routing") or [],
        "ab": link.get("ab") or [],
        "utm": link.get("utm") or {},
        "access": access,
        "expires_at": link.get("expires_at"),
        "click_limit": link.get("click_limit"),
        "one_time": bool(link.get("one_time")),
        "change_reason": reason,
    }


def assert_no_target(headers: dict[str, str], body: str) -> None:
    assert "location" not in headers, headers
    assert DESTINATION not in body, body


def run_contract() -> dict[str, Any]:
    reset_schema()

    create_payload = {
        "hostname": HOSTNAME,
        "domain_kind": "official",
        "code": CODE,
        "title": "P05 password gate",
        "primary_destination": DESTINATION,
        "redirect_status": 302,
        "routing": [],
        "ab": [],
        "utm": {},
        "access": {"password": PASSWORD},
        "expires_at": None,
        "click_limit": None,
        "one_time": False,
        "change_reason": "create password-protected link",
    }
    status, headers, link, raw = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links", create_payload, correlation="password-create"
    )
    assert status == 201, (status, link)
    assert isinstance(link, dict)
    assert link["access"] == {"password_protected": True}, link["access"]
    serialized = raw.decode(errors="replace")
    assert "password_hash" not in serialized and PASSWORD not in serialized
    assert headers.get("cache-control") == "no-store"

    access_json = mysql(f"SELECT access_json FROM links WHERE id={int(link['id'])};")
    stored_access = json.loads(access_json)
    verifier = stored_access.get("password_hash", "")
    parts = verifier.split("$")
    assert len(parts) == 5 and parts[0] == "pbkdf2-sha256" and parts[1] == "1" and parts[2] == "600000"
    assert PASSWORD not in access_json

    snapshot_json = mysql(
        f"SELECT snapshot_json FROM link_versions WHERE link_id={int(link['id'])} AND version=1;"
    )
    internal_snapshot = json.loads(snapshot_json)
    assert internal_snapshot["access"]["password_hash"] == verifier
    assert PASSWORD not in snapshot_json

    # Risk must run before password. Even a correct password must not be parsed or
    # rate-counted while the exact-current risk decision is review.
    set_risk(link, "review")
    status, r_headers, body = redirect("POST", password=PASSWORD)
    assert status == 200 and "under review" in body.lower(), (status, body)
    assert_no_target(r_headers, body)
    assert redis("KEYS", f"access:password:{link['id']}:*") == ""

    set_risk(link, "allow")
    status, r_headers, body = redirect("GET")
    assert status == 200 and "password required" in body.lower(), (status, body)
    assert_no_target(r_headers, body)
    assert r_headers.get("cache-control") == "no-store"
    assert r_headers.get("referrer-policy") == "no-referrer"
    assert r_headers.get("x-robots-tag") == "noindex, nofollow"
    csp = r_headers.get("content-security-policy", "")
    assert "form-action 'self'" in csp and "default-src 'none'" in csp
    assert mysql(f"SELECT click_count FROM links WHERE id={int(link['id'])};") == "0"

    # Read APIs must expose only the boolean public state.
    status, _, fetched, fetched_raw = api(
        "GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", correlation="password-get"
    )
    assert status == 200 and fetched["access"] == {"password_protected": True}
    fetched_text = fetched_raw.decode(errors="replace")
    assert "password_hash" not in fetched_text and verifier not in fetched_text and PASSWORD not in fetched_text

    status, _, history, history_raw = api(
        "GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}/history", correlation="password-history"
    )
    assert status == 200 and history["items"][0]["snapshot"]["access"] == {"password_protected": True}
    history_text = history_raw.decode(errors="replace")
    assert "password_hash" not in history_text and verifier not in history_text and PASSWORD not in history_text

    # The mutation contract must reject verifier injection and read-only state echo.
    invalid = base_update(link, {"password_hash": "attacker-controlled"}, "reject verifier injection")
    status, _, value, _ = api(
        "PATCH", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", invalid, correlation="password-reject-hash"
    )
    assert status == 400 and value["error"]["code"] == "invalid_json", (status, value)
    state_echo = base_update(link, {"password_protected": True}, "reject state echo")
    status, _, value, _ = api(
        "PATCH", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", state_echo, correlation="password-reject-state"
    )
    assert status == 400 and value["error"]["code"] == "invalid_json", (status, value)

    status, r_headers, body = redirect("POST", password=WRONG_PASSWORD)
    assert status == 401 and "not accepted" in body.lower(), (status, body)
    assert_no_target(r_headers, body)
    attempt_keys = [key for key in redis("KEYS", f"access:password:{link['id']}:*").splitlines() if key]
    assert len(attempt_keys) == 1, attempt_keys
    assert redis("GET", attempt_keys[0]) == "1"
    redis("SET", attempt_keys[0], "10", "EX", "300")
    status, r_headers, body = redirect("POST", password=WRONG_PASSWORD)
    assert status == 429 and r_headers.get("retry-after") == "300", (status, r_headers)
    assert_no_target(r_headers, body)
    redis("DEL", attempt_keys[0])

    status, r_headers, body = redirect("POST", password=PASSWORD)
    assert status == 302 and r_headers.get("location") == DESTINATION, (status, r_headers, body)
    assert redis("KEYS", f"access:password:{link['id']}:*") == ""
    assert mysql(f"SELECT click_count FROM links WHERE id={int(link['id'])};") == "1"

    # Clearing the password is a normal optimistic-concurrency mutation and must
    # not change the reachable-target fingerprint.
    clear_payload = base_update(link, {"clear_password": True}, "clear password protection")
    status, _, cleared, cleared_raw = api(
        "PATCH", f"/api/workspaces/{WORKSPACE}/links/{link['id']}", clear_payload, correlation="password-clear"
    )
    assert status == 200 and cleared["version"] == 2, (status, cleared)
    assert cleared["access"] == {"password_protected": False}
    assert cleared["risk_fingerprint"] == link["risk_fingerprint"]
    assert "password_hash" not in cleared_raw.decode(errors="replace")
    assert json.loads(mysql(f"SELECT access_json FROM links WHERE id={int(link['id'])};")) == {}

    status, r_headers, body = redirect("GET")
    assert status == 302 and r_headers.get("location") == DESTINATION, (status, r_headers, body)
    assert mysql(f"SELECT click_count FROM links WHERE id={int(link['id'])};") == "2"

    # Restore version 1 must restore the internal verifier while the API remains
    # redacted. Restore preserves click_count and does not consume access on GET.
    restore_payload = {
        "expected_version": cleared["version"],
        "restore_version": 1,
        "change_reason": "restore password-protected version",
    }
    status, _, restored, restored_raw = api(
        "POST", f"/api/workspaces/{WORKSPACE}/links/{link['id']}/restore", restore_payload, correlation="password-restore"
    )
    assert status == 200 and restored["version"] == 3, (status, restored)
    assert restored["access"] == {"password_protected": True}
    assert restored["risk_fingerprint"] == link["risk_fingerprint"]
    restored_text = restored_raw.decode(errors="replace")
    assert "password_hash" not in restored_text and verifier not in restored_text and PASSWORD not in restored_text
    restored_access = json.loads(mysql(f"SELECT access_json FROM links WHERE id={int(link['id'])};"))
    assert restored_access.get("password_hash") == verifier
    assert mysql(f"SELECT click_count FROM links WHERE id={int(link['id'])};") == "2"

    status, r_headers, body = redirect("GET")
    assert status == 200 and "password required" in body.lower(), (status, body)
    assert_no_target(r_headers, body)
    assert mysql(f"SELECT click_count FROM links WHERE id={int(link['id'])};") == "2"

    status, _, history, history_raw = api(
        "GET", f"/api/workspaces/{WORKSPACE}/links/{link['id']}/history", correlation="password-history-final"
    )
    assert status == 200 and [item["version"] for item in history["items"]][:3] == [3, 2, 1]
    public_states = [item["snapshot"]["access"]["password_protected"] for item in history["items"][:3]]
    assert public_states == [True, False, True], public_states
    history_text = history_raw.decode(errors="replace")
    assert "password_hash" not in history_text and verifier not in history_text and PASSWORD not in history_text

    return {
        "implementation_commit": exact_head(),
        "hash_algorithm": parts[0],
        "hash_version": int(parts[1]),
        "pbkdf2_iterations": int(parts[2]),
        "verifier_exposed_by_api": False,
        "plaintext_persisted": False,
        "risk_precedes_password": True,
        "challenge_status": 200,
        "wrong_password_status": 401,
        "rate_limited_status": 429,
        "correct_password_status": 302,
        "password_attempt_limit": 10,
        "clear_preserved_fingerprint": True,
        "restore_password_states": public_states,
        "click_count_after_restore": 2,
    }


def update_result(status: str, details: dict[str, Any] | None = None, error: str | None = None) -> None:
    if RESULT.is_file():
        result = json.loads(RESULT.read_text(encoding="utf-8"))
    else:
        result = {
            "case_id": "P05-T010",
            "name": "redirect-ordering-access",
            "status": status,
            "errors": [],
            "details": {},
            "implementation_commit": exact_head(),
        }
    result["implementation_commit"] = exact_head()
    result.setdefault("details", {})["password_contract"] = details or {}
    errors = result.setdefault("errors", [])
    errors[:] = [item for item in errors if not str(item).startswith("password_contract:")]
    if error:
        errors.append(f"password_contract: {error}")
        result["status"] = "FAIL"
    elif result.get("status") != "FAIL":
        result["status"] = status
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")


def main() -> int:
    try:
        details = run_contract()
        update_result("PASS", details=details)
        print("P05-T010 password contract: PASS")
        return 0
    except Exception as exc:  # evidence must be written even on failure
        update_result("FAIL", error=f"{type(exc).__name__}: {exc}")
        print(f"P05-T010 password contract: FAIL: {type(exc).__name__}: {exc}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
