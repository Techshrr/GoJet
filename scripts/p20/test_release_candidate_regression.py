"""Synthetic Git histories test execution scope; never product PASS evidence."""
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import regression_phase as phase
from candidate_phase import verify
from closure_ci import review_only


class RegressionExecutionTests(unittest.TestCase):
    def git(self, *args):
        return subprocess.check_output(['git', *args], cwd=self.root, text=True,
                                       stderr=subprocess.DEVNULL).strip()

    def write(self, name, text):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, message):
        self.git('add', '.')
        self.git('commit', '-qm', message)
        return self.git('rev-parse', 'HEAD')

    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.git('init', '-q')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'Synthetic test fixture')
        for name in phase.FROZEN:
            if '/P21/' not in name and '/p21/' not in name and 'p21-native' not in name:
                self.write(name, 'frozen fixture\n')
        self.prior = self.commit('pre-sign fixture')
        self.write('artifacts/v10/P20/review.md',
                   f'Reviewed pre-sign implementation SHA: `{self.prior}`\n'
                   'Pre-sign T049 closure run: `123`\nPre-sign T049 closure artifact: `456`\n'
                   'Pre-sign T049 closure digest: `sha256:' + 'c'*64 + '`\n'
                   'Evidence disposition: `P20-T001..P20-T048 PASS`\n'
                   'P0/P1/DECISION REQUIRED: `0/0/0`\n')
        self.signed = self.commit('review-only fixture')
        self.integration = self.git('commit-tree', 'HEAD^{tree}', '-p', self.prior,
                                    '-p', self.signed, '-m', 'integration fixture')
        self.git('reset', '--hard', self.integration)
        for name in phase.FROZEN:
            if not (self.root / name).exists():
                self.write(name, 'frozen P21 fixture\n')
        self.anchor = self.commit('P21 contract fixture')
        self.write('installer/change.txt', 'later implementation fixture\n')
        self.head = self.commit('P21 implementation fixture')
        patcher = patch.multiple(phase, P20_PRIOR=self.prior, P20_SIGNED=self.signed,
                                P20_INTEGRATION=self.integration, P21_AUTHORITY=self.anchor)
        patcher.start()
        self.addCleanup(patcher.stop)

    def record(self):
        return dict(implementation_commit=self.head, status='PASS', errors=[],
                    frozen_contract_preserved=True, merge_authoritative=False,
                    review_phase='signed', mode='signed-review-guard',
                    implementation_authorized=False)

    def test_descendant_can_execute_but_cannot_be_signed_child(self):
        scope = phase.verify_execution(self.record(), self.root, self.head)
        self.assertEqual(scope['mode'], 'p21-regression-only')
        self.assertEqual(scope['p20_signed_commit'], self.signed)
        self.assertEqual(set(scope['frozen_blobs']), set(phase.FROZEN))
        for key in ('merge_authoritative', 'formal_p20_t049_claim',
                    'new_signature_issued', 'p21_p22_completion_claim'):
            self.assertIs(scope[key], False)
        with self.assertRaises(ValueError):
            verify(self.record(), self.root, self.head)
        with self.assertRaises(ValueError):
            review_only(self.root, self.head, self.prior)

    def test_every_uncommitted_frozen_file_change_is_rejected(self):
        for name in phase.FROZEN:
            with self.subTest(path=name):
                path = self.root / name
                original = path.read_bytes()
                path.write_bytes(original + b'tampered\n')
                with self.assertRaises(ValueError):
                    phase.verify_execution(self.record(), self.root, self.head)
                path.write_bytes(original)

    def test_committed_replacement_and_symlink_rejected(self):
        path = self.root / 'artifacts/v10/P20/review.md'
        original = path.read_text()
        path.write_text(original + 'new approval\n')
        self.head = self.commit('unauthorized replacement fixture')
        with self.assertRaises(ValueError):
            phase.verify_execution(self.record(), self.root, self.head)
        self.git('reset', '--hard', 'HEAD^')
        self.head = self.git('rev-parse', 'HEAD')
        other = self.root / 'copy.txt'
        other.write_text(original)
        path.unlink()
        path.symlink_to(other)
        with self.assertRaises(ValueError):
            phase.verify_execution(self.record(), self.root, self.head)

    def test_invalid_contract_and_foreign_checkout_rejected(self):
        for key, value in [('implementation_commit', '0'*40), ('status', 'FAIL'),
                           ('errors', ['failure']), ('frozen_contract_preserved', False),
                           ('merge_authoritative', True), ('implementation_authorized', True),
                           ('review_phase', 'pending'), ('mode', 'verification-guard')]:
            with self.subTest(key=key):
                data = self.record(); data[key] = value
                with self.assertRaises(ValueError):
                    phase.verify_execution(data, self.root, self.head)
        with self.assertRaises(ValueError):
            phase.verify_execution(self.record(), self.root, self.prior)

    def test_unrelated_anchor_never_enables_regression(self):
        tree = self.git('rev-parse', 'HEAD^{tree}')
        orphan = self.git('commit-tree', tree, '-m', 'unrelated fixture')
        with patch.object(phase, 'P21_AUTHORITY', orphan):
            with self.assertRaises(ValueError):
                phase.verify_execution(self.record(), self.root, self.head)
        with patch.object(phase, 'P20_SIGNED', self.prior):
            with self.assertRaises(ValueError):
                phase.verify_execution(self.record(), self.root, self.head)

    def test_original_signed_child_still_uses_original_guard(self):
        self.git('reset', '--hard', self.signed)
        self.head = self.signed
        scope = phase.verify_execution(self.record(), self.root, self.head)
        self.assertEqual(scope['mode'], 'p20-candidate-execution')
        self.assertIs(scope['merge_authoritative'], False)


if __name__ == '__main__':
    unittest.main()
