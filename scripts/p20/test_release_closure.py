"""Adversarial closure fixtures; these are never published as product evidence."""
import copy
import io
import json
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

from common import ROOT
from release_sources import SOURCES, inspect, case_paths
from release_matrix import validate_inventory
from t028_sources import digest
from closure_ci import review_fields, review_only

HEAD = 'a' * 40


class OriginalArchiveAdmission(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        paths = case_paths(ROOT)
        self.manifest = {'implementation_commit': HEAD, 'matrix': {}, 'artifacts': {}}
        for identity, workflow in enumerate(validate_inventory(ROOT), 1):
            from release_matrix import JOB_AUTHORITY
            run = {'id': identity, 'head_sha': HEAD, 'path': '.github/workflows/' + workflow,
                   'event': 'workflow_dispatch', 'head_repository': {'full_name': 'Techshrr/GoJet'},
                   'status': 'completed', 'conclusion': 'success', 'run_attempt': 1}
            job = {'id': identity, 'run_id': identity, 'name': JOB_AUTHORITY.get(workflow, 'native'),
                   'status': 'completed', 'conclusion': 'success'}
            self.manifest['matrix'][workflow] = {'run': run, 'jobs': [job]}
        for identity, (workflow, (prefix, numbers)) in enumerate(SOURCES.items(), 1000):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as z:
                for n in numbers:
                    case = f'P20-T{n:03d}'
                    z.writestr('P20/' + paths[case], json.dumps({'case': case, 'implementation_commit': HEAD,
                               'status': 'PASS', 'errors': [], 'details': {}}))
                if workflow == 'p20-candidate-freeze.yml':
                    z.writestr('P20/authority/integrated-authority-ledger.json', json.dumps({
                        'implementation_commit': HEAD, 'live_bound_count': 20, 'ancestry_bound_count': 20}))
            raw = buffer.getvalue()
            (self.directory / f'{identity}.zip').write_bytes(raw)
            self.manifest['artifacts'][workflow] = {'id': identity, 'name': prefix + HEAD,
                'expired': False, 'digest': digest(raw), 'workflow_run': {
                    'id': self.manifest['matrix'][workflow]['run']['id'], 'head_sha': HEAD}}
        self.save()

    def save(self):
        (self.directory / 'collection.json').write_text(json.dumps(self.manifest))

    def verify(self):
        return inspect(ROOT, HEAD, self.directory)

    def change_case(self, transform):
        row = self.manifest['artifacts']['p20-p0-support.yml']
        path = self.directory / f'{row["id"]}.zip'
        with zipfile.ZipFile(path) as z:
            members = {name: z.read(name) for name in z.namelist()}
        name = 'P20/recovery/P20-T045.json'
        data = json.loads(members[name]); transform(data)
        members[name] = json.dumps(data).encode()
        with zipfile.ZipFile(path, 'w') as z:
            for name, raw in members.items(): z.writestr(name, raw)
        row['digest'] = digest(path.read_bytes()); self.save()

    def test_complete_fixture_and_missing_matrix(self):
        self.assertEqual(len(self.verify()['cases']), 46)
        self.manifest['matrix'].pop('p01-engineering.yml'); self.save()
        with self.assertRaisesRegex(ValueError, 'regression inventory'): self.verify()

    def test_changed_archive_rejected_before_reading_pass(self):
        row = self.manifest['artifacts']['p20-p0-support.yml']
        path = self.directory / f'{row["id"]}.zip'; path.write_bytes(path.read_bytes() + b'tampered')
        with self.assertRaisesRegex(ValueError, 'digest mismatch'): self.verify()

    def test_mixed_head_case_rejected_despite_valid_zip(self):
        self.change_case(lambda x: x.update(implementation_commit='b' * 40))
        with self.assertRaisesRegex(ValueError, 'invalid formal evidence'): self.verify()

    def test_pass_with_errors_rejected(self):
        self.change_case(lambda x: x.update(errors=['failed native assertion']))
        with self.assertRaisesRegex(ValueError, 'invalid formal evidence'): self.verify()

    def test_secret_rejected_without_printing_value(self):
        self.change_case(lambda x: x['details'].update(secret='sk-test-only-invalid-credential'))
        with self.assertRaisesRegex(ValueError, 'secret-bearing formal evidence'): self.verify()

    def test_foreign_artifact_run_rejected(self):
        self.manifest['artifacts']['p20-p0-support.yml']['workflow_run']['id'] = -1; self.save()
        with self.assertRaisesRegex(ValueError, 'foreign formal artifact'): self.verify()

    def test_failed_latest_job_rejected(self):
        row = self.manifest['matrix']['p01-engineering.yml']
        newer = copy.deepcopy(row['jobs'][0]); newer.update(id=999999, conclusion='failure')
        row['jobs'].append(newer); self.save()
        with self.assertRaisesRegex(ValueError, 'job failed'): self.verify()


class ReviewBoundary(unittest.TestCase):
    def test_missing_or_ambiguous_signature_fields(self):
        with self.assertRaises(ValueError): review_fields('Status: signed')
        with self.assertRaises(ValueError): review_fields(('Reviewed pre-sign implementation SHA: `' + HEAD + '`\n') * 2)

    def test_product_change_cannot_be_signed_child(self):
        with patch('closure_ci.subprocess.check_output', side_effect=[HEAD+' '+'b'*40+'\n',
                 'artifacts/v10/P20/review.md\ninternal/billing/callbacks.go\n']):
            with self.assertRaisesRegex(ValueError, 'implementation or evidence changes'):
                review_only(ROOT, HEAD, 'b'*40)


if __name__ == '__main__':
    unittest.main()
