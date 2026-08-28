#!/usr/bin/env python3
from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path

CASE_RE = re.compile(r"P17-T(\d{3})\Z")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="GoJet V10 P17 exact-case evidence router")
    parser.add_argument("--case", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    match = CASE_RE.fullmatch(args.case)
    if match is None:
        raise SystemExit(f"invalid P17 case id: {args.case}")
    number = int(match.group(1))
    if 1 <= number <= 21:
        target = "integration_t001_t021.py"
    elif 22 <= number <= 24:
        target = "api_key_integration.py"
    else:
        raise SystemExit(f"P17 integration case is not implemented at this stage: {args.case}")
    script = Path(__file__).resolve().with_name(target)
    proc = subprocess.run([sys.executable, str(script), "--case", args.case], check=False)
    return proc.returncode


if __name__ == "__main__":
    raise SystemExit(main())
