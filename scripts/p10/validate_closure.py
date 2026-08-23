#!/usr/bin/env python3
"""P10-T020 exact-head pre-sign/final signed closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P10 = ROOT / 'artifacts' / 'v10' / 'P10'
API = P10 / 'api'
HEADERS = P10 / 'headers'
BROWSER = P10 / 'browser'
RESULTS = P10 / 'results'
PLAN = P10 / 'test-plan.json'
REG = P10 / 'regression-manifest.json'
COH = P10 / 'evidence-index.json'
IDX = P10 / 'closure-evidence-index.json'
REVIEW = P10 / 'review.md'
CLOSURE = P10 / 'closure.json'
T020 = RESULTS / 'P10-T020.json'
P09A = P10 / 'inherited' / 'p09-authority.json'
P09 = P10 / 'inherited' / 'P09'
P09C = P09 / 'closure.json'
P09T = P09 / 'results' / 'P09-T027.json'
P09R = P09 / 'review.md'

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
)
CASES = tuple(f'P10-T{i:03d}' for i in range(1, 21))
INPUT = CASES[:-1]
PENDING = 'Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'
SIGNED = 'Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**'
P09_SHA = 'eafa369a9c150c22c2c14c9f21848a9544f4f96a'
P09_RUN = 32618657967
P09_ART = 9487743843
P09_DIG = 'sha256:f12aeeb5503bf375314f1d13a2d9833180d6617322765cef2aae0d728cc278d7'


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
    if number <= 5 or number in (12, 15):
        return API / f'{cid}.json'
    if number <= 14:
        return HEADERS / f'{cid}.json'
    if number <= 18:
        return BROWSER / f'{cid}.json'
    return RESULTS / f'{cid}.json'


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
        req(closure.get('required_case_range') == 'P10-T001..P10-T020', 'closure case range drift', errors)
        req(closure.get('review_required') is True, 'review requirement drift', errors)
        req((closure.get('p0_max'), closure.get('p1_max'), closure.get('decision_required_max')) == (0, 0, 0), 'closure defect limits drift', errors)
        req(closure.get('pre_sign_phase') == 'pre-sign / merge_authoritative=false', 'pre-sign phase contract drift', errors)
        req(closure.get('signed_phase') == 'signed / merge_authoritative=true', 'signed phase contract drift', errors)
        scope = str(closure.get('gate_scope', ''))
        req('P10 CAP-TEXT G3' in scope and 'P10 G7 UGC/noindex subset only' in scope and 'full G7' in scope, 'closure gate boundary drift', errors)
    capability = plan.get('capability_contract', {})
    req(isinstance(capability, dict), 'capability_contract missing', errors)
    if isinstance(capability, dict):
        req(capability.get('capability', {}).get('id') == 'CAP-TEXT', 'CAP-TEXT identity drift', errors)
        req('release-wide G7 remains later-owned by P18/P19/P20/P22' in str(capability.get('gate_scope', '')), 'release-wide G7 owner boundary drift', errors)
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
        req(set(excluded) == {'P08 Closure', 'P09 Closure'}, 'revision-specific closure exclusion set drift', errors)
        req('inherited' in str(excluded.get('P09 Closure', '')).lower(), 'P09 Closure exclusion lacks inherited-authority rationale', errors)
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
        if cid == 'P10-T019':
            obs = data.get('observations', {})
            req(obs.get('input_evidence_count') == 18, 'T019 input evidence count drift', errors)
            req(obs.get('same_exact_head') is True, 'T019 exact-head coherence false', errors)
            req(obs.get('capture_count', 0) >= 11, 'T019 capture count drift', errors)
            producer_ids = obs.get('producer_run_ids', {})
            req(isinstance(producer_ids, dict) and set(producer_ids) == {'P10 Text Contract', 'P10 Real Text Integration', 'P10 Workspace Text Browser'}, 'T019 producer bindings drift', errors)
            artifacts = obs.get('producer_artifacts', {})
            req(isinstance(artifacts, dict) and set(artifacts) == set(producer_ids), 'T019 producer artifact set drift', errors)
            if isinstance(artifacts, dict):
                for name, artifact in artifacts.items():
                    req(isinstance(artifact, dict) and isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0 and str(artifact.get('digest', '')).startswith('sha256:'), f'T019 {name} artifact binding invalid', errors)
        entries.append({'case_id': cid, 'path': str(path.relative_to(ROOT)), 'sha256': digest(path), 'status': data.get('status'), 'implementation_commit': data.get('implementation_commit')})
    req(tuple(item['case_id'] for item in entries) == INPUT, 'closure evidence set/order mismatch', errors)
    return entries


def validate_p09(errors: list[str]) -> dict[str, Any]:
    for path in (P09A, P09C, P09T, P09R):
        req(path.is_file(), f'missing inherited P09 authority: {path.name}', errors)
    if not all(path.is_file() for path in (P09A, P09C, P09T, P09R)):
        return {}
    try:
        authority = load(P09A)
        closure = load(P09C)
        t027 = load(P09T)
    except Exception as exc:
        errors.append(f'invalid inherited P09 JSON: {exc}')
        return {}
    req(authority.get('source_commit') == P09_SHA and authority.get('closure_run_id') == P09_RUN and authority.get('artifact_id') == P09_ART and authority.get('artifact_digest') == P09_DIG, 'P09 authority identity drift', errors)
    req(authority.get('workflow_head_sha') == P09_SHA and authority.get('workflow_conclusion') == 'success' and authority.get('artifact_expired') is False, 'P09 authority live metadata invalid', errors)
    req(authority.get('archive_sha256') == P09_DIG.removeprefix('sha256:'), 'P09 artifact archive digest mismatch', errors)
    req(closure.get('implementation_commit') == P09_SHA and closure.get('status') == 'PASS' and closure.get('phase') == 'signed' and closure.get('merge_authoritative') is True, 'P09 signed closure invalid', errors)
    req(closure.get('defects') == {'p0': 0, 'p1': 0, 'decision_required': 0}, 'P09 signed closure defect ledger invalid', errors)
    details = t027.get('details', {})
    req(t027.get('implementation_commit') == P09_SHA and t027.get('status') == 'PASS' and t027.get('errors') == [], 'P09 T027 evidence invalid', errors)
    req(details.get('closure_phase') == 'signed' and details.get('merge_authoritative') is True, 'P09 T027 signed authority invalid', errors)
    review = closure.get('review', {})
    t020_binding = closure.get('t027', {})
    req(review.get('review_sha256') == digest(P09R), 'P09 inherited review digest binding invalid', errors)
    req(t020_binding.get('sha256') == digest(P09T), 'P09 inherited T027 digest binding invalid', errors)
    return {
        'source_commit': P09_SHA,
        'closure_run_id': P09_RUN,
        'artifact_id': P09_ART,
        'artifact_digest': P09_DIG,
        'phase': closure.get('phase'),
        'merge_authoritative': closure.get('merge_authoritative'),
        'defects': closure.get('defects'),
        'closure_sha256': digest(P09C),
        'review_sha256': digest(P09R),
        't027_sha256': digest(P09T),
    }


def validate_review(errors: list[str]) -> dict[str, Any]:
    req(REVIEW.is_file(), 'missing P10 review.md', errors)
    if not REVIEW.is_file():
        return {'phase': 'missing', 'merge_authoritative': False, 'defects': None}
    text = REVIEW.read_text(encoding='utf-8')
    match = re.search(r'^Status:\s*.+$', text, re.M)
    line = match.group(0).strip() if match else ''
    req(line in (PENDING, SIGNED), 'review status invalid', errors)
    req('## 9. Signed-revision rule' in text or '## Signed-revision rule' in text, 'signed-revision rule missing', errors)
    req('release-wide G7' in text and 'P18/P19/P20/P22' in text, 'release-wide G7 boundary missing', errors)
    if line == PENDING:
        return {'phase': 'pre-sign', 'merge_authoritative': False, 'status': 'PENDING', 'review_sha256': digest(REVIEW), 'defects': None}

    parent = git('rev-parse', 'HEAD^')
    sha_match = re.search(r'Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`', text)
    pre_sign = sha_match.group(1) if sha_match else None
    req(pre_sign == parent, f'pre-sign SHA {pre_sign} != parent {parent}', errors)
    identity_match = re.search(r'Accountable reviewer identity:\s*\*\*(.+?)\*\*', text)
    date_match = re.search(r'Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*', text)
    req(identity_match is not None and identity_match.group(1).strip() == 'GPT-5.6 Sol — CAP-TEXT Technical Review', 'review identity missing/drifted', errors)
    req(date_match is not None, 'review date missing', errors)
    req('P10-T020: PASS — pre-sign closure / merge-authoritative=false' in text, 'pre-sign T020 record missing', errors)
    for role in ('Backend Lead', 'Frontend Lead', 'QA Lead', 'Accessibility Reviewer', 'Security Reviewer', 'Product/API Reviewer'):
        req(f'- {role}: APPROVED' in text, f'{role} approval missing', errors)
    req('- P0 defects: 0' in text and '- P1 defects: 0' in text and '- `DECISION REQUIRED`: 0' in text, 'review defect ledger nonzero/missing', errors)
    req('G3 P10' in text and 'PASS' in text, 'G3 P10 disposition missing', errors)
    req('G7 P10' in text and 'PASS' in text, 'G7 P10 disposition missing', errors)
    req('P18/P19/P20/P22' in text and 'later-owned' in text.lower(), 'later-owner boundary missing', errors)
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


def write_outputs(head: str, plan: dict[str, Any], regression: dict[str, Any], entries: list[dict[str, Any]], p09: dict[str, Any], review: dict[str, Any], errors: list[str]) -> None:
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
        'inherited_p09_signed_closure': p09,
        'review': review,
        'gate_scope': 'P10 CAP-TEXT G3 functional/API and P10 G7 UGC/noindex subset only; full/release-wide G7 and P18/P19/P20/P22 ownership remain later-owned.',
    }
    t020 = {
        'node': 'P10',
        'case_id': 'P10-T020',
        'name': 'same-exact-head-p10-signed-closure-and-affected-regression-matrix',
        'status': status,
        'generated_at': now(),
        'implementation_commit': head,
        'driver': 'python3 scripts/p10/validate.py --case P10-T020 --closure',
        'errors': list(errors),
        'details': details,
    }
    T020.write_text(json.dumps(t020, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    defects = review.get('defects') if phase == 'signed' else {'p0': None, 'p1': None, 'decision_required': None}
    gates = {
        'G3': 'PASS — P10 CAP-TEXT functional/API subset only',
        'G7': 'PASS — P10 Text UGC/noindex/sitemap-exclusion subset only',
        'later_owners': 'OPEN — full/release-wide G7 and P18/P19/P20/P22 remain later-owned',
    }
    closure = {
        'node': 'P10',
        'status': status,
        'phase': phase,
        'merge_authoritative': authoritative,
        'generated_at': now(),
        'implementation_commit': head,
        'case_range': 'P10-T001..P10-T020',
        'input_evidence_count': len(entries),
        'required_regression_workflow_count': len(WF),
        'defects': defects,
        'review': review,
        'inherited_p09_signed_closure': p09,
        'gate_scope': gates,
        't020': {'path': str(T020.relative_to(ROOT)), 'sha256': digest(T020), 'status': status, 'implementation_commit': head},
    }
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    index = {
        'node': 'P10',
        'generated_at': now(),
        'implementation_commit': head,
        'status': status,
        'test_plan_sha256': digest(PLAN) if PLAN.is_file() else None,
        'regression_manifest_sha256': digest(REG) if REG.is_file() else None,
        'coherence_evidence_index_sha256': digest(COH) if COH.is_file() else None,
        'review_sha256': digest(REVIEW) if REVIEW.is_file() else None,
        'input_evidence': entries,
        'coherence_result': next((item for item in entries if item['case_id'] == 'P10-T019'), None),
        'inherited_p09_signed_closure': p09,
        'closure_result': {'case_id': 'P10-T020', 'path': str(T020.relative_to(ROOT)), 'sha256': digest(T020), 'status': status, 'implementation_commit': head, 'phase': phase, 'merge_authoritative': authoritative},
        'closure_sha256': digest(CLOSURE),
    }
    IDX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def run_closure(flag: bool) -> int:
    if not flag:
        print('P10-T020: --closure is required')
        return 2
    head = git('rev-parse', 'HEAD')
    errors: list[str] = []
    plan = validate_plan(errors)
    regression = validate_regression(head, errors)
    entries = validate_cases(head, errors)
    p09 = validate_p09(errors)
    review = validate_review(errors)
    write_outputs(head, plan, regression, entries, p09, review, errors)
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
        write_outputs(head, plan, regression, entries, p09, review, errors)
        for error in errors:
            print(f'P10-T020: {error}')
        return 1
    if review.get('phase') == 'signed':
        print(f'P10-T020: PASS — 19/19 evidence, {len(WF)}/{len(WF)} exact-head workflows, inherited P09 signed closure and signed review green for {head}; merge-authoritative=true')
    else:
        print(f'P10-T020: PASS — pre-sign closure candidate with 19/19 evidence, {len(WF)}/{len(WF)} exact-head workflows and inherited P09 signed closure green for {head}; merge-authoritative=false')
    return 0
