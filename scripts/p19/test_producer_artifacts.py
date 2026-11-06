import unittest

from scripts.p19.producer_artifacts import select_artifact


class ProducerArtifactTests(unittest.TestCase):
    def setUp(self):
        self.run = {'id': 42, 'status': 'completed', 'conclusion': 'success',
                    'head_sha': 'candidate', 'run_started_at': '2026-10-28T12:00:00Z'}
        self.old = {'id': 1, 'name': 'proof', 'expired': False,
                    'created_at': '2026-10-27T12:00:00Z',
                    'workflow_run': {'id': 42, 'head_sha': 'candidate'}}
        self.current = dict(self.old, id=2, created_at='2026-10-28T12:01:00Z')

    def test_select_current_attempt_independent_of_listing_order(self):
        for rows in ([self.old, self.current], [self.current, self.old], [self.current]):
            self.assertEqual(select_artifact(rows, 'proof', self.run, 'candidate'), self.current)

    def test_reject_missing_stale_expired_and_ambiguous_uploads(self):
        for rows in ([], [self.old], [dict(self.current, expired=True)],
                     [dict(self.current, name='other')],
                     [self.current, dict(self.current, id=3)]):
            with self.subTest(rows=rows), self.assertRaises(ValueError):
                select_artifact(rows, 'proof', self.run, 'candidate')

    def test_reject_wrong_run_and_head(self):
        for binding in ({'id': 43, 'head_sha': 'candidate'}, {'id': 42, 'head_sha': 'other'}, {}):
            with self.subTest(binding=binding), self.assertRaises(ValueError):
                select_artifact([dict(self.current, workflow_run=binding)], 'proof', self.run, 'candidate')

    def test_reject_failed_pending_or_wrong_head_producer(self):
        for change in ({'conclusion': 'failure'}, {'status': 'in_progress'}, {'head_sha': 'other'}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                select_artifact([self.current], 'proof', dict(self.run, **change), 'candidate')

    def test_reject_unproven_time_boundary_and_incomplete_listing(self):
        for boundary in (None, '', 'not-a-time'):
            with self.subTest(boundary=boundary), self.assertRaises((ValueError, AttributeError)):
                select_artifact([self.current], 'proof', dict(self.run, run_started_at=boundary), 'candidate')
        for rows in (None, [self.old] * 99 + [self.current]):
            with self.assertRaises(ValueError):
                select_artifact(rows, 'proof', self.run, 'candidate')


if __name__ == '__main__':
    unittest.main()
