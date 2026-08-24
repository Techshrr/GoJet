#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path('artifacts/v10/P13')
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
CONTRACT_DIR = ROOT / 'contract'
INDEX = ROOT / 'evidence-index.json'
RESULT = ROOT / 'results' / 'P13-T026.json'

REQUIRED_PRODUCERS = (
    'P13 Billing Payments Entitlements Contract',
    'P13 Real Billing Payments Entitlements Integration',
    'P13 Billing Commerce Browser',
)

CASE_DIRS = {
    1: 'api', 2: 'api', 3: 'entitlement', 4: 'rbac', 5: 'api', 6: 'api',
    7: 'security', 8: 'security', 9: 'api', 10: 'entitlement', 11: 'entitlement',
    12: 'entitlement', 13: 'api', 14: 'entitlement', 15: 'entitlement',
    16: 'entitlement', 17: 'entitlement', 18: 'entitlement', 19: 'audit', 20: 'api',
    21: 'browser', 22: 'browser', 23: 'browser', 24: 'browser', 25: 'browser',
}

REQUIRED_RUNTIME_ERRORS = (
    'mysql-plans.err',
    'mysql-orders.err',
    'mysql-invoices.err',
    'mysql-transactions.err',
    'mysql-callback-events-safe.err',
    'mysql-subscriptions.err',
    'mysql-entitlements-safe.err',
    'mysql-fx.err',
    'mysql-audit-safe.err',
    'mysql-notifications-safe.err',
    'browser-plans.err',
    'browser-subscriptions.err',
    'browser-fx.err',
)


def exact_head() -> str:
    supplied = os.environ.get('EXACT_HEAD', '').strip()
    if supplied:
        return supplied
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def load_json(path: Path, errors: list[str]) -> dict:
    if not path.is_file():
        errors.append(f'missing {path}')
        return {}
    try:
        value = json.loads(path.read_text(encoding='utf-8'))
    except Exception as exc:
        errors.append(f'invalid JSON {path}: {exc}')
        return {}
    if not isinstance(value, dict):
        errors.append(f'JSON object required {path}')
        return {}
    return value


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def case_path(number: int) -> Path:
    return ROOT / CASE_DIRS[number] / f'P13-T{number:03d}.json'


def run() -> int:
    head = exact_head()
    errors: list[str] = []
    producer_manifest = load_json(PRODUCERS, errors)

    need(producer_manifest.get('implementation_commit') == head, 'producer manifest exact-head mismatch', errors)
    need(producer_manifest.get('missing') == [], f"producer manifest missing={producer_manifest.get('missing')}", errors)
    need(producer_manifest.get('pending') == [], f"producer manifest pending={producer_manifest.get('pending')}", errors)
    need(producer_manifest.get('failed') == [], f"producer manifest failed={producer_manifest.get('failed')}", errors)

    producer_rows = producer_manifest.get('required_workflows', {})
    need(isinstance(producer_rows, dict), 'producer required_workflows must be object', errors)
    need(set(producer_rows) == set(REQUIRED_PRODUCERS), f'producer workflow set mismatch: {sorted(producer_rows) if isinstance(producer_rows, dict) else producer_rows}', errors)

    producer_run_ids: dict[str, int] = {}
    producer_artifacts: dict[str, dict] = {}
    if isinstance(producer_rows, dict):
        for name in REQUIRED_PRODUCERS:
            row = producer_rows.get(name, {})
            artifact = row.get('artifact', {}) if isinstance(row, dict) else {}
            need(row.get('head_sha') == head, f'{name} producer head mismatch', errors)
            need(row.get('status') == 'completed', f"{name} producer status={row.get('status')}", errors)
            need(row.get('conclusion') == 'success', f"{name} producer conclusion={row.get('conclusion')}", errors)
            need(isinstance(row.get('run_id'), int) and row.get('run_id') > 0, f'{name} run id missing', errors)
            need(isinstance(artifact.get('id'), int) and artifact.get('id') > 0, f'{name} artifact id missing', errors)
            need(isinstance(artifact.get('name'), str) and artifact.get('name', '').endswith(head), f'{name} artifact name not exact-head bound', errors)
            need(isinstance(artifact.get('digest'), str) and artifact.get('digest', '').startswith('sha256:'), f'{name} artifact digest missing', errors)
            need(isinstance(artifact.get('size_in_bytes'), int) and artifact.get('size_in_bytes') > 0, f'{name} artifact size missing', errors)
            if isinstance(row.get('run_id'), int):
                producer_run_ids[name] = row['run_id']
            if isinstance(artifact, dict):
                producer_artifacts[name] = {
                    'id': artifact.get('id'),
                    'name': artifact.get('name'),
                    'digest': artifact.get('digest'),
                    'size_in_bytes': artifact.get('size_in_bytes'),
                }

    implementation_file = CONTRACT_DIR / 'implementation_commit.txt'
    need(implementation_file.is_file(), f'missing {implementation_file}', errors)
    if implementation_file.is_file():
        need(implementation_file.read_text(encoding='utf-8').strip() == head, 'contract implementation commit mismatch', errors)
    contract = load_json(CONTRACT_DIR / 'contract.json', errors)
    need(contract.get('node') == 'P13', 'contract node mismatch', errors)
    need(contract.get('status') == 'PASS', f"contract status={contract.get('status')}", errors)
    need(contract.get('errors') == [], f"contract errors={contract.get('errors')}", errors)

    cases: dict[str, dict] = {}
    evidence_entries: list[dict] = []
    for number in range(1, 26):
        cid = f'P13-T{number:03d}'
        path = case_path(number)
        data = load_json(path, errors)
        cases[cid] = data
        need(data.get('case_id') == cid, f'{cid} case_id mismatch', errors)
        need(data.get('implementation_commit') == head, f'{cid} implementation_commit mismatch', errors)
        need(data.get('status') == 'PASS', f"{cid} status={data.get('status')}", errors)
        need(data.get('errors') == [], f"{cid} errors={data.get('errors')}", errors)
        if number >= 21:
            need(data.get('details', {}).get('frozen_contract_completion') is True, f'{cid} missing frozen browser contract completion', errors)
        if path.is_file():
            evidence_entries.append({
                'case_id': cid,
                'path': path.as_posix(),
                'sha256': sha256(path),
                'size_in_bytes': path.stat().st_size,
                'implementation_commit': data.get('implementation_commit'),
            })

    runtime_dir = ROOT / 'runtime'
    runtime_errors_clean = True
    runtime_entries: list[dict] = []
    for name in REQUIRED_RUNTIME_ERRORS:
        path = runtime_dir / name
        if not path.is_file():
            errors.append(f'missing runtime error capture {path}')
            runtime_errors_clean = False
            continue
        if path.stat().st_size != 0:
            errors.append(f'non-empty runtime error capture {path}')
            runtime_errors_clean = False
        runtime_entries.append({'path': path.as_posix(), 'size_in_bytes': path.stat().st_size, 'sha256': sha256(path)})

    capture_dir = ROOT / 'captures'
    captures = sorted(capture_dir.glob('P13-T02*.png')) if capture_dir.is_dir() else []
    need(len(captures) >= 20, f'P13 browser capture count {len(captures)} < 20', errors)
    capture_entries = [
        {'path': path.as_posix(), 'size_in_bytes': path.stat().st_size, 'sha256': sha256(path)}
        for path in captures
    ]
    for entry in capture_entries:
        need(entry['size_in_bytes'] > 0, f"empty browser capture {entry['path']}", errors)

    same_exact_head = all(cases.get(f'P13-T{number:03d}', {}).get('implementation_commit') == head for number in range(1, 26))
    need(same_exact_head, 'mixed-head P13 case evidence detected', errors)

    index = {
        'node': 'P13',
        'case': 'P13-T026',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'same_exact_head': same_exact_head,
        'producer_manifest': producer_manifest,
        'case_evidence': evidence_entries,
        'runtime_error_captures': runtime_entries,
        'browser_captures': capture_entries,
    }
    INDEX.parent.mkdir(parents=True, exist_ok=True)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')

    result = {
        'case_id': 'P13-T026',
        'status': 'PASS' if not errors else 'FAIL',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'observations': {
            'input_evidence_count': len(evidence_entries),
            'same_exact_head': same_exact_head,
            'capture_count': len(captures),
            'runtime_error_files_clean': runtime_errors_clean,
            'producer_run_ids': producer_run_ids,
            'producer_artifacts': producer_artifacts,
            'mixed_head_rejected': True,
            'inspectable_runtime_and_browser_evidence': runtime_errors_clean and len(captures) >= 20,
        },
        'errors': errors,
    }
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == '__main__':
    raise SystemExit(run())
