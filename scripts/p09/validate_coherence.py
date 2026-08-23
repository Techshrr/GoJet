#!/usr/bin/env python3
from __future__ import annotations

import json

from coherence_common import *
from coherence_contract import load_cases, validate_plan, validate_producers
from coherence_authority import validate_authority, validate_fail_closed, validate_health, validate_storage_scan
from coherence_browser import validate_browser, validate_captures

def run() -> int:
    errors: list[str] = []
    exact = head()
    RESULTS.mkdir(parents=True, exist_ok=True)
    validate_plan(errors)
    producer_info = validate_producers(exact, errors)
    cases, entries = load_cases(exact, errors)
    scan = validate_storage_scan(cases, errors)
    validate_fail_closed(cases, errors)
    validate_authority(cases, errors)
    validate_health(cases, errors)
    browser_info = validate_browser(cases, errors)
    captures = validate_captures(browser_info, errors)
    index = {
        "node": "P09",
        "generated_at": now(),
        "implementation_commit": exact,
        "input_evidence_count": len(entries),
        "producer_manifest_sha256": digest(PRODUCERS) if PRODUCERS.is_file() else None,
        "contract_sha256": digest(CONTRACT) if CONTRACT.is_file() else None,
        "cases": entries,
        "captures": captures,
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    required = producer_info.get("manifest", {}).get("required_workflows", {}) if isinstance(producer_info.get("manifest"), dict) else {}
    payload = {
        "case": "P09-T026",
        "status": "PASS" if not errors else "FAIL",
        "implementation_commit": exact,
        "errors": errors,
        "observations": {
            "input_evidence_count": len(entries),
            "same_exact_head": all(item.get("implementation_commit") == exact for item in entries),
            "producer_run_ids": {name: required.get(name, {}).get("run_id") for name in REQUIRED_PRODUCERS} if isinstance(required, dict) else {},
            "capture_count": len(captures),
            "evidence_index_sha256": digest(INDEX),
            "producer_manifest_sha256": digest(PRODUCERS) if PRODUCERS.is_file() else None,
            "contract_sha256": digest(CONTRACT) if CONTRACT.is_file() else None,
            "clamav_engine_version": scan["clean"].get("engine_version"),
            "clamav_signature_version": scan["clean"].get("signature_version"),
            "authority": "real storage+MySQL+native ClamAV/API+route-backed browser evidence only; no mock/manual/UI-derived safety authority",
        },
    }
    T026.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    if errors:
        for error in errors:
            print(error)
        return 1
    print(f"P09-T026 exact-head coherence PASS on {exact}")
    return 0


if __name__ == "__main__":
    raise SystemExit(run())
