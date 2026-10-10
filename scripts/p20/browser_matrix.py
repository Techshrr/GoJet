"""Frozen T034/T035 driver: native authority and independently bound review."""
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


def admit_t035(root, head, review=None):
    from t035_matrix import inspect as inspect_accessibility
    from t035_review import load_review, validate_review
    details = inspect_accessibility(root, head)
    details['review'] = validate_review(root, head, details, review if review is not None else load_review())
    details['formal_p20_t035_claim'] = True
    details['next_case_unlocked'] = False
    details['scope'] = 'nine representative native states; WCAG 2.2 A/AA review; sampled real Orca controls; no audible-playback certificate'
    return details


def run_case(case='T034'):
    from common import ROOT, HEAD, emit
    errors = []
    details = {('formal_p20_t034_claim' if case == 'T034' else 'formal_p20_t035_claim'): False, 'next_case_unlocked': False}
    try:
        details = admit(ROOT, HEAD) if case == 'T034' else admit_t035(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        errors.append(case + ' admission failed: ' + str(error))
    name = 'Release-wide Design System and visual consistency' if case == 'T034' else 'Release-wide accessibility and responsive matrix'
    return emit('P20-' + case, 'browser', name, errors, details)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', choices=['T034','T035'], default='T034')
    args = parser.parse_args()
    raise SystemExit(run_case(args.case)['status'] != 'PASS')
