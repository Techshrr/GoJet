#!/usr/bin/env python3
"""Frozen P08 validator entrypoint for P08-T015 coherence and P08-T016 closure."""

from __future__ import annotations

import argparse
import sys

from validate_closure import run_closure
from validate_coherence import main as run_coherence


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--case", required=True, choices=["P08-T015", "P08-T016"])
    parser.add_argument("--closure", action="store_true")
    args = parser.parse_args()

    if args.case == "P08-T015":
        if args.closure:
            parser.error("--closure is only valid with P08-T016")
        sys.argv = [sys.argv[0], "--case", "P08-T015"]
        return run_coherence()

    return run_closure(args.closure)


if __name__ == "__main__":
    raise SystemExit(main())
