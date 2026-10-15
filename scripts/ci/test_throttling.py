import io
import unittest
from urllib.error import HTTPError
from scripts.ci.actions import github_json


class ThrottlingTests(unittest.TestCase):
    def run_response(self, code, headers, body):
        calls, waits = [], []
        def opener(*args, **kwargs):
            calls.append(1)
            if len(calls) == 1:
                raise HTTPError('https://api.github.com/test', code, 'error', headers, io.BytesIO(body))
            return io.BytesIO(b'{"verified":true}')
        result = github_json('https://api.github.com/test', {}, opener=opener,
                             sleep=waits.append, clock=lambda: 1000)
        return result, waits

    def test_retry_rate_limit_without_weakening_result(self):
        result, waits = self.run_response(403, {'X-RateLimit-Remaining': '0', 'X-RateLimit-Reset': '1065'}, b'{}')
        self.assertEqual(result, {'verified': True})
        self.assertEqual(waits, [60, 6])
        self.assertEqual(self.run_response(429, {'Retry-After': '2'}, b'{}')[1], [2])

    def test_permission_and_missing_artifact_are_not_retried(self):
        for code in (403, 404):
            with self.subTest(code=code), self.assertRaises(HTTPError):
                self.run_response(code, {}, b'{"message":"Resource not accessible"}')


if __name__ == '__main__':
    unittest.main()
