"""Diagnose the production OAuth boundary; never claim formal T025 coverage."""
import json
import secrets

from common import HEAD, ROOT, emit, fail_if_errors
from t022_case import http_json


def run_case():
    errors = []
    details = {'formal_p20_t025_claim': False, 'next_case_unlocked': False,
               'provider_exchange_verified': False, 'secret_material_recorded': False,
               'probe_scope': 'Unknown-state rejection before provider exchange',
               'callbacks': []}
    try:
        prior = json.loads((ROOT / 'artifacts/v10/P20/integration/P20-T024.json').read_text())
        assert prior['implementation_commit'] == HEAD and prior['status'] == 'PASS'
        assert prior['errors'] == [] and prior['details']['real_p15_session'] is True
        status, _, listing = http_json('GET', '/api/public/auth/providers')
        assert status == 200
        providers = listing['providers']
        assert {p['provider'] for p in providers} == {'google', 'facebook', 'github', 'qq', 'wechat', 'rainbow'}
        for provider in providers:
            name = provider['provider']
            # Unissued random state cannot authorize a provider exchange or login.
            state = 'gos_' + secrets.token_urlsafe(24)
            code = secrets.token_urlsafe(24)
            status, headers, response = http_json(
                'GET', f'/api/public/auth/{name}/callback?state={state}&code={code}')
            serialized = json.dumps(response)
            assert state not in serialized and code not in serialized
            assert not any(k.lower() == 'set-cookie' for k, _ in headers)
            details['callbacks'].append({'provider': name, 'enabled': provider['enabled'],
                                         'unknown_state_http_status': status})
            if status == 503:
                errors.append(name + ': production callback unavailable before state validation')
            elif status != 400:
                errors.append(name + ': unexpected unknown-state response')
        details['unknown_state_matrix_completed'] = True
    except Exception as exc:
        errors.append('T025 availability probe failed: ' + type(exc).__name__)
    return emit('P20-T025-availability', 'integration',
                'Production OAuth callback boundary diagnostic (partial T025)', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
