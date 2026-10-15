"""Collect exact-head navigation evidence from executed native/static producers."""
import json
import os
import sys
import time
from pathlib import Path, PurePosixPath
from t028_sources import download_archive, verified_archive, member, select_run, successful, require, digest, select_prefixed_artifact

PRODUCERS = {
    'prerequisite': ('p20-t030-consistency.yml', 'p20-t030-formal-', 'P20', None),
    'workspace': ('p12-browser.yml', 'gojet-v10-p12-browser-', 'P20', ['runtime/t027/workspace-browser.json', 'runtime/t027/workspace-browser.jsonl', 'runtime/t031/mail-template.jsonl']),
    'admin': ('p17-browser.yml', 'p17-P17-T031-browser-', 'P17', ['browser/P17-T031.json']),
    'auth': ('p15-browser.yml', 'p15-t024-auth-browser-', 'P15', ['browser/P15-T024.json']),
    'website': ('p19-website-discovery.yml', 'gojet-v10-p19-discovery-', 'P19', ['crawl/P19-T017.json', 'crawl/P19-T022.json']),
    'docs': ('p18-docs-discovery.yml', 'gojet-v10-p18-docs-discovery-', 'P18', ['navigation/P18-T012.json', 'crawl/P18-T013.json']),
    'notifications': ('p20-p0-support.yml', 'p20-t024-availability-', 'P20', ['p0/P20-T022.json']),
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
                require(not run or run['status'] != 'completed', f'T031 producer {key} failed: {run and run.get("conclusion")}')
                pending.append(key)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T031 producer timeout: ' + ','.join(pending))
        print('T031 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t031-sources'
    require(not directory.exists(), 'T031 collection must start empty')
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
            require(path not in manifest['files'], 'duplicate T031 source')
            target = directory / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][path] = {'sha256': digest(content), 'producer': key}
        if key == 'prerequisite':
            for entry in archive.infolist():
                path = PurePosixPath(entry.filename)
                require(not path.is_absolute() and '..' not in path.parts and entry.file_size <= 32 * 1024 * 1024, 'unsafe T030 member')
                if entry.is_dir():
                    continue
                require(entry.filename == 'consistency/P20-T030.json' or entry.filename.startswith('runtime/t030-sources/'), 'unexpected T030 member')
                save('prerequisite/artifacts/v10/P20/' + entry.filename, archive.read(entry))
        else:
            for path in paths:
                save(key + ('/artifacts/v10/P20/' if key == 'workspace' else '/') + path, member(archive, node, path))
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
