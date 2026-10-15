"""T028 authentication section backed by native and real browser evidence."""
import hashlib
import json
from pathlib import Path

AUTHORITY = '9ba89a42281709087b40cdcf0cb2eebd54952a99'
REQUIRED = {'P15-T004': {'checks': ['client_identity_is_not_login_input',
                         'forged_session_token_is_rejected',
                         'locked_account_fails_without_session',
                         'password_is_kdf_hash_only_at_rest',
                         'pending_account_requires_verification',
                         'session_identity_is_server_resolved',
                         'session_secrets_are_hash_only_at_rest',
                         'successful_login_establishes_server_session',
                         'successful_login_is_audited',
                         'successful_login_resets_failure_state',
                         'unknown_account_is_generic_unauthorized',
                         'wrong_password_fails_without_session'],
              'folder': 'api',
              'redaction': ['client_asserted_identity_present',
                            'raw_csrf_secret_present',
                            'raw_grant_key_present',
                            'raw_password_present',
                            'raw_session_token_present',
                            'raw_verification_code_present']},
 'P15-T007': {'checks': ['expired_reset_does_not_change_password',
                         'expired_reset_fails_closed',
                         'new_password_establishes_fresh_session',
                         'old_password_is_invalid_after_reset',
                         'reset_revokes_existing_sessions_server_side',
                         'reset_token_reuse_fails_closed',
                         'successful_reset_is_audited',
                         'valid_reset_consumes_grant_once'],
              'folder': 'security',
              'redaction': ['raw_grant_key_present',
                            'raw_new_password_present',
                            'raw_old_password_present',
                            'raw_reset_token_present',
                            'raw_session_token_present']},
 'P15-T008': {'checks': ['authenticated_response_is_no_store',
                         'authenticated_response_is_noindex',
                         'durable_session_authority_exists',
                         'frontend_has_no_formal_auth_token_storage',
                         'server_side_session_resolution_succeeds',
                         'session_cookie_has_host_prefix_constraints',
                         'session_cookie_is_secure_http_only',
                         'session_cookie_uses_host_prefix',
                         'session_cookie_uses_reviewed_samesite_policy'],
              'folder': 'security',
              'redaction': ['frontend_secret_present',
                            'local_storage_auth_token_present',
                            'raw_csrf_secret_present',
                            'raw_session_token_present']},
 'P15-T009': {'checks': ['expired_csrf_fails_closed',
                         'invalid_csrf_fails_closed',
                         'missing_csrf_fails_closed',
                         'rejected_requests_do_not_mutate_state',
                         'replayed_csrf_fails_closed',
                         'valid_csrf_allows_authorized_mutation'],
              'folder': 'security',
              'redaction': ['raw_csrf_secret_present',
                            'raw_csrf_token_present',
                            'raw_replay_key_material_present',
                            'raw_session_token_present']},
 'P15-T011': {'checks': ['expired_turnstile_fails_closed',
                         'invalid_turnstile_fails_closed',
                         'missing_turnstile_fails_closed',
                         'provider_failure_fails_closed',
                         'redis_replay_authority_uses_digest_keys_only',
                         'rejected_turnstile_never_mutates_state',
                         'replayed_turnstile_fails_closed',
                         'valid_turnstile_allows_one_protected_mutation'],
              'folder': 'security',
              'redaction': ['production_bypass_present',
                            'raw_session_token_present',
                            'raw_turnstile_replay_key_present',
                            'raw_turnstile_token_present']}}


def inspect(root: Path, head: str):
    refs = []
    def require(ok, reason):
        if not ok:
            raise ValueError(reason)
    def read(folder, case, native=False):
        path = root / 'artifacts/v10/P15' / folder / (case + '.json')
        raw = path.read_bytes()
        d = json.loads(raw)
        require(d.get('case' if native else 'case_id') == case
                and d.get('exact_head' if native else 'implementation_commit') == head
                and d.get('status') == 'PASS' and d.get('errors', []) == [],
                'auth evidence identity, SHA or verdict mismatch')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return d
    coherence = read('results', 'P15-T028')['observations']
    require(coherence.get('input_evidence_count') == 27 and all(coherence.get(k) is True
            for k in ['same_exact_head', 'producer_coherent', 'secret_safe',
                      'mixed_head_rejected', 'unsafe_evidence_rejected']), 'auth coherence missing')
    for case, spec in REQUIRED.items():
        d = read(spec['folder'], case, True)
        require(d.get('contract_authority') == AUTHORITY and d.get('node') == 'P15', 'auth authority drift')
        checks = d.get('checks', {})
        require(set(spec['checks']) <= checks.keys() and all(v is True for v in checks.values()),
                'auth denial checks incomplete')
        policy = d.get('evidence_policy', {})
        require(set(spec['redaction']) <= policy.keys() and all(v is False for v in policy.values()),
                'auth secrets or bypass evidence unsafe')
        require(d.get('environment', {}).get('mysql_version'), 'real auth database missing')
        if case in ('P15-T009', 'P15-T011'):
            require(d.get('record_counts', {}).get('authorized_mutations') == 1,
                    'rejected auth request mutated durable state')
    routes = read('browser', 'P15-T024')['details']
    account = read('browser', 'P15-T025')['details']
    for d in [routes, account]:
        require(d.get('real_local_api') is True and d.get('security')
                and all(v is True for v in d['security'].values()), 'browser authority unsafe')
    expected = {'login': {'invalid', 'success'}, 'verify': {'reused-token', 'success'},
                'reset': {'invalid-token', 'success'}, 'oauth': {'state-error'},
                'social': {'expired-handoff'}}
    for surface, states in expected.items():
        require(states <= set(routes.get('states', {}).get(surface, [])), 'auth browser denial state missing')
    require(all(account.get(k) is True for k in ['direct_load_authorization',
            'revoked_session_rejected_server_side', 'revoked_session_recovery', 'durable_session_revoke']),
            'revoked session browser/API parity missing')
    require('session-revoked' in account.get('states', {}).get('profile', []),
            'account page hides revoked-session state')
    return {'section': 'auth-session-csrf-turnstile', 'source_evidence': refs,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'scope': 'P15 native denial and real auth/account browser; cross-surface Turnstile and entitlement remain separately required'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 authentication safety evidence admission failed')
    fail_if_errors([emit('P20-T028-auth', 'consistency',
                         'Authentication safety section (not whole T028 acceptance)', errors, details)])
