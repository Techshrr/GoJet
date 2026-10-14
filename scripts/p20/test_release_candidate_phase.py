import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from candidate_phase import verify


class CandidatePhaseTests(unittest.TestCase):
    def record(self, signed=False):
        return dict(implementation_commit='a'*40, status='PASS', errors=[],
                    frozen_contract_preserved=True, merge_authoritative=False,
                    review_phase='signed' if signed else 'pending',
                    mode='signed-review-guard' if signed else 'verification-guard',
                    implementation_authorized=not signed)

    def test_pending_still_requires_implementation_authority(self):
        data=self.record(); verify(data, Path('.'), 'a'*40)
        data['implementation_authorized']=False
        with self.assertRaises(ValueError): verify(data, Path('.'), 'a'*40)

    def test_signed_requires_exact_review_only_child(self):
        with tempfile.TemporaryDirectory() as name:
            root=Path(name); review=root/'artifacts/v10/P20/review.md'
            review.parent.mkdir(parents=True)
            review.write_text('Reviewed pre-sign implementation SHA: `'+ 'b'*40+'`\n'
                'Pre-sign T049 closure run: `123`\nPre-sign T049 closure artifact: `456`\n'
                'Pre-sign T049 closure digest: `sha256:'+ 'c'*64+'`\n'
                'Evidence disposition: `P20-T001..P20-T048 PASS`\nP0/P1/DECISION REQUIRED: `0/0/0`')
            with patch('closure_ci.subprocess.check_output', side_effect=[
                    'a'*40+' '+'b'*40, 'artifacts/v10/P20/review.md']):
                verify(self.record(True), root, 'a'*40)
            with patch('closure_ci.subprocess.check_output', side_effect=[
                    'a'*40+' '+'b'*40, 'artifacts/v10/P20/review.md\ninternal/product.go']):
                with self.assertRaises(ValueError): verify(self.record(True), root, 'a'*40)
            with patch('closure_ci.subprocess.check_output', return_value='a'*40+' '+'d'*40):
                with self.assertRaises(ValueError): verify(self.record(True), root, 'a'*40)

    def test_signed_does_not_authorize_implementation_or_merge(self):
        for key in ('implementation_authorized', 'merge_authoritative'):
            data=self.record(True); data[key]=True
            with self.assertRaises(ValueError): verify(data, Path('.'), 'a'*40)
