#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import subprocess
from typing import Any

from integration_common import *

def sql_now_minus(seconds: int) -> str:
    return f"(UTC_TIMESTAMP(6) - INTERVAL {int(seconds)} SECOND)"

def seed_grant(workspace: str, capability: str, source_type: str, source_id: str, limit: int,
               *, starts_seconds_ago: int = 60, ends_seconds_from_now: int | None = None,
               revoked: bool = False, provenance: dict[str, Any] | None = None) -> int:
    starts = sql_now_minus(starts_seconds_ago)
    ends = "NULL" if ends_seconds_from_now is None else f"(UTC_TIMESTAMP(6) + INTERVAL {int(ends_seconds_from_now)} SECOND)"
    revoked_at = "UTC_TIMESTAMP(6)" if revoked else "NULL"
    provenance = provenance or {"fixture": "p13-real-integration", "source_type": source_type, "source_id": source_id}
    mysql(
        "INSERT INTO entitlement_grants "
        "(workspace_id,capability,source_type,source_id,limit_value,starts_at,ends_at,revoked_at,provenance_json) VALUES ("
        + ",".join([
            sql_quote(workspace), sql_quote(capability), sql_quote(source_type), sql_quote(source_id),
            str(int(limit)), starts, ends, revoked_at, f"CAST({sql_quote(json.dumps(provenance, separators=(',', ':')))} AS JSON)"
        ]) + ")"
    )
    return int(mysql_scalar(
        f"SELECT id FROM entitlement_grants WHERE workspace_id={sql_quote(workspace)} "
        f"AND capability={sql_quote(capability)} AND source_type={sql_quote(source_type)} "
        f"AND source_id={sql_quote(source_id)}"
    ))

def get_order(workspace: str, actor: str, email: str, order_id: str):
    return p13("GET", f"/api/workspaces/{workspace}/orders/{order_id}", actor, email)

def list_invoices(workspace: str, actor: str, email: str):
    return p13("GET", f"/api/workspaces/{workspace}/invoices", actor, email)

def list_payments(workspace: str, actor: str, email: str):
    return p13("GET", f"/api/workspaces/{workspace}/payments", actor, email)

def refund_order(case_id: str, order: dict[str, Any], paid_payload: dict[str, Any], *, provider: str = "stripe"):
    payload = callback_payload(
        order,
        event_id=unique(f"{case_id}-refund-event"),
        transaction_id=paid_payload["transaction_id"],
        outcome="refunded",
        event_type="payment.refunded",
    )
    result = send_callback(provider, payload)
    expect(result[0] == 200 and result[3].get("ok") is True, f"refund callback status={result[0]} body={result[2][:300]!r}")
    return payload, result

def run_internal_expiry_sweep(horizon_hours: int = 192) -> dict[str, Any]:
    cmd = [
        os.environ.get("GOJET_TEST_P13_PRODUCER", "/tmp/gojet-p13-producer"),
        "--action", "entitlement-expiring",
        "--horizon-hours", str(int(horizon_hours)),
    ]
    completed = subprocess.run(cmd, cwd=ROOT, text=True, capture_output=True, env=os.environ.copy())
    expect(completed.returncode == 0, f"expiry producer failed stdout={completed.stdout!r} stderr={completed.stderr!r}")
    try:
        data = json.loads(completed.stdout)
    except json.JSONDecodeError as exc:
        raise AssertionError(f"expiry producer non-JSON {completed.stdout!r}") from exc
    return data
