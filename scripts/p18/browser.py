#!/usr/bin/env python3
from __future__ import annotations

import argparse
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
BROWSER_CASES = ['P18-T011', 'P18-T019', 'P18-T020', 'P18-T021']


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=BROWSER_CASES)
    args = parser.parse_args()
    return subprocess.call(['node', 'scripts/p18/browser_cases.mjs', '--case', args.case], cwd=ROOT)


if __name__ == '__main__':
    raise SystemExit(main())
