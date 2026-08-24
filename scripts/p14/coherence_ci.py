#!/usr/bin/env python3
from __future__ import annotations

import json
import os
import shutil
import subprocess
import time
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path('artifacts/v10/P14')
MANIFEST = ROOT / 'evidence-producer-manifest.json'
INDEPENDENT = {
    'P14 Real Support Tickets and Mail Integration': lambda sha: f'p14-real-integration-{sha}',
    'P14 Workspace Support Browser': lambda sha: f'gojet-v10-p14-browser-{sha}',
    'P14 Admin Tickets Mail Contact Browser': lambda sha: f'gojet-v10-p14-browser-t023-{sha}',
}
CONTRACT_NAME = 'P14 Support Tickets and Mail Contract'

CASE_DIRS = {
    1: 'api', 2: 'rbac', 3: 'api', 4: 'security',
    5: 'entitlement', 6: 'entitlement', 7: 'entitlement',
    8: 'security', 9: 'security', 10: 'security', 11: 'security', 12: 'security',
    13: 'api', 14: 'mail', 15: 'mail', 16: 'mail', 17: 'mail',
    18: 'notification', 19: 'rbac', 20: 'api', 21: 'audit',
}


def need_env(name: str) -> str:
    value = os.environ.get(name, '').strip()
    if not value:
        raise SystemExit(f'{name} is required')
    return value


HEAD = need_env('EXACT_HEAD')
REPOSITORY = need_env('REPOSITORY')
TOKEN = need_env('GH_TOKEN')
CURRENT_RUN_ID = int(need_env('GITHUB_RUN_ID'))
CURRENT_RUN_NUMBER = int(os.environ.get('GITHUB_RUN_NUMBER', '0'))

HEADERS = {
    'Accept': 'application/vnd.github+json',
    'Authorization': f'Bearer {TOKEN}',
    'X-GitHub-Api-Version': '2022-11-28',
}


def api_get(url: str) -> dict:
    request = urllib.request.Request(url, headers=HEADERS)
    with urllib.request.urlopen(request, timeout=30) as response:
        return json.load(response)


def artifact_for(run_id: int, expected: str) -> dict | None:
    url = f'https://api.github.com/repos/{REPOSITORY}/actions/runs/{run_id}/artifacts?per_page=100'
    artifacts = api_get(url).get('artifacts', [])
    matches = [item for item in artifacts if item.get('name') == expected and not item.get('expired')]
    if len(matches) != 1:
        return None
    item = matches[0]
    return {
        'id': int(item['id']),
        'name': item['name'],
        'digest': item.get('digest'),
        'size_in_bytes': int(item.get('size_in_bytes', 0)),
    }


def bind_producers() -> dict:
    ROOT.mkdir(parents=True, exist_ok=True)
    contract_expected = f'p14-support-tickets-mail-contract-{HEAD}'
    deadline = time.time() + 35 * 60
    while time.time() < deadline:
        contract_artifact = artifact_for(CURRENT_RUN_ID, contract_expected)
        query = urllib.parse.urlencode({'head_sha': HEAD, 'event': 'pull_request', 'per_page': 100})
        runs_url = f'https://api.github.com/repos/{REPOSITORY}/actions/runs?{query}'
        runs = api_get(runs_url).get('workflow_runs', [])
        latest: dict[str, dict] = {}
        for run in runs:
            name = run.get('name')
            if name in INDEPENDENT and (
                name not in latest or int(run.get('id', 0)) > int(latest[name].get('id', 0))
            ):
                latest[name] = run

        missing = [name for name in INDEPENDENT if name not in latest]
        pending = [
            name for name in INDEPENDENT
            if name in latest and latest[name].get('status') != 'completed'
        ]
        failed = [
            name for name in INDEPENDENT
            if name in latest
            and latest[name].get('status') == 'completed'
            and latest[name].get('conclusion') != 'success'
        ]
        if contract_artifact is None:
            pending.append(f'{CONTRACT_NAME}:artifact')

        entries: dict[str, dict] = {}
        if contract_artifact is not None:
            current = api_get(
                f'https://api.github.com/repos/{REPOSITORY}/actions/runs/{CURRENT_RUN_ID}'
            )
            entries[CONTRACT_NAME] = {
                'run_id': CURRENT_RUN_ID,
                'run_number': CURRENT_RUN_NUMBER,
                'head_sha': HEAD,
                'status': 'completed',
                'conclusion': 'success',
                'authority_scope': 'contract-artifact-after-successful-contract-steps',
                'workflow_status_at_bind': current.get('status'),
                'artifact': contract_artifact,
            }

        if not missing and not pending and not failed:
            for name, run in latest.items():
                expected = INDEPENDENT[name](HEAD)
                artifact = artifact_for(int(run['id']), expected)
                if artifact is None:
                    failed.append(f'{name}:artifact:{expected}')
                    continue
                entries[name] = {
                    'run_id': int(run['id']),
                    'run_number': int(run.get('run_number', 0)),
                    'head_sha': run.get('head_sha'),
                    'status': run.get('status'),
                    'conclusion': run.get('conclusion'),
                    'authority_scope': 'completed-workflow',
                    'artifact': artifact,
                }

        manifest = {
            'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
            'implementation_commit': HEAD,
            'required_workflows': entries,
            'missing': missing,
            'pending': pending,
            'failed': failed,
        }
        MANIFEST.write_text(json.dumps(manifest, indent=2, sort_keys=True) + '\n', encoding='utf-8')

        if failed:
            raise SystemExit('required P14 producer/artifact failed: ' + ', '.join(failed))
        if not missing and not pending and len(entries) == 4:
            print(f'P14 T024 producer authority green for {HEAD}')
            return manifest
        print(f'Waiting P14 T024 producers missing={missing} pending={pending}', flush=True)
        time.sleep(10)

    raise SystemExit(f'timed out waiting for P14 T024 producers on {HEAD}')


def download(run_id: int, artifact_name: str, destination: Path) -> None:
    if destination.exists():
        shutil.rmtree(destination)
    destination.mkdir(parents=True)
    subprocess.run(
        [
            'gh', 'run', 'download', str(run_id), '--repo', REPOSITORY,
            '--name', artifact_name, '--dir', str(destination),
        ],
        check=True,
        env={**os.environ, 'GH_TOKEN': TOKEN},
    )


def first_file(root: Path, name: str) -> Path:
    matches = sorted(root.rglob(name))
    if not matches:
        raise SystemExit(f'missing {name} in {root}')
    return matches[0]


def first_dir(root: Path, name: str) -> Path:
    matches = sorted(path for path in root.rglob(name) if path.is_dir())
    if not matches:
        raise SystemExit(f'missing directory {name} in {root}')
    return matches[0]


def copy_file(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)


def copy_tree_contents(source: Path, destination: Path) -> None:
    destination.mkdir(parents=True, exist_ok=True)
    for child in source.iterdir():
        target = destination / child.name
        if child.is_dir():
            shutil.copytree(child, target, dirs_exist_ok=True)
        else:
            shutil.copy2(child, target)


def assemble_evidence(manifest: dict) -> None:
    temp = Path('/tmp/p14-t024')
    if temp.exists():
        shutil.rmtree(temp)
    temp.mkdir(parents=True)

    rows = manifest['required_workflows']
    destinations = {
        CONTRACT_NAME: temp / 'contract',
        'P14 Real Support Tickets and Mail Integration': temp / 'integration',
        'P14 Workspace Support Browser': temp / 'browser-022',
        'P14 Admin Tickets Mail Contact Browser': temp / 'browser-023',
    }
    for name, destination in destinations.items():
        row = rows[name]
        download(int(row['run_id']), row['artifact']['name'], destination)

    for dirname in (
        'contract', 'api', 'rbac', 'security', 'entitlement', 'mail', 'notification',
        'audit', 'browser', 'captures', 'runtime', 'results',
    ):
        (ROOT / dirname).mkdir(parents=True, exist_ok=True)

    contract_root = destinations[CONTRACT_NAME]
    for name in (
        'contract.json', 'implementation_commit.txt', 'base_integration_commit.txt',
        'predecessor_signed_source.txt', 'case_range.txt',
    ):
        copy_file(first_file(contract_root, name), ROOT / 'contract' / name)
    if (ROOT / 'contract' / 'implementation_commit.txt').read_text(encoding='utf-8').strip() != HEAD:
        raise SystemExit('downloaded contract exact-head mismatch')

    integration_root = destinations['P14 Real Support Tickets and Mail Integration']
    for number in range(1, 22):
        case_id = f'P14-T{number:03d}'
        copy_file(first_file(integration_root, f'{case_id}.json'), ROOT / CASE_DIRS[number] / f'{case_id}.json')
    copy_file(first_file(integration_root, 'integration-summary.json'), ROOT / 'results' / 'integration-summary.json')
    copy_tree_contents(first_dir(integration_root, 'runtime'), ROOT / 'runtime')

    browser_022_root = destinations['P14 Workspace Support Browser']
    copy_file(first_file(browser_022_root, 'P14-T022.json'), ROOT / 'browser' / 'P14-T022.json')
    captures_022 = sorted(browser_022_root.rglob('P14-T022-*.png'))
    for source in captures_022:
        copy_file(source, ROOT / 'captures' / source.name)
    copy_tree_contents(first_dir(browser_022_root, 'browser'), ROOT / 'runtime' / 'browser')

    browser_023_root = destinations['P14 Admin Tickets Mail Contact Browser']
    copy_file(first_file(browser_023_root, 'P14-T023.json'), ROOT / 'browser' / 'P14-T023.json')
    captures_023 = sorted(browser_023_root.rglob('P14-T023-*.png'))
    for source in captures_023:
        copy_file(source, ROOT / 'captures' / source.name)
    copy_tree_contents(first_dir(browser_023_root, 'browser-023'), ROOT / 'runtime' / 'browser-023')

    expected_counts = {
        'api': 4, 'rbac': 2, 'security': 6, 'entitlement': 3,
        'mail': 4, 'notification': 1, 'audit': 1, 'browser': 2,
    }
    for dirname, expected in expected_counts.items():
        actual = len(list((ROOT / dirname).glob('P14-T*.json')))
        if actual != expected:
            raise SystemExit(f'{dirname} case evidence count {actual} != {expected}')
    if len(captures_022) < 20:
        raise SystemExit(f'T022 capture count {len(captures_022)} < 20')
    if len(captures_023) < 36:
        raise SystemExit(f'T023 capture count {len(captures_023)} < 36')


def validate() -> None:
    env = {**os.environ, 'EXACT_HEAD': HEAD}
    subprocess.run(
        ['python3', 'scripts/p14/validate.py', '--case', 'P14-T024'],
        check=True,
        env=env,
    )
    result_path = ROOT / 'results' / 'P14-T024.json'
    result = json.loads(result_path.read_text(encoding='utf-8'))
    if result.get('status') != 'PASS' or result.get('errors') != []:
        raise SystemExit(f'P14-T024 did not PASS: {result}')
    if result.get('implementation_commit') != HEAD:
        raise SystemExit('P14-T024 exact-head mismatch')
    observations = result.get('observations', {})
    checks = {
        'input_evidence_count': observations.get('input_evidence_count') == 23,
        'same_exact_head': observations.get('same_exact_head') is True,
        't022_captures': int(observations.get('t022_capture_count', 0)) >= 20,
        't023_captures': int(observations.get('t023_capture_count', 0)) >= 36,
        'secret_safe': observations.get('secret_safe') is True,
        'mixed_head_rejected': observations.get('mixed_head_rejected') is True,
        'inspectable_runtime': observations.get('inspectable_runtime_browser_mail_clamav_evidence') is True,
        'four_producers': len(observations.get('producer_run_ids', {})) == 4,
        'four_artifacts': len(observations.get('producer_artifacts', {})) == 4,
    }
    failed = [name for name, passed in checks.items() if not passed]
    if failed:
        raise SystemExit(f'P14-T024 observation checks failed: {failed}: {observations}')
    print(f'P14-T024 exact-head evidence coherence PASS on {HEAD}')


def main() -> int:
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() != HEAD:
        raise SystemExit('checkout exact-head mismatch')
    manifest = bind_producers()
    assemble_evidence(manifest)
    validate()
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
