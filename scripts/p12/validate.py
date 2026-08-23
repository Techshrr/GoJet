#!/usr/bin/env python3
from __future__ import annotations

import argparse

def main() -> int:
    parser = argparse.ArgumentParser(description='GoJet V10 P12 evidence validator')
    parser.add_argument('--case', required=True)
    parser.add_argument('--closure', action='store_true')
    args = parser.parse_args()
    if args.case == 'P12-T024' and not args.closure:
        from validate_coherence import run
        return run()
    if args.case == 'P12-T025' and args.closure:
        raise SystemExit('P12-T025 closure validator is not installed until T024 is authoritative')
    raise SystemExit(f'unsupported P12 validation case/mode: {args.case} closure={args.closure}')

if __name__ == '__main__':
    raise SystemExit(main())
