#!/usr/bin/env python3
"""P09 evidence validator entrypoint for T026 coherence and T027 closure."""

from __future__ import annotations

import argparse

from validate_closure import run_closure
from validate_coherence import run as run_coherence


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P09-T026", "P09-T027"])
    parser.add_argument("--closure", action="store_true")
    args = parser.parse_args()
    if args.case == "P09-T026":
        if args.closure:
            parser.error("--closure is only valid with P09-T027")
        return run_coherence()
    return run_closure(args.closure)


if __name__ == "__main__":
    raise SystemExit(main())
