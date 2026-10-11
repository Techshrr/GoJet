"""Frozen T036 driver: independently recompute the three performance authorities."""
import json
import math
import re
from urllib.parse import urlsplit
from t028_sources import require, digest
from t034_matrix import inspect_files
from t036_sources import PRODUCERS, GATES

ANALYTICS_BUDGET = {'dom_content_loaded_ms': 3000, 'load_event_ms': 5000,
                    'resource_bytes': 1500000, 'resource_count': 60}


def number(value, label, minimum=0, maximum=None):
    require(type(value) in (int, float) and math.isfinite(value) and value >= minimum
            and (maximum is None or value <= maximum), 'invalid/over-budget ' + label)
    return value


def passed(data, case, head=None):
    require(data.get('case', data.get('case_id')) == case and data.get('status') == 'PASS'
            and data.get('errors') == [], 'failed/missing performance case: ' + case)
    if head is not None:
        require(data.get('implementation_commit') == head, 'stale performance case: ' + case)
    return data['details']


def inspect_shell(source, bundle, shift, coherence, report, gate, head):
    require(source.get('repository') == 'Techshrr/GoJet' and source.get('implementation_commit') == head,
            'stale shell authority')
    b = passed(bundle, 'P04-T006')
    require(b == gate and b['status'] == 'PASS' and b['errors'] == []
            and b['forbidden_product_dependencies'] == ['@gojet/workspace', '@gojet/admin'],
            'shell isolation/gate disagreement')
    files = b['site_bundles']
    require(files and len({r['name'] for r in files}) == len(files), 'empty/duplicate shell bundle inventory')
    total = sum(number(r['bytes'], 'shell bundle bytes', 1) for r in files)
    require(total == b['site_total_js_bytes'], 'incorrect shell bundle total')
    require(all(not re.search(r'(?:workspace|admin)(?:[-./]|$)', r['name'], re.I) for r in files),
            'product bundle in Website inventory')
    s = passed(shift, 'P04-T007')
    require(s['threshold'] == .02 and all(s[k] == [] for k in (
        'console_errors', 'page_errors', 'viewport_shift_findings', 'route_shift_findings')),
        'shell layout/error regression')
    rows = report['targets'] + report['route_transitions']
    require(len(report['targets']) == 18 and len(report['route_transitions']) == 3,
            'incomplete shell browser matrix')
    require(all(report[k] == [] for k in ('console_errors', 'page_errors', 'http_errors', 'request_failures')),
            'shell browser diagnostics failed')
    for row in rows: number(row['layoutShift'], 'shell layout shift', maximum=.02)
    c = passed(coherence, 'P04-T010')['g4']
    require(c['status'] == 'PASS' and c['implementation_commit'] == head
            and c['capture_count'] == len(report['captures']) and c['capture_count'] >= 19
            and c['spa_transition_count'] == 3 and c['console_error_count'] == c['page_error_count'] == 0,
            'shell coherence mismatch')
    return {'bundle_bytes': total, 'layout_observations': len(rows), 'layout_shift_max': .02}


def inspect_analytics(case, gate, head):
    d = passed(case, 'P07-T019', head)
    require(d['performance_budget'] == ANALYTICS_BUDGET, 'analytics budget drift')
    for key, maximum in ANALYTICS_BUDGET.items():
        number(d['performance'][key], key, maximum=maximum)
    require(all(d['diagnostics'][key] == [] for key in (
        'console_errors', 'page_errors', 'http_errors', 'request_failures')), 'analytics browser errors')
    require(gate['status'] == 'PASS' and gate['implementation_commit'] == head
            and gate['budget'] == ANALYTICS_BUDGET and gate['observed'] == d['performance']
            and gate['evidence']['no_client_generated_totals'] is True, 'analytics gate disagreement')
    return {'budgets': ANALYTICS_BUDGET, 'observed': d['performance']}


def inspect_website(bundle, cwv, head):
    b = passed(bundle, 'P19-T027', head)
    require(b['budgetBytes'] == 150 * 1024 and b['initialFiles'] and b['dynamicChunks'],
            'Website budget or route splitting absent')
    require(len({r['file'] for r in b['initialFiles']}) == len(b['initialFiles']), 'duplicate initial bundle')
    total = sum(number(r['gzipBytes'], 'initial gzip bytes', 1) for r in b['initialFiles'])
    require(total == b['initialGzipBytes'] and total <= 150 * 1024, 'Website initial JS budget exceeded')
    loaded = b['loadedScriptUrls']
    require(loaded and b['forbiddenBundleRequests'] == [], 'Website script inventory absent/forbidden')
    origins = {(urlsplit(u).scheme, urlsplit(u).netloc) for u in loaded}
    require(len(origins) == 1 and all(urlsplit(u).scheme in ('http', 'https') for u in loaded),
            'remote/invalid Website script origin')
    require(all(not re.search(r'(?:workspace|admin)(?:[-./]|$)', urlsplit(u).path, re.I) for u in loaded),
            'Website loaded product bundle')
    d = passed(cwv, 'P19-T028', head)
    profile = d['labProfile']
    require(all(profile[k] == v for k, v in {'viewport': '1440x900', 'latencyMs': 40,
            'downloadMbps': 10, 'uploadMbps': 5}.items()) and profile['browser'], 'frozen lab profile drift')
    number(d['lcpMs'], 'LCP', minimum=.001, maximum=2500)
    number(d['cls'], 'CLS', maximum=.1)
    entries = d['eventEntries']
    require(entries and all(e['entryType'] in ('event', 'first-input') and e['interactionId'] > 0
            for e in entries), 'trusted interaction evidence absent')
    observed_inp = max(number(e['duration'], 'event duration', maximum=200) for e in entries)
    require(d['inpMs'] == observed_inp, 'INP differs from native Event Timing')
    n = d['nativePerformance']
    require({'longtask', 'largest-contentful-paint', 'layout-shift', 'event', 'first-input'}
            <= set(n['supportedEntryTypes']), 'required performance observer unsupported')
    require(n['longTasks'] == [], 'long main-thread task regression')
    require(n['observationStartMs'] == 0 and len(n['navigation']) == 1, 'missing startup observation')
    nav = n['navigation'][0]
    require(nav['startTime'] == 0 and 0 < nav['domContentLoadedEventEnd'] <= nav['loadEventEnd']
            < n['observedUntilMs'], 'incomplete native observation window')
    number(n['observedUntilMs'], 'observation duration', .001)
    images = n['renderedImages']
    require(len(images) == d['renderedPrincipalImageCount'], 'image inventory mismatch')
    for image in images:
        require(image['complete'] is True and float(image['widthAttribute']) > 0
                and float(image['heightAttribute']) > 0 and image['naturalWidth'] > 0
                and image['naturalHeight'] > 0, 'image dimensions/decode missing')
        require((urlsplit(image['src']).scheme, urlsplit(image['src']).netloc) in origins,
                'remote principal image')
        require(all(any(s['type'] == kind and s['srcset'] and s['sizes'] for s in image['pictureSources'])
                    for kind in ('image/avif', 'image/webp')), 'image responsive alternatives missing')
    resources = n['resources']
    fonts = [r for r in resources if r['initiatorType'] == 'font'
             or re.search(r'\.(?:woff2?|ttf|otf)(?:\?|$)', r['name'], re.I)]
    require(fonts == d['fontResources'], 'font resource inventory mismatch')
    # Current Website intentionally uses local/system fonts. A future network
    # font needs explicit subset/header evidence rather than a permissive PASS.
    require(fonts == [] and n['fontFaces'] == [], 'network/font-face strategy requires subset/header review')
    cache = d['cache']
    require(cache['hashedAsset'] in {r['file'] for r in b['initialFiles']}, 'cache probe not an initial asset')
    require(re.search(r'(?:^|,)\s*public\s*(?:,|$)', cache['hashedCache'], re.I)
            and re.search(r'max-age=31536000\b', cache['hashedCache'], re.I)
            and re.search(r'\bimmutable\b', cache['hashedCache'], re.I), 'hashed cache budget failed')
    require(cache['socialCard'] == '/assets/social/gojet-en.png'
            and re.search(r'max-age=86400\b', cache['socialCache'], re.I)
            and not re.search(r'\bimmutable\b', cache['socialCache'], re.I), 'stable image cache budget failed')
    return {'initial_js_gzip_bytes': total, 'lcp_ms': d['lcpMs'], 'inp_ms': observed_inp,
            'cls': d['cls'], 'long_tasks': 0, 'principal_images': len(images),
            'network_fonts': len(fonts), 'lab_profile': profile,
            'field_data': 'unavailable: CI lab profile, not production telemetry'}


def inspect(root, head):
    directory = root / 'artifacts/v10/P20/runtime/t036-sources'
    manifest_path = directory / 'collection.json'
    manifest = json.loads(manifest_path.read_bytes())
    require(manifest['implementation_commit'] == head and set(manifest['artifacts']) == set(PRODUCERS)
            and set(manifest['gates']) == set(GATES), 'incomplete/mixed performance sources')
    expected = {f'{key}/{node}/{path}' for key, (_, _, paths) in PRODUCERS.items() for node, path in paths}
    inspect_files(directory, manifest['files'], expected)
    for key, row in manifest['artifacts'].items():
        require(row['head_sha'] == head and row['workflow'] == PRODUCERS[key][0] and row['run_id'] > 0
                and row['run_attempt'] > 0 and row['artifact_id'] > 0
                and re.fullmatch(r'sha256:[0-9a-f]{64}', row['archive_sha256']), 'invalid producer binding')
    for row in manifest['gates'].values():
        require(row['head_sha'] == head and row['id'] > 0 and row['conclusion'] == 'success', 'failed/stale gate')
    read = lambda path: json.loads((directory / path).read_bytes())
    require((directory / 'website/P19/quality-authority/implementation_commit.txt').read_text().strip() == head,
            'Website quality authority mismatch')
    shell = inspect_shell(read('shell/P04/source.json'), read('shell/P04/results/P04-T006.json'),
        read('shell/P04/results/P04-T007.json'), read('shell/P04/results/P04-T010.json'),
        read('shell/P04/browser/browser-report.json'), read('shell/gates/G9/performance/p04-shell-bundle.json'), head)
    analytics = inspect_analytics(read('analytics/P07/results/P07-T019.json'), read('analytics/gates/G9/p07-analytics.json'), head)
    website = inspect_website(read('website/P19/performance/P19-T027.json'), read('website/P19/performance/P19-T028.json'), head)
    return {'shell': shell, 'analytics': analytics, 'website': website,
            'collection_sha256': digest(manifest_path.read_bytes()), 'formal_p20_t036_claim': True,
            'next_case_unlocked': False, 'scope': 'same-head P04/P07/P19 performance; no release-wide closure claim'}


if __name__ == '__main__':
    from common import ROOT, HEAD, emit
    errors, details = [], {'formal_p20_t036_claim': False, 'next_case_unlocked': False}
    try: details = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError) as error: errors.append(str(error))
    result = emit('P20-T036', 'performance', 'Release-wide performance and bundle isolation', errors, details)
    raise SystemExit(result['status'] != 'PASS')
