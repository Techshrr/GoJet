"""Negative controls for formal evidence admission, not product substitutes."""
import unittest

from integration_cases import check_go_events


class NativeEvidenceAdmission(unittest.TestCase):
    def test_missing_skipped_failed_and_truncated_streams_are_rejected(self):
        passing = [{'Action': 'pass', 'Test': 'Required'}, {'Action': 'pass'}]
        for events in ([], passing[:1], passing[1:],
                       passing + [{'Action': 'skip', 'Test': 'Other'}],
                       passing + [{'Action': 'fail', 'Test': 'Other'}]):
            with self.subTest(events=events), self.assertRaises(ValueError):
                check_go_events(events, {'Required'})

    def test_all_required_tests_and_package_must_pass(self):
        events = [{'Action': 'pass', 'Test': 'Required'}, {'Action': 'pass'}]
        self.assertEqual(check_go_events(events, {'Required'}), ['Required'])


if __name__ == '__main__':
    unittest.main()
