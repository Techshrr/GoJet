"""Release-wide SEO admission from same-head executed predecessor evidence."""
import json
from pathlib import Path
from urllib.parse import urlsplit
from t028_sources import require, digest
from t032_sources import PRODUCERS


def equal_fields(actual, expected, scope):
    for key, value in expected.items():
        require(type(actual.get(key)) is type(value) and actual[key] == value, scope + ':' + key)


def inspect_private(data, head):
    require(data.get('implementation_commit') == head and data.get('production_session') is True
            and data.get('mocked_api') is False, 'invalid private browser authority')
    expected = {('/app/settings/danger', role) for role in ('owner', 'admin', 'member', 'viewer', 'anonymous')}
    expected |= {('/admin/operations/jobs', role) for role in ('owner', 'admin', 'member', 'viewer', 'anonymous', 'limited-admin')}
    expected.add(('/admin/platform/mail-templates/mail-test', 'mail-admin'))
    rows = data['rows']
    require(len(rows) == len(expected) and {(r['path'], r['role']) for r in rows} == expected, 'incomplete private indexation matrix')
    for row in rows:
        require(any('noindex' in {v.strip().lower() for v in value.split(',')} for value in row['robots']), 'private page indexable')
        equal_fields(row, {'canonical': 0, 'alternates': 0, 'structuredData': 0}, 'private acquisition leak')
    return len(rows)


def inspect_seo(sources, head):
    for path, value in sources.items():
        require(value.get('implementation_commit') == head and value.get('status') == 'PASS'
                and value.get('errors', []) == [], 'SEO source not exact-head PASS: ' + path)
        require(value.get('case', value.get('case_id')) == Path(path).stem, 'SEO case identity mismatch')
    def read(path, field=None):
        value = sources[path]
        return value[field] if field else value
    equal_fields(read('website/site-core/P19-T003.json', 'observations'),
                 {'canonical_chain': False, 'canonical_count': 52, 'fragment_in_canonical_count': 0,
                  'normalized_paths': True, 'query_in_canonical_count': 0}, 'Website canonical')
    equal_fields(read('website/site-core/P19-T004.json', 'observations'),
                 {'alternate_links_verified': 156, 'fabricated_alternates': 0, 'reciprocal': True,
                  'translation_pairs': 26, 'x_default': 'English canonical'}, 'Website alternates')
    equal_fields(read('website/site-core/P19-T006.json', 'observations'),
                 {'canonical_indexable_200_count': 52, 'extra_urls': 0, 'missing_urls': 0,
                  'private_or_ugc_urls': 0, 'lastmod_source': 'content registry'}, 'Website sitemap')
    crawl = read('crawl/crawl/P19-T020.json', 'details')
    equal_fields(crawl, {'sitemap_urls': 52, 'non_website_acquisition': []}, 'Website crawler policy')
    raw = read('crawl/crawl/P19-T021.json', 'details')
    equal_fields(raw, {'documents_checked': 52, 'raw_primary_content': True, 'crawler_specific_branch': False}, 'Website raw HTML')
    require(len(raw['paths']) == len(set(raw['paths'])) == 52, 'incomplete raw HTML paths')
    canonical = read('docs/seo/P18-T003.json')
    require(canonical['query_parameters_excluded'] is True, 'Docs canonical query leak')
    documents = canonical['documents']
    by_path = {row['path']: row for row in documents}
    require(len(documents) == len(by_path) == 13, 'Docs canonical inventory incomplete')
    for path, row in by_path.items():
        expected = 'https://gojet.cc' + path
        require(path.startswith(('/docs/en/', '/docs/zh-CN/')) and '?' not in path and '#' not in path
                and row['canonical'] == expected and row['canonical_links'] == [expected], 'Docs canonical mismatch')
    alternates = read('docs/seo/P18-T004.json')
    pairs = {row['path']: row['alternates'] for row in alternates['documents']}
    require(len(pairs) == alternates['reciprocal_pairs_checked'] == 12, 'Docs reciprocal set incomplete')
    for path, alts in pairs.items():
        locale = 'zh-CN' if path.startswith('/docs/zh-CN/') else 'en'
        require(alts.get(locale) == ['https://gojet.cc' + path], 'missing Docs self alternate')
        for language, targets in alts.items():
            require(language in ('en', 'zh-CN', 'x-default') and len(targets) == 1, 'invalid alternate language')
            target = urlsplit(targets[0])
            require(target.scheme == 'https' and target.netloc == 'gojet.cc' and not target.query and not target.fragment
                    and target.path in by_path, 'unpublished alternate')
            if language != 'x-default':
                require(pairs.get(target.path, {}).get(locale) == ['https://gojet.cc' + path], 'nonreciprocal Docs alternate')
        if path in ('/docs/en/', '/docs/zh-CN/'):
            require(alts.get('x-default') == ['https://gojet.cc/docs/en/'], 'Docs x-default mismatch')
    untranslated = read('docs/seo/P18-T005.json')
    require(untranslated['fabricated_translation_count'] == 0 and len(untranslated['documents']) == 1, 'fabricated Docs translation')
    for row in untranslated['documents']:
        require(row['path'] in by_path and row['path'] not in pairs and row['absent_translation'] not in by_path, 'untranslated peer fabricated')
        locale = 'zh-CN' if row['path'].startswith('/docs/zh-CN/') else 'en'
        require(row['alternates'] == {locale: ['https://gojet.cc' + row['path']]}, 'unexpected untranslated alternate')
    search = read('discovery/search/P18-T010.json')
    equal_fields(search, {'canonical_set_member': False, 'sitemap_member': False}, 'Docs search exclusion')
    sitemap = read('discovery/seo/P18-T014.json')
    equal_fields(sitemap, {'build_time_lastmod': False, 'content_owned_lastmod': True}, 'Docs lastmod authority')
    urls = []
    require(set(sitemap['locale_sitemaps']) == {'en', 'zh-CN'}, 'Docs sitemap locales')
    for locale, rows in sitemap['locale_sitemaps'].items():
        for row in rows:
            require(row['loc'].startswith('https://gojet.cc/docs/' + locale + '/') and bool(row['lastmod']), 'Docs locale sitemap mismatch')
            urls.append(row['loc'])
    require(len(urls) == len(set(urls)) == 13 and set(urls) == {r['canonical'] for r in documents}, 'Docs sitemap/canonical divergence')
    text = read('text/headers/P10-T013.json', 'observations')
    require(len(text['checks']) == 8 and text['unknown_status'] == 404 and 'noindex' in text['unknown_x_robots_tag'], 'Text noindex coverage')
    require(all('noindex' in row['x_robots_tag'] for row in text['checks']), 'Text indexation leak')
    bio = read('bio/headers/P11-T013.json', 'observations')
    require(len(bio['checks']) == 6 and all('noindex' in row['api_x_robots_tag'] and 'noindex' in row['html_x_robots_tag']
                and row['api_body_has_workspace_id'] is False for row in bio['checks']), 'Bio indexation or tenant leak')
    for path, hit_key, states in [
        ('text/headers/P10-T014.json', 'sitemap_text_hits', {'available': 200, 'consumed': 410, 'expired': 410, 'removed': 410, 'unknown': 404}),
        ('bio/sitemap/P11-T014.json', 'sitemap_bio_hits', {'draft': 404, 'published': 200, 'removed': 410, 'unknown': 404}),
    ]:
        equal_fields(read(path, 'observations'), {'canonical_present': False, 'hreflang_present': False,
                    'structured_data_present': False, hit_key: [], 'statuses': states}, 'UGC acquisition leak')
    opt_in = read('bio/headers/P11-T015.json', 'observations')
    equal_fields(opt_in, {'create_unknown_field_status': 400, 'update_unknown_field_status': 400,
                         'persisted_index_authority': False, 'forbidden_source_hits': []}, 'Bio opt-in expanded')
    require('noindex' in opt_in['query_x_robots_tag'], 'Bio query index override')
    return {'website_indexable_pages': 52, 'docs_indexable_pages': 13, 'untranslated_docs': 1,
            'ugc_noindex_observations': 14, 'crawler_differential': False, 'sitemap_canonical_parity': True}


def inspect(root, head, source_root=None):
    directory = root / 'artifacts/v10/P20/runtime/t032-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head and set(manifest['artifacts']) == set(PRODUCERS), 'missing/mixed-head SEO producers')
    for key, artifact in manifest['artifacts'].items():
        require(artifact['head_sha'] == head and artifact['workflow'] == PRODUCERS[key][0] and artifact['run_id'] > 0
                and artifact['artifact_id'] > 0 and artifact['archive_sha256'].startswith('sha256:')
                and len(artifact['archive_sha256']) == 71, 'invalid SEO provenance')
    files = manifest['files']
    require(set(files) == {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p != directory / 'collection.json'}, 'untracked SEO evidence')
    for path, record in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts and record['producer'] == path.split('/')[0]
                and record['producer'] in PRODUCERS and digest((directory / path).read_bytes()) == record['sha256'], 'SEO evidence binding mismatch')
    from t031_case import inspect as inspect_t031
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/consistency/P20-T031.json').read_bytes())
    require(formal.get('case') == 'P20-T031' and formal.get('status') == 'PASS' and formal.get('errors') == []
            and formal.get('implementation_commit') == head and formal['details'] == inspect_t031(prerequisite, head, source_root=source_root or root), 'invalid T031 prerequisite')
    sources = {key + '/' + path: json.loads((directory / key / path).read_bytes())
               for key, (_, _, _, paths) in PRODUCERS.items() if key not in ('prerequisite', 'private') for path in paths}
    private = json.loads((directory / 'private/runtime/t032/private-indexation.json').read_bytes())
    # Bind the extra private observations to the same successful native browser execution.
    from t027_native import PACKAGE
    rows = [json.loads(line) for line in (directory / 'private/runtime/t027/workspace-browser.jsonl').read_bytes().splitlines()]
    require(rows and not any(r.get('Action') in ('skip', 'fail') for r in rows), 'private browser failed/skipped')
    for test in ('TestP20WorkspaceProductionBrowser', None):
        require(sum(r.get('Action') == 'pass' and r.get('Package') == PACKAGE and r.get('Test') == test for r in rows) == 1, 'private browser verdict missing')
    return {'formal_p20_t032_claim': True, 'next_case_unlocked': False,
            'prerequisite': {'case': 'P20-T031', 'head': head, 'revalidated': True},
            'seo': inspect_seo(sources, head), 'private_browser_observations': inspect_private(private, head),
            'source_evidence': [{'path': p, 'sha256': files[p]['sha256']} for p in sorted(sources)],
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def run_case():
    from common import ROOT, HEAD, emit
    import subprocess
    details = {'formal_p20_t032_claim': False, 'next_case_unlocked': False}
    errors = []
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        errors.append('T032 admission failed: ' + str(error))
    return emit('P20-T032', 'consistency', 'Release-wide SEO and indexation consistency', errors, details)
