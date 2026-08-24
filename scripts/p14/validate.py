#!/usr/bin/env python3
from __future__ import annotations

import argparse


def main() -> int:
    parser = argparse.ArgumentParser(description='GoJet V10 P14 evidence validator')
    parser.add_argument('--case', required=True)
    parser.add_argument('--closure', action='store_true')
    args = parser.parse_args()
    if args.case == 'P14-T024' and not args.closure:
        from validate_coherence import run
        return run()
    if args.case == 'P14-T025' and args.closure:
        try:
            from validate_closure import run_closure
        except ModuleNotFoundError as exc:
            raise SystemExit('P14-T025 closure validator is not present on this pre-sign implementation head') from exc
        return run_closure(args.closure)
    raise SystemExit(f'unsupported P14 validator invocation: case={args.case} closure={args.closure}')


if __name__ == '__main__':
    raise SystemExit(main())
