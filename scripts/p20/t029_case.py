"""Executed domain/Link/Bio authority parity, with same-head formal T028 prerequisite."""
import hashlib
import json
from pathlib import Path
from t028_sources import digest, require
from t029_requirements import REQUIRED
from t029_sources import PRODUCERS

BIO_MODES = {'missing', 'review', 'block', 'malformed', 'stale', 'wrong-fingerprint', 'unknown', 'missing-policy'}


def inspect_bio(data):
    old, new = data.get('old_fingerprint'), data.get('new_fingerprint')
    expected = hashlib.sha256(b'gojet-v10-risk-targets-v1\nhttps://example.com/old\n').hexdigest()
    require(old == expected and data.get('shared_link_fingerprint') == expected
            and isinstance(new, str) and len(new) == 64 and new != old, 'Bio fingerprint authority mismatch')
    require(set(data.get('denied_risk_modes', [])) == BIO_MODES, 'Bio uncertainty coverage incomplete')
    require(data.get('public_status') == 200 and all(data.get(k) is True for k in [
        'canonical_equivalent_preserved_allow', 'old_allow_retained_but_non_authoritative',
        'html_api_denial_parity', 'current_exact_allow_recovered']), 'Bio public/API parity failed')


def inspect(root: Path, head: str):
    directory = root / 'artifacts/v10/P20/runtime/t029'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head, 'mixed-head T029 collection')
    require(set(manifest['artifacts']) == set(PRODUCERS), 'missing T029 producer')
    for node, producer in manifest['artifacts'].items():
        require(producer['head_sha'] == head and producer['workflow'] == PRODUCERS[node][0]
                and producer['run_id'] > 0 and producer['artifact_id'] > 0
                and producer['archive_sha256'].startswith('sha256:')
                and len(producer['archive_sha256']) == 71, 'invalid T029 provenance')
    files = manifest['files']
    require(set(files) == {str(p.relative_to(directory)) for p in directory.rglob('*')
                          if p.is_file() and p != directory / 'collection.json'}, 'untracked T029 files')
    for path, record in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts, 'unsafe T029 path')
        require(digest((directory / path).read_bytes()) == record['sha256'], 'T029 source digest mismatch')
        expected_node = 'P20' if path.startswith('prerequisite/') else path.split('/')[3]
        require(record['producer'] == expected_node and expected_node in PRODUCERS, 'wrong source producer')
    from t028_case import inspect as inspect_t028
    prerequisite_root = directory / 'prerequisite'
    formal = json.loads((prerequisite_root / 'artifacts/v10/P20/consistency/P20-T028.json').read_bytes())
    require(formal.get('case') == 'P20-T028' and formal.get('status') == 'PASS'
            and formal.get('errors') == [] and formal.get('implementation_commit') == head
            and formal['details'] == inspect_t028(prerequisite_root, head), 'T028 formal prerequisite invalid')
    refs = []
    def read(node, path):
        relative = f'sources/artifacts/v10/{node}/{path}'
        require(relative in files, 'unbound T029 source')
        data = json.loads((directory / relative).read_bytes())
        require(data.get('exact_head' if node == 'P16' else 'implementation_commit') == head
                and data.get('status') == 'PASS' and data.get('errors', []) == [], 'T029 source not same-head PASS')
        refs.append({'path': relative, 'sha256': files[relative]['sha256']})
        return data
    for path, spec in REQUIRED.items():
        data = read('P16', path)
        require(data.get('case') == Path(path).stem and data.get('contract_authority')
                == '43c5d4d7e1833c593ceacb48016abac6e3133893', 'P16 authority mismatch')
        checks = data.get('checks', {})
        require(set(spec['checks']) <= checks.keys() and all(v is True for v in checks.values()), 'P16 parity assertions missing')
        require(data.get('record_counts') == spec['counts'], 'P16 parity scenario counts mismatch')
        policy = data.get('evidence_policy', {})
        require({'dsn_present', 'raw_authorization_present', 'raw_provider_secret_present'} <= policy.keys()
                and all(v is False for v in policy.values()) and data.get('environment', {}).get('mysql_version'),
                'P16 native/redaction authority absent')
    bio = read('P11', 'api/P11-T010.json')
    require(bio.get('case_id') == 'P11-T010', 'wrong Bio case')
    inspect_bio(bio['observations'])
    visibility = read('P11', 'headers/P11-T009.json')
    require(visibility.get('case_id') == 'P11-T009' and visibility['observations'].get('html_status') == 200
            and visibility['observations'].get('api_urls') == ['https://example.com/a', None, None], 'Bio allow/review/block parity missing')
    # T028 already revalidated P06 T013/T019/T020 raw evidence: independent
    # entitlement/ownership/DNS/HTTPS/risk axes and official/custom fail-closed parity.
    return {'formal_p20_t029_claim': True, 'next_case_unlocked': False,
            'prerequisite': {'case': 'P20-T028', 'head': head, 'revalidated': True},
            'source_evidence': refs, 'bio_uncertainty_modes': sorted(BIO_MODES),
            'domain_axes_from_t028': formal['details']['sections']['domain']['source_evidence'],
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def run_case():
    from common import ROOT, HEAD, emit
    details = {'formal_p20_t029_claim': False, 'next_case_unlocked': False}
    errors = []
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError) as error:
        errors.append('T029 admission failed: ' + str(error))
    return emit('P20-T029', 'consistency', 'Domain and destination authority parity', errors, details)
