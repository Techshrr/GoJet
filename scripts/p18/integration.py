#!/usr/bin/env python3
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CORE = ROOT / "scripts/p18/core_cases.py"
DISCOVERY = ROOT / "scripts/p18/discovery_cases.py"
QUALITY = ROOT / "scripts/p18/quality_cases.py"
CORE_CASES = {f"P18-T{n:03d}" for n in range(1, 8)}
DISCOVERY_CASES = {f"P18-T{n:03d}" for n in range(8, 19) if n != 11}
QUALITY_CASES = {f"P18-T{n:03d}" for n in range(22, 25)}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CORE_CASES | DISCOVERY_CASES | QUALITY_CASES))
    args = parser.parse_args()
    if args.case in CORE_CASES:
        target = CORE
    elif args.case in DISCOVERY_CASES:
        target = DISCOVERY
    else:
        target = QUALITY
    return subprocess.call([sys.executable, str(target), "--case", args.case], cwd=ROOT)


if __name__ == "__main__":
    raise SystemExit(main())
