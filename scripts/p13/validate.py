#!/usr/bin/env python3
from __future__ import annotations

import argparse


def main() -> int:
    parser = argparse.ArgumentParser(description='GoJet V10 P13 evidence validator')
    parser.add_argument('--case', required=True)
    parser.add_argument('--closure', action='store_true')
    args = parser.parse_args()
    if args.case == 'P13-T026' and not args.closure:
        from validate_coherence import run
        return run()
    if args.case == 'P13-T027' and args.closure:
        raise SystemExit('P13-T027 closure validator is not installed until accountable review/signing stage')
    raise SystemExit(f'unsupported P13 validator invocation: case={args.case} closure={args.closure}')


if __name__ == '__main__':
    raise SystemExit(main())
