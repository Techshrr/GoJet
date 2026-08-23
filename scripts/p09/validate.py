#!/usr/bin/env python3
"""P09 evidence validator entrypoint. T027 closure is added only after T026 is green."""

from __future__ import annotations

import argparse

from validate_coherence import run as run_coherence


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P09-T026"])
    args = parser.parse_args()
    if args.case == "P09-T026":
        return run_coherence()
    raise AssertionError(args.case)


if __name__ == "__main__":
    raise SystemExit(main())
