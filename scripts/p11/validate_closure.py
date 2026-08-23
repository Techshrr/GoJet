#!/usr/bin/env python3
"""P11-T020 exact-head pre-sign/final signed closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P11 = ROOT / 'artifacts' / 'v10' / 'P11'
API = P11 / 'api'
HEADERS = P11 / 'headers'
SITEMAP = P11 / 'sitemap'
BROWSER = P11 / 'browser'
RESULTS = P11 / 'results'
PLAN = P11 / 'test-plan.json'
REG = P11 / 'regression-manifest.json'
COH = P11 / 'evidence-index.json'
IDX = P11 / 'closure-evidence-index.json'
REVIEW = P11 / 'review.md'
CLOSURE = P11 / 'closure.json'
T020 = RESULTS / 'P11-T020.json'
P10A = P11 / 'inherited' / 'p10-authority.json'
P10 = P11 / 'inherited' / 'P10'
P10C = P10 / 'closure.json'
P10T = P10 / 'results' / 'P10-T020.json'
P10R = P10 / 'review.md'

WF = (
    'P00 Bootstrap and G0 Traceability',
    'P01 Engineering Foundation',
    'P02 Brand Foundation',
    'P03 Design System',
    'P04 Product Shells',
    'P05 Links Domain Contract',
    'P05 Real Integration',
    'P05 Workspace Browser',
    'P05 Closure',
    'P06 Custom Domains',
    'P06 Real Integration',
    'P06 Workspace Domains Browser',
    'P06 Closure',
    'P07 Analytics Contract',
    'P07 Real Integration',
    'P07 Workspace Analytics Browser',
    'P07 Closure',
    'P08 QR Contract',
    'P08 Real QR Integration',
    'P08 Workspace QR Browser',
    'P08 Evidence Coherence',
    'P09 Files Contract',
    'P09 Real Files and ClamAV Integration',
    'P09 Files Health and Installer Preflight',
    'P09 Workspace Files Browser',
    'P09 Evidence Coherence',
    'P10 Text Contract',
    'P10 Real Text Integration',
    'P10 Workspace Text Browser',
    'P10 Evidence Coherence',
    'P11 Bio Contract',
    'P11 Real Bio Integration',
    'P11 Workspace Bio Browser',
    'P11 Evidence Coherence',
)
CASES = tuple(f'P11-T{i:03d}' for i in range(1, 21))
INPUT = CASES[:-1]
PENDING = 'Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'
SIGNED = 'Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**'
P10_SHA = '7db4fca49ba3fd8e60600ecdf41847c7e2f94776'
P10_RUN = 32643830718
P10_ART = 9494371271
P10_DIG = 'sha256:6a4bcaed870c6432df40e1fe71cb38dd05a84789d3539ab10dabcbfefe450c50'


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec='seconds').replace('+00:00', 'Z')


def git(*args: str) -> str:
    return subprocess.check_output(['git', *args], cwd=ROOT, text=True).strip()


def load(path: Path) -> Any:
    return json.loads(path.read_text(encoding='utf-8'))


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(131072), b''):
            h.update(chunk)
    return h.hexdigest()


def req(ok: bool, message: str, errors: list[str]) -> None:
    if not ok:
        errors.append(message)


def case_path(cid: str) -> Path:
    number = int(cid[-3:])
    if number in (1, 2, 3, 4, 10, 12):
        return API / f'{cid}.json'
    if number == 14:
        return SITEMAP / f'{cid}.json'
    if 16 <= number <= 18:
        return BROWSER / f'{cid}.json'
    if number == 19:
        return RESULTS / f'{cid}.json'
    return HEADERS / f'{cid}.json'


def validate_plan(errors: list[str]) -> dict[str, Any]:
    req(PLAN.is_file(), 'missing test-plan.json', errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load(PLAN)
    except Exception as exc:
        errors.append(f'invalid test-plan: {exc}')
        return {}
    ids = tuple(item.get('id') for item in plan.get('cases', []) if isinstance(item, dict))
    req(ids == CASES, 'test-plan case range/order drift', errors)
    closure = plan.get('closure_contract', {})
    req(isinstance(closure, dict), 'closure_contract missing', errors)
    if isinstance(closure, dict):
        req(closure.get('same_exact_head_required') is True, 'same-exact-head contract drift', errors)
        req(closure.get('required_case_range') == 'P11-T001..P11-T020', 'closure case range drift', errors)
        req(closure.get('review_required') is True, 'review requirement drift', errors)
        req((closure.get('p0_max'), closure.get('p1_max'), closure.get('decision_required_max')) == (0, 0, 0), 'closure defect limits drift', errors)
        req(closure.get('pre_sign_phase') == 'pre-sign / merge_authoritative=false', 'pre-sign phase contract drift', errors)
        req(closure.get('signed_phase') == 'signed / merge_authoritative=true', 'signed phase contract drift', errors)
        predecessor = str(closure.get('predecessor_rule', ''))
        req(P10_SHA in predecessor and 'must not rerun P10 revision-specific closure' in predecessor, 'predecessor signed-authority rule drift', errors)
        scope = str(closure.get('gate_scope', ''))
        req('P11 CAP-BIO G3' in scope and 'P11 Bio UGC/noindex G7 subset only' in scope and 'full G7' in scope and 'P16' in scope, 'closure gate boundary drift', errors)
    capability = plan.get('capability_contract', {})
    req(isinstance(capability, dict), 'capability_contract missing', errors)
    if isinstance(capability, dict):
        req(capability.get('capability', {}).get('id') == 'CAP-BIO', 'CAP-BIO identity drift', errors)
        gate_scope = str(capability.get('gate_scope', ''))
        req('release-wide G7 remains later-owned by P18/P19/P20/P22' in gate_scope and 'P16' in gate_scope, 'CAP-BIO owner boundary drift', errors)
    deferred = plan.get('deferred_contract', {})
    req(isinstance(deferred, dict) and deferred.get('capability') == 'CAP-BIO-OPT-IN-INDEX' and deferred.get('status') == 'DEFERRED', 'deferred Bio indexing contract drift', errors)
    predecessor = plan.get('predecessor_signed_authority', {})
    req(
        isinstance(predecessor, dict)
        and predecessor.get('node') == 'P10'
        and predecessor.get('signed_source_commit') == P10_SHA
        and predecessor.get('closure_run_id') == P10_RUN
        and predecessor.get('artifact_id') == P10_ART
        and predecessor.get('artifact_digest') == P10_DIG
        and predecessor.get('phase') == 'signed'
        and predecessor.get('merge_authoritative') is True,
        'test-plan predecessor P10 authority drift',
        errors,
    )
    return plan


def validate_regression(head: str, errors: list[str]) -> dict[str, Any]:
    req(REG.is_file(), 'missing regression-manifest.json', errors)
    if not REG.is_file():
        return {}
    try:
        manifest = load(REG)
    except Exception as exc:
        errors.append(f'invalid regression manifest: {exc}')
        return {}
    req(manifest.get('implementation_commit') == head, 'regression manifest head mismatch', errors)
    workflows = manifest.get('required_workflows', {})
    req(isinstance(workflows, dict) and set(workflows) == set(WF), 'regression workflow set mismatch', errors)
    req(manifest.get('missing') == [] and manifest.get('pending') == [] and manifest.get('failed') == [], 'regression matrix not fully green', errors)
    if isinstance(workflows, dict):
        for name in WF:
            item = workflows.get(name, {})
            req(
                isinstance(item, dict)
                and item.get('head_sha') == head
                and item.get('status') == 'completed'
                and item.get('conclusion') == 'success'
                and isinstance(item.get('run_id'), int)
                and item.get('run_id', 0) > 0,
                f'{name} exact-head success record invalid',
                errors,
            )
    excluded = manifest.get('excluded_revision_specific_workflows', {})
    req(isinstance(excluded, dict), 'excluded revision-specific workflow rationale missing', errors)
    if isinstance(excluded, dict):
        req(set(excluded) == {'P08 Closure', 'P09 Closure', 'P10 Closure'}, 'revision-specific closure exclusion set drift', errors)
        req('inherited' in str(excluded.get('P10 Closure', '')).lower() and 'P10' in str(excluded.get('P10 Closure', '')), 'P10 Closure exclusion lacks inherited-authority rationale', errors)
    return manifest


def validate_cases(head: str, errors: list[str]) -> list[dict[str, Any]]:
    entries: list[dict[str, Any]] = []
    for cid in INPUT:
        path = case_path(cid)
        req(path.is_file(), f'missing evidence {cid}', errors)
        if not path.is_file():
            continue
        try:
            data = load(path)
        except Exception as exc:
            errors.append(f'invalid {cid}: {exc}')
            continue
        req(data.get('case_id', data.get('case')) == cid, f'{cid} identity invalid', errors)
        req(data.get('status') == 'PASS' and data.get('errors') == [], f'{cid} evidence not PASS', errors)
        req(data.get('implementation_commit') == head, f'{cid} exact-head mismatch', errors)
        if cid == 'P11-T019':
            obs = data.get('observations', {})
            req(obs.get('input_evidence_count') == 18, 'T019 input evidence count drift', errors)
            req(obs.get('same_exact_head') is True, 'T019 exact-head coherence false', errors)
            req(obs.get('capture_count', 0) >= 11, 'T019 capture count drift', errors)
            req(obs.get('runtime_error_files_clean') is True, 'T019 runtime error-file cleanliness false', errors)
            req(obs.get('sitemap_diff_inspectable') is True, 'T019 sitemap evidence not inspectable', errors)
            producer_ids = obs.get('producer_run_ids', {})
            expected_producers = {'P11 Bio Contract', 'P11 Real Bio Integration', 'P11 Workspace Bio Browser'}
            req(isinstance(producer_ids, dict) and set(producer_ids) == expected_producers, 'T019 producer bindings drift', errors)
            artifacts = obs.get('producer_artifacts', {})
            req(isinstance(artifacts, dict) and set(artifacts) == expected_producers, 'T019 producer artifact set drift', errors)
            if isinstance(artifacts, dict):
                for name, artifact in artifacts.items():
                    req(isinstance(artifact, dict) and isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0 and str(artifact.get('digest', '')).startswith('sha256:'), f'T019 {name} artifact binding invalid', errors)
        entries.append({'case_id': cid, 'path': str(path.relative_to(ROOT)), 'sha256': digest(path), 'status': data.get('status'), 'implementation_commit': data.get('implementation_commit')})
    req(tuple(item['case_id'] for item in entries) == INPUT, 'closure evidence set/order mismatch', errors)
    return entries


def validate_p10(errors: list[str]) -> dict[str, Any]:
    for path in (P10A, P10C, P10T, P10R):
        req(path.is_file(), f'missing inherited P10 authority: {path.name}', errors)
    if not all(path.is_file() for path in (P10A, P10C, P10T, P10R)):
        return {}
    try:
        authority = load(P10A)
        closure = load(P10C)
        t020 = load(P10T)
    except Exception as exc:
        errors.append(f'invalid inherited P10 JSON: {exc}')
        return {}
    req(authority.get('source_commit') == P10_SHA and authority.get('closure_run_id') == P10_RUN and authority.get('artifact_id') == P10_ART and authority.get('artifact_digest') == P10_DIG, 'P10 authority identity drift', errors)
    req(authority.get('workflow_head_sha') == P10_SHA and authority.get('workflow_conclusion') == 'success' and authority.get('artifact_expired') is False, 'P10 authority live metadata invalid', errors)
    req(authority.get('archive_sha256') == P10_DIG.removeprefix('sha256:'), 'P10 artifact archive digest mismatch', errors)
    req(closure.get('implementation_commit') == P10_SHA and closure.get('status') == 'PASS' and closure.get('phase') == 'signed' and closure.get('merge_authoritative') is True, 'P10 signed closure invalid', errors)
    req(closure.get('defects') == {'p0': 0, 'p1': 0, 'decision_required': 0}, 'P10 signed closure defect ledger invalid', errors)
    req(closure.get('input_evidence_count') == 19 and closure.get('required_regression_workflow_count') == 30, 'P10 signed closure evidence/matrix count invalid', errors)
    details = t020.get('details', {})
    req(t020.get('implementation_commit') == P10_SHA and t020.get('status') == 'PASS' and t020.get('errors') == [], 'P10 T020 evidence invalid', errors)
    req(details.get('closure_phase') == 'signed' and details.get('merge_authoritative') is True and details.get('input_evidence_count') == 19 and details.get('regression_workflow_count') == 30, 'P10 T020 signed authority invalid', errors)
    review = closure.get('review', {})
    t020_binding = closure.get('t020', {})
    req(review.get('review_sha256') == digest(P10R), 'P10 inherited review digest binding invalid', errors)
    req(t020_binding.get('sha256') == digest(P10T), 'P10 inherited T020 digest binding invalid', errors)
    return {
        'source_commit': P10_SHA,
        'closure_run_id': P10_RUN,
        'artifact_id': P10_ART,
        'artifact_digest': P10_DIG,
        'phase': closure.get('phase'),
        'merge_authoritative': closure.get('merge_authoritative'),
        'defects': closure.get('defects'),
        'closure_sha256': digest(P10C),
        'review_sha256': digest(P10R),
        't020_sha256': digest(P10T),
    }


def validate_review(errors: list[str]) -> dict[str, Any]:
    req(REVIEW.is_file(), 'missing P11 review.md', errors)
    if not REVIEW.is_file():
        return {'phase': 'missing', 'merge_authoritative': False, 'defects': None}
    text = REVIEW.read_text(encoding='utf-8')
    match = re.search(r'^Status:\s*.+$', text, re.M)
    line = match.group(0).strip() if match else ''
    req(line in (PENDING, SIGNED), 'review status invalid', errors)
    req('## 9. Signed-revision rule' in text or '## Signed-revision rule' in text, 'signed-revision rule missing', errors)
    req('CAP-BIO-OPT-IN-INDEX' in text and 'DEFERRED' in text, 'deferred indexing boundary missing', errors)
    req('P16' in text and 'P18/P19/P20/P22' in text, 'later-owner boundary missing', errors)
    if line == PENDING:
        return {'phase': 'pre-sign', 'merge_authoritative': False, 'status': 'PENDING', 'review_sha256': digest(REVIEW), 'defects': None}

    parent = git('rev-parse', 'HEAD^')
    sha_match = re.search(r'Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`', text)
    pre_sign = sha_match.group(1) if sha_match else None
    req(pre_sign == parent, f'pre-sign SHA {pre_sign} != parent {parent}', errors)
    identity_match = re.search(r'Accountable reviewer identity:\s*\*\*(.+?)\*\*', text)
    date_match = re.search(r'Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*', text)
    req(identity_match is not None and identity_match.group(1).strip() == 'GPT-5.6 Sol — CAP-BIO Technical Review', 'review identity missing/drifted', errors)
    req(date_match is not None, 'review date missing', errors)
    req('P11-T020: PASS — pre-sign closure / merge-authoritative=false' in text, 'pre-sign T020 record missing', errors)
    for role in ('Backend Lead', 'Frontend Lead', 'QA Lead', 'Accessibility Reviewer', 'Security Reviewer', 'Product/API Reviewer'):
        req(f'- {role}: APPROVED' in text, f'{role} approval missing', errors)
    req('- P0 defects: 0' in text and '- P1 defects: 0' in text and '- `DECISION REQUIRED`: 0' in text, 'review defect ledger nonzero/missing', errors)
    req('G3 P11' in text and 'PASS' in text, 'G3 P11 disposition missing', errors)
    req('G7 P11' in text and 'PASS' in text, 'G7 P11 disposition missing', errors)
    req('CAP-BIO-OPT-IN-INDEX' in text and 'DEFERRED' in text, 'signed review deferred indexing boundary missing', errors)
    req('P16' in text and 'later-owned' in text.lower(), 'signed review P16 later-owner boundary missing', errors)
    req('P18/P19/P20/P22' in text and 'later-owned' in text.lower(), 'signed review release-wide G7 later-owner boundary missing', errors)
    req('signed revision itself must rerun' in text.lower(), 'signed revision rerun rule missing', errors)
    return {
        'phase': 'signed',
        'merge_authoritative': True,
        'status': 'APPROVED',
        'review_sha256': digest(REVIEW),
        'pre_sign_implementation_sha': pre_sign,
        'accountable_reviewer_identity': identity_match.group(1).strip() if identity_match else None,
        'review_date': date_match.group(1) if date_match else None,
        'defects': {'p0': 0, 'p1': 0, 'decision_required': 0},
    }


def write_outputs(head: str, plan: dict[str, Any], regression: dict[str, Any], entries: list[dict[str, Any]], p10: dict[str, Any], review: dict[str, Any], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    status = 'PASS' if not errors else 'FAIL'
    phase = review.get('phase', 'unknown')
    authoritative = bool(review.get('merge_authoritative')) and not errors
    details = {
        'closure_phase': phase,
        'merge_authoritative': authoritative,
        'test_plan_cases': len(plan.get('cases', [])) if isinstance(plan, dict) else 0,
        'input_evidence_count': len(entries),
        'required_input_evidence_count': 19,
        'required_regression_workflows': list(WF),
        'regression_workflow_count': len(regression.get('required_workflows', {})) if isinstance(regression, dict) else 0,
        'same_exact_head_required': True,
        'inherited_p10_signed_closure': p10,
        'review': review,
        'gate_scope': 'P11 CAP-BIO G3 functional/API/browser subset and P11 Bio UGC/noindex G7 subset only; CAP-BIO-OPT-IN-INDEX remains DEFERRED, P16 destination-risk admin/review and full/release-wide G7 with P18/P19/P20/P22 remain later-owned.',
    }
    t020 = {
        'node': 'P11',
        'case_id': 'P11-T020',
        'name': 'same-exact-head-p11-signed-closure-and-affected-regression-matrix',
        'status': status,
        'generated_at': now(),
        'implementation_commit': head,
        'driver': 'python3 scripts/p11/validate.py --case P11-T020 --closure',
        'errors': list(errors),
        'details': details,
    }
    T020.write_text(json.dumps(t020, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    defects = review.get('defects') if phase == 'signed' else {'p0': None, 'p1': None, 'decision_required': None}
    gates = {
        'G3': 'PASS — P11 CAP-BIO functional/API/browser subset only',
        'G7': 'PASS — P11 Bio UGC/noindex/sitemap-exclusion subset only',
        'deferred': 'OPEN — CAP-BIO-OPT-IN-INDEX remains DEFERRED',
        'later_owners': 'OPEN — P16 destination-risk admin/review and full/release-wide G7 with P18/P19/P20/P22 remain later-owned',
    }
    closure = {
        'node': 'P11',
        'status': status,
        'phase': phase,
        'merge_authoritative': authoritative,
        'generated_at': now(),
        'implementation_commit': head,
        'case_range': 'P11-T001..P11-T020',
        'input_evidence_count': len(entries),
        'required_regression_workflow_count': len(WF),
        'defects': defects,
        'review': review,
        'inherited_p10_signed_closure': p10,
        'gate_scope': gates,
        't020': {'path': str(T020.relative_to(ROOT)), 'sha256': digest(T020), 'status': status, 'implementation_commit': head},
    }
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    index = {
        'node': 'P11',
        'generated_at': now(),
        'implementation_commit': head,
        'status': status,
        'test_plan_sha256': digest(PLAN) if PLAN.is_file() else None,
        'regression_manifest_sha256': digest(REG) if REG.is_file() else None,
        'coherence_evidence_index_sha256': digest(COH) if COH.is_file() else None,
        'review_sha256': digest(REVIEW) if REVIEW.is_file() else None,
        'input_evidence': entries,
        'coherence_result': next((item for item in entries if item['case_id'] == 'P11-T019'), None),
        'inherited_p10_signed_closure': p10,
        'closure_result': {'case_id': 'P11-T020', 'path': str(T020.relative_to(ROOT)), 'sha256': digest(T020), 'status': status, 'implementation_commit': head, 'phase': phase, 'merge_authoritative': authoritative},
        'closure_sha256': digest(CLOSURE),
    }
    IDX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def run_closure(flag: bool) -> int:
    if not flag:
        print('P11-T020: --closure is required')
        return 2
    head = git('rev-parse', 'HEAD')
    errors: list[str] = []
    plan = validate_plan(errors)
    regression = validate_regression(head, errors)
    entries = validate_cases(head, errors)
    p10 = validate_p10(errors)
    review = validate_review(errors)
    write_outputs(head, plan, regression, entries, p10, review, errors)
    try:
        t020 = load(T020)
        closure = load(CLOSURE)
        index = load(IDX)
        req(t020.get('implementation_commit') == head and closure.get('implementation_commit') == head and index.get('implementation_commit') == head, 'written closure head mismatch', errors)
        req(index.get('input_evidence') == entries and index.get('review_sha256') == digest(REVIEW) and index.get('closure_sha256') == digest(CLOSURE), 'written closure digest/input binding mismatch', errors)
        req(closure.get('phase') == review.get('phase'), 'written closure phase mismatch', errors)
    except Exception as exc:
        errors.append(f'invalid written closure: {exc}')
    if errors:
        write_outputs(head, plan, regression, entries, p10, review, errors)
        for error in errors:
            print(f'P11-T020: {error}')
        return 1
    if review.get('phase') == 'signed':
        print(f'P11-T020: PASS — 19/19 evidence, {len(WF)}/{len(WF)} exact-head workflows, inherited P10 signed closure and signed review green for {head}; merge-authoritative=true')
    else:
        print(f'P11-T020: PASS — pre-sign closure candidate with 19/19 evidence, {len(WF)}/{len(WF)} exact-head workflows and inherited P10 signed closure green for {head}; merge-authoritative=false')
    return 0


if __name__ == '__main__':
    raise SystemExit(run_closure(True))
