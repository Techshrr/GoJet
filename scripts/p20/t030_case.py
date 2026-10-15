"""HTTP status consistency with raw evidence and recursive predecessor admission."""
import json
from pathlib import Path
from t028_sources import digest, require
from t030_sources import PRODUCERS
from t030_native import inspect as inspect_native

EXPECTED = {
    'website/site-core/P19-T005.json': ('observations', {'canonical_200_pages': 52, 'unknown_404': 3, 'withdrawn_410': 2, 'soft_404': 0, 'query_changes_status': False, 'query_changes_canonical': False}),
    'bio/headers/P11-T005.json': ('observations', {'html_status': 410, 'api_status': 410}),
    'bio/headers/P11-T011.json': ('observations', {'published': 200, 'paused': 200, 'draft': 404, 'removed': 410, 'unknown': 404}),
    'bio/api/P11-T012.json': ('observations', {'over_quota_status': 429, 'unresolved_publish_status': 409, 'authoritative_status': 'draft'}),
    'text/api/P10-T005.json': ('observations', {'delete_status': 204, 'public_after_delete': 410, 'stale_update': 410, 'repeat_delete': 410}),
    'text/headers/P10-T007.json': ('observations', {'page': 403, 'action': 403, 'download': 403}),
    'text/headers/P10-T010.json': ('observations', {'preconsume_page': 200, 'concurrent_statuses': [200, 410], 'postconsume_page': 410, 'consumed_at': 'SET'}),
    'text/headers/P10-T011.json': ('observations', {'unknown': 404, 'malformed': 404}),
    'redirect/results/P05-T016.json': ('details', {'r301': 301, 'r302': 302, 'r307': 307, 'r308': 308, 'paused': 200, 'expired': 410, 'limited_first': 302, 'limited_second': 410, 'deleted': 410}),
    'developer/integration/P20-T024-runtime.json': ('details', {'api_key_create_http_status': 201, 'api_key_read_http_status': 200, 'api_key_rate_limited_http_status': 429, 'api_key_scope_denied_http_status': 403, 'missing_csrf_http_status': 403, 'csrf_replay_http_status': 403, 'unauthenticated_http_status': 401, 'expired_key_http_status': 401, 'real_p15_session': True, 'mock_authority': False, 'test_header_authority': False}),
}


def inspect_observations(path, data):
    container, expected = EXPECTED[path]
    actual = data.get(container, {})
    for key, value in expected.items():
        require(type(actual.get(key)) is type(value) and actual[key] == value, f'HTTP semantic mismatch: {path}:{key}')


def inspect(root, head):
    directory = root / 'artifacts/v10/P20/runtime/t030-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head, 'mixed-head T030 collection')
    require(set(manifest['artifacts']) == set(PRODUCERS), 'missing HTTP producer')
    for key, artifact in manifest['artifacts'].items():
        require(artifact['head_sha'] == head and artifact['workflow'] == PRODUCERS[key][0]
                and artifact['run_id'] > 0 and artifact['artifact_id'] > 0
                and artifact['archive_sha256'].startswith('sha256:') and len(artifact['archive_sha256']) == 71, 'bad HTTP provenance')
    files = manifest['files']
    require(set(files) == {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p != directory / 'collection.json'}, 'untracked HTTP evidence')
    for path, record in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts, 'unsafe HTTP evidence path')
        require(record['producer'] == path.split('/')[0] and record['producer'] in PRODUCERS, 'HTTP producer mismatch')
        require(digest((directory / path).read_bytes()) == record['sha256'], 'HTTP source hash mismatch')
    from t029_case import inspect as inspect_t029
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/consistency/P20-T029.json').read_bytes())
    require(formal.get('case') == 'P20-T029' and formal.get('status') == 'PASS' and formal.get('errors') == []
            and formal.get('implementation_commit') == head and formal['details'] == inspect_t029(prerequisite, head), 'invalid T029 prerequisite')
    refs = []
    def read(path):
        require(path in files, 'unbound HTTP evidence')
        data = json.loads((directory / path).read_bytes())
        require(data.get('implementation_commit') == head and data.get('status') == 'PASS' and data.get('errors', []) == [], 'HTTP source not exact-head PASS')
        require(data.get('case', data.get('case_id')) == Path(path).stem.removesuffix('-runtime'), 'HTTP case identity mismatch')
        refs.append({'path': path, 'sha256': files[path]['sha256']})
        return data
    for path in EXPECTED:
        inspect_observations(path, read(path))
    docs = read('docs/http/P18-T006.json')
    require(docs.get('soft_404_count') == 0 and docs.get('unknown') == {'path': '/docs/en/this-document-does-not-exist', 'status': 404}
            and docs.get('withdrawn') == [{'path': '/docs/en/legacy-api', 'status': 410}, {'path': '/docs/zh-CN/legacy-api', 'status': 410}], 'Docs soft-404 or lifecycle mismatch')
    native_path = 'native/runtime/t030/http-status.jsonl'
    native_manifest = json.loads((directory / 'native/runtime/t030/native-manifest.json').read_bytes())
    require(native_manifest == inspect_native((directory / native_path).read_bytes(), head), 'native HTTP admission mismatch')
    refs.append({'path': native_path, 'sha256': files[native_path]['sha256']})
    # The recursively verified T028 file-scanning authority also proves 403
    # public denials and 409 publish conflicts for real ClamAV uncertainty.
    return {'formal_p20_t030_claim': True, 'next_case_unlocked': False,
            'prerequisite': {'case': 'P20-T029', 'head': head, 'revalidated': True},
            'source_evidence': refs, 'native_tests': native_manifest['passed_tests'],
            'csrf_expiry_scope': 'Account/Admin unsafe mutation after valid session: 419; Workspace resource denials retain 403; grant expiry remains 410',
            'valid_200_states': ['published Bio', 'paused Bio without navigable targets', 'paused redirect without destination', 'Text pre-consume page without content'],
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def run_case():
    from common import ROOT, HEAD, emit
    details = {'formal_p20_t030_claim': False, 'next_case_unlocked': False}
    errors = []
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError) as error:
        errors.append('T030 admission failed: ' + str(error))
    return emit('P20-T030', 'consistency', 'HTTP status and error-state consistency', errors, details)
