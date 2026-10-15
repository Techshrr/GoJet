"""Transport/admission attacks; synthetic fixtures are not product evidence."""
import io
import json
from pathlib import Path
import tempfile
import unittest
import zipfile
import urllib.error
from unittest.mock import patch, Mock
from t028_sources import digest, member, verified_archive, select_run, successful
from t028_sources import download_archive, select_artifact
from t028_case import inspect


class SourceAdmissionTests(unittest.TestCase):
    head = 'a' * 40

    def test_retry_artifact_is_bound_to_current_attempt(self):
        run = {'id': 9, 'run_started_at': '2026-10-05T08:00:00Z'}
        old = {'id': 1, 'name': 'proof', 'expired': False, 'created_at': '2026-10-05T07:00:00Z', 'workflow_run': {'id': 9}}
        current = dict(old, id=2, created_at='2026-10-05T08:01:00Z')
        self.assertEqual(select_artifact([old, current], 'proof', run)['id'], 2)
        for rows in ([old, dict(old, id=3)], [current, dict(current, id=3)], [dict(current, workflow_run={'id': 10})]):
            with self.assertRaises(ValueError):
                select_artifact(rows, 'proof', run)
        with self.assertRaises(ValueError):
            select_artifact([old, current], 'proof', {'id': 9})

    def test_archive_download_redirect_does_not_forward_authorization(self):
        url = 'https://api.github.com/repos/Techshrr/GoJet/actions/artifacts/1/zip'
        location = 'https://artifact.example/signed-download'
        opener = Mock()
        opener.open.side_effect = urllib.error.HTTPError(url, 302, 'Found', {'Location': location}, None)
        with patch('t028_sources.urllib.request.build_opener', return_value=opener), \
             patch('t028_sources.urllib.request.urlopen', return_value=io.BytesIO(b'archive')) as signed:
            self.assertEqual(download_archive(url, {'Authorization': 'Bearer test-only'}), b'archive')
            self.assertEqual(opener.open.call_args.args[0].get_header('Authorization'), 'Bearer test-only')
            signed.assert_called_once_with(location, timeout=120)

    def test_archive_download_direct_response_and_invalid_redirect(self):
        opener = Mock()
        opener.open.return_value = io.BytesIO(b'archive')
        with patch('t028_sources.urllib.request.build_opener', return_value=opener):
            self.assertEqual(download_archive('https://api.github.com/archive', {}), b'archive')
        for code, location in [(403, 'https://artifact.example/zip'), (302, 'http://artifact.example/zip')]:
            opener.open.side_effect = urllib.error.HTTPError('https://api.github.com/archive', code, 'Error',
                                                            {'Location': location}, None)
            with patch('t028_sources.urllib.request.build_opener', return_value=opener), \
                 patch('t028_sources.urllib.request.urlopen') as signed, self.assertRaises(ValueError):
                download_archive('https://api.github.com/archive', {})
            signed.assert_not_called()

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

    def test_same_head_manual_repair_replaces_older_pr_run(self):
        pr = {'id': 5, 'path': '.github/workflows/check.yml', 'head_sha': self.head,
              'event': 'pull_request', 'head_repository': {'full_name': 'Techshrr/GoJet'}}
        manual = dict(pr, id=6, event='workflow_dispatch')
        self.assertEqual(select_run([pr, manual], 'check.yml', self.head, 'Techshrr/GoJet'), manual)
        for changed in ({'head_sha': 'b' * 40}, {'head_repository': {'full_name': 'outsider/GoJet'}},
                        {'path': '.github/workflows/other.yml'}):
            bad = dict(manual, **changed)
            self.assertEqual(select_run([pr, bad], 'check.yml', self.head, 'Techshrr/GoJet'), pr)

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
