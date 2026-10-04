"""Bind executed domain/entitlement/redirect safety; not whole T028 proof."""
import hashlib
import json
from pathlib import Path


def require(value, reason):
    if not value:
        raise ValueError(reason)


def inspect(root: Path, head: str):
    refs = []

    def read(case):
        path = root / 'artifacts/v10/P06/results' / (case + '.json')
        raw = path.read_bytes()
        data = json.loads(raw)
        require(data.get('node') == 'P06' and data.get('case_id') == case,
                'domain evidence identity mismatch')
        require(data.get('implementation_commit') == head and data.get('status') == 'PASS'
                and data.get('errors') == [], 'domain evidence not exact-head PASS')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return data['details']

    risk = read('P06-T013')
    expected = ['missing', 'review', 'block', 'malformed', 'stale', 'allow', 'block', 'allow']
    require(risk.get('risk_sequence') == expected and risk.get('risk_evaluator_calls') == 8,
            'domain uncertainty sequence incomplete')
    ready = risk.get('ready_sequence', [])
    require(len(ready) == 8 and all(type(v) is bool for v in ready)
            and ready == [False, False, False, False, False, True, False, True],
            'uncertain domain became ready')
    require(risk.get('revalidation_results') == ['pending', 'fail', 'fail', 'fail', 'stale', 'pass', 'fail', 'pass'],
            'domain revalidation outcomes drifted')
    require(risk.get('ownership_status') == 'verified' and risk.get('ingress_status') == 'valid'
            and risk.get('https_status') == 'active', 'independent domain axes not ready')
    require(risk.get('audit_evidence_leak') is False and risk.get('domain_json_evidence_leak') is False
            and risk.get('provider_evidence_persisted_internal') is True, 'domain redaction incomplete')

    redirect = read('P06-T019')
    require(set(redirect.get('runtime_denied_authorities', [])) == {
        'routing_pending', 'ownership', 'ingress_dns', 'https', 'domain_risk', 'security',
        'entitlement_expired', 'grace_expired'}, 'domain/entitlement denial matrix incomplete')
    for key in ['destination_risk_block_fail_closed', 'final_claim_recheck_caught_raced_suspension',
                'normal_downgrade_grace_preserved_existing_routing', 'official_same_code_decoy_present',
                'raced_suspension_click_count_unchanged']:
        require(redirect.get(key) is True, 'redirect authority missing: ' + key)
    require(redirect.get('denied_responses_exposed_destination') is False
            and redirect.get('official_host_fallback_observed') is False, 'unsafe redirect fallback or leak')

    parity = read('P06-T020')
    require(set(parity.get('non_allow_states_fail_closed_on_both_hosts', [])) == {
        'missing', 'review', 'block', 'malformed', 'stale'}, 'official/custom non-allow parity incomplete')
    for key in ['custom_domain_authority_is_additional_gate', 'custom_domain_gate_did_not_change_fingerprint',
                'custom_path_fails_closed_when_domain_suspended',
                'official_path_continues_when_only_custom_domain_is_suspended',
                'risk_precedes_ab_selection', 'risk_precedes_primary_selection', 'risk_precedes_routing_selection']:
        require(parity.get(key) is True, 'domain/destination independence missing: ' + key)
    return {'section': 'domain-entitlement-redirect', 'source_evidence': refs,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'domain_risk_transitions': 8, 'denied_authority_axes': 8,
            'scope': 'server domain readiness and official/custom redirect; UI and other entitlement consumers remain separate'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 domain section evidence admission failed')
    fail_if_errors([emit('P20-T028-domain', 'consistency',
                         'Domain safety section (not whole T028 acceptance)', errors, details)])
