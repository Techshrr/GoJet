"""Admit pending candidates or a verified direct review-only signed child."""
from closure_ci import review_fields, review_only
from t028_sources import require


def verify(data, root, head):
    require(data['implementation_commit'] == head and data['status'] == 'PASS'
            and data['errors'] == [] and data['frozen_contract_preserved'] is True
            and data['merge_authoritative'] is False, 'invalid candidate contract')
    if data['review_phase'] == 'pending':
        require(data['mode'] == 'verification-guard' and data['implementation_authorized'] is True,
                'invalid pending candidate phase')
        return
    require(data['review_phase'] == 'signed' and data['mode'] == 'signed-review-guard'
            and data['implementation_authorized'] is False, 'invalid signed candidate phase')
    fields = review_fields((root / 'artifacts/v10/P20/review.md').read_text())
    review_only(root, head, fields['head'])
    # This admits execution only. T049 still live-verifies the original pre-sign
    # run/archive and independently reproduces this child's complete evidence.
