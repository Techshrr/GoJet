"""Executed administrator safety section; never whole T028 acceptance."""
import hashlib
import json
from pathlib import Path

AUTHORITY = '30174f40df28678360f644b8fed79736906b0ea0'
REQUIRED = {'P17-T012': {'admin_quarantine_clears_publication_and_restore_stays_denied_until_p09_safe',
              'blocked_file_restore_fails_closed',
              'delete_preserves_workspace_quota_accounting',
              'files_manage_required_and_content_manage_cannot_escalate',
              'rescan_returns_to_quarantine_and_queues_exact_new_generation',
              'restore_consumes_p09_safe_state_without_creating_safe_verdict',
              'successful_file_mutations_audit_clamav_bypass_false'},
 'P17-T013': {'p16_authorizer_binds_exact_actor_and_permission',
              'p16_provider_target_abuse_and_domain_evidence_remains_redacted',
              'p17_bridge_never_mutates_p16_verdicts',
              'p17_domains_risk_manage_consumes_latest_p16_domain_authority',
              'p17_security_manage_consumes_p16_destination_and_abuse_authority',
              'security_and_domain_risk_permissions_remain_independent'},
 'P17-T018': {'audit_contains_no_secret',
              'incomplete_fail_closed',
              'provider_error_fail_closed',
              'read_projection_masks_secret',
              'secret_encrypted_at_rest',
              'settings_manage_required'}}


def inspect(root: Path, head: str):
    refs = []
    def require(ok, reason):
        if not ok:
            raise ValueError(reason)
    def read(folder, case, native=True):
        path = root / 'artifacts/v10/P17' / folder / (case + '.json')
        raw = path.read_bytes()
        d = json.loads(raw)
        require(d.get('case') == case and d.get('node') == 'P17'
                and d.get('exact_head' if native else 'implementation_commit') == head
                and d.get('status') == 'PASS' and d.get('errors', []) == []
                and d.get('contract_authority') == AUTHORITY, 'administrator evidence mismatch')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return d
    obs = read('results', 'P17-T034', False)['observations']
    require(obs.get('input_evidence_count') == 33 and obs.get('same_exact_head') is True
            and obs.get('mixed_head_rejected') is True and obs.get('unsafe_evidence_rejected') is True,
            'administrator coherence missing')
    for case, required in REQUIRED.items():
        d = read('cases', case)
        checks = d.get('checks', {})
        require(required <= checks.keys() and all(v is True for v in checks.values()),
                'administrator safety checks incomplete')
        policy = d.get('evidence_policy', {})
        require({'dsn_present', 'raw_password_present', 'raw_session_present', 'raw_totp_present'}
                <= policy.keys() and all(v is False for v in policy.values()), 'administrator redaction missing')
        require(d.get('environment', {}).get('mysql_version'), 'real administrator database missing')
    return {'section': 'administrator-files-risk-turnstile', 'source_evidence': refs,
            'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'scope': 'P17 executed native authority; browser and other control sections remain separately required'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 administrator safety evidence admission failed')
    fail_if_errors([emit('P20-T028-admin', 'consistency',
                         'Administrator safety section (not whole T028 acceptance)', errors, details)])
