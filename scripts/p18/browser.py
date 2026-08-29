#!/usr/bin/env python3
from __future__ import annotations

import argparse
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['P18-T011'])
    args = parser.parse_args()
    return subprocess.call(['node', 'scripts/p18/browser_cases.mjs', '--case', args.case], cwd=ROOT)


if __name__ == '__main__':
    raise SystemExit(main())
