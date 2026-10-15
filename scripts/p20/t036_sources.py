"""Retain original same-head P04/P07/P19 performance evidence, not summary flags."""
import json
import os
import sys
import time
from pathlib import Path
from t028_sources import (require, digest, select_run, successful,
                          select_prefixed_artifact, download_archive, verified_archive, member)
from t036_guards import GUARDS, verify

PRODUCERS = {
    'shell': ('p04-product-shells.yml', 'gojet-v10-p04-', [
        ('P04', 'source.json'), ('P04', 'browser/browser-report.json'),
        ('P04', 'results/P04-T006.json'), ('P04', 'results/P04-T007.json'),
        ('P04', 'results/P04-T010.json'), ('gates', 'G9/performance/p04-shell-bundle.json')]),
    'analytics': ('p07-browser.yml', 'gojet-v10-p07-browser-', [
        ('P07', 'results/P07-T019.json'), ('gates', 'G9/p07-analytics.json')]),
    'website': ('p19-website-quality.yml', 'gojet-v10-p19-quality-', [
        ('P19', 'performance/P19-T027.json'), ('P19', 'performance/P19-T028.json'),
        ('P19', 'quality-authority/implementation_commit.txt')]),
}
GATES = tuple(GUARDS)


def collect(root, head):
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from ci.actions import github_json, workflow_runs
    repo = os.environ['GITHUB_REPOSITORY']
    require(repo == 'Techshrr/GoJet', 'untrusted performance repository')
    base = 'https://api.github.com/repos/' + repo
    headers = {'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json'}
    api = lambda url: github_json(url, headers)
    definitions = verify(root)
    own_id = int(os.environ['GITHUB_RUN_ID'])
    own = api(base + f'/actions/runs/{own_id}')
    require(own['head_sha'] == head and own['path'] == '.github/workflows/p20-t036-performance.yml'
            and own['head_repository']['full_name'] == repo, 'invalid scoped guard run')
    jobs = api(base + f'/actions/runs/{own_id}/jobs?per_page=100&filter=all')['jobs']
    require(len(jobs) < 100, 'truncated guard jobs')
    gates = {}
    for key, (workflow, name) in GUARDS.items():
        # GitHub reuses successful dependencies on failed-job retries. Keep
        # their same-run authority, but never choose an older green over a
        # newer failed/running execution of that same job.
        matching = [j for j in jobs if j['name'] == name]
        latest = [max(matching, key=lambda j: j['id'])] if matching else []
        require(successful(own, name, latest), 'canonical guard not successful: ' + key)
        job = latest[0]
        gates[key] = {'id': own_id, 'job_id': job['id'], 'head_sha': head,
                      'conclusion': job['conclusion'], 'canonical_workflow': workflow,
                      'definition_sha256': definitions[key]}
    deadline = time.monotonic() + 180 * 60
    while True:
        runs = workflow_runs(api, repo, head)
        selected, pending = {}, []
        for workflow in [p[0] for p in PRODUCERS.values()]:
            run = select_run(runs, workflow, head, repo)
            if run and successful(run, None, []): selected[workflow] = run
            else: pending.append(workflow)
        if not pending: break
        require(time.monotonic() < deadline, 'T036 producer timeout: ' + ','.join(pending))
        print('T036 waiting: ' + ','.join(pending), flush=True)
        time.sleep(45)
    directory = root / 'artifacts/v10/P20/runtime/t036-sources'
    require(not directory.exists(), 'T036 collection must start empty')
    directory.mkdir(parents=True)
    manifest = {'implementation_commit': head, 'artifacts': {}, 'files': {}, 'gates': gates}
    for key, (workflow, prefix, paths) in PRODUCERS.items():
        run = selected[workflow]
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'truncated performance artifacts')
        artifact = select_prefixed_artifact(rows, prefix, run)
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archive = verified_archive(raw, artifact, head)
        manifest['artifacts'][key] = {'workflow': workflow, 'head_sha': head, 'run_id': run['id'],
            'run_attempt': run['run_attempt'], 'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        for node, path in paths:
            name = f'{key}/{node}/{path}'
            content = member(archive, node, path)
            target = directory / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content)
            manifest['files'][name] = {'producer': key, 'sha256': digest(content)}
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
