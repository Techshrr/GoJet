"""Transport/admission attacks; synthetic fixtures are not product evidence."""
import io
import json
from pathlib import Path
import tempfile
import unittest
import zipfile
from t028_sources import digest, member, verified_archive, select_run, successful
from t028_case import inspect


class SourceAdmissionTests(unittest.TestCase):
    head = 'a' * 40

    def archive(self, entries):
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as archive:
            for name, content in entries:
                archive.writestr(name, content)
        return stream.getvalue()

    def test_archive_digest_expiry_and_head(self):
        raw = self.archive([('results/example.json', '{}')])
        meta = {'digest': digest(raw), 'expired': False, 'workflow_run': {'head_sha': self.head}}
        self.assertEqual(verified_archive(raw, meta, self.head).namelist(), ['results/example.json'])
        for changed in ({'digest': 'sha256:' + '0' * 64}, {'expired': True},
                        {'workflow_run': {'head_sha': 'b' * 40}}):
            with self.assertRaises(ValueError):
                verified_archive(raw, dict(meta, **changed), self.head)

    def test_unique_member_and_known_roots(self):
        for path in ['results/x.json', 'P06/results/x.json', 'artifacts/v10/P06/results/x.json']:
            with zipfile.ZipFile(io.BytesIO(self.archive([(path, '{}')]))) as archive:
                self.assertEqual(member(archive, 'P06', 'results/x.json'), b'{}')
                with self.assertRaises(ValueError):
                    member(archive, 'P06', '../x.json')
                with self.assertRaises(ValueError):
                    member(archive, 'P06', 'results/missing.json')
        raw = self.archive([('results/x.json', '{}'), ('P06/results/x.json', '{}')])
        with zipfile.ZipFile(io.BytesIO(raw)) as archive, self.assertRaises(ValueError):
            member(archive, 'P06', 'results/x.json')

    def test_run_selection_rejects_wrong_head_repository_event_or_workflow(self):
        good = {'id': 5, 'path': '.github/workflows/check.yml', 'head_sha': self.head,
                'event': 'pull_request', 'head_repository': {'full_name': 'Techshrr/GoJet'}}
        for changed in ({'head_sha': 'b' * 40}, {'event': 'push'}, {'path': '.github/workflows/other.yml'},
                        {'head_repository': {'full_name': 'outsider/GoJet'}}):
            bad = dict(good, id=6, **changed)
            self.assertEqual(select_run([good, bad], 'check.yml', self.head, 'Techshrr/GoJet'), good)

    def test_failed_or_pending_producer_never_passes(self):
        run = {'path': 'check.yml', 'status': 'in_progress'}
        self.assertFalse(successful(run, None, []))
        run.update(status='completed', conclusion='failure')
        with self.assertRaises(ValueError):
            successful(run, None, [])
        self.assertFalse(successful(run, 'contract', []))
        job = {'name': 'contract', 'status': 'completed', 'conclusion': 'failure'}
        with self.assertRaises(ValueError):
            successful(run, 'contract', [job])
        job['conclusion'] = 'success'
        self.assertTrue(successful(run, 'contract', [job]))
        with self.assertRaises(ValueError):
            successful(run, 'contract', [job, job])

    def test_combined_admission_never_accepts_empty_or_mixed_collection(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(OSError):
                inspect(root, self.head)
            runtime = root / 'artifacts/v10/P20/runtime/t028'
            runtime.mkdir(parents=True)
            for data in [{'implementation_commit': 'b' * 40},
                         {'implementation_commit': self.head, 'files': {}, 'artifacts': {}, 'gates': {}}]:
                (runtime / 'collection.json').write_text(json.dumps(data))
                with self.assertRaises(ValueError):
                    inspect(root, self.head)


if __name__ == '__main__':
    unittest.main()
