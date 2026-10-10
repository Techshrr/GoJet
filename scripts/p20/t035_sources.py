"""Collect same-head accessibility observations and the complete T034 authority."""
import json
import os
import sys
import time
from pathlib import Path, PurePosixPath
from t028_sources import (download_archive, verified_archive, member, select_run,
                          successful, require, digest, select_prefixed_artifact)
from t034_sources import PRODUCERS as VISUAL_PRODUCERS

PRODUCERS = {key: value for key, value in VISUAL_PRODUCERS.items()
             if key not in ('prerequisite', 'foundation')}
PRODUCERS['prerequisite'] = ('p20-t034-matrix.yml', 'p20-t034-native-matrix-', 'P20', None)
SIZES = ('desktop', 'tablet', 'mobile', 'reflow320', 'zoom200', 'textspacing320')
MENU_TRACES = [f'T025-menu-{size}-{locale}.json' for size in ('mobile', 'compact320') for locale in ('en', 'zh-CN')]
INTERACTIONS = {
    'website': ['P19-T025'], 'docs': ['P18-T019', 'P18-T020', 'P18-T021'],
    'auth': ['P15-T024'], 'workspace': ['P10-T016', 'P10-T017', 'P10-T018'],
    'admin': ['P17-T030'],
}


def prerequisite_member(name):
    path = PurePosixPath(name)
    require(not path.is_absolute() and '..' not in path.parts and str(path) == name,
            'unsafe T034 member')
    return name == 'browser/P20-T034.json' or name.startswith('runtime/t034-sources/')


def save_archive(directory, manifest, key, archive):
    def save(path, content):
        require(path not in manifest['files'], 'duplicate T035 source')
        target = directory / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(content)
        manifest['files'][path] = {'sha256': digest(content), 'producer': key}
    _, _, node, surfaces = PRODUCERS[key]
    if key == 'prerequisite':
        for entry in archive.infolist():
            if entry.is_dir():
                continue
            include = prerequisite_member(entry.filename)
            require(include or entry.filename.startswith('runtime/t034-matrix/'),
                    'unexpected T034 archive member')
            require(entry.file_size <= 32 * 1024 * 1024, 'oversized T034 member')
            if include:
                save('prerequisite/artifacts/v10/P20/' + entry.filename, archive.read(entry))
        return
    if key == 'website':
        for name in MENU_TRACES:
            save(f'website/menu-traces/{name}', member(archive, node, 'browser/' + name))
    if key == 'workspace':
        save('workspace/interactions/P10-T017-delete-confirmation.png', member(archive, node, 'captures/P10-T017-delete-confirmation.png'))
    if key in ('workspace','admin'):
        for theme in ('light','dark'):
            name = f'{key}-navigation320-{theme}.png'
            save(f'{key}/{name}', member(archive, node, 't035/' + name))
    for case in INTERACTIONS[key]:
        save(f'{key}/interactions/{case}.json', member(archive, node, f'browser/{case}.json'))
    for surface in surfaces:
        save(f'{key}/{surface}.json', member(archive, node, f't035/{surface}.json'))
        for size in SIZES:
            for theme in ('light', 'dark'):
                name = f'{surface}-{size}-{theme}.png'
                save(f'{key}/{name}', member(archive, node, 't035/' + name))


def collect(root, head):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from ci.actions import github_json, workflow_runs
    repo = os.environ['GITHUB_REPOSITORY']
    base = 'https://api.github.com/repos/' + repo
    headers = {'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json'}
    api = lambda url: github_json(url, headers)
    deadline = time.monotonic() + 180 * 60
    while True:
        runs = workflow_runs(api, repo, head)
        selected, pending = {}, []
        for key, (workflow, _, _, _) in PRODUCERS.items():
            run = select_run(runs, workflow, head, repo)
            if run and successful(run, None, []):
                selected[key] = run
            else:
                pending.append(key)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T035 producer timeout: ' + ','.join(pending))
        print('T035 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t035-sources'
    require(not directory.exists(), 'T035 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
    for key, (workflow, prefix, _, _) in PRODUCERS.items():
        run = selected[key]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'truncated artifact list')
        artifact = select_prefixed_artifact(rows, prefix, run)
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][key] = {'workflow': workflow, 'head_sha': head,
            'run_id': run['id'], 'run_attempt': run.get('run_attempt', 1),
            'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        save_archive(directory, manifest, key, archive)
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
