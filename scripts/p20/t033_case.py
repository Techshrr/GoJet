"""Locale discovery parity against recursively admitted, same-head raw sources."""
import json
import subprocess
from pathlib import Path
from urllib.parse import urlsplit
from t028_sources import require, digest
from t033_sources import PRODUCERS


def normalized_doc(href):
    value = urlsplit(href)
    require(not value.scheme and not value.netloc and value.path.startswith('/docs/'), 'external/non-Docs acquisition target')
    return value.path.rstrip('/')


def inspect_locales(graph, canonical, links, articles, searches, head):
    nodes = set(graph)
    english = {p for p in nodes if not p.startswith('/zh-CN')}
    chinese = nodes - english
    require(len(english) == len(chinese) == 26, 'Website locale inventory incomplete')
    require(chinese == {'/zh-CN/' if p == '/' else '/zh-CN' + p for p in english}, 'Website locale peer mismatch')
    for path, targets in graph.items():
        own = chinese if path in chinese else english
        require(set(targets) <= own, 'Website invented or cross-locale acquisition edge')
    docs = {normalized_doc(row['path']) for row in canonical}
    require(len(canonical) == len(docs) == 13, 'Docs published set incomplete')
    require({normalized_doc(p) for p in links} == docs and len(links) == 13, 'Website/Docs published target divergence')
    article_links = 0
    for row in articles:
        source = normalized_doc(row['path'])
        require(source in docs and row['internal_links'], 'missing Docs navigation sample')
        locale = source.split('/')[2]
        for href in row['internal_links']:
            target = normalized_doc(href)
            require(target in docs and target.split('/')[2] == locale, 'unpublished or cross-locale Docs navigation')
            article_links += 1
    require(len(articles) == 3, 'Docs navigation sample incomplete')
    counts = {}
    for locale, case, eligible in [('en', 'P18-T008', 'eligible_published_english'), ('zh-CN', 'P18-T009', 'eligible_published_zh_cn')]:
        data = searches[case]
        require(data.get('implementation_commit') == head and data.get('status') == 'PASS'
                and data.get('case') == case and data.get('errors', []) == [], 'search source not exact-head PASS')
        expected = {p for p in docs if p.split('/')[2] == locale}
        require({normalized_doc(p) for p in data[eligible]} == expected and len(data[eligible]) == len(expected), 'search published inventory mismatch')
        probe = data['probe']
        require(probe['state'] == 'results' and probe['offline'] is False and probe['pagefind_requests'] > 0
                and probe['external_requests'] == [] and probe['hrefs'], 'missing native Pagefind results')
        require(all(normalized_doc(href) in expected for href in probe['hrefs']), 'search leaked another locale or unpublished page')
        if locale == 'en': require(data['pagefind_bundle'] is True, 'missing Pagefind bundle')
        else: require(data['locale_canonical_preserved'] is True, 'Chinese canonical locale lost')
        counts[locale] = {'published': len(expected), 'search_results': len(probe['hrefs'])}
    return {'website_translation_pairs': 26, 'docs_locales': counts,
            'docs_navigation_links_checked': article_links, 'invented_locale_targets': 0}


def inspect(root, head):
    directory = root / 'artifacts/v10/P20/runtime/t033-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest.get('implementation_commit') == head and set(manifest['artifacts']) == set(PRODUCERS), 'mixed/missing locale collection')
    for key, row in manifest['artifacts'].items():
        require(row['head_sha'] == head and row['workflow'] == PRODUCERS[key][0] and row['run_id'] > 0
                and row['artifact_id'] > 0 and row['archive_sha256'].startswith('sha256:')
                and len(row['archive_sha256']) == 71, 'invalid locale provenance')
    files = manifest['files']
    require(set(files) == {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p != directory / 'collection.json'}, 'untracked locale evidence')
    for path, row in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts and row['producer'] == path.split('/')[0]
                and row['producer'] in PRODUCERS and digest((directory / path).read_bytes()) == row['sha256'], 'locale evidence binding mismatch')
    from t032_case import inspect as inspect_t032
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/consistency/P20-T032.json').read_bytes())
    require(formal.get('case') == 'P20-T032' and formal.get('status') == 'PASS' and formal.get('errors') == []
            and formal.get('implementation_commit') == head and formal['details'] == inspect_t032(prerequisite, head, source_root=root), 'invalid T032 prerequisite')
    seo = prerequisite / 'artifacts/v10/P20/runtime/t032-sources'
    navigation = seo / 'prerequisite/artifacts/v10/P20/runtime/t031-sources'
    def read(path): return json.loads(path.read_bytes())
    graph = read(navigation / 'website/crawl/P19-T017.json')['details']['graph']
    canonical = read(seo / 'docs/seo/P18-T003.json')['documents']
    links = read(navigation / 'website/crawl/P19-T022.json')['details']['published_docs_targets']
    articles = read(navigation / 'docs/navigation/P18-T012.json')['articles']
    searches = {case: read(directory / f'search/search/{case}.json') for case in ('P18-T008', 'P18-T009')}
    return {'formal_p20_t033_claim': True, 'next_case_unlocked': False,
            'prerequisite': {'case': 'P20-T032', 'head': head, 'revalidated': True},
            'locale_discovery': inspect_locales(graph, canonical, links, articles, searches, head),
            'reciprocal_alternates_and_x_default': 'recursively revalidated T032 raw Website and Docs authority',
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def run_case():
    from common import ROOT, HEAD, emit
    errors = []
    details = {'formal_p20_t033_claim': False, 'next_case_unlocked': False}
    try:
        details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        errors.append('T033 admission failed: ' + str(error))
    return emit('P20-T033', 'consistency', 'Website and Docs locale-discovery parity', errors, details)
