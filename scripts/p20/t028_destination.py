"""Executed destination-safety section of T028; never whole-case authority."""
import hashlib
import json
from pathlib import Path

AUTHORITY = '43c5d4d7e1833c593ceacb48016abac6e3133893'
PROVIDER = {
    'correlated_audit_covers_each_failure_lifecycle', 'failure_evidence_is_redacted_and_safe',
    'failure_observations_and_decisions_are_durable', 'failure_scans_close_with_explicit_authority',
    'malformed_payload_maps_to_unknown_non_allow', 'no_failure_mode_can_produce_allow',
    'partial_response_maps_to_unknown_non_allow', 'provider_503_maps_to_unavailable_non_allow',
    'timeout_maps_to_unavailable_non_allow', 'transport_error_maps_to_unavailable_non_allow',
}
REDIRECT = {
    'all_frozen_non_allow_states_fail_closed', 'block_fails_closed',
    'exact_current_allow_is_sole_redirect_authority', 'malformed_fails_closed',
    'missing_fails_closed', 'pending_is_not_projectable_or_redirectable',
    'provider_unavailable_is_non_allow', 'review_fails_closed', 'stale_fails_closed',
    'unknown_is_not_projectable_or_redirectable',
}
PUBLIC = {
    'link_reason_allowlist', 'unsafe_code_not_echoed', 'destination_target_not_disclosed',
    'no_continue_anyway_control', 'safety_no_store_noindex', 'abuse_no_store_noindex',
    'server_turnstile_enforced', 'rate_limit_enforced', 'persistent_success_receipt',
    'durable_intake_and_audit',
}


def require(value, reason):
    if not value:
        raise ValueError(reason)


def checks(data, required, label):
    require(isinstance(data, dict) and required <= data.keys()
            and all(v is True for v in data.values()), label + ' checks incomplete')


def inspect(root: Path, head: str):
    refs = []

    def read(folder, case, native=False):
        path = root / 'artifacts/v10/P16' / folder / (case + '.json')
        raw = path.read_bytes()
        data = json.loads(raw)
        require(data.get('case' if native else 'case_id') == case, 'case identity mismatch')
        require(data.get('exact_head' if native else 'implementation_commit') == head,
                'mixed-head destination evidence')
        require(data.get('status') == 'PASS' and data.get('errors', []) == [], 'upstream case failed')
        require(data.get('contract_authority') == AUTHORITY, 'destination contract drift')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return data

    coherence = read('results', 'P16-T028')['observations']
    require(coherence.get('same_exact_head') is True and coherence.get('secret_safe') is True
            and coherence.get('input_evidence_count') == 27, 'P16 coherence missing')
    provider = read('security', 'P16-T006', True)
    redirect = read('security', 'P16-T009', True)
    for data, required in [(provider, PROVIDER), (redirect, REDIRECT)]:
        checks(data.get('checks'), required, data['case'])
        require(data.get('environment', {}).get('mysql_version'), 'real database evidence missing')
        policy = data.get('evidence_policy', {})
        require({'dsn_present', 'raw_authorization_present', 'raw_provider_secret_present'} <= policy.keys()
                and all(v is False for v in policy.values()), 'native redaction unproven')
    counts = provider['record_counts']
    require(counts.get('failure_modes') == 5 and counts.get('allow_decisions') == 0
            and counts.get('completed_scans') == 5 and counts.get('provider_observations') == 5
            and counts.get('policy_decisions') == 5 and counts.get('secret_matches') == 0,
            'provider failure matrix incomplete')
    require(redirect['record_counts'].get('runtime_non_allow_states') == 8,
            'redirect non-allow matrix incomplete')
    browser = read('browser', 'P16-T027')['details']
    checks(browser.get('security_checks'), PUBLIC, 'public safety')
    require(browser.get('frozen_contract_completion') is True, 'public browser incomplete')
    states = browser.get('states', {})
    require({'pending', 'review', 'blocked', 'domain-suspended', 'domain-revoked',
             'domain-expired', 'operational-unavailable'} <= set(states.get('linkunavailable', [])),
            'public non-allow states incomplete')
    require({'validation-error', 'Turnstile-error', 'rate-limited', 'success-persistent'}
            <= set(states.get('abuse_report', [])), 'public intake states incomplete')
    return {'section': 'destination-provider-redirect-public', 'source_evidence': refs,
            'provider_failure_modes': 5, 'redirect_non_allow_states': 8,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'remaining_controls': ['domain', 'clamav', 'turnstile-cross-surface',
                                   'auth-session', 'entitlement']}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 destination section evidence admission failed')
    fail_if_errors([emit('P20-T028-destination', 'consistency',
                         'Destination safety section (not whole T028 acceptance)', errors, details)])
