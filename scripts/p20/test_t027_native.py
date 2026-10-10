"""Adversarial admission tests, not product authority."""
import json
import unittest
from scripts.p20.t027_native import PACKAGE, required_tests, validate_native


class NativeAdmissionTests(unittest.TestCase):
    def setUp(self):
        self.rows = [{'Package': PACKAGE, 'Test': name, 'Action': 'pass'}
                     for name in sorted(required_tests())]
        self.rows.append({'Package': PACKAGE, 'Action': 'pass'})

    def validate(self, rows=None, head='a' * 40):
        raw = '\n'.join(json.dumps(row) for row in (self.rows if rows is None else rows)).encode()
        return validate_native(raw, head, PACKAGE)

    def test_complete_matrix_remains_nonformal(self):
        self.assertIs(self.validate()['formal_p20_t027_claim'], False)

    def test_every_missing_case_is_rejected(self):
        for index in range(len(self.rows)):
            with self.subTest(index=index), self.assertRaises(ValueError):
                self.validate(self.rows[:index] + self.rows[index + 1:])

    def test_skip_or_fail_rejected_even_with_pass(self):
        for action in ('skip', 'fail'):
            with self.subTest(action=action), self.assertRaises(ValueError):
                self.validate(self.rows + [{'Action': action}])

    def test_wrong_package_rejected(self):
        with self.assertRaises(ValueError):
            self.validate([dict(row, Package='wrong/package') for row in self.rows])

    def test_duplicate_verdict_rejected(self):
        with self.assertRaises(ValueError):
            self.validate(self.rows + [self.rows[0]])

    def test_truncated_sha_rejected(self):
        with self.assertRaises(ValueError):
            self.validate(head='abcdef0')

    def test_empty_evidence_rejected(self):
        with self.assertRaises(ValueError):
            self.validate([])
