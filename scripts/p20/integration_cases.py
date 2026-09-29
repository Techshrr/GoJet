"""P20 integrated authority binding; upstream protocol fixtures follow P15.

This does not certify live provider credentials or a deployed production site.
No fixture substitutes for the correlated P20 P0 runtime.
"""
import argparse
import json

from common import HEAD, ROOT, emit, fail_if_errors, sha256_file

RUNTIME = 'artifacts/v10/P20/runtime/t025/'
CONTRACTS = {
    'P15': '9ba89a42281709087b40cdcf0cb2eebd54952a99',
    'P16': '43c5d4d7e1833c593ceacb48016abac6e3133893',
    'P17': '30174f40df28678360f644b8fed79736906b0ea0',
}
GO_TESTS = {
    'oauth-browser-start.jsonl': {'TestOAuthBrowserStartPersistence', 'TestOAuthBrowserBindingLifecycle', 'TestOAuthBrowserRejectsInvalidAuthority'},
    'auth-turnstile-state.jsonl': {'TestAuthChallengeMutationOrdering', 'TestAuthChallengeAdministratorConfigAndRedis', 'TestAuthChallengeAdministratorConfigAndRedis/OfficialCloudflareRegistration'},
    'optional-provider-state.jsonl': {'TestHTTPProviderAdapterOptionalDirect', 'TestHTTPProviderAdapterOptionalDirectDenials', 'TestHTTPProviderAdapterOptionalDurableState'},
    'rainbow-protocol-state.jsonl': {'TestHTTPProviderAdapterRainbowProtocol', 'TestHTTPProviderAdapterRainbowRejectsUnsafeResponses', 'TestHTTPProviderAdapterRainbowDurableState'},
    'provider-protocol.jsonl': {'TestHTTPProviderAdapterGoogleProtocol', 'TestHTTPProviderAdapterGitHubProtocol', 'TestHTTPProviderAdapterQQProtocol', 'TestHTTPProviderAdapterWeChatProtocol', 'TestHTTPProviderAdapterFacebookProtocol', 'TestHTTPProviderAdapterGoogleOneTap'},
}


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def check_go_events(events, required):
    """Reject empty, skipped, failed or incomplete go test streams."""
    passed = set()
    package_pass = False
    for event in events:
        require(event.get('Action') not in ('skip', 'fail'), 'native test skipped or failed')
        if event.get('Action') == 'pass':
            if event.get('Test'):
                passed.add(event['Test'])
            else:
                package_pass = True
    require(package_pass and required <= passed, 'required native tests did not finish')
    return sorted(passed)


def t025():
    errors, refs = [], []
    details = {'formal_p20_t025_claim': False, 'next_case_unlocked': False,
               'promotion_requires_same_head_gates': True,
               'live_provider_credentials_verified': False,
               'production_deployment_verified': False,
               'provider_protocol_authority': 'P15 frozen deterministic protocol fixtures with real MySQL state/handoff and Redis authority',
               'p0_mock_substitution': False, 'secret_material_recorded': False}

    def read(path):
        file = ROOT / path
        data = json.loads(file.read_text())
        refs.append({'path': path, 'sha256': sha256_file(file)})
        return data

    def p20(case, folder):
        data = read(f'artifacts/v10/P20/{folder}/{case}.json')
        require(data.get('case') == case and data.get('implementation_commit') == HEAD
                and data.get('status') == 'PASS' and data.get('errors') == [], 'P20 predecessor not exact-head PASS: ' + case)
        return data['details']

    try:
        prior = p20('P20-T024', 'integration')
        require(prior.get('formal_p20_t024_claim') is True, 'T024 is not formal')
        support = p20('P20-T020', 'p0')
        require(all(support.get(k) is True for k in ('real_session_authenticated', 'p14_turnstile_proven', 'p14_real_integration_bound')), 'real support authority missing')
        diagnostic = p20('P20-T025-availability', 'integration')
        require(diagnostic.get('unknown_state_matrix_completed') is True, 'unknown-state matrix missing')
        require(len(diagnostic.get('callbacks', [])) == 8 and all(c['unknown_state_http_status'] == 400 for c in diagnostic['callbacks']), 'unknown-state rejection incomplete')

        paths = [(f'P15-T{n:03d}', 'oauth') for n in range(15, 21)] + [('P16-T019', 'abuse'), ('P17-T018', 'platform')]
        for case, folder in paths:
            node = case[:3]
            data = read(f'artifacts/v10/{node}/{folder}/{case}.json')
            require(data.get('node') == node and data.get('case') == case and data.get('exact_head') == HEAD
                    and data.get('status') == 'PASS' and data.get('contract_authority') == CONTRACTS[node], 'predecessor identity or status mismatch: ' + case)
            require(data.get('checks') and all(v is True for v in data['checks'].values()), 'predecessor checks incomplete: ' + case)
            require(data.get('evidence_policy') and all(v is False for v in data['evidence_policy'].values()), 'predecessor redaction failed: ' + case)
            require(data.get('environment', {}).get('mysql_version'), 'real database evidence missing: ' + case)

        for case, folder in [('P14-T011', 'security'), ('P14-T013', 'api')]:
            data = read(f'artifacts/v10/P14/{folder}/{case}.json')
            require(data.get('case_id') == case and data.get('implementation_commit') == HEAD
                    and data.get('status') == 'PASS' and data.get('errors') == [], 'P14 authority missing: ' + case)
            c = data['checks']
            if case == 'P14-T011':
                require(c.get('invalid_status') == 400 and c.get('valid_status') == 201 and c.get('replay_status') == 400
                        and c.get('ticket_rows_after_replay') == 1 and c.get('raw_token_in_redis_key') is False, 'ticket Turnstile authority incomplete')
            else:
                require(c.get('persistent_success') is True and c.get('public_contact_rows') == 1
                        and c.get('response_contains_email') is False and c.get('idempotent_replay_created') is False, 'contact authority incomplete')

        admin = read(RUNTIME + 'oauth-admin-governance.json')
        require(admin.get('source_sha') == HEAD and admin.get('passed') is True and admin.get('checks')
                and all(v is True for v in admin['checks'].values()), 'OAuth administrator authority incomplete')
        manifest = read(RUNTIME + 'native-manifest.json')
        require(manifest.get('implementation_commit') == HEAD, 'native manifest SHA mismatch')
        native = {}
        for filename, required in GO_TESTS.items():
            path = ROOT / RUNTIME / filename
            digest = sha256_file(path)
            require(manifest.get('files', {}).get(filename) == digest, 'native evidence digest mismatch: ' + filename)
            native[filename] = check_go_events([json.loads(line) for line in path.read_text().splitlines() if line.strip()], required)
            refs.append({'path': RUNTIME + filename, 'sha256': digest})
        details.update(formal_p20_t025_claim=True, native_tests=native,
                       oauth_state_pkce_handoff_bind_unbind_verified=True,
                       turnstile_auth_support_contact_abuse_verified=True,
                       live_cloudflare_official_test_registration_verified=True)
    except (OSError, ValueError, KeyError, TypeError) as exc:
        # Never serialize provider responses, tokens or arbitrary upstream errors.
        errors.append('T025 evidence binding failed: ' + type(exc).__name__)
    details['evidence'] = refs
    return emit('P20-T025', 'integration', 'OAuth and Turnstile integrated protection matrix', errors, details)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--case', required=True, choices=['P20-T024', 'P20-T025'])
    args = parser.parse_args()
    if args.case == 'P20-T024':
        from t024_formal import run_case
        fail_if_errors([run_case()])
    else:
        fail_if_errors([t025()])
