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


def contrast(foreground, background):
    def luminance(value):
        match = re.fullmatch(r'rgb\((\d+),\s*(\d+),\s*(\d+)\)', value)
        require(match is not None, 'nonopaque notice color')
        channels = [int(c) / 255 for c in match.groups()]
        channels = [c / 12.92 if c <= .04045 else ((c + .055) / 1.055) ** 2.4 for c in channels]
        return sum(c * weight for c, weight in zip(channels, (.2126, .7152, .0722)))
    a, b = sorted((luminance(foreground), luminance(background)))
    return (b + .05) / (a + .05)


AUTH_STATES = {'auth': ('input', None), 'auth-invalid': ('invalid', 'error'),
               'auth-code-sent': ('code-sent', 'success'), 'auth-verified': ('success', 'success')}


def inspect_assets_and_states(row, surface):
    images = row['images']
    require(isinstance(images, list), 'missing native image inventory')
    for image in images:
        require(image['alt_present'] is True and image['width'] > 0 and image['height'] > 0,
                'image missing alt or intrinsic dimensions')
        require(image['complete'] is True and image['natural_width'] > 0 and image['natural_height'] > 0
                and image['same_origin'] is True, 'broken or hotlinked image')
        require(not image['responsive'] or image['sizes'], 'responsive image missing sizes')
        require(image['priority'] != 'high' or image['loading'] != 'lazy', 'lazy high-priority image')
    require(sum(image['priority'] == 'high' for image in images) <= 1, 'multiple high-priority images')
    if surface not in AUTH_STATES: return
    state, tone = AUTH_STATES[surface]
    require(row['auth_state'] == state, 'native auth state mismatch')
    notices = row['notices']
    require(len(notices) == (1 if tone else 0), 'missing/unexpected auth notice')
    if not tone: return
    notice = notices[0]; icon = notice['icon']
    require(notice['tone'] == tone and notice['has_text'] is True
            and notice['role'] == ('alert' if tone == 'error' else 'status'), 'missing semantic state text')
    require(icon is not None and icon['hidden'] == 'true' and icon['view_box'] == '0 0 24 24'
            and ('lucide-circle-alert' if tone == 'error' else 'lucide-circle-check') in icon['classes'],
            'missing canonical state icon')
    require(icon['width'] == icon['height'] == 16 and icon['stroke'] == '1.75px', 'noncanonical state icon geometry')
    require(contrast(notice['foreground'], notice['background']) >= 4.5, 'state text contrast failed')
    require(tone != 'error' or notice['focused'] is True, 'error notice lost focus')


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
        inspect_assets_and_states(row, surface)
        name = f"{surface}-{row['size']}-{theme}.png"
        require(row['capture'] == name and hashlib.sha256(captures[name]).hexdigest() == row['capture_sha256'], 'capture hash mismatch')
        require(captures[name].startswith(b'\x89PNG\r\n\x1a\n'), 'capture is not PNG')
    return len(rows)


def inspect_files(directory, files, expected):
    require(set(files) == expected, 'unexpected or missing evidence file')
    # Only this collection's own manifest is outside its member inventory.
    # Nested prerequisite manifests are evidence and must remain accounted for.
    actual = {str(p.relative_to(directory)) for p in directory.rglob('*')
              if p.is_file() and p != directory / 'collection.json'}
    require(expected == actual, 'untracked raw evidence')
    for path, row in files.items():
        require(not Path(path).is_absolute() and '..' not in Path(path).parts, 'unsafe evidence member')
        require(row['producer'] == path.split('/')[0] and digest((directory / path).read_bytes()) == row['sha256'], 'member digest mismatch')


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
        if key == 'prerequisite':
            prefix = 'prerequisite/artifacts/v10/P20/'
            prerequisite_files = {path for path in files if path.startswith(prefix)}
            require(prefix + 'consistency/P20-T033.json' in prerequisite_files, 'missing T033 formal result')
            require(all(path == prefix + 'consistency/P20-T033.json' or path.startswith(prefix + 'runtime/t033-sources/')
                        for path in prerequisite_files), 'unexpected prerequisite evidence')
            expected.update(prerequisite_files)
            continue
        if key == 'foundation':
            expected.update(key + '/' + path for path in surfaces)
            continue
        for surface in surfaces:
            expected.add(f'{key}/{surface}.json')
            expected.update(f'{key}/{surface}-{size}-{theme}.png' for size in ('desktop', 'mobile') for theme in ('light', 'dark'))
    inspect_files(directory, files, expected)
    css = (root / 'frontend/packages/tokens/generated/tokens.css').read_text()
    tokens = json.loads((root / 'frontend/packages/tokens/generated/design-variables.json').read_text())['tokens']['composite']
    viewports = {size: dict(zip(('width', 'height'), map(int, tokens['viewport.' + size]['dimensions'].split('×')))) for size in ('desktop', 'mobile')}
    surfaces = {}
    for key, (_, _, node, names) in PRODUCERS.items():
        if key in ('foundation', 'prerequisite'): continue
        for name in names:
            data = json.loads((directory / key / (name + '.json')).read_bytes())
            captures = {p.name: p.read_bytes() for p in (directory / key).glob(name + '-*.png')}
            surfaces[name] = inspect_surface(data, name, node, head, css, viewports, captures)
    from t034_foundation import inspect as inspect_foundation
    foundation = inspect_foundation(directory / 'foundation', root, head)
    from t033_case import inspect as inspect_t033
    prerequisite = directory / 'prerequisite'
    formal = json.loads((prerequisite / 'artifacts/v10/P20/consistency/P20-T033.json').read_bytes())
    require(formal.get('case') == 'P20-T033' and formal.get('status') == 'PASS' and formal.get('errors') == []
            and formal.get('implementation_commit') == head
            and formal['details'] == inspect_t033(prerequisite, head, source_root=root), 'invalid T033 prerequisite')
    return {'foundation': foundation, 'surfaces': surfaces, 'observations': sum(surfaces.values()), 'formal_p20_t034_claim': False,
            'prerequisite': {'case': 'P20-T033', 'head': head, 'revalidated': True},
            'native_state_coverage': sorted(AUTH_STATES), 'native_image_inventory_checked': True,
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
