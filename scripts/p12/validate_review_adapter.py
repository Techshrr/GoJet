#!/usr/bin/env python3
from __future__ import annotations

import re
from typing import Any

FROZEN_REVIEW_MARKERS = (
    'Required P12 case range: **P12-T001..P12-T025**.',
    'no `/app/folders` route',
    'does **not** implement or claim P15',
    'GET /api/invitations/{token}',
    'SAME-REVISION CI REQUIRED',
)
SIGNED_REVIEWER = 'GPT-5.6 Sol — P12 Technical Review'
ROLES = (
    'Backend Lead',
    'Frontend Lead',
    'QA Lead',
    'Accessibility Reviewer',
    'Security Reviewer',
    'Product/API Reviewer',
)


def install(closure: Any) -> None:
    """Install the frozen P12 review parser into the T025 closure validator."""

    def validate_review(head: str, errors: list[str]) -> dict[str, Any]:
        review = closure.REVIEW
        closure.req(review.is_file(), 'missing P12 review.md', errors)
        if not review.is_file():
            return {'phase': 'missing', 'merge_authoritative': False, 'defects': None}

        text = review.read_text(encoding='utf-8')
        status_lines = [line.strip() for line in text.splitlines() if line.strip().startswith('Status:')]
        pending = status_lines == [closure.PENDING]
        signed = status_lines == [closure.SIGNED]
        closure.req(pending ^ signed, f'review status invalid: {status_lines}', errors)
        closure.req(
            '## 11. Signed-revision rule' in text or '## Signed-revision rule' in text,
            'signed-revision rule missing',
            errors,
        )
        for marker in FROZEN_REVIEW_MARKERS:
            closure.req(marker in text, f'review marker missing {marker}', errors)
        for needle in ('P15', 'P13-P17', 'P07', 'no `/app/folders` route'):
            closure.req(needle in text, f'review ownership/route boundary missing: {needle}', errors)

        if pending:
            closure.req('No P12 PASS or Exit claim is made in this state.' in text, 'pending no-PASS marker missing', errors)
            closure.req('Accountable reviewer identity:' not in text, 'pending review contains signature', errors)
            closure.req(
                re.search(r'(?mi)^\s*(?:[-*]\s*)?P12-T\d{3}\s*[:=-]\s*PASS\b', text) is None,
                'pending review contains case PASS',
                errors,
            )
            return {
                'status': 'PENDING',
                'phase': 'pre-sign',
                'merge_authoritative': False,
                'defects': {'p0': None, 'p1': None, 'decision_required': None},
                'review_sha256': closure.digest(review),
            }

        parent = closure.git('rev-parse', 'HEAD^')
        changed = [item for item in closure.git('diff', '--name-only', 'HEAD^', 'HEAD').splitlines() if item]
        closure.req(changed == ['artifacts/v10/P12/review.md'], 'signed revision must be review-only child', errors)

        pre = re.search(r'^Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`\s*$', text, re.M)
        reviewer = re.search(
            r'^Accountable reviewer identity:\s*\*\*(GPT-5\.6 Sol — P12 Technical Review)\*\*\s*$',
            text,
            re.M,
        )
        date = re.search(r'^Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*\s*$', text, re.M)
        closure.req(bool(pre), 'signed review pre-sign exact implementation SHA missing', errors)
        closure.req(bool(reviewer), 'signed review accountable reviewer identity missing', errors)
        closure.req(bool(date), 'signed review date missing', errors)
        if pre:
            closure.req(pre.group(1) == parent, 'signed review pre-sign SHA is not HEAD parent', errors)
            closure.req(pre.group(1) != head, 'signed review pre-sign SHA cannot equal signed HEAD', errors)
        if reviewer:
            closure.req(reviewer.group(1) == SIGNED_REVIEWER, 'signed review accountable reviewer identity invalid', errors)

        closure.req(re.search(r'^- P0 defects:\s*0\s*$', text, re.M) is not None, 'signed review P0 ledger invalid', errors)
        closure.req(re.search(r'^- P1 defects:\s*0\s*$', text, re.M) is not None, 'signed review P1 ledger invalid', errors)
        closure.req(re.search(r'^- `DECISION REQUIRED`:\s*0\s*$', text, re.M) is not None, 'signed review decision ledger invalid', errors)
        for role in ROLES:
            closure.req(f'- {role}: APPROVED' in text, f'signed review missing approval: {role}', errors)
        closure.req('P12-T025' in text and 'PASS' in text, 'signed final closure record missing', errors)
        closure.req('signed revision itself must rerun' in text.lower(), 'signed rerun rule missing', errors)

        return {
            'status': 'APPROVED',
            'phase': 'signed',
            'merge_authoritative': True,
            'defects': {'p0': 0, 'p1': 0, 'decision_required': 0},
            'pre_sign_implementation_sha': pre.group(1) if pre else None,
            'accountable_reviewer_identity': reviewer.group(1) if reviewer else None,
            'review_date': date.group(1) if date else None,
            'review_sha256': closure.digest(review),
        }

    closure.validate_review = validate_review
