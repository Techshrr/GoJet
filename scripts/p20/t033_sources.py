"""Collect exact-head navigation evidence from executed native/static producers."""
import json
import os
import sys
import time
from pathlib import Path, PurePosixPath
from t028_sources import download_archive, verified_archive, member, select_run, successful, require, digest, select_prefixed_artifact

PRODUCERS = {
    'prerequisite': ('p20-t032-consistency.yml', 'p20-t032-formal-', 'P20', None),
    'search': ('p18-docs-discovery.yml', 'gojet-v10-p18-docs-discovery-', 'P18', ['search/P18-T008.json', 'search/P18-T009.json']),
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
                require(not run or run['status'] != 'completed', f'T033 producer {key} failed: {run and run.get("conclusion")}')
                pending.append(key)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T033 producer timeout: ' + ','.join(pending))
        print('T033 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t033-sources'
    require(not directory.exists(), 'T033 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
    for key, (workflow, prefix, node, paths) in PRODUCERS.items():
        run = selected[key]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'truncated artifact list')
        # Some predecessor workflows name uploads with the PR merge SHA.
        # Bind by workflow run and verified artifact head/digest, never by filename SHA.
        artifact = select_prefixed_artifact(rows, prefix, run)
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][key] = {'workflow': workflow, 'head_sha': head, 'run_id': run['id'],
                                     'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        def save(path, content):
            require(path not in manifest['files'], 'duplicate T033 source')
            target = directory / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][path] = {'sha256': digest(content), 'producer': key}
        if key == 'prerequisite':
            for entry in archive.infolist():
                path = PurePosixPath(entry.filename)
                require(not path.is_absolute() and '..' not in path.parts and entry.file_size <= 32 * 1024 * 1024, 'unsafe T032 member')
                if entry.is_dir():
                    continue
                require(entry.filename == 'consistency/P20-T032.json' or entry.filename.startswith('runtime/t032-sources/'), 'unexpected T032 member')
                save('prerequisite/artifacts/v10/P20/' + entry.filename, archive.read(entry))
        else:
            for path in paths:
                save(key + '/' + path, member(archive, node, path))
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
