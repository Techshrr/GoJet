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


BROWSER_CHECKS = {'owner', 'admin', 'member', 'viewer', 'anonymous'} | {
    'admin-route-' + role for role in ('anonymous', 'owner', 'admin', 'member', 'viewer', 'limited-admin')}


def inspect_browser(root: Path, head: str):
    from t027_native import PACKAGE
    directory = root / 'artifacts/v10/P20/runtime/t027'
    path = directory / 'workspace-browser.json'
    raw = path.read_bytes()
    result = json.loads(raw)
    if result.get('implementation_commit') != head:
        raise ValueError('browser evidence belongs to another commit')
    if result.get('production_session') is not True or result.get('mocked_api') is not False:
        raise ValueError('production browser authority missing')
    checks = result.get('checks', {})
    if set(checks) != BROWSER_CHECKS or any(value is not True for value in checks.values()):
        raise ValueError('incomplete browser permission matrix')
    log = (directory / 'workspace-browser.jsonl').read_bytes()
    rows = [json.loads(line) for line in log.splitlines()]
    if any(row.get('Action') in ('fail', 'skip') for row in rows):
        raise ValueError('failed or skipped browser test')
    for test in ('TestP20WorkspaceProductionBrowser', None):
        if sum(row.get('Action') == 'pass' and row.get('Package') == PACKAGE
               and row.get('Test') == test for row in rows) != 1:
            raise ValueError('browser or package verdict missing/ambiguous')
    return {'checks': sorted(checks),
            'result_sha256': 'sha256:' + hashlib.sha256(raw).hexdigest(),
            'jsonl_sha256': 'sha256:' + hashlib.sha256(log).hexdigest()}


def run_case():
    from common import ROOT, HEAD, emit
    errors = []
    details = {'formal_p20_t027_claim': False, 'next_case_unlocked': False,
               'promotion_requires_same_head_gates': True,
               'mock_authority': False, 'static_matrix_is_not_runtime_evidence': True}
    for name, inspect in [('native', inspect_native), ('browser', inspect_browser)]:
        try:
            details[name] = inspect(ROOT, HEAD)
        except (OSError, ValueError, KeyError, TypeError, ImportError) as error:
            errors.append('T027 ' + name + ' evidence invalid: ' + str(error))
    details['formal_p20_t027_claim'] = not errors
    return emit('P20-T027', 'consistency', 'Cross-resource tenant and RBAC consistency', errors, details)


if __name__ == '__main__':
    from common import fail_if_errors
    fail_if_errors([run_case()])
