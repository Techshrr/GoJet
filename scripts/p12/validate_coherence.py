#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path
import subprocess

ROOT = Path('artifacts/v10/P12')
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
CONTRACT = ROOT / 'contract' / 'contract.json'
INDEX = ROOT / 'evidence-index.json'
RESULT = ROOT / 'results' / 'P12-T024.json'
REQUIRED_PRODUCERS = (
    'P12 Workspace Organization Contract',
    'P12 Real Workspace Organization Integration',
    'P12 Workspace Organization Browser',
)
PREDECESSOR = 'b59dfbe794f7d2f7bf63fdc79116217c5d893e87'
BASE_INTEGRATION = '638a6988c03eed6d287af0d2fdc63a3a3355ef68'

CASE_DIRS = {
    1: 'api', 2: 'api', 3: 'rbac', 4: 'rbac', 5: 'rbac', 6: 'api',
    7: 'api', 8: 'rbac', 9: 'api', 10: 'api', 11: 'api', 12: 'api',
    13: 'audit', 14: 'api', 15: 'api', 16: 'rbac', 17: 'security', 18: 'api',
    19: 'browser', 20: 'browser', 21: 'browser', 22: 'browser', 23: 'browser',
}
REQUIRED_RUNTIME_ERRORS = (
    'mysql-audit-safe.err',
    'mysql-campaigns.err',
    'mysql-folders.err',
    'mysql-invitations-safe.err',
    'mysql-link-organization.err',
    'mysql-link-tags.err',
    'mysql-memberships.err',
    'mysql-notification-state.err',
    'mysql-notifications-safe.err',
    'mysql-tags.err',
    'mysql-workspaces.err',
    'browser-memberships.err',
    'browser-notification-state.err',
    'browser-workspaces.err',
)
REQUIRED_CAPTURES = (
    'P12-T019-overview-switcher.png',
    'P12-T019-workspace-settings.png',
    'P12-T020-invitation-accepted.png',
    'P12-T021-tags-folders.png',
    'P12-T022-notifications.png',
    'P12-T023-api-offline.png',
    'P12-T023-desktop.png',
    'P12-T023-tablet.png',
    'P12-T023-mobile.png',
)

def head() -> str:
    supplied = os.environ.get('EXACT_HEAD', '').strip()
    if supplied:
        return supplied
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

def require(condition: bool, message: str, errors: list[str]) -> None:
    if not condition:
        errors.append(message)

def case_path(number: int) -> Path:
    return ROOT / CASE_DIRS[number] / f'P12-T{number:03d}.json'

def obs(cases: dict[str, dict], number: int) -> dict:
    value = cases.get(f'P12-T{number:03d}', {})
    return value.get('observations', {}) if isinstance(value, dict) else {}

def details(cases: dict[str, dict], number: int) -> dict:
    value = cases.get(f'P12-T{number:03d}', {})
    return value.get('details', {}) if isinstance(value, dict) else {}

def validate_semantics(cases: dict[str, dict], errors: list[str]) -> None:
    t1 = obs(cases, 1)
    require(t1.get('membership_role') == 'owner', 'T001 owner membership authority mismatch', errors)
    require(isinstance(t1.get('visible_workspace_ids'), list) and t1.get('visible_workspace_ids') == [t1.get('workspace_id')], 'T001 Workspace list leaked or omitted authority', errors)

    t2 = obs(cases, 2)
    require(t2 == {'final_version': 3, 'member_status': 403, 'stale_status': 409, 'viewer_status': 403}, 'T002 version/RBAC observation mismatch', errors)

    t3 = obs(cases, 3)
    require(t3.get('mysql_roles') == {'owner':'owner','admin':'admin','member':'member','viewer':'viewer'}, 'T003 MySQL role matrix mismatch', errors)
    require(t3.get('forged_owner_viewer_update') == 403 and t3.get('forged_viewer_owner_update') == 200, 'T003 client/test role header became authorization authority', errors)

    t4 = obs(cases, 4)
    require(t4.get('foreign_status') == t4.get('mutation_status') == t4.get('unknown_status') == 403 and t4.get('error_code') == 'forbidden', 'T004 cross-Workspace denial/no-leak mismatch', errors)

    t5 = obs(cases, 5)
    require(all(t5.get(k) == 403 for k in ('admin_grant_owner','admin_touch_owner','member_self_escalate','viewer_manage')), 'T005 prohibited role mutation boundary mismatch', errors)
    require(t5.get('admin_normal_manage') == 200 and t5.get('owner_promote') == 200, 'T005 allowed role mutation boundary mismatch', errors)

    t6 = obs(cases, 6)
    require(t6.get('admin_owner_status') == 403 and t6.get('owner_role_status') == 400 and t6.get('duplicate_status') == 409 and t6.get('revoke_status') == 204, 'T006 invite create/revoke authority mismatch', errors)
    require(t6.get('raw_token_persisted') is False and t6.get('token_hash_length') == 64, 'T006 raw invitation token persistence/hash mismatch', errors)

    t7 = obs(cases, 7)
    require(t7.get('unauthenticated') == 401 and t7.get('mismatch_accept') == 403 and t7.get('accepted') == 200 and t7.get('replay') == 409 and t7.get('unknown') == 404, 'T007 invitation lifecycle status mismatch', errors)
    require(t7.get('expired') == ['expired', 410] and t7.get('revoked') == ['revoked', 409] and t7.get('rejected') == [204, 409], 'T007 expired/revoked/rejected lifecycle mismatch', errors)
    valid = t7.get('valid', {})
    require(valid.get('account_match') is True and valid.get('role') == 'member' and valid.get('status') == 'pending' and valid.get('workspace_name') == 'T007', 'T007 safe authenticated inspection mismatch', errors)
    require(set(valid) == {'workspace_id','workspace_name','role','status','expires_at','account_match'}, 'T007 inspection exposed non-safe fields', errors)

    t8 = obs(cases, 8)
    outcomes = t8.get('outcomes', [])
    require(t8.get('remaining_owner_count') == 1 and isinstance(outcomes, list) and len(outcomes) == 2, 'T008 last-owner concurrency shape mismatch', errors)
    require(sum(1 for row in outcomes if not row.get('error')) == 1 and sum(1 for row in outcomes if row.get('error') == 'last workspace owner is protected') == 1, 'T008 last-owner concurrency invariant mismatch', errors)

    t9 = obs(cases, 9)
    require(t9 == {'final_name':'组织二','final_version':3,'invalid':400,'member':403,'stale':409,'viewer':403}, 'T009 organization optimistic/RBAC mismatch', errors)

    t10 = obs(cases, 10)
    require(str(t10.get('campaign_id','')).startswith('cmp_') and t10.get('clicks') == 1 and t10.get('conversion_status') == 201 and t10.get('conversions') == 1 and t10.get('report_state') == 'success', 'T010 P07 campaign continuity mismatch', errors)

    t11 = obs(cases, 11)
    require(t11.get('duplicate_status') == 409 and t11.get('filtered_link_ids') == [2] and t11.get('in_use_delete') == 409 and t11.get('tag_name') == t11.get('normalized_name') == '重要标签', 'T011 tag organization/Unicode/filter mismatch', errors)

    t12 = obs(cases, 12)
    require(t12.get('filtered_link_ids') == [4] and t12.get('folder_name') == '客户资料' and t12.get('foreign_folder_status') == 403 and t12.get('in_use_delete') == 409 and t12.get('unselected_link_id') == 5, 'T012 folder explicit-ID/cross-Workspace mismatch', errors)

    t13 = obs(cases, 13)
    rows = t13.get('audit_rows', [])
    require(t13.get('success_status') == 200 and t13.get('denied_status') == 403 and t13.get('raw_secret_absent') is True, 'T013 audit/redaction status mismatch', errors)
    require(isinstance(rows, list) and len(rows) == 2 and '[redacted]' in rows[0] and '\tdenied\t' in rows[1], 'T013 audit success/denied/redacted rows mismatch', errors)

    t14 = obs(cases, 14)
    require(t14.get('first_inserted') is True and t14.get('replay_inserted') is False and t14.get('api_count') == t14.get('db_count') == 1, 'T014 notification dedupe mismatch', errors)

    t15 = obs(cases, 15)
    require(t15.get('owner_counts') == [2,1,2,0] and t15.get('mark_all_updated') == 2 and t15.get('other_unread') == 1 and t15.get('cross_recipient_status') == 404, 'T015 recipient-scoped notification read-state mismatch', errors)

    t16 = obs(cases, 16)
    require(t16.get('authorized') == '/app/links/6' and t16.get('foreign_requested') == '/app/links/7', 'T016 deep-link fixture authority mismatch', errors)
    rendered = t16.get('rendered_links', [])
    require('/app/links/6' in rendered and '/app/settings/workspace' in rendered and '/app/notifications' in rendered and '/app/links/7' not in rendered, 'T016 deep-link reauthorization/fallback mismatch', errors)

    t17 = obs(cases, 17)
    pairs = t17.get('redacted_pairs', [])
    require(t17.get('raw_secrets_absent') is True and isinstance(pairs, list) and len(pairs) >= 4, 'T017 notification secret redaction evidence missing', errors)
    require(any(pair == ['[redacted]','[redacted]'] for pair in pairs) and any(pair == ['JWT','[redacted]'] for pair in pairs) and any(pair == ['Credential','[redacted]'] for pair in pairs), 'T017 email/JWT/Bearer redaction mismatch', errors)

    t18 = obs(cases, 18)
    require(t18.get('offline_status') == 500, 'T018 dependency failure masqueraded as success', errors)
    partial, stale = t18.get('partial_state', {}), t18.get('stale_state', {})
    require(partial.get('status') == 'partial' and partial.get('state_reason') == 'dependency_lag' and partial == t18.get('producer_partial'), 'T018 partial-state producer/API mismatch', errors)
    require(stale.get('status') == 'stale' and stale.get('state_reason') == 'source_stale' and stale == t18.get('producer_stale'), 'T018 stale-state producer/API mismatch', errors)

    d19 = details(cases, 19)
    require(d19.get('workspace_switch') == {'primary':'ws-p12-browser','alternate':'ws-p12-alt'} and d19.get('settings_persisted') is True and d19.get('settings_version') == 2 and d19.get('viewer_read_only') is True, 'T019 overview/switcher/settings authority mismatch', errors)
    f19 = d19.get('frozen_contract_completion', {})
    require(f19.get('visible_switcher_memberships_only') is True and 'denied' in str(f19.get('unauthorized_switch','')), 'T019 unauthorized switch completion mismatch', errors)

    d20 = details(cases, 20)
    require(d20.get('accepted_role') == 'member' and d20.get('account_mismatch_fail_closed') is True and d20.get('raw_token_persisted') is False, 'T020 invitation browser lifecycle mismatch', errors)
    require(d20.get('safe_fields') == ['workspace_name','role','status','expires_at','account_match'], 'T020 safe invitation fields mismatch', errors)
    f20 = d20.get('frozen_contract_completion', {})
    require(f20.get('last_owner_protected') is True and set(f20.get('invite_states_added', [])) == {'rejected','expired','revoked','unauthenticated'} and f20.get('raw_tokens_recorded_in_evidence') is False, 'T020 frozen invitation/last-owner states mismatch', errors)

    d21 = details(cases, 21)
    require(d21.get('organization_version') == 2 and d21.get('campaign_created_and_archived') is True and d21.get('unicode_tag_and_folder') is True and d21.get('app_folders_route_absent') is True, 'T021 organization/campaign/tag/folder browser mismatch', errors)
    f21 = d21.get('frozen_contract_completion', {})
    require(all(f21.get(k) is True for k in ('folder_route_remains_absent','viewer_organization_read_only','viewer_tag_folder_read_only')), 'T021 viewer/folder-route frozen completion mismatch', errors)

    d22 = details(cases, 22)
    expected_link = '/app/settings/workspace'
    require(d22.get('producer_inserted') == 2 and d22.get('unread_lifecycle') == [2,1,0,1] and d22.get('filter_verified') == 'security', 'T022 notification producer/filter/read lifecycle mismatch', errors)
    require(all(d22.get(k) == expected_link for k in ('producer_deep_link','stored_deep_link','api_deep_link','scoped_dom_deep_link')), 'T022 producer->DB->API->DOM deep-link authority mismatch', errors)
    f22 = d22.get('frozen_contract_completion', {})
    require(f22.get('shell_badge_and_popover') is True and f22.get('view_all') is True and f22.get('explicit_states') == ['partial','stale','error'], 'T022 shell/explicit-state completion mismatch', errors)

    d23 = details(cases, 23)
    layouts = d23.get('layouts', {})
    require(set(layouts) == {'desktop','tablet','mobile'} and all(row.get('root_overflow_px') == 0 and row.get('body_overflow_px') == 0 and row.get('clipped') == [] for row in layouts.values()), 'T023 canonical responsive layout mismatch', errors)
    require(d23.get('viewer_read_only') is True and d23.get('native_api_recovered') is True and d23.get('offline', {}).get('page_state') == 'error' and d23.get('offline', {}).get('shell_state') == 'api-offline', 'T023 viewer/offline/recovery mismatch', errors)
    f23 = d23.get('frozen_contract_completion', {})
    require(f23.get('esc_focus_return') is True and f23.get('mobile_full_height_sheet') is True and f23.get('overlay_stack_max') == 1, 'T023 sheet/focus/overlay completion mismatch', errors)
    width320 = f23.get('width_320', {})
    require(width320.get('root_overflow_px') == 0 and width320.get('body_overflow_px') == 0 and width320.get('clipped') == [], 'T023 320px layout mismatch', errors)
    reduced = f23.get('reduced_motion', {})
    require(reduced.get('matches') is True and bool(reduced.get('transition')), 'T023 reduced-motion evidence missing', errors)
    require('no visible unlabeled interactive controls' in str(d23.get('accessibility','')) and 'one main h1' in str(d23.get('accessibility','')), 'T023 accessibility evidence mismatch', errors)

def validate_runtime(errors: list[str]) -> list[dict[str, object]]:
    runtime = ROOT / 'runtime'
    rows: list[dict[str, object]] = []
    for name in REQUIRED_RUNTIME_ERRORS:
        path = runtime / name
        require(path.is_file(), f'missing runtime error evidence {name}', errors)
        if not path.is_file():
            continue
        size = path.stat().st_size
        require(size == 0, f'runtime error evidence not empty: {name} ({size} bytes)', errors)
        rows.append({'path': str(path), 'size_bytes': size, 'sha256': digest(path)})
    return rows

def validate_captures(errors: list[str]) -> list[dict[str, object]]:
    root = ROOT / 'captures'
    rows: list[dict[str, object]] = []
    for name in REQUIRED_CAPTURES:
        path = root / name
        require(path.is_file(), f'missing browser capture {name}', errors)
        if not path.is_file():
            continue
        size = path.stat().st_size
        require(size > 0, f'empty browser capture {name}', errors)
        rows.append({'path': str(path), 'size_bytes': size, 'sha256': digest(path)})
    return rows

def run() -> int:
    errors: list[str] = []
    exact = head()
    (ROOT / 'results').mkdir(parents=True, exist_ok=True)

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
        require(isinstance(artifact.get('digest'), str) and artifact.get('digest','').startswith('sha256:'), f'{name} artifact digest missing', errors)
        require(isinstance(artifact.get('size_in_bytes'), int) and artifact.get('size_in_bytes', 0) > 0, f'{name} artifact size missing', errors)

    contract = load_json(CONTRACT, errors)
    require(contract.get('status') == 'PASS' and contract.get('errors') == [], 'contract artifact is not PASS', errors)
    require(contract.get('implementation_commit') == exact, 'contract artifact exact-head mismatch', errors)
    require(contract.get('case_range') == 'P12-T001..P12-T025' and contract.get('case_count') == 25, 'contract case range/count drift', errors)
    require(contract.get('base_integration_commit') == BASE_INTEGRATION, 'contract base integration drift', errors)
    require(contract.get('predecessor_signed_source') == PREDECESSOR, 'contract predecessor signed source drift', errors)

    cases: dict[str, dict] = {}
    entries: list[dict[str, object]] = []
    for number in range(1, 24):
        cid = f'P12-T{number:03d}'
        path = case_path(number)
        data = load_json(path, errors)
        cases[cid] = data
        require(data.get('status') == 'PASS', f'{cid} status is not PASS', errors)
        require(data.get('errors') == [], f'{cid} errors not empty', errors)
        require(data.get('implementation_commit') == exact, f'{cid} exact-head mismatch', errors)
        if path.is_file():
            entries.append({'case_id':cid,'path':str(path),'implementation_commit':data.get('implementation_commit'),'sha256':digest(path)})

    validate_semantics(cases, errors)
    runtime_errors = validate_runtime(errors)
    captures = validate_captures(errors)

    index = {
        'node': 'P12',
        'generated_at': datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00','Z'),
        'implementation_commit': exact,
        'input_evidence_count': len(entries),
        'producer_manifest_sha256': digest(PRODUCERS) if PRODUCERS.is_file() else None,
        'contract_sha256': digest(CONTRACT) if CONTRACT.is_file() else None,
        'cases': entries,
        'captures': captures,
        'runtime_error_files': runtime_errors,
        'producer_run_ids': {name: required.get(name,{}).get('run_id') for name in REQUIRED_PRODUCERS},
        'producer_artifacts': {name: required.get(name,{}).get('artifact') for name in REQUIRED_PRODUCERS},
    }
    INDEX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')

    payload = {
        'case_id': 'P12-T024',
        'status': 'PASS' if not errors else 'FAIL',
        'implementation_commit': exact,
        'errors': errors,
        'observations': {
            'input_evidence_count': len(entries),
            'same_exact_head': len(entries) == 23 and all(item.get('implementation_commit') == exact for item in entries),
            'producer_run_ids': index['producer_run_ids'],
            'producer_artifacts': index['producer_artifacts'],
            'capture_count': len(captures),
            'runtime_error_files_clean': len(runtime_errors) == len(REQUIRED_RUNTIME_ERRORS) and all(item.get('size_bytes') == 0 for item in runtime_errors),
            'evidence_index_sha256': digest(INDEX),
            'producer_manifest_sha256': index['producer_manifest_sha256'],
            'contract_sha256': index['contract_sha256'],
            'authority': 'real MySQL/Redis + native Go platformapi + P05/P07 continuity + built owner/viewer/invitee/unauthenticated Workspace Chromium; exact-head producer artifacts only; no request interception/manual/fixture-only success authority',
        },
    }
    RESULT.write_text(json.dumps(payload, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    if errors:
        print('\n'.join(errors))
        return 1
    print(f'P12-T024 exact-head evidence coherence PASS on {exact}')
    return 0

if __name__ == '__main__':
    raise SystemExit(run())
