#!/usr/bin/env python3
"""Read-only production-session availability gate; never a full T024 claim."""
import os

from common import emit, fail_if_errors
from t022_case import HEAD, http_json, predecessor, session_cookie


def run_case():
    details = {
        'formal_p20_t024_claim': False,
        'availability_only': True,
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
    except Exception as exc:
        errors.append('T024 availability gate error: ' + type(exc).__name__)
    return emit('P20-T024', 'integration', 'Production-session developer API availability (partial)', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
