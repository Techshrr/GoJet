"""Admission fixtures only; never substitute for real product runtime evidence."""
import copy
import hashlib
import unittest
from t029_case import BIO_MODES, inspect_bio


class BioAdmissionTests(unittest.TestCase):
    def setUp(self):
        old = hashlib.sha256(b'gojet-v10-risk-targets-v1\nhttps://example.com/old\n').hexdigest()
        self.data = {'old_fingerprint': old, 'shared_link_fingerprint': old,
                     'new_fingerprint': 'a' * 64, 'public_status': 200,
                     'denied_risk_modes': sorted(BIO_MODES),
                     'canonical_equivalent_preserved_allow': True,
                     'old_allow_retained_but_non_authoritative': True,
                     'html_api_denial_parity': True, 'current_exact_allow_recovered': True}

    def test_complete(self):
        inspect_bio(self.data)

    def test_missing_each_denial_mode(self):
        for mode in BIO_MODES:
            bad = copy.deepcopy(self.data)
            bad['denied_risk_modes'].remove(mode)
            with self.subTest(mode=mode), self.assertRaises(ValueError):
                inspect_bio(bad)

    def test_missing_every_observation(self):
        for key in self.data:
            bad = copy.deepcopy(self.data)
            del bad[key]
            with self.subTest(key=key), self.assertRaises(ValueError):
                inspect_bio(bad)

    def test_old_allow_and_optimistic_success_rejected(self):
        for change in [{'new_fingerprint': self.data['old_fingerprint']},
                       {'html_api_denial_parity': False}, {'current_exact_allow_recovered': 1}]:
            with self.assertRaises(ValueError):
                inspect_bio(dict(self.data, **change))


if __name__ == '__main__':
    unittest.main()
