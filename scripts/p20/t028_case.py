"""Whole T028 admission from digest-bound, executed cross-surface evidence."""
import importlib
import json
from pathlib import Path
from t028_sources import SOURCES, EXTRA, GATES, digest, require


def inspect(root: Path, head: str):
    directory = root / 'artifacts/v10/P20/runtime/t028'
    sources = directory / 'sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head, 'mixed-head collection')
    files, artifacts = manifest['files'], manifest['artifacts']
    require(set(files) == {str(p.relative_to(sources)) for p in sources.rglob('*') if p.is_file()},
            'untracked or missing source files')
    for path, record in files.items():
        require(path.startswith('artifacts/v10/') and '..' not in Path(path).parts, 'unsafe manifest path')
        require(digest((sources / path).read_bytes()) == record['sha256'], 'collected file digest mismatch')
        producer = artifacts[record['artifact']]
        require(producer['head_sha'] == head and producer['run_id'] > 0 and producer['artifact_id'] > 0
                and producer['archive_sha256'].startswith('sha256:')
                and len(producer['archive_sha256']) == 71, 'missing producer provenance')
    require(set(manifest['gates']) == set(GATES), 'required gates incomplete')
    for gate in manifest['gates'].values():
        require(gate['head_sha'] == head and gate['conclusion'] == 'success' and gate['run_id'] > 0,
                'required gate not exact-head success')
    for workflow, prefix, node, paths in EXTRA.values():
        for relative in paths:
            require(files[f'artifacts/v10/{node}/{relative}']['artifact'] == prefix + head
                    and artifacts[prefix + head]['workflow'] == workflow, 'extra source provenance mismatch')
    admin_prefix = SOURCES['admin'][3]
    require(files['artifacts/v10/P17/cases/P17-T033.json']['artifact'] == admin_prefix + head,
            'Admin browser producer mismatch')
    sections = {}
    for section, (workflow, _, section_prefix, raw_prefix, _) in SOURCES.items():
        # Execute the same strict validator again on the raw source files.
        details = importlib.import_module('t028_' + section).inspect(sources, head)
        path = f'artifacts/v10/P20/consistency/P20-T028-{section}.json'
        section_record = files[path]
        require(section_record['artifact'] == section_prefix + head, 'wrong section artifact')
        result = json.loads((sources / path).read_bytes())
        require(result.get('implementation_commit') == head and result.get('status') == 'PASS'
                and result.get('errors') == [] and result.get('details') == details,
                'section differs from recomputed raw evidence')
        for ref in details['source_evidence']:
            record = files[ref['path']]
            require(record['sha256'] == ref['sha256'] and record['artifact'] == raw_prefix + head,
                    'raw source provenance differs from section')
            require(artifacts[record['artifact']]['workflow'] == workflow, 'wrong producer workflow')
        sections[section] = details

    def read(node, relative, native=False):
        path = f'artifacts/v10/{node}/{relative}'
        require(path in files, 'extra source not collected')
        data = json.loads((sources / path).read_bytes())
        require(data.get('exact_head' if native else 'implementation_commit') == head
                and data.get('status') == 'PASS' and data.get('errors', []) == [], 'extra browser evidence failed')
        return data

    domain = read('P06', 'browser/P06-T023.json')
    require(domain.get('case_id') == 'P06-T023'
            and {'ingress_dns_invalid', 'https_error', 'risk_review'}
            <= set(domain['details'].get('persistent_problem_states_after_reload', [])),
            'Workspace domain uncertainty not persistent after reload')
    admin = read('P17', 'cases/P17-T033.json', True)
    require(admin.get('case') == 'P17-T033' and admin.get('contract_authority')
            == '30174f40df28678360f644b8fed79736906b0ea0', 'Admin browser authority mismatch')
    security = admin['details'].get('security_checks', {})
    require({'direct_route_permissions', 'persistent_provider_error'} <= security.keys()
            and all(value is True for value in security.values())
            and 'admin-turnstile:error' in admin['details'].get('platform_states', [])
            and admin['details'].get('frozen_contract_completion') is True,
            'Admin provider uncertainty hidden by browser')
    from consistency_cases import inspect_native, inspect_browser
    prerequisite = read('P20', 'consistency/P20-T027.json')
    require(prerequisite.get('case') == 'P20-T027'
            and prerequisite['details'].get('formal_p20_t027_claim') is True,
            'T027 prerequisite not formal')
    t027 = {'native': inspect_native(sources, head), 'browser': inspect_browser(sources, head)}
    return {'formal_p20_t028_claim': True, 'next_case_unlocked': False,
            'promotion_requires_same_head_gates': True,
            'sections': sections, 't027_revalidated': t027,
            'collection_sha256': digest((directory / 'collection.json').read_bytes()),
            'source_file_count': len(files), 'producer_artifact_count': len(artifacts),
            'coverage': {
                'destination': ['provider failure API', 'official/custom redirect', 'Public deny page', 'Admin authority'],
                'domain': ['readiness API', 'redirect final recheck', 'Workspace persistent uncertainty', 'Admin authority'],
                'clamav': ['real engine uncertainty', 'publish API', 'Public binary/page', 'Workspace', 'Admin quarantine/restore'],
                'turnstile': ['auth API/browser', 'Website contact', 'Public abuse', 'Workspace support', 'Admin provider error'],
                'auth_session': ['Website auth', 'Workspace revoked session', 'Admin permission', 'API one-time CSRF'],
                'entitlement': ['mutation API', 'redirect grace/expiry', 'Workspace billing', 'Admin/viewer denial'],
            }}


def run_case():
    from common import ROOT, HEAD, emit
    details = {'formal_p20_t028_claim': False, 'next_case_unlocked': False}
    errors = []
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError) as error:
        errors.append('T028 combined admission failed: ' + str(error))
    return emit('P20-T028', 'consistency', 'Cross-surface fail-closed safety parity', errors, details)
