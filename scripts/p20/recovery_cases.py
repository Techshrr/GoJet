"""Formal recovery admission; unimplemented cases cannot be reported as PASS."""
import argparse
from common import fail_if_errors

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=['T039','T040','T041','T042','T043','T044','T045','T046'], required=True)
    args = parser.parse_args()
    from importlib import import_module
    run_case = import_module(args.case.lower()+'_case').run_case
    fail_if_errors([run_case()])
