"""Validate the complete native RBAC matrix; does not claim UI/formal closure."""
import argparse
import hashlib
import json
import re
from pathlib import Path

PACKAGE = 'github.com/Techshrr/GoJet/services/platformapi/cmd/server'
WORKSPACE = 'TestP20WorkspaceRBACDurableSessions'
ADMIN = 'TestP20AdministratorPermissionBoundary'


def required_tests():
    roles = ('owner', 'admin', 'member', 'viewer', 'outsider')
    names = {WORKSPACE, ADMIN}
    def add(name):
        names.add(WORKSPACE + '/' + name)
    for resource in ('organization', 'members', 'campaigns', 'tags', 'folders',
                     'notifications', 'text-shares', 'bio-pages', 'links', 'domains', 'qr-codes', 'files'):
        add('read-' + resource)
    for role in roles:
        add('organization-write-' + role)
        for resource in ('campaigns', 'tags', 'folders'):
            add('create-' + resource + '-' + role)
        for resource in ('text-shares', 'bio-pages'):
            add('product-create-' + resource + '-' + role)
    for resource in ('links', 'domains', 'qr-codes', 'files'):
        for role in ('viewer', 'outsider'):
            add('deny-create-' + resource + '-' + role)
    for role in (*roles, 'anonymous'):
        add('billing-summary-' + role)
        add('support-list-' + role)
    for identity in ('anonymous', 'workspace', 'limited-admin'):
        for resource in ('overview', 'administrators', 'users', 'workspaces'):
            names.add(ADMIN + '/' + identity + '/api/admin/' + resource)
    return names


def validate_native(raw, head, package):
    if not re.fullmatch(r'[0-9a-f]{40}', head):
        raise ValueError('full implementation SHA required')
    rows = [json.loads(line) for line in raw.splitlines()]
    if not rows or any(row.get('Action') in ('fail', 'skip') for row in rows):
        raise ValueError('empty, failed or skipped native evidence')
    passed = [row.get('Test') for row in rows
              if row.get('Action') == 'pass' and row.get('Package') == package]
    missing = required_tests() - set(passed)
    if missing:
        raise ValueError('missing native coverage: ' + ', '.join(sorted(missing)))
    if passed.count(None) != 1 or any(passed.count(name) != 1 for name in required_tests()):
        raise ValueError('missing package verdict or ambiguous repeated test verdict')
    return {'implementation_commit': head, 'formal_p20_t027_claim': False,
            'scope': 'Durable production-session Workspace resource and administrator API matrix; UI evidence required separately',
            'required_test_count': len(required_tests()),
            'passed_required_tests': sorted(required_tests()),
            'files': {'workspace-rbac.jsonl': 'sha256:' + hashlib.sha256(raw).hexdigest()}}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--head', required=True)
    parser.add_argument('--package', required=True)
    args = parser.parse_args()
    manifest = validate_native((args.root / 'workspace-rbac.jsonl').read_bytes(), args.head, args.package)
    (args.root / 'native-manifest.json').write_text(json.dumps(manifest, sort_keys=True, indent=2) + '\n')
