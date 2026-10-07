"""Frozen T034 driver: native visual evidence plus recursively admitted authority."""
import argparse
import subprocess
from t028_sources import require
from t034_matrix import inspect


def admit(root, head):
    details = inspect(root, head)
    require(details['surfaces'] == {name: 4 for name in (
        'website', 'docs', 'auth', 'auth-invalid', 'auth-code-sent', 'auth-verified',
        'workspace', 'public', 'admin')} and details['observations'] == 36,
        'incomplete representative native state matrix')
    details['formal_p20_t034_claim'] = True
    details['next_case_unlocked'] = False
    details['scope'] = 'six native product surfaces; input/error/code-sent/verified auth states; separate P03 component authority'
    return details


def run_case():
    from common import ROOT, HEAD, emit
    errors = []
    details = {'formal_p20_t034_claim': False, 'next_case_unlocked': False}
    try:
        details = admit(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        errors.append('T034 admission failed: ' + str(error))
    return emit('P20-T034', 'browser', 'Release-wide Design System and visual consistency', errors, details)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=['T034'], default='T034')
    parser.parse_args()
    raise SystemExit(run_case()['status'] != 'PASS')
