"""Synthetic admission tests, not product/runtime evidence."""
import json
from pathlib import Path
import tempfile
import unittest

from t028_destination import AUTHORITY, PROVIDER, REDIRECT, PUBLIC, inspect


class DestinationAdmission(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.head = 'a' * 40
        self.rows = {}
        for folder, case, native in [('results', 'P16-T028', False),
                                     ('security', 'P16-T006', True),
                                     ('security', 'P16-T009', True),
                                     ('browser', 'P16-T027', False)]:
            self.rows[case] = (folder, {
                'case' if native else 'case_id': case,
                'exact_head' if native else 'implementation_commit': self.head,
                'status': 'PASS', 'errors': [], 'contract_authority': AUTHORITY,
            })
        self.data('P16-T028')['observations'] = {
            'same_exact_head': True, 'secret_safe': True, 'input_evidence_count': 27}
        for case, required in [('P16-T006', PROVIDER), ('P16-T009', REDIRECT)]:
            self.data(case).update(checks=dict.fromkeys(required, True),
                environment={'mysql_version': '8.4'}, evidence_policy={
                    'dsn_present': False, 'raw_authorization_present': False,
                    'raw_provider_secret_present': False})
        self.data('P16-T006')['record_counts'] = dict(failure_modes=5, allow_decisions=0,
            completed_scans=5, provider_observations=5, policy_decisions=5, secret_matches=0)
        self.data('P16-T009')['record_counts'] = {'runtime_non_allow_states': 8}
        self.data('P16-T027')['details'] = {
            'security_checks': dict.fromkeys(PUBLIC, True), 'frozen_contract_completion': True,
            'states': {'linkunavailable': ['pending', 'review', 'blocked', 'domain-suspended',
                       'domain-revoked', 'domain-expired', 'operational-unavailable'],
                       'abuse_report': ['validation-error', 'Turnstile-error', 'rate-limited',
                                        'success-persistent']}}

    def data(self, case):
        return self.rows[case][1]

    def run_inspect(self):
        for case, (folder, data) in self.rows.items():
            path = self.root / 'artifacts/v10/P16' / folder / (case + '.json')
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(data))
        return inspect(self.root, self.head)

    def test_complete_section_does_not_claim_whole_case(self):
        result = self.run_inspect()
        self.assertFalse(result['formal_p20_t028_claim'])
        self.assertFalse(result['next_case_unlocked'])
        self.assertEqual(len(result['source_evidence']), 4)

    def test_every_required_native_check_is_mandatory(self):
        for case in ['P16-T006', 'P16-T009']:
            checks = self.data(case)['checks']
            for key in list(checks):
                with self.subTest(case=case, check=key):
                    del checks[key]
                    with self.assertRaises(ValueError): self.run_inspect()
                    checks[key] = True

    def test_mixed_head_rejected(self):
        self.data('P16-T009')['exact_head'] = 'b' * 40
        with self.assertRaises(ValueError): self.run_inspect()

    def test_optimistic_allow_rejected(self):
        self.data('P16-T006')['record_counts']['allow_decisions'] = 1
        with self.assertRaises(ValueError): self.run_inspect()

    def test_missing_browser_state_rejected(self):
        self.data('P16-T027')['details']['states']['linkunavailable'].remove('review')
        with self.assertRaises(ValueError): self.run_inspect()

    def test_false_or_nonboolean_checks_rejected(self):
        for value in [False, 1, 'true']:
            self.data('P16-T027')['details']['security_checks']['no_continue_anyway_control'] = value
            with self.assertRaises(ValueError): self.run_inspect()

    def test_secret_leak_rejected(self):
        self.data('P16-T006')['evidence_policy']['raw_provider_secret_present'] = True
        with self.assertRaises(ValueError): self.run_inspect()
