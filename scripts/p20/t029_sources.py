"""Collect real T029 predecessors and raw evidence, preserving exact-SHA authority."""
import json
import os
from pathlib import Path, PurePosixPath
import sys
import time
from t028_sources import download_archive, verified_archive, member, select_run, successful, require, digest
from t029_requirements import REQUIRED

PRODUCERS = {
    'P20': ('p20-t028-consistency.yml', 'p20-t028-formal-', None),
    'P16': ('p16-evidence.yml', 'gojet-v10-p16-evidence-', sorted(REQUIRED)),
    'P11': ('p11-integration.yml', 'gojet-v10-p11-integration-', ['headers/P11-T009.json', 'api/P11-T010.json']),
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
        for node, (workflow, _, _) in PRODUCERS.items():
            run = select_run(runs, workflow, head, repo)
            if run and successful(run, None, []):
                selected[node] = run
            else:
                pending.append(node)
        if not pending:
            break
        require(time.monotonic() < deadline, 'T029 producer wait timed out: ' + ','.join(pending))
        print('T029 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t029'
    require(not directory.exists(), 'T029 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}}
    for node, (workflow, prefix, paths) in PRODUCERS.items():
        run = selected[node]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'artifact listing truncated')
        matches = [a for a in rows if a['name'] == prefix + head and a['expired'] is False]
        require(len(matches) == 1, 'required T029 artifact missing or ambiguous')
        artifact = matches[0]
        require(artifact.get('workflow_run', {}).get('id') == run['id'], 'artifact/run mismatch')
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][node] = {'workflow': workflow, 'head_sha': head, 'run_id': run['id'],
                                      'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        def save(relative, content):
            require(relative not in manifest['files'], 'duplicate T029 source')
            target = directory / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][relative] = {'sha256': digest(content), 'producer': node}
        if node == 'P20':
            # The formal T028 upload root is P20. Reconstruct it in an isolated
            # prerequisite root so the full raw-source admission can run again.
            for entry in archive.infolist():
                path = PurePosixPath(entry.filename)
                require(not path.is_absolute() and '..' not in path.parts, 'unsafe T028 archive path')
                require(entry.file_size <= 32 * 1024 * 1024, 'oversized T028 entry')
                if entry.is_dir():
                    continue
                require(entry.filename == 'consistency/P20-T028.json'
                        or entry.filename.startswith('runtime/t028/'), 'unexpected T028 artifact member')
                save('prerequisite/artifacts/v10/P20/' + entry.filename, archive.read(entry))
        else:
            for path in paths:
                save(f'sources/artifacts/v10/{node}/{path}', member(archive, node, path))
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
