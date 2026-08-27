#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path('artifacts/v10/P16')
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
INDEX = ROOT / 'evidence-index.json'
RESULT = ROOT / 'results' / 'P16-T028.json'
CONTRACT_AUTHORITY = '43c5d4d7e1833c593ceacb48016abac6e3133893'

REQUIRED_PRODUCERS = (
    'P16 Trust Destination Risk Abuse Contract',
    'P16 Real Destination Risk Integration',
    'P16 Security Notification Producer',
    'P16 Admin Risk API Integration',
    'P16 Trust Browser Authority',
)

CASE_DIRS = {
    1: 'api', 2: 'api', 3: 'security', 4: 'security', 5: 'risk', 6: 'security',
    7: 'risk', 8: 'risk', 9: 'security', 10: 'security', 11: 'security', 12: 'audit',
    13: 'security', 14: 'security', 15: 'domain', 16: 'domain', 17: 'security', 18: 'security',
    19: 'abuse', 20: 'abuse', 21: 'abuse', 22: 'audit', 23: 'notifications',
    24: 'api', 25: 'api', 26: 'browser', 27: 'browser',
}

FORBIDDEN_MARKERS = (
    b'p16-provider-secret-fixture',
    b'p16-provider-token-fixture',
    b'p16-transport-secret-fixture',
    b'p16-partial-secret-fixture',
    b'p16-malformed-secret-fixture',
    b'p16-unavailable-secret-fixture',
    b'p16-domain-provider-secret-fixture',
    b'p16-t024-provider-secret',
    b'p16-t025-domain-provider-secret',
    b'p16-browser-provider-secret-marker',
    b'p16-browser-valid-turnstile-token',
    b'p16-browser-invalid-turnstile-token',
    b'customer.example/p16-browser-sensitive-target',
    b'unsafe-admin-leak.example',
    b'unsafe-domain-evidence.example',
    b'authorization: bearer',
    b'client_secret',
    b'api_secret',
    b'gojet_mysql_dsn',
)


def exact_head() -> str:
    supplied = os.environ.get('EXACT_HEAD', '').strip()
    return supplied or subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()


def need(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


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


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def case_path(number: int) -> Path:
    return ROOT / CASE_DIRS[number] / f'P16-T{number:03d}.json'


def collect_files(path: Path) -> list[Path]:
    if not path.exists():
        return []
    if path.is_file():
        return [path]
    return sorted(item for item in path.rglob('*') if item.is_file())


def case_identity(data: dict, number: int) -> tuple[str | None, str | None]:
    case_id = data.get('case') if number <= 25 else data.get('case_id')
    head = data.get('exact_head') if number <= 25 else data.get('implementation_commit')
    return case_id, head


def secret_safe(paths: list[Path], errors: list[str]) -> bool:
    clean = True
    for path in paths:
        if path.suffix.lower() == '.png':
            continue
        try:
            raw = path.read_bytes().lower()
        except OSError as exc:
            errors.append(f'cannot read evidence {path}: {exc}')
            clean = False
            continue
        for marker in FORBIDDEN_MARKERS:
            if marker in raw:
                errors.append(f'forbidden evidence marker {marker!r} in {path}')
                clean = False
    return clean


def run() -> int:
    head = exact_head()
    errors: list[str] = []
    manifest = load_json(PRODUCERS, errors)
    need(manifest.get('implementation_commit') == head, 'producer manifest exact-head mismatch', errors)
    need(manifest.get('missing') == [], f"producer manifest missing={manifest.get('missing')}", errors)
    need(manifest.get('pending') == [], f"producer manifest pending={manifest.get('pending')}", errors)
    need(manifest.get('failed') == [], f"producer manifest failed={manifest.get('failed')}", errors)

    producer_rows = manifest.get('required_workflows', {})
    need(isinstance(producer_rows, dict), 'producer required_workflows must be object', errors)
    need(isinstance(producer_rows, dict) and set(producer_rows) == set(REQUIRED_PRODUCERS), 'producer workflow set mismatch', errors)
    producer_run_ids: dict[str, int] = {}
    producer_artifacts: dict[str, dict] = {}
    if isinstance(producer_rows, dict):
        for name in REQUIRED_PRODUCERS:
            row = producer_rows.get(name, {}) if isinstance(producer_rows.get(name, {}), dict) else {}
            artifact = row.get('artifact', {}) if isinstance(row.get('artifact', {}), dict) else {}
            need(row.get('head_sha') == head, f'{name} producer head mismatch', errors)
            need(row.get('status') == 'completed', f"{name} producer status={row.get('status')}", errors)
            need(row.get('conclusion') == 'success', f"{name} producer conclusion={row.get('conclusion')}", errors)
            need(isinstance(row.get('run_id'), int) and row.get('run_id', 0) > 0, f'{name} run id missing', errors)
            need(isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0, f'{name} artifact id missing', errors)
            need(isinstance(artifact.get('name'), str) and artifact.get('name', '').endswith(head), f'{name} artifact not exact-head named', errors)
            need(isinstance(artifact.get('digest'), str) and artifact.get('digest', '').startswith('sha256:'), f'{name} artifact digest missing', errors)
            need(isinstance(artifact.get('size_in_bytes'), int) and artifact.get('size_in_bytes', 0) > 0, f'{name} artifact size missing', errors)
            if isinstance(row.get('run_id'), int):
                producer_run_ids[name] = row['run_id']
            producer_artifacts[name] = {key: artifact.get(key) for key in ('id', 'name', 'digest', 'size_in_bytes')}

    contract = load_json(ROOT / 'contract-guard' / 'contract.json', errors)
    need(contract.get('node') == 'P16', 'contract node mismatch', errors)
    need(contract.get('status') == 'PASS', f"contract status={contract.get('status')}", errors)
    need(contract.get('implementation_commit') == head, 'contract implementation commit mismatch', errors)
    need(contract.get('contract_authority') == CONTRACT_AUTHORITY, 'contract authority mismatch', errors)
    need(contract.get('case_range') == 'P16-T001..P16-T029', 'contract case range mismatch', errors)
    need(contract.get('frozen_contract_preserved') is True, 'frozen contract not preserved', errors)
    review_phase = contract.get('review_phase')
    need(review_phase in ('pending', 'signed'), f'review phase invalid for T028 coherence: {review_phase}', errors)
    need(contract.get('merge_authoritative') is False, 'coherence stage must not be merge authoritative', errors)
    implementation_marker = ROOT / 'contract-guard' / 'implementation_commit.txt'
    need(implementation_marker.is_file() and implementation_marker.read_text(encoding='utf-8').strip() == head, 'contract implementation marker mismatch', errors)
    authority_marker = ROOT / 'contract-guard' / 'contract_authority.txt'
    need(authority_marker.is_file() and authority_marker.read_text(encoding='utf-8').strip() == CONTRACT_AUTHORITY, 'contract authority marker mismatch', errors)

    evidence_entries: list[dict] = []
    same_exact_head = True
    for number in range(1, 28):
        cid = f'P16-T{number:03d}'
        path = case_path(number)
        data = load_json(path, errors)
        case_id, evidence_head = case_identity(data, number)
        need(case_id == cid, f'{cid} case identity mismatch: {case_id}', errors)
        need(evidence_head == head, f'{cid} exact-head mismatch: {evidence_head}', errors)
        need(data.get('status') == 'PASS', f"{cid} status={data.get('status')}", errors)
        if evidence_head != head:
            same_exact_head = False
        if number <= 25:
            need(data.get('contract_authority') == CONTRACT_AUTHORITY, f'{cid} contract authority mismatch', errors)
            checks = data.get('checks') or {}
            need(isinstance(checks, dict) and bool(checks) and all(value is True for value in checks.values()), f'{cid} checks not all PASS', errors)
            evidence_policy = data.get('evidence_policy') or {}
            need(isinstance(evidence_policy, dict) and not any(evidence_policy.values()), f'{cid} unsafe evidence policy flags', errors)
        else:
            need(data.get('contract_authority') == CONTRACT_AUTHORITY, f'{cid} browser contract authority mismatch', errors)
            need(data.get('errors') == [], f"{cid} browser errors={data.get('errors')}", errors)
            details = data.get('details') or {}
            need(details.get('frozen_contract_completion') is True, f'{cid} browser frozen completion missing', errors)
            need(details.get('closure_claim') is False, f'{cid} premature closure claim', errors)
            checks = details.get('security_checks') or {}
            need(isinstance(checks, dict) and bool(checks) and all(value is True for value in checks.values()), f'{cid} browser security checks not all true', errors)
        if path.is_file():
            evidence_entries.append({
                'case_id': cid,
                'path': path.as_posix(),
                'sha256': sha256(path),
                'size_in_bytes': path.stat().st_size,
                'implementation_commit': evidence_head,
            })

    need(len(evidence_entries) == 27, f'expected 27 producer case files, got {len(evidence_entries)}', errors)
    need(same_exact_head, 'mixed-head P16 case evidence detected', errors)

    captures_026 = sorted((ROOT / 'captures').glob('P16-T026-*.png')) if (ROOT / 'captures').is_dir() else []
    captures_027 = sorted((ROOT / 'captures').glob('P16-T027-*.png')) if (ROOT / 'captures').is_dir() else []
    need(len(captures_026) >= 12, f'P16-T026 capture count {len(captures_026)} < 12', errors)
    need(len(captures_027) >= 12, f'P16-T027 capture count {len(captures_027)} < 12', errors)
    capture_entries = []
    for path in [*captures_026, *captures_027]:
        need(path.stat().st_size > 0, f'empty browser capture {path}', errors)
        capture_entries.append({'path': path.as_posix(), 'size_in_bytes': path.stat().st_size, 'sha256': sha256(path)})

    inspectable: list[Path] = []
    for dirname in ('contract-guard', 'api', 'security', 'risk', 'audit', 'domain', 'abuse', 'notifications', 'browser', 'captures'):
        inspectable.extend(collect_files(ROOT / dirname))
    is_secret_safe = secret_safe(inspectable, errors)

    index = {
        'node': 'P16',
        'case': 'P16-T028',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'contract_authority': CONTRACT_AUTHORITY,
        'same_exact_head': same_exact_head,
        'review_phase': review_phase,
        'producer_manifest': manifest,
        'case_evidence': evidence_entries,
        'browser_captures': capture_entries,
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')

    result = {
        'case_id': 'P16-T028',
        'status': 'PASS' if not errors else 'FAIL',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': head,
        'contract_authority': CONTRACT_AUTHORITY,
        'observations': {
            'input_evidence_count': len(evidence_entries),
            'same_exact_head': same_exact_head,
            't026_capture_count': len(captures_026),
            't027_capture_count': len(captures_027),
            'secret_safe': is_secret_safe,
            'producer_run_ids': producer_run_ids,
            'producer_artifacts': producer_artifacts,
            'producer_count': len(producer_run_ids),
            'mixed_head_rejected': True,
            'review_phase': review_phase,
            'review_phase_pending': review_phase == 'pending',
            'merge_authoritative': False,
        },
        'errors': errors,
    }
    RESULT.parent.mkdir(parents=True, exist_ok=True)
    RESULT.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    print(json.dumps(result, indent=2, sort_keys=True))
    return 0 if not errors else 1


if __name__ == '__main__':
    raise SystemExit(run())
