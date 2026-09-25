import hashlib
import io
import json
import unittest
from unittest.mock import patch
import zipfile

from scripts.p18 import p04_replay as replay


class ReplayVerificationTests(unittest.TestCase):
    def archive(self, source=None, failed=False, bad_index=False):
        files = {'P04/source.json': json.dumps({'implementation_commit': source or replay.SOURCE}).encode()}
        for n in range(1, 11):
            case = f'P04-T{n:03d}'
            files[f'P04/results/{case}.json'] = json.dumps({
                'case_id': case, 'status': 'FAIL' if failed and n == 10 else 'PASS', 'errors': []}).encode()
        index = {'implementation_commit': replay.SOURCE, 'results': {'passed': 10, 'failed': 0, 'total': 10},
                 'files': [{'path': 'artifacts/v10/' + path,
                            'sha256': 'wrong' if bad_index else hashlib.sha256(data).hexdigest()}
                           for path, data in files.items()]}
        files['P04/evidence-index.json'] = json.dumps(index).encode()
        out = io.BytesIO()
        with zipfile.ZipFile(out, 'w') as z:
            for path, data in files.items():
                z.writestr(path, data)
        return out.getvalue()

    def verify_fixture(self, data):
        # Use a controlled archive pin to exercise checks below the outer digest.
        with patch.dict(replay.REPLAY, {'artifact_digest': 'sha256:' + hashlib.sha256(data).hexdigest()}):
            replay.verify_archive(data)

    def test_all_cases_and_hashes(self):
        self.verify_fixture(self.archive())

    def test_wrong_source_failure_and_modified_index_are_rejected(self):
        for args in ({'source': 'other-source'}, {'failed': True}, {'bad_index': True}):
            with self.subTest(args=args), self.assertRaises(RuntimeError):
                self.verify_fixture(self.archive(**args))

    def test_unpinned_archive_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, 'archive digest'):
            replay.verify_archive(self.archive())

    def test_download_redirect_does_not_forward_token(self):
        request = replay.urllib.request.Request('https://api.github.com/artifact', headers={'Authorization': 'Bearer fixture'})
        redirect = replay.PublicDownloadRedirect().redirect_request(
            request, None, 302, 'Found', {}, 'https://artifact.example/download')
        self.assertIsNone(redirect.get_header('Authorization'))


if __name__ == '__main__':
    unittest.main()
