"""Collect exact-head HTTP evidence from executed native/static HTTP producers."""
import json
import os
import sys
import time
from pathlib import Path, PurePosixPath
from t028_sources import download_archive, verified_archive, member, select_run, successful, require, digest

PRODUCERS = {
    'prerequisite': ('p20-t029-consistency.yml', 'p20-t029-formal-', 'P20', None),
    'native': ('p12-browser.yml', 'gojet-v10-p12-browser-', 'P20', ['runtime/t030/http-status.jsonl', 'runtime/t030/native-manifest.json']),
    'website': ('p19-website-core.yml', 'gojet-v10-p19-site-core-', 'P19', ['site-core/P19-T005.json']),
    'docs': ('p18-docs-core.yml', 'gojet-v10-p18-docs-core-', 'P18', ['http/P18-T006.json']),
    'bio': ('p11-integration.yml', 'gojet-v10-p11-integration-', 'P11', ['headers/P11-T005.json', 'headers/P11-T011.json', 'api/P11-T012.json']),
    'text': ('p10-integration.yml', 'gojet-v10-p10-integration-', 'P10', ['api/P10-T005.json', 'headers/P10-T007.json', 'headers/P10-T010.json', 'headers/P10-T011.json']),
    'redirect': ('p05-integration.yml', 'gojet-v10-p05-integration-', 'P05', ['results/P05-T016.json']),
    'developer': ('p20-p0-support.yml', 'p20-t024-availability-', 'P20', ['integration/P20-T024-runtime.json']),
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
                require(not run or run['status'] != 'completed', f'T030 producer {key} failed: {run and run.get("conclusion")}')
                pending.append(key)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T030 producer timeout: ' + ','.join(pending))
        print('T030 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t030-sources'
    require(not directory.exists(), 'T030 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
    for key, (workflow, prefix, node, paths) in PRODUCERS.items():
        run = selected[key]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'truncated artifact list')
        matches = [a for a in rows if a['name'] == prefix + head and a['expired'] is False]
        require(len(matches) == 1, 'T030 artifact absent or ambiguous: ' + key)
        artifact = matches[0]
        require(artifact.get('workflow_run', {}).get('id') == run['id'], 'artifact/run mismatch')
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][key] = {'workflow': workflow, 'head_sha': head, 'run_id': run['id'],
                                     'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        def save(path, content):
            require(path not in manifest['files'], 'duplicate T030 source')
            target = directory / path
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][path] = {'sha256': digest(content), 'producer': key}
        if key == 'prerequisite':
            for entry in archive.infolist():
                path = PurePosixPath(entry.filename)
                require(not path.is_absolute() and '..' not in path.parts and entry.file_size <= 32 * 1024 * 1024, 'unsafe T029 member')
                if entry.is_dir():
                    continue
                require(entry.filename == 'consistency/P20-T029.json' or entry.filename.startswith('runtime/t029/'), 'unexpected T029 member')
                save('prerequisite/artifacts/v10/P20/' + entry.filename, archive.read(entry))
        else:
            for path in paths:
                save(key + '/' + path, member(archive, node, path))
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
