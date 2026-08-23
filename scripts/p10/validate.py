#!/usr/bin/env python3
from __future__ import annotations

import argparse


def main() -> int:
    parser=argparse.ArgumentParser()
    parser.add_argument('--case',required=True)
    parser.add_argument('--closure',action='store_true')
    args=parser.parse_args()
    if args.case=='P10-T019' and not args.closure:
        from validate_coherence import run
        return run()
    raise SystemExit(f'unsupported P10 validation invocation: case={args.case} closure={args.closure}')

if __name__=='__main__': raise SystemExit(main())
