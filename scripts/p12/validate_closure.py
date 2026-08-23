#!/usr/bin/env python3
"""P12-T025 exact-head pre-sign/final signed closure validator."""
from __future__ import annotations

import datetime as dt
import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[2]
P12 = ROOT / 'artifacts' / 'v10' / 'P12'
API = P12 / 'api'
RBAC = P12 / 'rbac'
AUDIT = P12 / 'audit'
SECURITY = P12 / 'security'
BROWSER = P12 / 'browser'
RESULTS = P12 / 'results'
PLAN = P12 / 'test-plan.json'
REG = P12 / 'regression-manifest.json'
COH = P12 / 'evidence-index.json'
IDX = P12 / 'closure-evidence-index.json'
REVIEW = P12 / 'review.md'
CLOSURE = P12 / 'closure.json'
T025 = RESULTS / 'P12-T025.json'
P11A = P12 / 'inherited' / 'p11-authority.json'
P11 = P12 / 'inherited' / 'P11'
P11C = P11 / 'closure.json'
P11T = P11 / 'results' / 'P11-T020.json'
P11R = P11 / 'review.md'

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
)
EXCLUDED = (
    'P05 Closure',
    'P06 Closure',
    'P07 Closure',
    'P08 Closure',
    'P09 Closure',
    'P10 Closure',
    'P11 Closure',
)
CASES = tuple(f'P12-T{i:03d}' for i in range(1, 26))
INPUT = CASES[:-1]
PENDING = 'Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'
SIGNED = 'Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**'
P11_SHA = 'b59dfbe794f7d2f7bf63fdc79116217c5d893e87'
P11_RUN = 32649713397
P11_ART = 9495896748
P11_DIG = 'sha256:fe0edc8308cb4520929590efb261b87052423805ef02099066e818ff4cc5ae4f'
P11_EVIDENCE_COUNT = 19
P11_WORKFLOW_COUNT = 34


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
    if number in {1, 2, 6, 7, 9, 10, 11, 12, 14, 15, 18}:
        return API / f'{cid}.json'
    if number in {3, 4, 5, 8, 16}:
        return RBAC / f'{cid}.json'
    if number == 13:
        return AUDIT / f'{cid}.json'
    if number == 17:
        return SECURITY / f'{cid}.json'
    if 19 <= number <= 23:
        return BROWSER / f'{cid}.json'
    if number == 24:
        return RESULTS / f'{cid}.json'
    raise ValueError(cid)


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
        req(closure.get('version') == 1, 'closure version drift', errors)
        req(closure.get('same_exact_head_required') is True, 'same-exact-head contract drift', errors)
        req(closure.get('required_case_range') == 'P12-T001..P12-T025', 'closure case range drift', errors)
        req(closure.get('review_required') is True, 'review requirement drift', errors)
        req((closure.get('p0_max'), closure.get('p1_max'), closure.get('decision_required_max')) == (0, 0, 0), 'closure defect limits drift', errors)
        req(closure.get('pre_sign_phase') == 'pre-sign / merge_authoritative=false', 'pre-sign phase contract drift', errors)
        req(closure.get('signed_phase') == 'signed / merge_authoritative=true', 'signed phase contract drift', errors)
        predecessor = str(closure.get('predecessor_rule', ''))
        req(P11_SHA in predecessor and 'do not rerun/reinterpret P11 revision-specific closure' in predecessor, 'predecessor signed-authority rule drift', errors)
        scope = str(closure.get('scope', ''))
        req('P12 only' in scope and 'P07 analytics' in scope and 'P15 identity lifecycle' in scope and 'P13-P17 notification producers' in scope, 'P12 closure scope drift', errors)
    capability = plan.get('capability_contract', {})
    caps = capability.get('capabilities', []) if isinstance(capability, dict) else []
    capmap = {item.get('id'): item for item in caps if isinstance(item, dict)}
    req(set(capmap) == {'CAP-WORKSPACE', 'CAP-FOLDERS-TAGS', 'CAP-CAMPAIGNS', 'CAP-NOTIFICATIONS'}, 'P12 capability set drift', errors)
    if capmap:
        req(capmap['CAP-WORKSPACE'].get('owner') == 'P12' and capmap['CAP-WORKSPACE'].get('gates') == ['G3', 'G6', 'G10'], 'CAP-WORKSPACE authority drift', errors)
        req(capmap['CAP-FOLDERS-TAGS'].get('owner') == 'P12' and capmap['CAP-FOLDERS-TAGS'].get('gates') == ['G3', 'G6'], 'CAP-FOLDERS-TAGS authority drift', errors)
        req(capmap['CAP-CAMPAIGNS'].get('owner') == 'P07/P12' and capmap['CAP-CAMPAIGNS'].get('gates') == ['G3'], 'CAP-CAMPAIGNS authority drift', errors)
        req(capmap['CAP-NOTIFICATIONS'].get('owner') == 'P12/P13-P17' and capmap['CAP-NOTIFICATIONS'].get('gates') == ['G3', 'G5', 'G6', 'G10'], 'CAP-NOTIFICATIONS authority drift', errors)
    predecessor = plan.get('predecessor_signed_authority', {})
    req(
        isinstance(predecessor, dict)
        and predecessor.get('node') == 'P11'
        and predecessor.get('signed_source_commit') == P11_SHA
        and predecessor.get('closure_run_id') == P11_RUN
        and predecessor.get('artifact_id') == P11_ART
        and predecessor.get('artifact_digest') == P11_DIG
        and predecessor.get('phase') == 'signed'
        and predecessor.get('merge_authoritative') is True,
        'test-plan predecessor P11 authority drift',
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
    req(isinstance(excluded, dict) and set(excluded) == set(EXCLUDED), 'revision-specific closure exclusion set drift', errors)
    if isinstance(excluded, dict):
        for name in EXCLUDED:
            rationale = str(excluded.get(name, ''))
            req('P11 signed' in rationale and 'inherited' in rationale.lower(), f'{name} exclusion lacks inherited P11 signed-authority rationale', errors)
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
        if cid == 'P12-T024':
            obs = data.get('observations', {})
            req(obs.get('input_evidence_count') == 23, 'T024 input evidence count drift', errors)
            req(obs.get('same_exact_head') is True, 'T024 exact-head coherence false', errors)
            req(obs.get('capture_count') == 9, 'T024 capture count drift', errors)
            req(obs.get('runtime_error_files_clean') is True, 'T024 runtime error-file cleanliness false', errors)
            producer_ids = obs.get('producer_run_ids', {})
            expected = {'P12 Workspace Organization Contract', 'P12 Real Workspace Organization Integration', 'P12 Workspace Organization Browser'}
            req(isinstance(producer_ids, dict) and set(producer_ids) == expected, 'T024 producer bindings drift', errors)
            artifacts = obs.get('producer_artifacts', {})
            req(isinstance(artifacts, dict) and set(artifacts) == expected, 'T024 producer artifact set drift', errors)
            if isinstance(artifacts, dict):
                for name, artifact in artifacts.items():
                    req(isinstance(artifact, dict) and isinstance(artifact.get('id'), int) and artifact.get('id', 0) > 0 and str(artifact.get('digest', '')).startswith('sha256:'), f'T024 {name} artifact binding invalid', errors)
        entries.append({'case_id': cid, 'path': str(path.relative_to(ROOT)), 'sha256': digest(path), 'status': data.get('status'), 'implementation_commit': data.get('implementation_commit')})
    req(tuple(item['case_id'] for item in entries) == INPUT, 'closure evidence set/order mismatch', errors)
    return entries


def validate_p11(errors: list[str]) -> dict[str, Any]:
    for path in (P11A, P11C, P11T, P11R):
        req(path.is_file(), f'missing inherited P11 authority: {path.name}', errors)
    if not all(path.is_file() for path in (P11A, P11C, P11T, P11R)):
        return {}
    try:
        authority = load(P11A)
        closure = load(P11C)
        t020 = load(P11T)
    except Exception as exc:
        errors.append(f'invalid inherited P11 JSON: {exc}')
        return {}
    req(authority.get('source_commit') == P11_SHA and authority.get('closure_run_id') == P11_RUN and authority.get('artifact_id') == P11_ART and authority.get('artifact_digest') == P11_DIG, 'P11 authority identity drift', errors)
    req(authority.get('workflow_head_sha') == P11_SHA and authority.get('workflow_conclusion') == 'success' and authority.get('artifact_expired') is False, 'P11 authority live metadata invalid', errors)
    req(authority.get('archive_sha256') == P11_DIG.removeprefix('sha256:'), 'P11 artifact archive digest mismatch', errors)
    req(closure.get('implementation_commit') == P11_SHA and closure.get('status') == 'PASS' and closure.get('phase') == 'signed' and closure.get('merge_authoritative') is True, 'P11 signed closure invalid', errors)
    req(closure.get('defects') == {'p0': 0, 'p1': 0, 'decision_required': 0}, 'P11 signed closure defect ledger invalid', errors)
    req(closure.get('input_evidence_count') == P11_EVIDENCE_COUNT and closure.get('required_regression_workflow_count') == P11_WORKFLOW_COUNT, 'P11 signed closure evidence/matrix count invalid', errors)
    details = t020.get('details', {})
    req(t020.get('implementation_commit') == P11_SHA and t020.get('status') == 'PASS' and t020.get('errors') == [], 'P11 T020 evidence invalid', errors)
    req(details.get('closure_phase') == 'signed' and details.get('merge_authoritative') is True and details.get('input_evidence_count') == P11_EVIDENCE_COUNT and details.get('regression_workflow_count') == P11_WORKFLOW_COUNT, 'P11 T020 signed authority invalid', errors)
    review = closure.get('review', {})
    binding = closure.get('t020', {})
    req(review.get('review_sha256') == digest(P11R), 'P11 inherited review digest binding invalid', errors)
    req(binding.get('sha256') == digest(P11T), 'P11 inherited T020 digest binding invalid', errors)
    return {
        'source_commit': P11_SHA,
        'closure_run_id': P11_RUN,
        'artifact_id': P11_ART,
        'artifact_digest': P11_DIG,
        'phase': closure.get('phase'),
        'merge_authoritative': closure.get('merge_authoritative'),
        'defects': closure.get('defects'),
        'closure_sha256': digest(P11C),
        'review_sha256': digest(P11R),
        't020_sha256': digest(P11T),
    }


def validate_review(head: str, errors: list[str]) -> dict[str, Any]:
    req(REVIEW.is_file(), 'missing P12 review.md', errors)
    if not REVIEW.is_file():
        return {'phase': 'missing', 'merge_authoritative': False, 'defects': None}
    text = REVIEW.read_text(encoding='utf-8')
    match = re.search(r'^Status:\s*.+$', text, re.M)
    line = match.group(0).strip() if match else ''
    req(line in (PENDING, SIGNED), 'review status invalid', errors)
    req('## 11. Signed-revision rule' in text or '## Signed-revision rule' in text, 'signed-revision rule missing', errors)
    for needle in ('P15', 'P13-P17', 'P07', 'no `/app/folders` route'):
        req(needle in text, f'review ownership/route boundary missing: {needle}', errors)
    if line == PENDING:
        return {'status': 'PENDING', 'phase': 'pre-sign', 'merge_authoritative': False, 'defects': {'p0': None, 'p1': None, 'decision_required': None}, 'review_sha256': digest(REVIEW)}

    parent = git('rev-parse', 'HEAD^')
    changed = [item for item in git('diff', '--name-only', 'HEAD^', 'HEAD').splitlines() if item]
    req(changed == ['artifacts/v10/P12/review.md'], 'signed revision must be review-only child', errors)
    pre = re.search(r'^Pre-sign implementation commit:\s*`([0-9a-f]{40})`\s*$', text, re.M)
    reviewer = re.search(r'^Accountable reviewer:\s*(.+?)\s*$', text, re.M)
    date = re.search(r'^Review date:\s*`?(\d{4}-\d{2}-\d{2})`?\s*$', text, re.M)
    req(bool(pre), 'signed review pre-sign implementation commit missing', errors)
    req(bool(reviewer), 'signed review accountable reviewer missing', errors)
    req(bool(date), 'signed review date missing', errors)
    if pre:
        req(pre.group(1) == parent, 'signed review pre-sign SHA is not HEAD parent', errors)
        req(pre.group(1) != head, 'signed review pre-sign SHA cannot equal signed HEAD', errors)
    if reviewer:
        req('GPT-5.6 Sol' in reviewer.group(1) and 'Workspace' in reviewer.group(1), 'signed review accountable reviewer identity invalid', errors)
    req(re.search(r'^- P0 defects:\s*0\s*$', text, re.M) is not None, 'signed review P0 ledger invalid', errors)
    req(re.search(r'^- P1 defects:\s*0\s*$', text, re.M) is not None, 'signed review P1 ledger invalid', errors)
    req(re.search(r'^- `DECISION REQUIRED`:\s*0\s*$', text, re.M) is not None, 'signed review decision ledger invalid', errors)
    for role in ('Backend Lead', 'Frontend Lead', 'QA Lead', 'Accessibility Reviewer', 'Security Reviewer', 'Product/API Reviewer'):
        req(f'- {role}: APPROVED' in text, f'signed review missing approval: {role}', errors)
    return {
        'status': 'APPROVED',
        'phase': 'signed',
        'merge_authoritative': True,
        'defects': {'p0': 0, 'p1': 0, 'decision_required': 0},
        'pre_sign_implementation_sha': pre.group(1) if pre else None,
        'accountable_reviewer_identity': reviewer.group(1).strip() if reviewer else None,
        'review_date': date.group(1) if date else None,
        'review_sha256': digest(REVIEW),
    }


def write_outputs(head: str, entries: list[dict[str, Any]], regression: dict[str, Any], inherited: dict[str, Any], review: dict[str, Any], errors: list[str]) -> None:
    RESULTS.mkdir(parents=True, exist_ok=True)
    phase = review.get('phase', 'invalid')
    merge_authoritative = phase == 'signed' and not errors
    defects = review.get('defects') if phase == 'signed' else {'p0': None, 'p1': None, 'decision_required': None}
    gate_scope = (
        'P12 CAP-WORKSPACE G3/G6/G10, CAP-FOLDERS-TAGS G3/G6, CAP-CAMPAIGNS governance G3, '
        'and CAP-NOTIFICATIONS core G3/G5/G6/G10 only; P07 analytics is inherited, P15 identity lifecycle '
        'and P13-P17 notification producers remain later-owned.'
    )
    result = {
        'case_id': 'P12-T025',
        'implementation_commit': head,
        'status': 'PASS' if not errors else 'FAIL',
        'errors': errors,
        'details': {
            'closure_phase': phase,
            'merge_authoritative': merge_authoritative,
            'input_evidence_count': len(entries),
            'required_input_evidence_count': 24,
            'regression_workflow_count': len(regression.get('required_workflows', {})) if isinstance(regression, dict) else 0,
            'required_regression_workflows': list(WF),
            'excluded_revision_specific_workflows': list(EXCLUDED),
            'inherited_p11_signed_closure': inherited,
            'defects': defects,
            'gate_scope': gate_scope,
        },
    }
    T025.write_text(json.dumps(result, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    closure = {
        'node': 'P12',
        'status': 'PASS' if not errors else 'FAIL',
        'implementation_commit': head,
        'case_range': 'P12-T001..P12-T025',
        'generated_at': now(),
        'input_evidence_count': len(entries),
        'required_regression_workflow_count': len(WF),
        'phase': phase,
        'merge_authoritative': merge_authoritative,
        'defects': defects,
        'inherited_p11_signed_closure': inherited,
        'gate_scope': {
            'G3': 'PASS — P12 Workspace/organization/campaign/tag/folder/notification-core subset only' if not errors else 'FAIL',
            'G5': 'PASS — P12 notification-core security/redaction/deep-link subset only' if not errors else 'FAIL',
            'G6': 'PASS — P12 Workspace/RBAC/organization/notification state subset only' if not errors else 'FAIL',
            'G10': 'PASS — P12 Workspace/notification UX/accessibility/offline subset only' if not errors else 'FAIL',
            'inherited': 'P07 analytics authority remains inherited and is not redefined by P12',
            'later_owners': 'OPEN — P15 identity lifecycle and P13-P17 notification producers remain later-owned',
        },
        'review': review,
        't025': {
            'path': 'artifacts/v10/P12/results/P12-T025.json',
            'status': result['status'],
            'implementation_commit': head,
            'sha256': digest(T025),
        },
    }
    CLOSURE.write_text(json.dumps(closure, indent=2, sort_keys=True) + '\n', encoding='utf-8')
    index = {
        'node': 'P12',
        'implementation_commit': head,
        'generated_at': now(),
        'phase': phase,
        'merge_authoritative': merge_authoritative,
        'input_evidence': entries,
        'final_case': {'case_id': 'P12-T025', 'path': 'artifacts/v10/P12/results/P12-T025.json', 'sha256': digest(T025)},
        'closure': {'path': 'artifacts/v10/P12/closure.json', 'sha256': digest(CLOSURE)},
        'review': {'path': 'artifacts/v10/P12/review.md', 'sha256': digest(REVIEW)},
        'regression_manifest': {'path': 'artifacts/v10/P12/regression-manifest.json', 'sha256': digest(REG)} if REG.is_file() else None,
        'inherited_p11_signed_closure': inherited,
    }
    IDX.write_text(json.dumps(index, indent=2, sort_keys=True) + '\n', encoding='utf-8')


def run_closure(flag: bool) -> int:
    if not flag:
        print('P12-T025: --closure is required')
        return 2
    head = git('rev-parse', 'HEAD')
    errors: list[str] = []
    validate_plan(errors)
    regression = validate_regression(head, errors)
    entries = validate_cases(head, errors)
    inherited = validate_p11(errors)
    review = validate_review(head, errors)
    req(len(entries) == 24, 'P12 closure requires 24 input evidence files', errors)
    req(len(regression.get('required_workflows', {})) == len(WF), 'P12 closure requires 35 exact-head workflows', errors)
    if review.get('phase') == 'signed':
        req(review.get('defects') == {'p0': 0, 'p1': 0, 'decision_required': 0}, 'signed defect ledger must be zero', errors)
    write_outputs(head, entries, regression, inherited, review, errors)
    phase = review.get('phase', 'invalid')
    if errors:
        print(f'P12-T025 {phase} closure FAIL on {head}')
        for item in errors:
            print(' -', item)
        return 1
    print(f'P12-T025 {phase} closure PASS on {head}; merge-authoritative={phase == "signed"}')
    return 0


if __name__ == '__main__':
    raise SystemExit(run_closure(True))
