"""Collect exact-head navigation evidence from executed native/static producers."""
import json
import os
import sys
import time
from pathlib import Path, PurePosixPath
from t028_sources import download_archive, verified_archive, member, select_run, successful, require, digest, select_artifact

from t034_foundation import paths as foundation_paths

PRODUCERS = {
    'foundation': ('p03-design-system.yml', 'gojet-v10-p03-', 'P03', foundation_paths()),
    'website': ('p19-website-browser.yml', 'gojet-v10-p19-browser-', 'P19', ['website']),
    'docs': ('p18-docs-quality.yml', 'gojet-v10-p18-docs-quality-', 'P18', ['docs']),
    'auth': ('p15-browser.yml', 'p15-t024-auth-browser-', 'P15', ['auth']),
    'workspace': ('p10-browser.yml', 'gojet-v10-p10-browser-', 'P10', ['workspace', 'public']),
    'admin': ('p17-browser.yml', 'p17-P17-T030-browser-', 'P17', ['admin']),
}


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
                require(not run or run['status'] != 'completed', f'T034 producer {key} failed: {run and run.get("conclusion")}')
                pending.append(key)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T034 producer timeout: ' + ','.join(pending))
        print('T034 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t034-sources'
    require(not directory.exists(), 'T034 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
    for key, (workflow, prefix, node, paths) in PRODUCERS.items():
        run = selected[key]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'truncated artifact list')
        # Some predecessor workflows name uploads with the PR merge SHA.
        # Bind by workflow run and verified artifact head/digest, never by filename SHA.
        names = {a['name'] for a in rows if a['name'].startswith(prefix) and a.get('expired') is False}
        require(len(names) == 1, 'T034 artifact name absent or ambiguous: ' + key)
        artifact = select_artifact(rows, names.pop(), run)
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][key] = {'workflow': workflow, 'head_sha': head, 'run_id': run['id'],
                                     'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        def save(path, content):
            require(path not in manifest['files'], 'duplicate T034 source')
            target = directory / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][path] = {'sha256': digest(content), 'producer': key}
        if key == 'foundation':
            for path in paths:
                save(key + '/' + path, member(archive, node, path))
            continue
        for surface in paths:
            save(key + '/' + surface + '.json', member(archive, node, 't034/' + surface + '.json'))
            for size in ('desktop', 'mobile'):
                for theme in ('light', 'dark'):
                    name = f'{surface}-{size}-{theme}.png'
                    save(key + '/' + name, member(archive, node, 't034/' + name))
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
