import copy
import unittest
from t030_case import EXPECTED, inspect_observations


class HTTPAdmissionTests(unittest.TestCase):
    def test_reviewed_observations(self):
        for path, (key, expected) in EXPECTED.items():
            inspect_observations(path, {key: expected})

    def test_every_observation_is_required(self):
        for path, (key, expected) in EXPECTED.items():
            for field in expected:
                with self.subTest(path=path, field=field), self.assertRaises(ValueError):
                    changed = dict(expected)
                    del changed[field]
                    inspect_observations(path, {key: changed})

    def test_error_status_cannot_be_success_or_string(self):
        for path, (key, expected) in EXPECTED.items():
            for field, value in expected.items():
                if type(value) is int and value >= 400:
                    for wrong in (200, str(value)):
                        with self.subTest(path=path, field=field, wrong=wrong), self.assertRaises(ValueError):
                            changed = copy.deepcopy(expected)
                            changed[field] = wrong
                            inspect_observations(path, {key: changed})


if __name__ == '__main__':
    unittest.main()
