#!/usr/bin/env python3
"""P13-T027 exact-head pre-sign/final signed accountable closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P13 = ROOT / 'artifacts' / 'v10' / 'P13'
API = P13 / 'api'
RBAC = P13 / 'rbac'
SECURITY = P13 / 'security'
ENTITLEMENT = P13 / 'entitlement'
AUDIT = P13 / 'audit'
BROWSER = P13 / 'browser'
RESULTS = P13 / 'results'
PLAN = P13 / 'test-plan.json'
REG = P13 / 'regression-manifest.json'
COH = P13 / 'evidence-index.json'
IDX = P13 / 'closure-evidence-index.json'
REVIEW = P13 / 'review.md'
CLOSURE = P13 / 'closure.json'
T027 = RESULTS / 'P13-T027.json'

P12A = P13 / 'inherited' / 'p12-authority.json'
P12 = P13 / 'inherited' / 'P12'
P12C = P12 / 'closure.json'
P12T = P12 / 'results' / 'P12-T025.json'
P12R = P12 / 'review.md'
P12I = P12 / 'closure-evidence-index.json'

P06A = P13 / 'inherited' / 'p06-authority.json'
P06 = P13 / 'inherited' / 'P06'
P06T = P06 / 'results' / 'P06-T024.json'
P06I = P06 / 'evidence-index.json'
P06R = P06 / 'review.md'

WF = (
    'P00 Bootstrap and G0 Traceability',
    'P01 Engineering Foundation',
    'P02 Brand Foundation',
    'P03 Design System',
    'P04 Product Shells',
    'P05 Links Domain Contract',
    'P05 Real Integration',
    'P05 Workspace Browser',
    'P06 Custom Domains',
    'P06 Real Integration',
    'P06 Workspace Domains Browser',
    'P07 Analytics Contract',
    'P07 Real Integration',
    'P07 Workspace Analytics Browser',
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
    'P12 Workspace Organization Contract',
    'P12 Real Workspace Organization Integration',
    'P12 Workspace Organization Browser',
    'P12 Evidence Coherence',
    'P13 Billing Payments Entitlements Contract',
    'P13 Real Billing Payments Entitlements Integration',
    'P13 Billing Commerce Browser',
    'P13 Evidence Coherence',
)
EXCLUDED = (
    'P05 Closure',
    'P06 Closure',
    'P07 Closure',
    'P08 Closure',
    'P09 Closure',
    'P10 Closure',
    'P11 Closure',
    'P12 Closure',
)
CASES = tuple(f'P13-T{i:03d}' for i in range(1, 28))
INPUT = CASES[:-1]
PENDING = 'Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'
SIGNED = 'Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**'

P12_SOURCE = '9d49d5ebf0e697ae9cd6537c432c27a15edc60bd'
P12_INTEGRATION = '7f39da389052b08f145e69dac2a715b9d303294d'
P12_RUN = 32663159008
P12_ART = 9499336765
P12_DIG = 'sha256:72ed65c48303654b589edce23e9118ecc963940a7400e27a0f174d7e8ea07c9a'

P06_SOURCE = '4079d1ee7c4876cab3e6bccccc3e4ac62cf97f23'
P06_INTEGRATION = '3aa80b566d144963130b8f61fa63a4ee677ebc99'
P06_RUN = 32519298309
P06_ART = 9460016077
P06_DIG = 'sha256:21e2fe5898a047e166aac520870070e8072f00885a3c89aaf86736f6ac22a2c8'

ZERO_DEFECTS = {'p0': 0, 'p1': 0, 'decision_required': 0}
P13_PRODUCERS = {
    'P13 Billing Payments Entitlements Contract',
    'P13 Real Billing Payments Entitlements Integration',
    'P13 Billing Commerce Browser',
}


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
    n = int(cid[-3:])
    if n in {1, 2, 5, 6, 9, 13, 20}:
        return API / f'{cid}.json'
    if n in {3, 10, 11, 12, 14, 15, 16, 17, 18}:
        return ENTITLEMENT / f'{cid}.json'
    if n == 4:
        return RBAC / f'{cid}.json'
    if n in {7, 8}:
        return SECURITY / f'{cid}.json'
    if n == 19:
        return AUDIT / f'{cid}.json'
    if 21 <= n <= 25:
        return BROWSER / f'{cid}.json'
    if n == 26:
        return RESULTS / f'{cid}.json'
    raise ValueError(cid)


def validate_plan(errors: list[str]) -> dict[str, Any]:
    req(PLAN.is_file(), 'missing P13 test-plan.json', errors)
    if not PLAN.is_file():
        return {}
    try:
        plan = load(PLAN)
    except Exception as exc:
        errors.append(f'invalid P13 test-plan: {exc}')
        return {}
    ids = tuple(item.get('id') for item in plan.get('cases', []) if isinstance(item, dict))
    req(ids == CASES, 'P13 test-plan case range/order drift', errors)
    closure = plan.get('closure', {})
    req(isinstance(closure, dict), 'P13 closure contract missing', errors)
    if isinstance(closure, dict):
        req(closure.get('same_exact_head_required') is True, 'P13 same-exact-head closure rule drift', errors)
        req(closure.get('required_case_range') == 'P13-T001..P13-T027', 'P13 closure case range drift', errors)
        req(closure.get('review_required') is True, 'P13 accountable review requirement drift', errors)
        req(closure.get('defect_limits') == ZERO_DEFECTS, 'P13 defect limits drift', errors)
        req(closure.get('phases') == {
            'pre-sign': {'review_status': 'PENDING', 'merge_authoritative': False},
            'signed': {'review_status': 'SIGNED', 'merge_authoritative': True},
        }, 'P13 closure phases drift', errors)
    pred = plan.get('predecessor_signed_authority', {})
    req(
        isinstance(pred, dict)
        and pred.get('node') == 'P12'
        and pred.get('integration_commit') == P12_INTEGRATION
        and pred.get('signed_source_commit') == P12_SOURCE
        and pred.get('closure_run_id') == P12_RUN
        and pred.get('artifact_id') == P12_ART
        and pred.get('artifact_digest') == P12_DIG
        and pred.get('phase') == 'signed'
        and pred.get('merge_authoritative') is True,
        'P12 frozen predecessor authority drift', errors,
    )
    inherited = plan.get('inherited_functional_authority', {})
    req(
        isinstance(inherited, dict)
        and inherited.get('node') == 'P06'
        and inherited.get('integration_commit') == P06_INTEGRATION
        and inherited.get('signed_source_commit') == P06_SOURCE
        and inherited.get('closure_run_id') == P06_RUN
        and inherited.get('artifact_id') == P06_ART
        and inherited.get('artifact_digest') == P06_DIG,
        'P06 inherited authority drift', errors,
    )
    return plan


def validate_regression(head: str, errors: list[str]) -> dict[str, Any]:
    req(REG.is_file(), 'missing P13 regression-manifest.json', errors)
    if not REG.is_file():
        return {}
    try:
        manifest = load(REG)
    except Exception as exc:
        errors.append(f'invalid P13 regression manifest: {exc}')
        return {}
    req(manifest.get('implementation_commit') == head, 'P13 regression manifest head mismatch', errors)
    workflows = manifest.get('required_workflows', {})
    req(isinstance(workflows, dict) and set(workflows) == set(WF), 'P13 regression workflow set mismatch', errors)
    req(manifest.get('missing') == [] and manifest.get('pending') == [] and manifest.get('failed') == [], 'P13 regression matrix not fully green', errors)
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
                f'{name} exact-head success record invalid', errors,
            )
    excluded = manifest.get('excluded_revision_specific_workflows', {})
    req(isinstance(excluded, dict) and set(excluded) == set(EXCLUDED), 'revision-specific closure exclusion set drift', errors)
    if isinstance(excluded, dict):
        for name in EXCLUDED:
            rationale = str(excluded.get(name, ''))
            req('revision-specific' in rationale.lower() and 'inherited' in rationale.lower(), f'{name} exclusion rationale invalid', errors)
        req(P12_SOURCE in str(excluded.get('P12 Closure', '')), 'P12 Closure exclusion does not bind frozen signed source', errors)
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
        if cid == 'P13-T026':
            obs = data.get('observations', {})
            req(obs.get('input_evidence_count') == 25, 'T026 input evidence count drift', errors)
            req(obs.get('same_exact_head') is True, 'T026 exact-head coherence false', errors)
            req(isinstance(obs.get('capture_count'), int) and obs.get('capture_count', 0) >= 20, 'T026 browser capture count insufficient', errors)
            req(obs.get('runtime_error_files_clean') is True, 'T026 runtime error-file cleanliness false', errors)
            req(obs.get('mixed_head_rejected') is True, 'T026 mixed-head rejection false', errors)
            req(obs.get('inspectable_runtime_and_browser_evidence') is True, 'T026 runtime/browser evidence not inspectable', errors)
            producer_ids = obs.get('producer_run_ids', {})
            req(isinstance(producer_ids, dict) and set(producer_ids) == P13_PRODUCERS, 'T026 producer run binding set drift', errors)
            if isinstance(producer_ids, dict):
                for name, value in producer_ids.items():
                    req(isinstance(value, int) and value > 0, f'T026 invalid producer run id: {name}', errors)
            artifacts = obs.get('producer_artifacts', {})
            req(isinstance(artifacts, dict) and set(artifacts) == P13_PRODUCERS, 'T026 producer artifact set drift', errors)
            if isinstance(artifacts, dict):
                for name, artifact in artifacts.items():
                    req(
                        isinstance(artifact, dict)
                        and isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0
                        and str(artifact.get('digest', '')).startswith('sha256:')
                        and int(artifact.get('size_in_bytes', 0)) > 0,
                        f'T026 invalid producer artifact binding: {name}', errors,
                    )
        entries.append({
            'case_id': cid,
            'path': str(path.relative_to(ROOT)),
            'sha256': digest(path),
            'status': data.get('status'),
            'implementation_commit': data.get('implementation_commit'),
        })
    req(tuple(item['case_id'] for item in entries) == INPUT, 'P13 closure evidence set/order mismatch', errors)
    return entries


def validate_p12(errors: list[str]) -> dict[str, Any]:
    for path in (P12A, P12C, P12T, P12R, P12I):
        req(path.is_file(), f'missing inherited P12 authority: {path.name}', errors)
    if not all(path.is_file() for path in (P12A, P12C, P12T, P12R, P12I)):
        return {}
    try:
        authority, closure, t025, index = load(P12A), load(P12C), load(P12T), load(P12I)
    except Exception as exc:
        errors.append(f'invalid inherited P12 JSON: {exc}')
        return {}
    req(authority.get('source_commit') == P12_SOURCE and authority.get('closure_run_id') == P12_RUN and authority.get('artifact_id') == P12_ART and authority.get('artifact_digest') == P12_DIG, 'P12 authority identity drift', errors)
    req(authority.get('workflow_head_sha') == P12_SOURCE and authority.get('workflow_status') == 'completed' and authority.get('workflow_conclusion') == 'success' and authority.get('artifact_expired') is False, 'P12 live signed authority metadata invalid', errors)
    req(authority.get('archive_sha256') == P12_DIG.removeprefix('sha256:'), 'P12 signed artifact archive digest mismatch', errors)
    req(closure.get('implementation_commit') == P12_SOURCE and closure.get('status') == 'PASS' and closure.get('phase') == 'signed' and closure.get('merge_authoritative') is True, 'P12 signed closure invalid', errors)
    req(closure.get('defects') == ZERO_DEFECTS and closure.get('input_evidence_count') == 24 and closure.get('required_regression_workflow_count') == 35, 'P12 signed closure defect/evidence/matrix binding invalid', errors)
    req(closure.get('review', {}).get('review_sha256') == digest(P12R), 'P12 review digest binding invalid', errors)
    req(closure.get('t025', {}).get('sha256') == digest(P12T), 'P12 T025 digest binding invalid', errors)
    details = t025.get('details', {})
    req(t025.get('implementation_commit') == P12_SOURCE and t025.get('status') == 'PASS' and t025.get('errors') == [], 'P12 T025 signed evidence invalid', errors)
    req(details.get('closure_phase') == 'signed' and details.get('merge_authoritative') is True and details.get('input_evidence_count') == 24 and details.get('regression_workflow_count') == 35 and details.get('defects') == ZERO_DEFECTS, 'P12 T025 signed details invalid', errors)
    req(index.get('implementation_commit') == P12_SOURCE and index.get('phase') == 'signed' and index.get('merge_authoritative') is True, 'P12 closure evidence index phase/head invalid', errors)
    req(index.get('review', {}).get('sha256') == digest(P12R) and index.get('final_case', {}).get('sha256') == digest(P12T), 'P12 closure evidence index digest binding invalid', errors)
    return {
        'source_commit': P12_SOURCE,
        'integration_commit': P12_INTEGRATION,
        'closure_run_id': P12_RUN,
        'artifact_id': P12_ART,
        'artifact_digest': P12_DIG,
        'phase': closure.get('phase'),
        'merge_authoritative': closure.get('merge_authoritative'),
        'defects': closure.get('defects'),
        'closure_sha256': digest(P12C),
        'review_sha256': digest(P12R),
        't025_sha256': digest(P12T),
    }


def validate_p06(errors: list[str]) -> dict[str, Any]:
    for path in (P06A, P06T, P06I, P06R):
        req(path.is_file(), f'missing inherited P06 authority: {path.name}', errors)
    if not all(path.is_file() for path in (P06A, P06T, P06I, P06R)):
        return {}
    try:
        authority, t024, index = load(P06A), load(P06T), load(P06I)
    except Exception as exc:
        errors.append(f'invalid inherited P06 JSON: {exc}')
        return {}
    req(authority.get('source_commit') == P06_SOURCE and authority.get('closure_run_id') == P06_RUN and authority.get('artifact_id') == P06_ART and authority.get('artifact_digest') == P06_DIG, 'P06 authority identity drift', errors)
    req(authority.get('workflow_head_sha') == P06_SOURCE and authority.get('workflow_status') == 'completed' and authority.get('workflow_conclusion') == 'success' and authority.get('artifact_expired') is False, 'P06 live signed authority metadata invalid', errors)
    req(authority.get('archive_sha256') == P06_DIG.removeprefix('sha256:'), 'P06 signed artifact archive digest mismatch', errors)
    details = t024.get('details', {})
    req(t024.get('implementation_commit') == P06_SOURCE and t024.get('status') == 'PASS' and t024.get('errors') == [], 'P06 T024 signed evidence invalid', errors)
    req(details.get('input_evidence_count') == 23 and details.get('regression_workflow_count') == 12 and details.get('same_exact_head_required') is True, 'P06 T024 evidence/matrix binding invalid', errors)
    req(index.get('node') == 'P06' and index.get('implementation_commit') == P06_SOURCE and index.get('status') == 'PASS', 'P06 evidence index head/status invalid', errors)
    req(len(index.get('input_evidence', [])) == 23, 'P06 evidence index input count invalid', errors)
    closure_result = index.get('closure_result', {})
    req(closure_result.get('case_id') == 'P06-T024' and closure_result.get('implementation_commit') == P06_SOURCE and closure_result.get('status') == 'PASS' and closure_result.get('sha256') == digest(P06T), 'P06 evidence index T024 digest binding invalid', errors)
    review = P06R.read_text(encoding='utf-8')
    req(SIGNED in review, 'P06 inherited review is not signed', errors)
    for marker in ('- P0 defects: 0', '- P1 defects: 0', '- `DECISION REQUIRED`: 0'):
        req(marker in review, f'P06 inherited signed review ledger missing {marker}', errors)
    return {
        'source_commit': P06_SOURCE,
        'integration_commit': P06_INTEGRATION,
        'closure_run_id': P06_RUN,
        'artifact_id': P06_ART,
        'artifact_digest': P06_DIG,
        'phase': 'signed',
        'merge_authoritative': True,
        'defects': ZERO_DEFECTS,
        'review_sha256': digest(P06R),
        't024_sha256': digest(P06T),
        'evidence_index_sha256': digest(P06I),
    }


def validate_review(head: str, errors: list[str]) -> dict[str, Any]:
    req(REVIEW.is_file(), 'missing P13 review.md', errors)
    if not REVIEW.is_file():
        return {'phase': 'missing', 'merge_authoritative': False, 'defects': None}
    text = REVIEW.read_text(encoding='utf-8')
    match = re.search(r'^Status:\s*.+$', text, re.M)
    line = match.group(0).strip() if match else ''
    req(line in (PENDING, SIGNED), 'P13 review status invalid', errors)
    for marker in ('P06', 'P12', 'P15', 'P17', 'P19', 'P13-T027', 'signed revision itself must rerun'):
        req(marker.lower() in text.lower(), f'P13 review boundary/signed rule missing {marker}', errors)
    if line == PENDING:
        return {
            'status': 'PENDING',
            'phase': 'pre-sign',
            'merge_authoritative': False,
            'defects': {'p0': None, 'p1': None, 'decision_required': None},
            'review_sha256': digest(REVIEW),
        }

    parent = git('rev-parse', 'HEAD^')
    changed = [item for item in git('diff', '--name-only', 'HEAD^', 'HEAD').splitlines() if item]
    req(changed == ['artifacts/v10/P13/review.md'], 'signed revision must be review-only child', errors)
    pre = re.search(r'^Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`\s*$', text, re.M)
    reviewer = re.search(r'^Accountable reviewer identity:\s*\*\*(.+?)\*\*\s*$', text, re.M)
    date = re.search(r'^Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*\s*$', text, re.M)
    req(bool(pre), 'signed review pre-sign exact implementation SHA missing', errors)
    req(bool(reviewer), 'signed review accountable reviewer identity missing', errors)
    req(bool(date), 'signed review date missing', errors)
    if pre:
        req(pre.group(1) == parent, 'signed review pre-sign SHA is not HEAD parent', errors)
        req(pre.group(1) != head, 'signed review pre-sign SHA cannot equal signed HEAD', errors)
    if reviewer:
        req(reviewer.group(1).strip() == 'GPT-5.6 Sol — P13 Technical Review', 'signed review accountable reviewer identity invalid', errors)
    req(re.search(r'^- P0 defects:\s*0\s*$', text, re.M) is not None, 'signed review P0 ledger invalid', errors)
    req(re.search(r'^- P1 defects:\s*0\s*$', text, re.M) is not None, 'signed review P1 ledger invalid', errors)
    req(re.search(r'^- `DECISION REQUIRED`:\s*0\s*$', text, re.M) is not None, 'signed review decision ledger invalid', errors)
    for role in ('Backend Lead', 'Frontend Lead', 'QA Lead', 'Accessibility Reviewer', 'Security Reviewer', 'Product/API Reviewer'):
        req(f'- {role}: APPROVED' in text, f'signed review missing approval: {role}', errors)
    req('P13-T001..P13-T027' in text and 'PASS' in text, 'signed review missing full P13 case PASS disposition', errors)
    for marker in ('Pre-sign closure run:', 'Pre-sign closure artifact:', 'Pre-sign closure artifact digest:'):
        req(marker in text, f'signed review missing {marker}', errors)
    return {
        'status': 'APPROVED',
        'phase': 'signed',
        'merge_authoritative': True,
        'defects': ZERO_DEFECTS,
        'pre_sign_implementation_sha': pre.group(1) if pre else None,
        'accountable_reviewer_identity': reviewer.group(1).strip() if reviewer else None,
        'review_date': date.group(1) if date else None,
        'review_sha256': digest(REVIEW),
    }


def write_outputs(
    head: str,
    entries: list[dict[str, Any]],
    regression: dict[str, Any],
    inherited_p12: dict[str, Any],
    inherited_p06: dict[str, Any],
    review: dict[str, Any],
    errors: list[str],
) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    phase = review.get('phase', 'invalid')
    merge_authoritative = phase == 'signed' and not errors
    defects = review.get('defects') if phase == 'signed' else {'p0': None, 'p1': None, 'decision_required': None}
    gate_scope_text = (
        'P13 CAP-BILLING G3/G10, CAP-PAYMENTS G3/G6/G10, CAP-PAYMENT-CALLBACKS G3/G6/G10, '
        'and CAP-DOMAIN-ENTITLEMENT shared P06/P13 G3/G6 subset only; P06 domain safety and P12 membership/notification core '
        'remain inherited; P15 identity lifecycle, P17 permission lifecycle and P19 Website/SEO remain later-owned.'
    )
    result = {
        'case_id': 'P13-T027',
        'implementation_commit': head,
        'status': 'PASS' if not errors else 'FAIL',
        'errors': list(errors),
        'details': {
            'closure_phase': phase,
            'merge_authoritative': merge_authoritative,
            'input_evidence_count': len(entries),
            'required_input_evidence_count': 26,
            'regression_workflow_count': len(regression.get('required_workflows', {})) if isinstance(regression, dict) else 0,
            'required_regression_workflows': list(WF),
            'excluded_revision_specific_workflows': list(EXCLUDED),
            'inherited_p12_signed_closure': inherited_p12,
            'inherited_p06_signed_authority': inherited_p06,
            'defects': defects,
            'gate_scope': gate_scope_text,
        },
    }
    T027.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    closure = {
        'node': 'P13',
        'status': 'PASS' if not errors else 'FAIL',
        'implementation_commit': head,
        'case_range': 'P13-T001..P13-T027',
        'generated_at': now(),
        'input_evidence_count': len(entries),
        'required_regression_workflow_count': len(WF),
        'phase': phase,
        'merge_authoritative': merge_authoritative,
        'defects': defects,
        'inherited_p12_signed_closure': inherited_p12,
        'inherited_p06_signed_authority': inherited_p06,
        'gate_scope': {
            'G3': 'PASS — P13 billing/payment/entitlement functional subset only' if not errors else 'FAIL',
            'G6': 'PASS — P13 callback/tenant/RBAC/domain-conjunctive security subset only' if not errors else 'FAIL',
            'G10': 'PASS — P13 billing/commerce UX/accessibility/offline subset only' if not errors else 'FAIL',
            'inherited': 'P06 domain safety and P12 Workspace membership/notification-core authority remain inherited and are not redefined by P13',
            'later_owners': 'OPEN — P15 production identity lifecycle, P17 permission lifecycle and P19 final Website/SEO remain later-owned',
        },
        'review': review,
        't027': {
            'path': 'artifacts/v10/P13/results/P13-T027.json',
            'status': result['status'],
            'implementation_commit': head,
            'sha256': digest(T027),
        },
    }
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    index = {
        'node': 'P13',
        'implementation_commit': head,
        'generated_at': now(),
        'phase': phase,
        'merge_authoritative': merge_authoritative,
        'input_evidence': entries,
        'final_case': {'case_id': 'P13-T027', 'path': 'artifacts/v10/P13/results/P13-T027.json', 'sha256': digest(T027)},
        'closure': {'path': 'artifacts/v10/P13/closure.json', 'sha256': digest(CLOSURE)},
        'review': {'path': 'artifacts/v10/P13/review.md', 'sha256': digest(REVIEW)},
        'regression_manifest': {'path': 'artifacts/v10/P13/regression-manifest.json', 'sha256': digest(REG)} if REG.is_file() else None,
        'inherited_p12_signed_closure': inherited_p12,
        'inherited_p06_signed_authority': inherited_p06,
    }
    IDX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def run_closure(flag: bool) -> int:
    if not flag:
        print('P13-T027: --closure is required')
        return 2
    head = git('rev-parse', 'HEAD')
    errors: list[str] = []
    validate_plan(errors)
    regression = validate_regression(head, errors)
    entries = validate_cases(head, errors)
    inherited_p12 = validate_p12(errors)
    inherited_p06 = validate_p06(errors)
    review = validate_review(head, errors)
    req(len(entries) == 26, 'P13 closure requires 26 input evidence files', errors)
    req(len(regression.get('required_workflows', {})) == len(WF), 'P13 closure requires 39 exact-head workflows', errors)
    if review.get('phase') == 'signed':
        req(review.get('defects') == ZERO_DEFECTS, 'signed defect ledger must be zero', errors)
    write_outputs(head, entries, regression, inherited_p12, inherited_p06, review, errors)
    phase = review.get('phase', 'invalid')
    if errors:
        print(f'P13-T027 {phase} closure FAIL on {head}')
        for item in errors:
            print(' -', item)
        return 1
    print(f'P13-T027 {phase} closure PASS on {head}; merge-authoritative={phase == "signed"}')
    return 0


if __name__ == '__main__':
    raise SystemExit(run_closure(True))
