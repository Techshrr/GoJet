"""Collect original Actions archives for the final whole-product evidence range.

No repository-resident PASS file is admitted. Sources must come from the newest
successful exact-head producer in the reviewed complete regression matrix.
"""
import json
import os
import sys
import time
from pathlib import Path

from common import ROOT, HEAD
from evidence_index import valid_case, unsafe_secret
from release_matrix import validate_inventory, select_latest, admit_run, REPOSITORY
from t028_sources import require, digest, member, verified_archive, download_archive, select_prefixed_artifact

SUPPORT = 'p20-p0-support.yml'
SOURCES = {
    'p20-candidate-freeze.yml': ('gojet-v10-p20-candidate-', tuple(range(1, 9))),
    SUPPORT: ('p20-t024-availability-', tuple(range(9, 27)) + tuple(range(37, 47))),
    'p12-browser.yml': ('gojet-v10-p12-browser-', (27,)),
    **{f'p20-t{n:03d}-consistency.yml': (f'p20-t{n:03d}-formal-', (n,)) for n in range(28, 34)},
    'p20-t034-matrix.yml': ('p20-t034-native-matrix-', (34,)),
    'p20-t035-matrix.yml': ('p20-t035-accessibility-review-', (35,)),
    'p20-t036-performance.yml': ('p20-t036-performance-', (36,)),
}


def case_paths(root):
    plan = json.loads((root / 'artifacts/v10/P20/test-plan.json').read_bytes())
    return {c['id']: c['evidence'].removeprefix('artifacts/v10/P20/') for c in plan['cases']}


def inspect(root, head, directory=None):
    directory = directory or root / 'artifacts/v10/P20/runtime/closure-sources'
    manifest = json.loads((directory / 'collection.json').read_bytes())
    require(manifest['implementation_commit'] == head, 'mixed-head closure collection')
    required = validate_inventory(root)
    require(set(manifest['matrix']) == set(required), 'incomplete regression inventory')
    matrix = {}
    for workflow, row in manifest['matrix'].items():
        matrix[workflow] = admit_run(row['run'], row['jobs'], workflow, head)
    require(set(manifest['artifacts']) == set(SOURCES), 'incomplete formal producer inventory')
    paths, cases, bindings = case_paths(root), {}, {}
    for workflow, (prefix, numbers) in SOURCES.items():
        artifact = manifest['artifacts'][workflow]
        run = manifest['matrix'][workflow]['run']
        require(artifact['workflow_run']['id'] == run['id']
                and artifact['name'].startswith(prefix), 'foreign formal artifact')
        raw = (directory / f"{artifact['id']}.zip").read_bytes()
        with verified_archive(raw, artifact, head) as archive:
            for number in numbers:
                case = f'P20-T{number:03d}'
                content = member(archive, 'P20', paths[case])
                data = json.loads(content)
                require(valid_case(data, case, head), 'invalid formal evidence: ' + case)
                require(not unsafe_secret(data), 'secret-bearing formal evidence: ' + case)
                require(case not in cases, 'duplicate case authority')
                cases[case] = data
                bindings[case] = {'workflow': workflow, 'run_id': run['id'],
                                 'artifact_id': artifact['id'], 'archive_sha256': digest(raw),
                                 'member': paths[case], 'sha256': digest(content)}
            if workflow == 'p20-candidate-freeze.yml':
                authority = json.loads(member(archive, 'P20', 'authority/integrated-authority-ledger.json'))
    require(set(cases) == {f'P20-T{n:03d}' for n in range(1, 47)}, 'incomplete T001-T046')
    require(authority['implementation_commit'] == head and authority['live_bound_count'] == 20
            and authority['ancestry_bound_count'] == 20, 'incomplete inherited authority')
    return {'implementation_commit': head, 'cases': cases, 'bindings': bindings,
            'matrix': matrix, 'authority': authority,
            'collection_sha256': digest((directory / 'collection.json').read_bytes())}


def api_context():
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from ci.actions import github_json
    require(os.environ.get('GITHUB_REPOSITORY') == REPOSITORY, 'untrusted closure repository')
    headers = {'Authorization': 'Bearer ' + os.environ['GH_TOKEN'], 'Accept': 'application/vnd.github+json'}
    return 'https://api.github.com/repos/' + REPOSITORY, headers, lambda url: github_json(url, headers)


def list_complete(api, base, field):
    rows = []
    for page in range(1, 21):
        payload = api(base + ('&' if '?' in base else '?') + f'per_page=100&page={page}')
        batch = payload[field]
        require(isinstance(batch, list) and len(batch) <= 100, 'malformed Actions listing')
        rows.extend(batch)
        if len(batch) < 100:
            require(len(rows) == payload['total_count'], 'incomplete Actions listing')
            return rows
    raise ValueError('Actions pagination limit; completeness unproven')


def collect(root, head):
    base, headers, api = api_context()
    from ci.actions import workflow_runs
    required = validate_inventory(root)
    directory = root / 'artifacts/v10/P20/runtime/closure-sources'
    require(not directory.exists(), 'closure collection must start empty')
    directory.mkdir(parents=True)
    deadline = time.monotonic() + 300 * 60
    while True:
        runs = workflow_runs(api, REPOSITORY, head)
        selected = {w: select_latest(runs, w, head) for w in required}
        pending = [w for w, r in selected.items() if not r or r['status'] != 'completed']
        if not pending:
            break
        require(time.monotonic() < deadline, 'closure matrix timed out: ' + ', '.join(pending))
        print(f'Closure waiting for {len(pending)}/{len(required)} workflows: ' + ', '.join(pending), flush=True)
        time.sleep(45)
    manifest = {'implementation_commit': head, 'matrix': {}, 'artifacts': {}}
    for workflow, run in selected.items():
        jobs = list_complete(api, base + f'/actions/runs/{run["id"]}/jobs?filter=all', 'jobs')
        admit_run(run, jobs, workflow, head)
        # Store audit metadata only; never tokens, API headers or signed URLs.
        kept_run = {k: run[k] for k in ('id', 'head_sha', 'path', 'event', 'head_repository',
                                       'status', 'conclusion', 'run_attempt', 'run_started_at')}
        kept_run['head_repository'] = {'full_name': run['head_repository']['full_name']}
        kept_jobs = [{k: j[k] for k in ('id', 'run_id', 'name', 'status', 'conclusion')} for j in jobs]
        manifest['matrix'][workflow] = {'run': kept_run, 'jobs': kept_jobs}
    for workflow, (prefix, _) in SOURCES.items():
        run = selected[workflow]
        rows = list_complete(api, base + f'/actions/runs/{run["id"]}/artifacts', 'artifacts')
        artifact = select_prefixed_artifact(rows, prefix, run)
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        verified_archive(raw, artifact, head).close()
        (directory / f'{artifact["id"]}.zip').write_bytes(raw)
        manifest['artifacts'][workflow] = {k: artifact[k] for k in
            ('id', 'name', 'digest', 'expired', 'created_at', 'workflow_run')}
    # A newer failed/running rerun cannot be hidden by the archived green.
    current = workflow_runs(api, REPOSITORY, head)
    for workflow, run in selected.items():
        latest = select_latest(current, workflow, head)
        require(latest is not None and (latest['id'], latest['run_attempt'], latest['status'], latest['conclusion'])
                == (run['id'], run['run_attempt'], run['status'], run['conclusion']),
                'matrix changed during collection: ' + workflow)
    (directory / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')
    inspect(root, head, directory)


if __name__ == '__main__':
    collect(ROOT, HEAD)
