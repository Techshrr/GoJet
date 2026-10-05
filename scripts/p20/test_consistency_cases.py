"""Admission attack cases only; synthetic inputs are never product evidence."""
import json
from pathlib import Path
import tempfile
import unittest
from consistency_cases import BROWSER_CHECKS, inspect_browser, inspect_native
from t027_native import PACKAGE, required_tests, validate_native

class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.directory = self.root / 'artifacts/v10/P20/runtime/t027'
        self.directory.mkdir(parents=True)
        self.head = 'a' * 40
        self.browser = {'implementation_commit': self.head, 'production_session': True,
                        'mocked_api': False, 'checks': dict.fromkeys(BROWSER_CHECKS, True)}
        self.rows = [{'Package': PACKAGE, 'Action': 'pass', 'Test': 'TestP20WorkspaceProductionBrowser'},
                     {'Package': PACKAGE, 'Action': 'pass'}]

    def check_browser(self):
        (self.directory / 'workspace-browser.json').write_text(json.dumps(self.browser))
        (self.directory / 'workspace-browser.jsonl').write_text('\n'.join(map(json.dumps, self.rows)))
        return inspect_browser(self.root, self.head)

    def test_complete_admission(self):
        self.assertEqual(len(self.check_browser()['checks']), 28)

    def test_missing_each_browser_case(self):
        for name in BROWSER_CHECKS:
            with self.subTest(name=name):
                self.browser['checks'] = dict.fromkeys(BROWSER_CHECKS - {name}, True)
                with self.assertRaises(ValueError): self.check_browser()

    def test_mixed_sha(self):
        self.browser['implementation_commit'] = 'b' * 40
        with self.assertRaises(ValueError): self.check_browser()

    def test_mocked_api(self):
        self.browser['mocked_api'] = True
        with self.assertRaises(ValueError): self.check_browser()

    def test_skip_even_with_pass(self):
        self.rows.append({'Action': 'skip'})
        with self.assertRaises(ValueError): self.check_browser()

    def test_missing_package_verdict(self):
        self.rows.pop()
        with self.assertRaises(ValueError): self.check_browser()

    def test_native_hash_and_sha(self):
        rows = [{'Package': PACKAGE, 'Action': 'pass', 'Test': name} for name in required_tests()]
        rows.append({'Package': PACKAGE, 'Action': 'pass'})
        raw = '\n'.join(map(json.dumps, rows)).encode()
        path = self.directory / 'workspace-rbac.jsonl'
        path.write_bytes(raw)
        manifest = validate_native(raw, self.head, PACKAGE)
        manifest_path = self.directory / 'native-manifest.json'
        manifest_path.write_text(json.dumps(manifest))
        inspect_native(self.root, self.head)
        with self.assertRaises(ValueError): inspect_native(self.root, 'b' * 40)
        path.write_bytes(raw + b'\n')
        with self.assertRaises(ValueError): inspect_native(self.root, self.head)
