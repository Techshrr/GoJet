"""Executed file safety parity section; does not certify all of T028."""
import hashlib
import json
from pathlib import Path


def require(value, reason):
    if not value:
        raise ValueError(reason)


def inspect(root: Path, head: str):
    refs = []

    def read(folder, number, browser=False):
        case = 'P09-T' + number
        path = root / 'artifacts/v10/P09' / folder / (case + '.json')
        raw = path.read_bytes()
        data = json.loads(raw)
        require(data.get('case_id' if browser else 'case') == case
                and data.get('implementation_commit') == head
                and data.get('status') == 'PASS' and data.get('errors') == [],
                'file evidence not exact-head PASS')
        refs.append({'case': case, 'path': str(path.relative_to(root)),
                     'sha256': 'sha256:' + hashlib.sha256(raw).hexdigest()})
        return data['details' if browser else 'observations']

    coherence = read('results', '026')
    require(coherence.get('same_exact_head') is True and coherence.get('input_evidence_count') == 25
            and coherence.get('clamav_engine_version') and coherence.get('clamav_signature_version'),
            'file coherence or real ClamAV missing')
    infected = read('clamav', '006')
    require(infected.get('scan_state') == 'blocked' and infected.get('scan_status') == 'infected'
            and infected.get('public_status') == 403 and infected.get('verdict_code') == 'Eicar-Test-Signature',
            'real EICAR distribution not blocked')
    codes = {'007': {'clamav_unavailable'}, '008': {'scan_read_failed', 'scan_write_failed'},
             '009': {'signature_stale'}, '010': {'indeterminate_response'}}
    for number, allowed in codes.items():
        observation = read('clamav', number)
        require(observation.get('scan_state') == 'scan_error' and observation.get('error_code') in allowed,
                'uncertain scan not retained as scan_error')
        require(type(observation.get('published')) is int and observation['published'] == 0
                and observation.get('publish_status') == 409 and observation.get('public_status') == 403
                and observation.get('public_content_leaked') is False,
                'uncertain file publish/public denial missing')
    permission = read('results', '015')
    require(permission.get('premature_publish') == 409 and permission.get('viewer_publish') == 403
            and permission.get('admin_publish') == 200 and permission.get('safe_auto_published') is False,
            'safe verdict became publication authority')
    workspace = read('browser', '021', True)
    require({'blocked', 'quarantined', 'safe', 'scan_error', 'scanning'}
            <= set(workspace.get('authoritative_states', []))
            and workspace.get('fake_success_before_server_confirmation') is False,
            'workspace optimistic or incomplete safety states')
    public = read('browser', '022', True)
    require(public.get('preauth_binary_status') == 403 and public.get('authorized_binary_status') == 200
            and public.get('rescan_public_state') == 'scan-pending' and public.get('rescan_binary_status') == 403
            and public.get('blocked_public_state') == 'blocked' and public.get('blocked_binary_status') == 403,
            'public safety state differs from binary authority')
    installer = read('browser', '024', True)
    require(installer.get('fault_installer') == {'status': 503, 'state': 'hard-failure'}
            and installer.get('private_dependency_detail_leaked') is False,
            'installer failure masked or dependency details leaked')
    return {'section': 'clamav-api-workspace-public-installer', 'source_evidence': refs,
            'uncertainty_modes': 4, 'formal_p20_t028_claim': False, 'next_case_unlocked': False,
            'admin_governance_bound': False,
            'scope': 'P09 native/API and browser authority; P17 administrator actions require separate binding'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit, fail_if_errors
    errors = []
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T028 file safety evidence admission failed')
    fail_if_errors([emit('P20-T028-files', 'consistency',
                         'File safety section (not whole T028 acceptance)', errors, details)])
