"""Formal recovery admission; unimplemented cases cannot be reported as PASS."""
import argparse
from common import fail_if_errors

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=['T039'], required=True)
    args = parser.parse_args()
    from t039_case import run_case
    fail_if_errors([run_case()])
