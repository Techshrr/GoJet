"""Admit native HTTP boundary evidence; not a complete T030 closure."""
import argparse
import hashlib
import json
import re
from pathlib import Path

PACKAGE = 'github.com/Techshrr/GoJet/services/platformapi/cmd/server'
REQUIRED = {
    'TestP20HTTPStatusExpiryContexts', 'TestP20HTTPStatusDurableCSRF',
    *('TestP20HTTPStatusExpiryContexts/' + name for name in
      ['csrf-expired', 'grant-expired', 'grant-revoked', 'forbidden', 'unauthenticated', 'rate-limited']),
    *('TestP20HTTPStatusDurableCSRF/' + name for name in
      ['expired-csrf', 'missing-csrf', 'invalid-csrf', 'invalid-origin', 'missing-session', 'admin-oauth-expired', 'admin-trust-expired', 'refresh-and-retry', 'database-unavailable']),
}


def inspect(raw, head):
    if not re.fullmatch(r'[0-9a-f]{40}', head):
        raise ValueError('full exact head required')
    rows = [json.loads(line) for line in raw.splitlines()]
    if not rows or any(r.get('Action') in ('fail', 'skip') for r in rows):
        raise ValueError('empty, failed or skipped HTTP status evidence')
    passed = [r.get('Test') for r in rows if r.get('Action') == 'pass' and r.get('Package') == PACKAGE]
    if any(passed.count(name) != 1 for name in REQUIRED | {None}):
        raise ValueError('native HTTP status coverage incomplete or repeated')
    return {'implementation_commit': head, 'formal_p20_t030_claim': False,
            'scope': 'Account/Admin CSRF HTTP mapping and real durable account expiry/recovery',
            'passed_tests': sorted(REQUIRED),
            'files': {'http-status.jsonl': 'sha256:' + hashlib.sha256(raw).hexdigest()}}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--head', required=True)
    args = parser.parse_args()
    result = inspect((args.root / 'http-status.jsonl').read_bytes(), args.head)
    (args.root / 'native-manifest.json').write_text(json.dumps(result, indent=2) + '\n')
