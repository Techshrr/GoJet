"""Admit separate P03 component authority; never substitute it for native pages."""
import hashlib
import json
from itertools import product
from pathlib import Path
from t028_sources import require

MATRIX = list(product(('light', 'dark'), ('en', 'zh-cn'), ('desktop', 'tablet', 'mobile')))
TOKEN_FILES = {'frontend/packages/tokens/' + name for name in ('src/tokens.json', 'generated/design-variables.json', 'generated/responsive.css', 'generated/tokens.css', 'generated/tokens.ts')}

def stem(theme, locale, viewport):
    return f'components/gjv10__workspace__p03-components__default__{theme}__{locale}__{viewport}'

def paths():
    return ['source.json'] + [f'results/P03-T{i:03}.json' for i in range(1, 11)] + [stem(*row) + suffix for row in MATRIX for suffix in ('.png', '__manifest.json')]

def inspect(directory, root, head):
    def read(path): return json.loads((directory / path).read_bytes())
    require(read('source.json')['implementation_commit'] == head, 'foreign P03 source')
    results = {}
    for i in range(1, 11):
        case = f'P03-T{i:03}'; data = read('results/' + case + '.json')
        require(data['case_id'] == case and data['status'] == 'PASS' and data['errors'] == [], 'P03 case failed: ' + case)
        results[i] = data['details']
    hashes = results[1]['artifact_hashes']
    require(set(hashes) == TOKEN_FILES and results[1]['git_diff_bytes'] == 0, 'incomplete canonical token authority')
    for path, sha in hashes.items():
        require(hashlib.sha256((root / path).read_bytes()).hexdigest() == sha, 'P03 generated token drift')
    require(results[2]['bad_name_count'] == 0 and results[2]['token_count'] > 0, 'invalid token schema')
    require(results[3]['finding_count'] == 0 and results[3]['sample'] == [] and results[3]['scanned_files'] > 0, 'raw visual values detected')
    require(results[4]['incomplete_count'] == 0 and results[4]['themed_token_count'] > 0, 'incomplete theme mapping')
    require(results[5]['focus_visible_css'] is True and results[5]['missing_keys'] == [] and results[5]['positive_tabindex'] == [] and all(results[5]['aria_markers'].values()), 'invalid component focus semantics')
    require(results[6]['failures'] == 0 and results[6]['evaluated_pairs'] >= 26, 'component contrast failed')
    require(set(results[7]['icons']) == {'CircleAlert', 'CheckCircle2', 'AlertTriangle', 'Info'} and set(results[7]['categories']) == {'controls', 'data', 'feedback', 'layout', 'navigation', 'overlay'} and results[7]['component_count'] >= 20, 'missing component state/icon coverage')
    require(set(results[8]['density_modes']) == {'compact', 'default', 'relaxed'}, 'missing density modes')
    require(results[9]['continuous_animation_findings'] == [], 'continuous component animation found')
    captures = results[10]['captures']
    require(results[10]['capture_count'] == len(captures) == 12 and {(r['theme'], r['locale'], r['viewport']) for r in captures} == set(MATRIX), 'incomplete component capture matrix')
    for row in captures:
        base = stem(row['theme'], row['locale'], row['viewport'])
        require(row['path'] == 'artifacts/v10/P03/' + base + '.png' and row['result'] == 'pass', 'wrong component capture path/result')
        png = (directory / (base + '.png')).read_bytes()
        require(png.startswith(b'\x89PNG\r\n\x1a\n') and len(png) == row['bytes'], 'missing/corrupt component capture')
        metadata = read(base + '__manifest.json')
        require(metadata['implementation_commit'] == head and metadata['result'] == 'pass' and metadata['theme'] == row['theme'] and metadata['locale'] == row['locale'] and metadata['viewport_token'] == 'viewport.' + row['viewport'], 'mixed component capture provenance')
    return {'cases': 10, 'component_captures': 12, 'component_categories': sorted(results[7]['categories']), 'state_icons': sorted(results[7]['icons']), 'native_page_substitute': False}
