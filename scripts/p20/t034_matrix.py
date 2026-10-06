"""Admit native theme observations, without claiming full T034 visual closure."""
import hashlib
import json
import re
from pathlib import Path
from t028_sources import require, digest
from t034_sources import PRODUCERS


def color(value):
    value = value.strip().lower()
    return '#' + ''.join(c * 2 for c in value[1:]) if re.fullmatch(r'#[0-9a-f]{3}', value) else value


def inspect_surface(data, surface, node, head, css, viewports, captures):
    require(data['implementation_commit'] == head and data['surface'] == surface and data['node'] == node,
            'wrong surface or implementation')
    require(data['status'] == 'PASS' and data['errors'] == [], 'native surface failed')
    require(data['token_css_sha256'] == hashlib.sha256(css.encode()).hexdigest(), 'token source drift')
    rows = data['observations']
    expected_pairs = {(size, theme) for size in ('desktop', 'mobile') for theme in ('light', 'dark')}
    require(len(rows) == 4 and {(r['size'], r['theme']) for r in rows} == expected_pairs, 'incomplete/duplicate visual matrix')
    for row in rows:
        theme = row['theme']
        block = css.split(':root {', 1)[1].split('}', 1)[0] if theme == 'light' else css.split(':root[data-theme="dark"] {', 1)[1].split('}', 1)[0]
        for name in ('--gojet-surface-canvas', '--gojet-surface-default', '--gojet-text-primary', '--gojet-text-secondary'):
            expected = re.search(re.escape(name) + r':\s*([^;]+);', block).group(1)
            require(color(row['tokens'][name]) == color(expected), 'noncanonical semantic color')
        require(row['viewport'] == viewports[row['size']], 'noncanonical viewport')
        require(row['reduced_motion'] is True and row['active_animations'] == 0, 'persistent reduced-mode animation')
        require(row['body']['margin'] == '0px' and row['browser_default_links'] == 0, 'unthemed document defaults')
        require(row['overflow'] is False and row['broken_images'] == 0 and row['placeholder_elements'] == 0,
                'invalid layout/image observations')
        name = f"{surface}-{row['size']}-{theme}.png"
        require(row['capture'] == name and hashlib.sha256(captures[name]).hexdigest() == row['capture_sha256'], 'capture hash mismatch')
        require(captures[name].startswith(b'\x89PNG\r\n\x1a\n'), 'capture is not PNG')
    return len(rows)


def inspect(root, head):
    directory = root / 'artifacts/v10/P20/runtime/t034-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest['implementation_commit'] == head and set(manifest['artifacts']) == set(PRODUCERS), 'incomplete/mixed-head producers')
    for key, row in manifest['artifacts'].items():
        require(row['head_sha'] == head and row['workflow'] == PRODUCERS[key][0] and row['run_id'] > 0
                and row['artifact_id'] > 0 and re.fullmatch(r'sha256:[0-9a-f]{64}', row['archive_sha256']), 'invalid producer binding')
    files = manifest['files']
    expected = set()
    for key, (_, _, _, surfaces) in PRODUCERS.items():
        if key == 'foundation':
            expected.update(key + '/' + path for path in surfaces)
            continue
        for surface in surfaces:
            expected.add(f'{key}/{surface}.json')
            expected.update(f'{key}/{surface}-{size}-{theme}.png' for size in ('desktop', 'mobile') for theme in ('light', 'dark'))
    require(set(files) == expected, 'unexpected or missing evidence file')
    require(expected == {str(p.relative_to(directory)) for p in directory.rglob('*') if p.is_file() and p.name != 'collection.json'}, 'untracked raw evidence')
    for path, row in files.items():
        require(row['producer'] == path.split('/')[0] and digest((directory / path).read_bytes()) == row['sha256'], 'member digest mismatch')
    css = (root / 'frontend/packages/tokens/generated/tokens.css').read_text()
    tokens = json.loads((root / 'frontend/packages/tokens/generated/design-variables.json').read_text())['tokens']['composite']
    viewports = {size: dict(zip(('width', 'height'), map(int, tokens['viewport.' + size]['dimensions'].split('×')))) for size in ('desktop', 'mobile')}
    surfaces = {}
    for key, (_, _, node, names) in PRODUCERS.items():
        if key == 'foundation': continue
        for name in names:
            data = json.loads((directory / key / (name + '.json')).read_bytes())
            captures = {p.name: p.read_bytes() for p in (directory / key).glob(name + '-*.png')}
            surfaces[name] = inspect_surface(data, name, node, head, css, viewports, captures)
    from t034_foundation import inspect as inspect_foundation
    foundation = inspect_foundation(directory / 'foundation', root, head)
    return {'foundation': foundation, 'surfaces': surfaces, 'observations': sum(surfaces.values()), 'formal_p20_t034_claim': False,
            'remaining_authority': ['native state/icon/image coverage', 'formal predecessor admission'],
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


if __name__ == '__main__':
    from common import ROOT, HEAD
    result = {'implementation_commit': HEAD, 'formal_p20_t034_claim': False, 'errors': []}
    try:
        result['details'] = inspect(ROOT, HEAD)
    except (OSError, ValueError, KeyError, TypeError) as error:
        result['errors'].append(str(error))
    result['status'] = 'FAIL' if result['errors'] else 'PASS'
    output = ROOT / 'artifacts/v10/P20/runtime/t034-matrix/result.json'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2) + '\n')
    raise SystemExit(bool(result['errors']))
