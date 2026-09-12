#!/usr/bin/env python3
"""Production-session management regression; never a full T024 claim."""
import json
import os

from common import emit, fail_if_errors
from t022_case import HEAD, http_json, predecessor, session_cookie, scalar, mysql, q


def verify_management(workspace, user, auth_headers):
    """Exercise ordinary key management without delivering to external hosts."""
    origin = os.environ.get('GOJET_AUTH_ALLOWED_ORIGIN', 'http://localhost:4185')
    path = f'/api/workspaces/{workspace}/api-keys'
    payload = {'name': 'P20 D017 regression', 'scopes': ['links.read'], 'rate_limit_per_minute': 10}

    def check(value):
        if not value:
            raise ValueError('management assertion failed')

    def csrf_headers():
        status, _, me = http_json('GET', '/api/me', headers=auth_headers)
        check(status == 200 and bool(me.get('csrf_token')))
        return {**auth_headers, 'Origin': origin, 'X-CSRF-Token': me['csrf_token']}

    for route in ('api-keys', 'webhooks'):
        status, _, _ = http_json('GET', f'/api/workspaces/{workspace}/{route}')
        check(status == 401)
        status, _, _ = http_json('POST', f'/api/workspaces/{workspace}/{route}', {},
                                 {**auth_headers, 'Origin': origin})
        check(status == 403)

    once = csrf_headers()
    status, _, created = http_json('POST', path, payload, once)
    check(status == 201)
    key = created['key']; secret = created['secret']; key_id = key['id']
    check(bool(secret) and key['workspace_id'] == workspace and key['created_by'] == user
          and key['scopes'] == ['links.read'])
    count = lambda: int(scalar(f'SELECT COUNT(*) FROM workspace_api_keys WHERE workspace_id={q(workspace)}'))
    before = count()
    status, _, _ = http_json('POST', path, payload, once)
    check(status == 403 and count() == before)
    # The same replay namespace protects the neighboring developer surface.
    status, _, _ = http_json('POST', f'/api/workspaces/{workspace}/webhooks', {}, once)
    check(status == 403)

    status, _, page = http_json('GET', path, headers=auth_headers)
    check(status == 200 and secret not in json.dumps(page) and 'secret' not in page)
    status, _, rotated = http_json('POST', path + '/' + key_id + '/rotate', {}, csrf_headers())
    check(status == 200 and rotated['key']['id'] == key_id and rotated['secret'] != secret)
    new_secret = rotated['secret']
    status, _, revoked = http_json('POST', path + '/' + key_id + '/revoke', {}, csrf_headers())
    check(status == 200 and revoked['key']['status'] == 'revoked')
    status, _, page = http_json('GET', path, headers=auth_headers)
    check(status == 200 and secret not in json.dumps(page) and new_secret not in json.dumps(page))
    audit = mysql('SELECT action,actor_id,result,metadata_json FROM workspace_audit_events '
                  f'WHERE workspace_id={q(workspace)} AND resource_id={q(key_id)} '
                  "AND action IN ('api_key.create','api_key.rotate','api_key.revoke') ORDER BY id")
    rows = audit.splitlines()
    check(len(rows) == 3 and all(user in row and '\tsuccess\t' in row for row in rows))
    check(secret not in audit and new_secret not in audit)
    check(scalar(f'SELECT status FROM workspace_api_keys WHERE id={q(key_id)} AND workspace_id={q(workspace)}') == 'revoked')
    return {'management_regression_passed': True, 'api_key_create_http_status': 201,
            'api_key_rotation_http_status': 200, 'api_key_revocation_http_status': 200,
            'unauthenticated_http_status': 401, 'missing_csrf_http_status': 403,
            'csrf_replay_http_status': 403, 'cross_surface_csrf_replay_http_status': 403,
            'secret_once_verified': True, 'correlated_audit_count': 3,
            'audit_secret_safe': True, 'api_key_final_status': 'revoked',
            'outbound_delivery_attempted': False}


def run_case():
    details = {
        'formal_p20_t024_claim': False,
        'availability_only': True,
        'management_regression_passed': False,
        'mock_authority': False,
        'test_header_authority': False,
        'secret_material_recorded': False,
        'next_case_unlocked': False,
    }
    errors = []
    try:
        previous = predecessor('P20-T023')['details']
        if previous.get('formal_p20_t023_claim') is not True or previous.get('next_case') != 'P20-T024':
            raise ValueError('T023 authority missing')
        if os.environ.get('GOJET_TEST_AUTH_ENABLED') != '0':
            raise ValueError('production authentication required')
        user, workspace = previous['user_id'], previous['workspace_id']
        status, headers, body = http_json('POST', '/api/auth/login', {
            'email': 'p20-t009-' + HEAD[:12].lower() + '@example.test',
            'password': os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],
            'correlation_id': 'p20-t024-availability-' + HEAD[:12],
        })
        cookie = session_cookie(headers)
        if status != 200 or body.get('status') != 'authenticated' or not cookie:
            raise ValueError('real login unavailable')
        auth_headers = {'Cookie': '__Host-gojet_session=' + cookie}
        status, _, me = http_json('GET', '/api/me', headers=auth_headers)
        if status != 200 or me.get('user', {}).get('id') != user:
            raise ValueError('real session identity mismatch')
        details.update(real_p15_session=True, t023_evidence_bound=True,
                       user_id=user, workspace_id=workspace)
        for route, field in [('api-keys', 'api_keys'), ('webhooks', 'webhooks')]:
            status, _, result = http_json('GET', f'/api/workspaces/{workspace}/{route}', headers=auth_headers)
            details[field + '_http_status'] = status
            # Store only a known diagnostic, never arbitrary response data.
            unavailable = result.get('error', {}).get('code') == 'auth_dependency_unavailable'
            details[field + '_auth_dependency_unavailable'] = unavailable
            if status != 200:
                errors.append(f'production {field} list expected HTTP 200, observed {status}')
        if not errors:
            details.update(verify_management(workspace, user, auth_headers))
    except Exception as exc:
        errors.append('T024 availability gate error: ' + type(exc).__name__)
    return emit('P20-T024', 'integration', 'Production-session developer API availability (partial)', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
