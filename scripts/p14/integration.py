#!/usr/bin/env python3
from __future__ import annotations

import argparse

from integration_common import run_case
from integration_cases_001_007 import CASES as CASES_001_007
from integration_cases_008_013 import CASES as CASES_008_013
from integration_cases_014_018 import CASES as CASES_014_018
from integration_cases_019_021 import CASES as CASES_019_021

CASES = {}
for group in (CASES_001_007, CASES_008_013, CASES_014_018, CASES_019_021):
    overlap = set(CASES).intersection(group)
    if overlap:
        raise RuntimeError(f"duplicate P14 cases: {sorted(overlap)}")
    CASES.update(group)

EXPECTED = {f"P14-T{index:03d}" for index in range(1, 22)}
if set(CASES) != EXPECTED:
    missing = sorted(EXPECTED - set(CASES))
    extra = sorted(set(CASES) - EXPECTED)
    raise RuntimeError(f"P14 real-integration case map mismatch missing={missing} extra={extra}")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CASES))
    args = parser.parse_args()
    run_case(args.case, CASES[args.case])


if __name__ == "__main__":
    main()
