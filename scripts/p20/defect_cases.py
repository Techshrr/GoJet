#!/usr/bin/env python3
from __future__ import annotations

import json

from common import HEAD, ROOT, emit, fail_if_errors, sha256_file


def main() -> int:
    errors: list[str] = []
    path = ROOT / "artifacts/v10/P20/defect-ledger.json"
    if not path.is_file():
        errors.append("tracked P20 defect ledger missing")
        ledger = {}
    else:
        ledger = json.loads(path.read_text(encoding="utf-8"))

    if ledger.get("schema") != "gojet.p20-defect-ledger.v1":
        errors.append("P20 defect ledger schema drift")
    if ledger.get("node") != "P20" or ledger.get("status") != "ACTIVE":
        errors.append("P20 defect ledger node/status drift")
    open_rows = ledger.get("open", []) if isinstance(ledger.get("open", []), list) else []
    decision_rows = ledger.get("decision_required", []) if isinstance(ledger.get("decision_required", []), list) else []
    p0 = [row for row in open_rows if str(row.get("severity", "")).upper() == "P0"]
    p1 = [row for row in open_rows if str(row.get("severity", "")).upper() == "P1"]
    lower_without_disposition = [
        row for row in open_rows
        if str(row.get("severity", "")).upper() not in {"P0", "P1"}
        and not str(row.get("disposition", "")).strip()
    ]
    hard_failure_downgrades = [row for row in open_rows if row.get("gate_hard_failure") is True and str(row.get("severity", "")).upper() not in {"P0", "P1"}]
    if p0:
        errors.append(f"open P0 defects: {[row.get('id') for row in p0]}")
    if p1:
        errors.append(f"open P1 defects: {[row.get('id') for row in p1]}")
    if decision_rows:
        errors.append(f"open DECISION REQUIRED rows: {[row.get('id') for row in decision_rows]}")
    if lower_without_disposition:
        errors.append(f"lower-severity defects missing disposition: {[row.get('id') for row in lower_without_disposition]}")
    if hard_failure_downgrades:
        errors.append(f"Gate hard failures cannot be downgraded: {[row.get('id') for row in hard_failure_downgrades]}")

    rules = ledger.get("rules", {})
    if rules != {
        "p0_open_max": 0,
        "p1_open_max": 0,
        "decision_required_max": 0,
        "lower_severity_requires_disposition": True,
        "gate_hard_failure_cannot_be_downgraded": True,
    }:
        errors.append("P20 defect-ledger closure rules drift")

    payload = emit(
        "P20-T008",
        "defects",
        "Defect and decision closure ledger",
        errors,
        {
            "ledger_path": path.relative_to(ROOT).as_posix(),
            "ledger_sha256": sha256_file(path) if path.is_file() else None,
            "p0_open": len(p0),
            "p1_open": len(p1),
            "decision_required": len(decision_rows),
            "lower_severity_open": len([row for row in open_rows if str(row.get("severity", "")).upper() not in {"P0", "P1"}]),
            "closed_count": len(ledger.get("closed", [])) if isinstance(ledger.get("closed", []), list) else None,
            "candidate_commit": HEAD,
        },
    )
    fail_if_errors([payload])
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
