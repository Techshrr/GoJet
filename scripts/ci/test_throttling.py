import io
import json
import unittest
from http.client import IncompleteRead
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


class TruncatedResponseTests(unittest.TestCase):
    class TruncatedBody(io.BytesIO):
        def read(self, *args):
            # Even a parseable prefix must never become accepted evidence.
            raise IncompleteRead(b'{"status":"PASS"}', 100)

    def test_retry_whole_request_and_discard_partial_body(self):
        responses = [self.TruncatedBody(), io.BytesIO(b'{"status":"FAIL"}')]
        requests, waits = [], []
        def opener(request, **kwargs):
            requests.append((request.full_url, request.get_method(), dict(request.header_items()), kwargs))
            return responses[len(requests) - 1]
        result = github_json('https://api.github.com/test?page=2', {'Authorization': 'Bearer test'},
                             opener=opener, sleep=waits.append, clock=lambda: 1000)
        self.assertEqual(result, {'status': 'FAIL'})
        self.assertEqual(waits, [1])
        self.assertEqual(requests[0], requests[1])
        self.assertEqual(requests[0][1], 'GET')
        self.assertTrue(all(response.closed for response in responses))

    def test_persistent_truncation_is_bounded_and_fails(self):
        responses, waits = [], []
        def opener(*args, **kwargs):
            response = self.TruncatedBody()
            responses.append(response)
            return response
        with self.assertRaises(IncompleteRead):
            github_json('https://api.github.com/test', {}, opener=opener,
                        sleep=waits.append, clock=lambda: 1000)
        self.assertEqual(len(responses), 3)
        self.assertEqual(waits, [1, 2])
        self.assertTrue(all(response.closed for response in responses))

    def test_truncation_obeys_shared_retry_deadline(self):
        waits, now = [], [1000]
        def sleep(delay):
            waits.append(delay)
            now[0] += delay
        with self.assertRaises(IncompleteRead):
            github_json('https://api.github.com/test', {}, opener=lambda *a, **kw: self.TruncatedBody(),
                        sleep=sleep, clock=lambda: now[0], max_wait=2)
        self.assertEqual(waits, [1])

    def test_malformed_complete_json_is_not_retried(self):
        calls, waits = [], []
        def opener(*args, **kwargs):
            calls.append(1)
            return io.BytesIO(b'{"status":')
        with self.assertRaises(json.JSONDecodeError):
            github_json('https://api.github.com/test', {}, opener=opener, sleep=waits.append)
        self.assertEqual(calls, [1])
        self.assertEqual(waits, [])


if __name__ == '__main__':
    unittest.main()
