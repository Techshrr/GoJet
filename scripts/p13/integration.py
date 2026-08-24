#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
from typing import Any

from integration_common import *
from integration_cases_001_005 import *
from integration_cases_006_010 import *
from integration_cases_011_015 import *
from integration_cases_016_018 import *
from integration_cases_019_020 import *

SUPPORTED = {f"P13-T{i:03d}" for i in range(1, 21)}

CASE_DIRS = {
    3: ENTITLEMENT_DIR, 4: RBAC_DIR, 7: SECURITY_DIR, 8: SECURITY_DIR,
    10: ENTITLEMENT_DIR, 11: ENTITLEMENT_DIR, 12: ENTITLEMENT_DIR,
    14: ENTITLEMENT_DIR, 15: ENTITLEMENT_DIR, 16: ENTITLEMENT_DIR,
    17: ENTITLEMENT_DIR, 18: ENTITLEMENT_DIR, 19: AUDIT_DIR,
}

def directory_for(case_id: str):
    return CASE_DIRS.get(int(case_id[-3:]), API_DIR)

CASES = {
    "P13-T001": case_001, "P13-T002": case_002, "P13-T003": case_003, "P13-T004": case_004,
    "P13-T005": case_005, "P13-T006": case_006, "P13-T007": case_007, "P13-T008": case_008,
    "P13-T009": case_009, "P13-T010": case_010, "P13-T011": case_011, "P13-T012": case_012,
    "P13-T013": case_013, "P13-T014": case_014, "P13-T015": case_015, "P13-T016": case_016,
    "P13-T017": case_017, "P13-T018": case_018, "P13-T019": case_019, "P13-T020": case_020,
}

def run_case(case_id: str) -> int:
    if case_id not in SUPPORTED or case_id not in CASES:
        raise SystemExit(f"unsupported case {case_id}")
    observations: dict[str, Any] = {}
    errors: list[str] = []
    try:
        observations = CASES[case_id]()
    except Exception as exc:
        errors.append(f"{type(exc).__name__}: {exc}")
    path = record(case_id, observations, errors, directory_for(case_id))
    print(json.dumps({
        "case_id": case_id,
        "implementation_commit": HEAD,
        "status": "PASS" if not errors else "FAIL",
        "errors": errors,
        "evidence": str(path.relative_to(ROOT)),
    }, sort_keys=True))
    return 0 if not errors else 1

def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True)
    args = parser.parse_args()
    return run_case(args.case)

if __name__ == "__main__":
    raise SystemExit(main())
