#!/usr/bin/env python3
from __future__ import annotations

import argparse
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CORE = ROOT / "scripts/p18/core_cases.py"
DISCOVERY = ROOT / "scripts/p18/discovery_cases.py"
CORE_CASES = {f"P18-T{n:03d}" for n in range(1, 8)}
DISCOVERY_CASES = {f"P18-T{n:03d}" for n in range(8, 19) if n != 11}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=sorted(CORE_CASES | DISCOVERY_CASES))
    args = parser.parse_args()
    target = CORE if args.case in CORE_CASES else DISCOVERY
    return subprocess.call([sys.executable, str(target), "--case", args.case], cwd=ROOT)


if __name__ == "__main__":
    raise SystemExit(main())
