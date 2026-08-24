#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path('artifacts/v10/P14')
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
CONTRACT_DIR = ROOT / 'contract'
INDEX = ROOT / 'evidence-index.json'
RESULT = ROOT / 'results' / 'P14-T024.json'

REQUIRED_PRODUCERS = (
    'P14 Support Tickets and Mail Contract',
    'P14 Real Support Tickets and Mail Integration',
    'P14 Workspace Support Browser',
    'P14 Admin Tickets Mail Contact Browser',
)

CASE_DIRS = {
    1: 'api', 2: 'rbac', 3: 'api', 4: 'security',
    5: 'entitlement', 6: 'entitlement', 7: 'entitlement',
    8: 'security', 9: 'security', 10: 'security', 11: 'security', 12: 'security',
    13: 'api',
    14: 'mail', 15: 'mail', 16: 'mail', 17: 'mail',
    18: 'notification', 19: 'rbac', 20: 'api', 21: 'audit',
    22: 'browser', 23: 'browser',
}

FORBIDDEN_EVIDENCE_MARKERS = (
    b'p14-ci-deterministic-turnstile-token',
    b'p14-browser-valid-turnstile-token',
    b'p14-browser-invalid-token',
    b'@example.test',
    b'@gojet.local',
    b'recipient_value',
    b'smtp_password',
    b'smtp_username',
    b'claim_token',
    b'authorization:',
)

CRITICAL_RUNTIME_FILES = (
    'runtime/implementation_commit.txt',
    'runtime/case-range.txt',
    'runtime/clamav-version.txt',
    'results/integration-summary.json',
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
    return ROOT / CASE_DIRS[number] / f'P14-T{number:03d}.json'


def collect_files(directory: Path) -> list[Path]:
    if not directory.is_dir():
        return []
    return sorted(path for path in directory.rglob('*') if path.is_file())


def evidence_is_secret_safe(paths: list[Path], errors: list[str]) -> bool:
    clean = True
    for path in paths:
        if path.suffix.lower() == '.png':
            continue
        try:
            data = path.read_bytes().lower()
        except OSError as exc:
            errors.append(f'cannot read evidence {path}: {exc}')
            clean = False
            continue
        for marker in FORBIDDEN_EVIDENCE_MARKERS:
            if marker.lower() in data:
                errors.append(f'forbidden evidence marker {marker!r} in {path}')
                clean = False
    return clean


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
    need(
        isinstance(producer_rows, dict) and set(producer_rows) == set(REQUIRED_PRODUCERS),
        f'producer workflow set mismatch: {sorted(producer_rows) if isinstance(producer_rows, dict) else producer_rows}',
        errors,
    )

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
            need(
                isinstance(artifact.get('name'), str) and artifact.get('name', '').endswith(head),
                f'{name} artifact name not exact-head bound',
                errors,
            )
            need(
                isinstance(artifact.get('digest'), str) and artifact.get('digest', '').startswith('sha256:'),
                f'{name} artifact digest missing',
                errors,
            )
            need(
                isinstance(artifact.get('size_in_bytes'), int) and artifact.get('size_in_bytes') > 0,
                f'{name} artifact size missing',
                errors,
            )
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
    need(contract.get('node') == 'P14', 'contract node mismatch', errors)
    need(contract.get('status') == 'PASS', f"contract status={contract.get('status')}", errors)
    need(contract.get('errors') == [], f"contract errors={contract.get('errors')}", errors)
    need(contract.get('case_range') == 'P14-T001..P14-T025', f"contract case_range={contract.get('case_range')}", errors)

    cases: dict[str, dict] = {}
    evidence_entries: list[dict] = []
    for number in range(1, 24):
        cid = f'P14-T{number:03d}'
        path = case_path(number)
        data = load_json(path, errors)
        cases[cid] = data
        need(data.get('case_id') == cid, f'{cid} case_id mismatch', errors)
        need(data.get('implementation_commit') == head, f'{cid} implementation_commit mismatch', errors)
        need(data.get('status') == 'PASS', f"{cid} status={data.get('status')}", errors)
        need(data.get('errors') == [], f"{cid} errors={data.get('errors')}", errors)
        if number >= 22:
            details = data.get('details', {})
            need(isinstance(details, dict), f'{cid} details must be object', errors)
            need(details.get('frozen_contract_completion') is True, f'{cid} missing frozen browser contract completion', errors)
            need(details.get('closure_claim') is False, f'{cid} must not claim closure', errors)
        if path.is_file():
            evidence_entries.append({
                'case_id': cid,
                'path': path.as_posix(),
                'sha256': sha256(path),
                'size_in_bytes': path.stat().st_size,
                'implementation_commit': data.get('implementation_commit'),
            })

    integration_summary = load_json(ROOT / 'results' / 'integration-summary.json', errors)
    need(integration_summary.get('node') == 'P14', 'integration summary node mismatch', errors)
    need(integration_summary.get('implementation_commit') == head, 'integration summary exact-head mismatch', errors)
    need(integration_summary.get('case_range') == 'P14-T001..P14-T021', 'integration summary case range mismatch', errors)
    need(integration_summary.get('status') == 'PASS', f"integration summary status={integration_summary.get('status')}", errors)
    need(integration_summary.get('errors') == [], f"integration summary errors={integration_summary.get('errors')}", errors)

    same_exact_head = all(cases.get(f'P14-T{number:03d}', {}).get('implementation_commit') == head for number in range(1, 24))
    need(same_exact_head, 'mixed-head P14 case evidence detected', errors)

    runtime_marker = ROOT / 'runtime' / 'implementation_commit.txt'
    need(runtime_marker.is_file(), f'missing {runtime_marker}', errors)
    if runtime_marker.is_file():
        need(runtime_marker.read_text(encoding='utf-8').strip() == head, 'runtime implementation commit mismatch', errors)

    case_range_marker = ROOT / 'runtime' / 'case-range.txt'
    need(case_range_marker.is_file(), f'missing {case_range_marker}', errors)
    if case_range_marker.is_file():
        need(case_range_marker.read_text(encoding='utf-8').strip() == 'P14-T001..P14-T021', 'runtime case range mismatch', errors)

    critical_runtime_entries: list[dict] = []
    for relative in CRITICAL_RUNTIME_FILES:
        path = ROOT / relative
        need(path.is_file(), f'missing critical runtime evidence {path}', errors)
        if path.is_file():
            critical_runtime_entries.append({
                'path': path.as_posix(),
                'size_in_bytes': path.stat().st_size,
                'sha256': sha256(path),
            })

    runtime_files = collect_files(ROOT / 'runtime')
    need(any('browser/' in path.as_posix() for path in runtime_files), 'missing T022 browser runtime evidence', errors)
    need(any('browser-023/' in path.as_posix() for path in runtime_files),'missing T023 browser runtime evidence', errors)
    need(any(path.name == 'clamav-version.txt' for path in runtime_files),'missing inspectable ClamAV runtime evidence', errors)

    captures_022 = sorted((ROOT / 'captures').glob('P14-T022-*.png')) if (ROOT / 'captures').is_dir() else []
    captures_023 = sorted((ROOT / 'captures').glob('P14-T023-*.png')) if (ROOT / 'captures').is_dir() else []
    need(len(captures_022) >= 20, f'P14-T022 browser capture count {len(captures_022)} < 20', errors)
    need(len(captures_023) >= 36, f'P14-T023 browser capture count {len(captures_023)} < 36', errors)

    capture_entries = [
        {'path': path.as_posix(), 'size_in_bytes': path.stat().st_size, 'sha256': sha256(path)}
        for path in [*captures_022, *captures_023]
    ]
    for entry in capture_entries:
        need(entry['size_in_bytes'] > 0, f"empty browser capture {entry['path']}", errors)

    inspectable_paths = []
    for dirname in ('api', 'rbac', 'security', 'entitlement', 'mail', 'notification', 'audit', 'browser', 'runtime', 'results', 'contract'):
        inspectable_paths.extend(collect_files(ROOT / dirname))
    secret_safe = evidence_is_secret_safe(inspectable_paths, errors)

    index = {
        'node': 'P14',
        'case': 'P14-T024',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'same_exact_head': same_exact_head,
        'producer_manifest': producer_manifest,
        'case_evidence': evidence_entries,
        'critical_runtime_evidence': critical_runtime_entries,
        'browser_captures': capture_entries,
    }
    INDEX.parent.mkdir(parents=True, exist_ok=True)
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')

    result = {
        'case_id': 'P14-T024',
        'status': 'PASS' if not errors else 'FAIL',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'observations': {
            'input_evidence_count': len(evidence_entries),
            'same_exact_head': same_exact_head,
            't022_capture_count': len(captures_022),
            't023_capture_count': len(captures_023),
            'runtime_file_count': len(runtime_files),
            'secret_safe': secret_safe,
            'producer_run_ids': producer_run_ids,
            'producer_artifacts': producer_artifacts,
            'mixed_head_rejected': True,
            'inspectable_runtime_browser_mail_clamav_evidence': (
                len(runtime_files) > 0
                and len(captures_022) >= 20
                and len(captures_023) >= 36
                and any(path.name == 'clamav-version.txt' for path in runtime_files)
                and all(case_path(number).is_file() for number in (14, 15, 16, 17))
            ),
        },
        'errors': errors,
    }
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == '__main__':
    raise SystemExit(run())
