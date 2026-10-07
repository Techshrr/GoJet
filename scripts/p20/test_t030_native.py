import json
import unittest
from t030_native import inspect, REQUIRED, PACKAGE


class NativeAdmissionTests(unittest.TestCase):
    def rows(self):
        return [{'Action': 'pass', 'Package': PACKAGE, 'Test': name} for name in sorted(REQUIRED)] + [
            {'Action': 'pass', 'Package': PACKAGE}]

    def check(self, rows):
        return inspect('\n'.join(json.dumps(r) for r in rows).encode(), 'a' * 40)

    def test_complete_is_partial_only(self):
        self.assertIs(self.check(self.rows())['formal_p20_t030_claim'], False)

    def test_every_required_verdict_is_mandatory(self):
        for name in REQUIRED:
            with self.subTest(name=name), self.assertRaises(ValueError):
                self.check([r for r in self.rows() if r.get('Test') != name])

    def test_skip_failure_duplicate_and_wrong_package_rejected(self):
        for action in ['skip', 'fail']:
            with self.subTest(action=action), self.assertRaises(ValueError):
                self.check(self.rows() + [{'Action': action, 'Package': PACKAGE}])
        with self.assertRaises(ValueError):
            self.check(self.rows() + [self.rows()[0]])
        with self.assertRaises(ValueError):
            self.check([{**r, 'Package': 'other'} for r in self.rows()])


if __name__ == '__main__':
    unittest.main()
