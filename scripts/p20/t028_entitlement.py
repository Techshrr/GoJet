"""T028 server entitlement and billing browser safety section."""
import hashlib
import json
from pathlib import Path

EXPECTED = {
    'P13-T003': ('entitlement', {'hard_deny_result': 'hard_deny', 'non_additive': True}),
    'P13-T008': ('security', {'billing_grants_delta': 0, 'callback_events_delta': 0,
                            'invalid_signature_status': 401, 'missing_signature_status': 401, 'order_status': 'pending'}),
    'P13-T011': ('entitlement', {'billing_grants': 0, 'order_status': 'failed', 'transaction_status': 'failed'}),
    'P13-T016': ('entitlement', {'destructive_cleanup': False, 'during_grace_error': 'entitlement_required',
                               'during_grace_mutation_status': 409, 'post_boundary_error': 'domain_limit_reached',
                               'post_boundary_mutation_status': 409}),
    'P13-T017': ('entitlement', {'no_entitlement_error': 'entitlement_required', 'no_entitlement_mutation_status': 409,
                               'routing_enabled_by_payment': False,
                               'post_payment_p06_axes': ['pending', 'pending', 'pending', 'pending', 'missing']}),
}


def inspect(root: Path, head: str):
    refs = []
    def require(ok, reason):
        if not ok:
            raise ValueError(reason)
    def read(folder, case, browser=False):
        path = root / 'artifacts/v10/P13' / folder / (case + '.json')
        raw = path.read_bytes()
        d = json.loads(raw)
        require(d.get('case_id') == case and d.get('implementation_commit') == head
                and d.get('status') == 'PASS' and d.get('errors') == [], 'entitlement evidence mismatch')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return d['details' if browser else 'observations']
    for case, (folder, expected) in EXPECTED.items():
        d = read(folder, case)
        for key, value in expected.items():
            require(type(d.get(key)) is type(value) and d[key] == value, 'unsafe entitlement outcome: ' + key)
        if case == 'P13-T011':
            require(type(d.get('manual_limit_before')) is int and d['manual_limit_before'] > 0
                    and d['manual_limit_before'] == d.get('manual_limit_after'), 'failed payment changed valid grant')
        if case == 'P13-T016':
            require(type(d.get('existing_domains_before')) is int and d['existing_domains_before'] > 0
                    and d['existing_domains_before'] == d.get('existing_domains_after'), 'expiry destroyed existing resources')
    billing = read('browser', 'P13-T021', True)
    require(billing.get('frozen_contract_completion') is True and billing.get('owner_only_mutation_authority') is True,
            'billing browser authority missing')
    states = billing.get('observed_states', {})
    require(all(states.get(k) is True for k in ['active', 'payment-pending', 'payment-failed',
            'overdue', 'canceled', 'provider-partial', 'error']) and states.get('viewer_mutation_denied') == 403,
            'billing uncertainty or denial state missing')
    privacy = read('browser', 'P13-T025', True)
    require(privacy.get('authorization') == {'viewer_summary': 403, 'unauthenticated_api': 401, 'denied_admin': 403}
            and privacy.get('offline_recovery') is True and privacy.get('frozen_contract_completion') is True,
            'billing auth/offline safety missing')
    headers = privacy.get('private_surface_headers', {})
    require({'workspace', 'admin', 'workspace_api', 'admin_api'} <= headers.keys()
            and all(v is True for v in headers.values()), 'billing private headers missing')
    return {'section': 'entitlement-billing-browser', 'source_evidence': refs,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'scope': 'P13 server authority and real billing browser; whole T028 requires all safety sections'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 entitlement evidence admission failed')
    fail_if_errors([emit('P20-T028-entitlement', 'consistency',
                         'Entitlement safety section (not whole T028 acceptance)', errors, details)])
