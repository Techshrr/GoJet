"""Real HTTP scope, rate and wall-clock expiry checks; not full T024."""
import datetime
import os
import time

from t022_case import http_json, scalar, q


def verify_key_lifecycle(workspace, auth_headers):
    def check(value):
        if not value:
            raise ValueError('API key lifecycle assertion failed')

    key_path = f'/api/workspaces/{workspace}/api-keys'
    link_path = f'/api/workspaces/{workspace}/links'

    def headers():
        status, _, me = http_json('GET', '/api/me', headers=auth_headers)
        check(status == 200 and bool(me.get('csrf_token')))
        return {**auth_headers, 'Origin': os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],
                'X-CSRF-Token': me['csrf_token']}

    def create(name, limit, expires=None):
        payload = {'name': name, 'scopes': ['links:read'], 'rate_limit_per_minute': limit}
        if expires:
            payload['expires_at'] = expires
        status, _, body = http_json('POST', key_path, payload, headers())
        check(status == 201 and bool(body.get('secret')))
        return body['key']['id'], {'Authorization': 'Bearer ' + body['secret']}

    def revoke(key_id):
        status, _, _ = http_json('POST', key_path + '/' + key_id + '/revoke', {}, headers())
        check(status == 200)

    key_id, credential = create('P20 scope verification', 10)
    try:
        before = int(scalar(f'SELECT COUNT(*) FROM links WHERE workspace_id={q(workspace)}'))
        status, _, _ = http_json('POST', link_path, {}, credential)
        check(status == 403)
        check(int(scalar(f'SELECT COUNT(*) FROM links WHERE workspace_id={q(workspace)}')) == before)
    finally:
        revoke(key_id)

    rate_verified = False
    for attempt in range(2):
        key_id, credential = create('P20 rate verification', 1)
        try:
            window = int(time.time()) // 60
            first, _, _ = http_json('GET', link_path, headers=credential)
            second, _, _ = http_json('GET', link_path, headers=credential)
            if int(time.time()) // 60 == window:
                check(first == 200 and second == 429)
                rate_verified = True
                break
            # Retry with a fresh key only if the real minute boundary crossed.
        finally:
            revoke(key_id)
    check(rate_verified)

    expiry = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=8)
    key_id, credential = create('P20 expiry verification', 10, expiry.isoformat())
    try:
        status, _, _ = http_json('GET', link_path, headers=credential)
        check(status == 200)
        time.sleep(max(0, expiry.timestamp() - time.time()) + 0.5)
        status, _, _ = http_json('GET', link_path, headers=credential)
        check(status == 401)
    finally:
        revoke(key_id)
    return {'real_api_key_scope_verified': True, 'api_key_scope_denied_http_status': 403,
            'real_api_key_rate_verified': True, 'api_key_rate_limited_http_status': 429,
            'real_api_key_expiry_verified': True, 'expired_key_http_status': 401}
