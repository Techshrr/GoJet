"""T028 support/contact safety section using executed API and browser evidence."""
import hashlib
import json
from pathlib import Path


def inspect(root: Path, head: str):
    refs = []
    def require(ok, reason):
        if not ok:
            raise ValueError(reason)
    def read(folder, number):
        case = 'P14-T' + number
        path = root / 'artifacts/v10/P14' / folder / (case + '.json')
        raw = path.read_bytes()
        d = json.loads(raw)
        require(d.get('case_id') == case and d.get('implementation_commit') == head
                and d.get('status') == 'PASS' and d.get('errors') == [],
                'support evidence not exact-head PASS')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return d
    csrf = read('security', '011')['checks']
    require(csrf.get('invalid_status') == 400 and csrf.get('replay_status') == 400
            and csrf.get('valid_status') == 201 and csrf.get('ticket_rows_after_replay') == 1
            and csrf.get('raw_token_in_redis_key') is False, 'support Turnstile denial missing')
    contact = read('api', '013')['checks']
    require(contact.get('persistent_success') is True and contact.get('public_contact_rows') == 1
            and contact.get('public_ticket_rows') == 1 and contact.get('idempotent_replay_created') is False
            and contact.get('workspace_membership_granted') is False
            and contact.get('response_contains_email') is False, 'contact success authority unsafe')
    support = read('browser', '022')['details']
    for key in ['frozen_contract_completion', 'requester_internal_note_isolation',
                'attachment_http_authority_not_invented', 'turnstile_replay_failed_closed',
                'rate_limit_failed_closed', 'foreign_direct_url_failed_closed']:
        require(support.get(key) is True, 'support browser denial missing: ' + key)
    require({'Turnstile-required', 'error', 'success', 'rate-limited'}
            <= set(support.get('states', {}).get('app_support_new', [])), 'new ticket states incomplete')
    require({'attachment-blocked', 'forbidden', 'error'}
            <= set(support.get('states', {}).get('app_support_thread', [])), 'thread denies missing')
    require(support.get('exact_ticket_count') == 1, 'rejected ticket request created a row')
    website = read('browser', '023')['details']
    require(website.get('frozen_contract_completion') is True and website.get('permission_denial') is True
            and website.get('mail_recipient_redacted') is True and website.get('public_contact_durable_count') == 1,
            'contact/Admin authority incomplete')
    require({'validation-error', 'Turnstile-error', 'rate-limited', 'success-persistent'}
            <= set(website.get('states', {}).get('web_contact', [])), 'website contact denies missing')
    return {'section': 'support-contact-turnstile', 'source_evidence': refs,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'scope': 'P14 executed support/contact API and Workspace/Website/Admin browsers; other controls remain separately required'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 support/contact evidence admission failed')
    fail_if_errors([emit('P20-T028-support', 'consistency',
                         'Support/contact safety section (not whole T028 acceptance)', errors, details)])
