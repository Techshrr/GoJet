#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
from datetime import datetime, timezone
from pathlib import Path
import subprocess

ROOT = Path('artifacts/v10/P11')
RESULTS = ROOT / 'results'
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
CONTRACT = ROOT / 'contract' / 'contract.json'
INDEX = ROOT / 'evidence-index.json'
T019 = RESULTS / 'P11-T019.json'
RUNTIME = ROOT / 'runtime'
REQUIRED_PRODUCERS = ('P11 Bio Contract', 'P11 Real Bio Integration', 'P11 Workspace Bio Browser')


def head() -> str:
    return subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip()


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


def load_json(path: Path, errors: list[str]):
    if not path.is_file():
        errors.append(f'missing {path}')
        return {}
    try:
        return json.loads(path.read_text(encoding='utf-8'))
    except Exception as exc:
        errors.append(f'invalid JSON {path}: {exc}')
        return {}


def case_path(number: int) -> Path:
    cid = f'P11-T{number:03d}'
    if number in (1, 2, 3, 4, 10, 12):
        return ROOT / 'api' / f'{cid}.json'
    if number == 14:
        return ROOT / 'sitemap' / f'{cid}.json'
    if 16 <= number <= 18:
        return ROOT / 'browser' / f'{cid}.json'
    return ROOT / 'headers' / f'{cid}.json'


def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)


def validate_semantics(cases: dict[str, dict], errors: list[str]) -> None:
    obs = lambda n: cases.get(f'P11-T{n:03d}', {}).get('observations', {}) if isinstance(cases.get(f'P11-T{n:03d}'), dict) else {}

    t1 = obs(1)
    require(t1.get('normalized_destination') == 'https://example.com/b?a=1&z=2', 'T001 destination normalization mismatch', errors)
    require(t1.get('quota') == {'used': 1, 'limit': 3, 'reached': False}, 'T001 quota observation mismatch', errors)

    t2 = obs(2)
    require(t2.get('before') == t2.get('after') == 1 and t2.get('statuses') == [400, 400, 400, 400], 'T002 validation/fail-atomic observation mismatch', errors)

    t3 = obs(3)
    require(t3 == {'signed_out': 403, 'viewer_patch': 403, 'viewer_publish': 403, 'cross_workspace': 404}, 'T003 auth/RBAC/tenant observation mismatch', errors)

    t4 = obs(4)
    require(t4.get('stale_status') == 409 and t4.get('current_version') == 2 and t4.get('title') == 'Version 2', 'T004 optimistic conflict observation mismatch', errors)

    t5 = obs(5)
    require(t5.get('html_status') == 410 and t5.get('api_status') == 410 and t5.get('stale_status') == 410, 'T005 durable removal observation mismatch', errors)

    t6 = obs(6)
    require(t6.get('html_status') == 200 and t6.get('api_status') == 200 and t6.get('published_version') == 2, 'T006 published authority observation mismatch', errors)
    require(str(t6.get('risk_key', '')).startswith('risk:bio-child:'), 'T006 risk authority key missing', errors)

    t7 = obs(7)
    require(t7 == {'draft_status': 404, 'paused_html': 200, 'paused_api': 200}, 'T007 draft/pause authority mismatch', errors)

    t8 = obs(8)
    require(t8.get('ordered') is True and t8.get('rel_count') == 2, 'T008 ordered UGC nofollow observation mismatch', errors)

    t9 = obs(9)
    urls = t9.get('api_urls')
    require(t9.get('html_status') == 200 and isinstance(urls, list) and len(urls) == 3 and urls[0] == 'https://example.com/a' and urls[1:] == [None, None], 'T009 review/blocked fail-closed observation mismatch', errors)

    t10 = obs(10)
    require(t10.get('old_fingerprint') and t10.get('new_fingerprint') and t10.get('old_fingerprint') != t10.get('new_fingerprint') and t10.get('public_status') == 200, 'T010 destination fingerprint invalidation mismatch', errors)

    t11 = obs(11)
    require(t11 == {'published': 200, 'paused': 200, 'draft': 404, 'removed': 410, 'unknown': 404}, 'T011 public API lifecycle observation mismatch', errors)

    t12 = obs(12)
    require(t12.get('quota_limit') == 3 and t12.get('persisted_count') == 3 and t12.get('over_quota_status') == 429 and t12.get('unresolved_publish_status') == 409 and t12.get('authoritative_status') == 'draft', 'T012 quota/unresolved-publish authority mismatch', errors)

    checks = obs(13).get('checks', [])
    expected_labels = {'published', 'paused', 'risk-blocked-child', 'removed', 'draft', 'unknown'}
    require(isinstance(checks, list) and {row.get('label') for row in checks} == expected_labels, 'T013 noindex lifecycle set mismatch', errors)
    require(bool(checks) and all(row.get('html_x_robots_tag') == 'noindex, nofollow' and row.get('api_x_robots_tag') == 'noindex, nofollow' and row.get('api_body_has_workspace_id') is False for row in checks), 'T013 noindex/header/leakage observation mismatch', errors)

    t14 = obs(14)
    require(t14.get('canonical_present') is False and t14.get('hreflang_present') is False and t14.get('structured_data_present') is False and t14.get('sitemap_bio_hits') == [], 'T014 sitemap/canonical/hreflang/structured-data observation mismatch', errors)
    require(t14.get('statuses') == {'published': 200, 'draft': 404, 'removed': 410, 'unknown': 404}, 'T014 HTTP lifecycle observation mismatch', errors)

    t15 = obs(15)
    require(t15.get('create_unknown_field_status') == 400 and t15.get('update_unknown_field_status') == 400 and t15.get('query_status') == 200 and t15.get('query_x_robots_tag') == 'noindex, nofollow', 'T015 deferred index input/query observation mismatch', errors)
    require(t15.get('forbidden_source_hits') == [] and t15.get('persisted_index_authority') is False, 'T015 deferred index authority leaked into source/persistence', errors)

    d16 = cases.get('P11-T016', {}).get('details', {})
    history16 = d16.get('loading_empty_states', [])
    require(history16[:2] == ['bio-list:loading', 'bio-list:empty'], 'T016 loading/empty route-state observation mismatch', errors)
    require(all(d16.get(key) is True for key in ('preview', 'edit', 'read_only', 'quota', 'error', 'publish_error')) and '/app/bio/' in str(d16.get('created_url', '')), 'T016 route-backed list/create state evidence mismatch', errors)

    d17 = cases.get('P11-T017', {}).get('details', {})
    history17 = d17.get('loading_draft_states', [])
    require(history17[:2] == ['bio-detail:loading', 'bio-detail:draft'], 'T017 loading/draft route-state observation mismatch', errors)
    require(all(d17.get(key) is True for key in ('published', 'review', 'blocked', 'conflict', 'deleted')) and d17.get('public_preview_status') == 200 and str(d17.get('public_href', '')).startswith('/p/'), 'T017 detail/public/risk/conflict/deleted evidence mismatch', errors)

    d18 = cases.get('P11-T018', {}).get('details', {})
    layouts = d18.get('layouts', [])
    require(len(d18.get('captures', [])) >= 11, 'T018 capture count below contract', errors)
    require(bool(layouts) and all(row.get('root_overflow_px') == 0 and row.get('body_overflow_px') == 0 and row.get('clipped') == [] for row in layouts), 'T018 responsive layout overflow/clipping mismatch', errors)
    focus = d18.get('keyboard_focus', {})
    require(isinstance(focus, dict) and (focus.get('outline') != 'none' or focus.get('boxShadow') != 'none'), 'T018 keyboard-visible focus evidence missing', errors)
    require(bool(d18.get('non_color_state_text')) and d18.get('non_color_risk_summary') is not None and d18.get('reduced_motion_usable') is True, 'T018 non-color/reduced-motion evidence mismatch', errors)


def validate_runtime(errors: list[str]) -> list[dict[str, object]]:
    rows: list[dict[str, object]] = []
    required = (
        'mysql-bio-pages.err',
        'mysql-bio-links.err',
        'mysql-bio-counters.err',
        'mysql-bio-audit.err',
        'redis-bio-risk-keys.err',
    )
    for name in required:
        path = RUNTIME / name
        require(path.is_file(), f'missing runtime evidence {name}', errors)
        if not path.is_file():
            continue
        size = path.stat().st_size
        require(size == 0, f'runtime evidence error file not empty: {name} ({size} bytes)', errors)
        rows.append({'path': str(path), 'size_bytes': size, 'sha256': digest(path)})
    return rows


def run() -> int:
    errors: list[str] = []
    exact = head()
    RESULTS.mkdir(parents=True, exist_ok=True)

    manifest = load_json(PRODUCERS, errors)
    require(manifest.get('implementation_commit') == exact, 'producer manifest exact-head mismatch', errors)
    required = manifest.get('required_workflows', {}) if isinstance(manifest, dict) else {}
    require(set(required) == set(REQUIRED_PRODUCERS), f'producer set mismatch: {sorted(required)}', errors)
    for name in REQUIRED_PRODUCERS:
        entry = required.get(name, {})
        require(entry.get('head_sha') == exact, f'{name} head mismatch', errors)
        require(entry.get('status') == 'completed' and entry.get('conclusion') == 'success', f'{name} not successful', errors)
        require(isinstance(entry.get('run_id'), int) and entry.get('run_id', 0) > 0, f'{name} run id missing', errors)
        artifact = entry.get('artifact', {})
        require(isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0, f'{name} artifact id missing', errors)
        require(isinstance(artifact.get('digest'), str) and artifact.get('digest', '').startswith('sha256:'), f'{name} artifact digest missing', errors)

    contract = load_json(CONTRACT, errors)
    require(contract.get('status') == 'PASS' and contract.get('errors') == [], 'contract artifact is not PASS', errors)
    require(contract.get('implementation_commit') == exact, 'contract artifact exact-head mismatch', errors)
    require(contract.get('case_range') == 'P11-T001..P11-T020' and contract.get('case_count') == 20, 'contract case range/count drift', errors)

    cases: dict[str, dict] = {}
    entries: list[dict[str, object]] = []
    for number in range(1, 19):
        cid = f'P11-T{number:03d}'
        path = case_path(number)
        data = load_json(path, errors)
        cases[cid] = data
        require(data.get('status') == 'PASS', f'{cid} status is not PASS', errors)
        require(data.get('errors') == [], f'{cid} errors not empty', errors)
        require(data.get('implementation_commit') == exact, f'{cid} exact-head mismatch', errors)
        if path.is_file():
            entries.append({'case_id': cid, 'path': str(path), 'implementation_commit': data.get('implementation_commit'), 'sha256': digest(path)})
    validate_semantics(cases, errors)
    runtime_errors = validate_runtime(errors)

    captures = []
    for path in sorted((ROOT / 'captures').glob('P11-T018-*.png')):
        captures.append({'path': str(path), 'sha256': digest(path), 'size_bytes': path.stat().st_size})
    require(len(captures) >= 11, f'expected >=11 T018 captures, got {len(captures)}', errors)

    index = {
        'node': 'P11',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z'),
        'implementation_commit': exact,
        'input_evidence_count': len(entries),
        'producer_manifest_sha256': digest(PRODUCERS) if PRODUCERS.is_file() else None,
        'contract_sha256': digest(CONTRACT) if CONTRACT.is_file() else None,
        'cases': entries,
        'captures': captures,
        'runtime_error_files': runtime_errors,
        'producer_run_ids': {name: required.get(name, {}).get('run_id') for name in REQUIRED_PRODUCERS},
        'producer_artifacts': {name: required.get(name, {}).get('artifact') for name in REQUIRED_PRODUCERS},
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')

    payload = {
        'case_id': 'P11-T019',
        'status': 'PASS' if not errors else 'FAIL',
        'implementation_commit': exact,
        'errors': errors,
        'observations': {
            'input_evidence_count': len(entries),
            'same_exact_head': all(item.get('implementation_commit') == exact for item in entries),
            'producer_run_ids': index['producer_run_ids'],
            'producer_artifacts': index['producer_artifacts'],
            'capture_count': len(captures),
            'runtime_error_files_clean': bool(runtime_errors) and all(item.get('size_bytes') == 0 for item in runtime_errors),
            'sitemap_diff_inspectable': (ROOT / 'sitemap' / 'P11-T014.json').is_file(),
            'evidence_index_sha256': digest(INDEX),
            'producer_manifest_sha256': index['producer_manifest_sha256'],
            'contract_sha256': index['contract_sha256'],
            'authority': 'real MySQL/Redis + native Go platformapi + route-backed owner/viewer Workspace/Public Chromium + raw Bio UGC HTTP/SEO evidence; no mocked request interception/manual/fixture-only success authority',
        },
    }
    T019.write_text(json.dumps(payload, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    if errors:
        print('\n'.join(errors))
        return 1
    print(f'P11-T019 exact-head coherence PASS on {exact}')
    return 0


if __name__ == '__main__':
    raise SystemExit(run())
