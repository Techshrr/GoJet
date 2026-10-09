"""Synthetic retry-boundary checks; never native product evidence."""
import copy
import unittest
from t028_sources import select_prefixed_artifact, verified_archive


class ArtifactAttemptTest(unittest.TestCase):
    def setUp(self):
        self.run = {'id': 12, 'head_sha': 'head', 'run_attempt': 2,
                    'run_started_at': '2026-10-09T14:18:00Z'}
        self.old = {'id': 1, 'name': 'native-merge-sha', 'expired': False,
                    'created_at': '2026-10-09T13:59:54Z',
                    'workflow_run': {'id': 12, 'head_sha': 'head'}}
        self.new = {'id': 2, 'name': 'native-head', 'expired': False,
                    'created_at': '2026-10-09T14:22:42Z',
                    'workflow_run': {'id': 12, 'head_sha': 'head'}}

    def select(self, rows):
        return select_prefixed_artifact(rows, 'native-', self.run)

    def test_different_and_identical_upload_names_select_retry(self):
        self.assertEqual(self.select([self.old, self.new])['id'], 2)
        old = dict(self.old, name=self.new['name'])
        self.assertEqual(self.select([old, self.new])['id'], 2)

    def test_missing_or_ambiguous_attempt_never_falls_back(self):
        for rows in ([self.old, dict(self.old, name='native-other')],
                     [self.old, self.new, dict(self.new, id=3, name='native-other')],
                     [self.new, dict(self.new, id=3)], []):
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                self.select(rows)
        self.run.pop('run_started_at')
        with self.assertRaisesRegex(ValueError, 'boundary missing'):
            self.select([self.old, self.new])

    def test_expiration_run_head_and_digest_boundaries_stay_enforced(self):
        with self.assertRaises(ValueError):
            self.select([dict(self.new, expired=True)])
        wrong = copy.deepcopy(self.new); wrong['workflow_run']['id'] = 13
        with self.assertRaisesRegex(ValueError, 'run mismatch'):
            self.select([wrong])
        import hashlib
        raw = b'not a zip'
        wrong = copy.deepcopy(self.new)
        wrong['digest'] = 'sha256:' + hashlib.sha256(raw).hexdigest()
        wrong['workflow_run']['head_sha'] = 'other'
        with self.assertRaisesRegex(ValueError, 'head mismatch'):
            verified_archive(raw, self.select([wrong]), 'head')
        wrong['workflow_run']['head_sha'] = 'head'; wrong['digest'] = 'forged'
        with self.assertRaisesRegex(ValueError, 'digest mismatch'):
            verified_archive(raw, self.select([wrong]), 'head')

    def test_sole_retained_success_from_unrerun_matrix_job_is_not_discarded(self):
        self.assertEqual(self.select([self.old])['id'], 1)


if __name__ == '__main__':
    unittest.main()
