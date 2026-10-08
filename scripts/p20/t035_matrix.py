"""Revalidate archived T035 diagnostics; manual WCAG admission remains separate."""
import json
import re
import subprocess
from t028_sources import require, digest
from t034_matrix import inspect_files, inspect as inspect_t034
from t035_sources import PRODUCERS, SIZES, INTERACTIONS, MENU_TRACES, prerequisite_member


def inspect_collection(directory, manifest, head):
    require(manifest['implementation_commit'] == head and set(manifest['artifacts']) == set(PRODUCERS),
            'incomplete/mixed-head producers')
    for key, row in manifest['artifacts'].items():
        require(row['head_sha'] == head and row['workflow'] == PRODUCERS[key][0]
                and row['run_id'] > 0 and row['run_attempt'] > 0 and row['artifact_id'] > 0
                and re.fullmatch(r'sha256:[0-9a-f]{64}', row['archive_sha256']), 'invalid producer binding')
    expected, files = set(), manifest['files']
    for key, (_, _, _, surfaces) in PRODUCERS.items():
        if key == 'prerequisite':
            prefix = 'prerequisite/artifacts/v10/P20/'
            paths = {p for p in files if p.startswith(prefix)}
            require(prefix + 'browser/P20-T034.json' in paths, 'missing T034 authority')
            require(all(prerequisite_member(p[len(prefix):]) for p in paths), 'unexpected T034 evidence')
            expected.update(paths)
            continue
        expected.update(f'{key}/interactions/{case}.json' for case in INTERACTIONS[key])
        for surface in surfaces:
            expected.add(f'{key}/{surface}.json')
            expected.update(f'{key}/{surface}-{size}-{theme}.png' for size in SIZES for theme in ('light', 'dark'))
    expected.update('website/menu-traces/' + name for name in MENU_TRACES)
    inspect_files(directory, files, expected)


def inspect_menu(directory, head):
    data = json.loads((directory / 'website/interactions/P19-T025.json').read_bytes())
    rows = data['details']['mobileMenu']
    pairs = {(size, locale) for size in ('mobile', 'compact320') for locale in ('en', 'zh-CN')}
    require(len(rows) == 4 and {(r['viewport'], r['locale']) for r in rows} == pairs, 'incomplete menu matrix')
    for row in rows:
        require(all(row.get(key) is True for key in ('enter_open', 'modal', 'forward_and_reverse_contained',
            'escape_close', 'close_button', 'trigger_focus_returned', 'link_navigation'))
            and row.get('rapid_reopen_cycles') == 5 and row.get('keyboard_controls') == 6,
            'native menu interaction failed')
        path = directory / f"website/menu-traces/T025-menu-{row['viewport']}-{row['locale']}.json"
        trace = json.loads(path.read_bytes())
        require(trace['implementation_commit'] == head and trace['errors'] == []
                and trace['viewport'] == row['viewport'] and trace['locale'] == row['locale'], 'invalid menu trace')
        steps = trace['steps']
        require(len(steps) == 24 and [s['key'] for s in steps] == ['Tab'] * 12 + ['Shift+Tab'] * 12,
                'incomplete menu keyboard trace')
        for offset in (0, 12):
            require({s['index'] for s in steps[offset:offset+12]} == set(range(6)), 'menu controls not covered')
        require(all(s['inside'] is True and s['modal'] is True and s['document_has_focus'] is True
                    and s['tag'] in ('BUTTON', 'A') for s in steps), 'menu focus escaped')
    return {'variants': 4, 'raw_tab_steps': 96, 'rapid_reopen_cycles': 20}


def inspect(root, head, source_root=None):
    source_root = source_root or root
    directory = root / 'artifacts/v10/P20/runtime/t035-sources'
    manifest_path = directory / 'collection.json'
    manifest = json.loads(manifest_path.read_bytes())
    inspect_collection(directory, manifest, head)
    for key, cases in INTERACTIONS.items():
        for case in cases:
            data = json.loads((directory / key / 'interactions' / (case + '.json')).read_bytes())
            case_field = 'case_id' if key in ('workspace', 'auth') else 'case'
            head_field = 'exact_head' if key == 'admin' else 'implementation_commit'
            # P18 emits direct assertion records without an errors array.
            require(data.get(case_field) == case and data.get(head_field) == head
                    and data.get('status') == 'PASS'
                    and (data.get('errors', []) == [] if key == 'docs' else data.get('errors') == []),
                    'invalid native interaction evidence: ' + case)
    menu = inspect_menu(directory, head)
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/browser/P20-T034.json').read_bytes())
    details = inspect_t034(prerequisite, head, source_root=source_root)
    details.update(formal_p20_t034_claim=True, next_case_unlocked=False,
        scope='six native product surfaces; input/error/code-sent/verified auth states; separate P03 component authority')
    require(formal.get('case') == 'P20-T034' and formal.get('status') == 'PASS'
            and formal.get('errors') == [] and formal.get('implementation_commit') == head
            and formal.get('details') == details, 'invalid T034 prerequisite')
    paths = [str(directory / key / (surface + '.json'))
             for key, (_, _, _, surfaces) in PRODUCERS.items() if surfaces for surface in surfaces]
    result = subprocess.run(['node', str(source_root / 'scripts/p20/t035_audit.mjs'), head, *paths],
                            cwd=source_root, text=True, capture_output=True, check=True)
    diagnostics = json.loads(result.stdout)
    require(diagnostics['head'] == head and diagnostics['observations'] == 72
            and diagnostics['formal_p20_t035_claim'] is False, 'invalid native audit result')
    diagnostics['outstanding'] = ['interactive and manual WCAG review']
    return {'menu_interactions': menu, 'diagnostics': diagnostics, 'collection_sha256': digest(manifest_path.read_bytes()),
            'prerequisite': {'case': 'P20-T034', 'head': head, 'revalidated': True},
            'native_interaction_cases': [case for cases in INTERACTIONS.values() for case in cases],
            'formal_p20_t035_claim': False, 'next_case_unlocked': False}


if __name__ == '__main__':
    from common import ROOT, HEAD
    result = {'implementation_commit': HEAD, 'formal_p20_t035_claim': False, 'errors': []}
    try:
        result['details'] = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError) as error:
        result['errors'].append(str(error))
    result['status'] = 'FAIL' if result['errors'] else 'PASS'
    output = ROOT / 'artifacts/v10/P20/runtime/t035-matrix/result.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2) + '\n')
    raise SystemExit(bool(result['errors']))
