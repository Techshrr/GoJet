#!/usr/bin/env python3
"""T027 admission: declarations of desired coverage are never executed evidence."""
import hashlib
import json
from pathlib import Path


def inspect_native(root: Path, head: str):
    from t027_native import PACKAGE, validate_native
    directory = root / 'artifacts/v10/P20/runtime/t027'
    raw = (directory / 'workspace-rbac.jsonl').read_bytes()
    manifest = json.loads((directory / 'native-manifest.json').read_text())
    if manifest.get('implementation_commit') != head:
        raise ValueError('native evidence belongs to another commit')
    digest = 'sha256:' + hashlib.sha256(raw).hexdigest()
    if manifest.get('files', {}).get('workspace-rbac.jsonl') != digest:
        raise ValueError('native evidence digest mismatch')
    verified = validate_native(raw, head, PACKAGE)
    return {'required_test_count': verified['required_test_count'], 'jsonl_sha256': digest}


def run_case():
    from common import ROOT, HEAD, emit
    errors = []
    details = {'formal_p20_t027_claim': False, 'next_case_unlocked': False,
               'static_matrix_is_not_runtime_evidence': True}
    try:
        details['native'] = inspect_native(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, ImportError) as error:
        errors.append('T027 native runtime evidence unavailable or invalid: ' + str(error))
    # No admitted production-session browser producer exists yet. Do not turn a
    # coverage declaration, file existence or arbitrary booleans into authority.
    errors.append('T027 production-session UI/Admin evidence is not yet admitted; formal closure blocked')
    return emit('P20-T027', 'consistency', 'Cross-resource tenant and RBAC consistency', errors, details)


if __name__ == '__main__':
    from common import fail_if_errors
    fail_if_errors([run_case()])
