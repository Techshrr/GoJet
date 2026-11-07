import io
import json
import socket
import threading
import unittest
import urllib.error
import urllib.request
from email.message import Message
from unittest.mock import patch

from adapter import GoogleLookup, NoRedirect, Server, THREATS, strict_json, valid_target

KEY = "test-key-never-a-real-credential"


class Response(io.BytesIO):
    def __init__(self, body, status=200, content_type="application/json"):
        super().__init__(body)
        self.status = status
        self.headers = Message()
        self.headers["Content-Type"] = content_type


class Transport:
    def __init__(self, body=b"{}", status=200, error=None, content_type="application/json"):
        self.body, self.status, self.error, self.content_type = body, status, error, content_type
        self.calls = []

    def open(self, request, timeout):
        self.calls.append((request, timeout))
        if self.error:
            raise self.error
        return Response(self.body, self.status, self.content_type)


class LookupTests(unittest.TestCase):
    def test_no_match_request_contract_and_secret_header(self):
        transport = Transport()
        status, data = GoogleLookup(KEY, transport).check("https://example.com/a?x=1&y=two")
        self.assertEqual((status, data["complete"], data["verdict"]), (200, True, "allow"))
        request, timeout = transport.calls[0]
        self.assertEqual(request.get_method(), "GET")
        self.assertEqual(request.get_header("X-goog-api-key"), KEY)
        query = urllib.parse.parse_qs(urllib.parse.urlsplit(request.full_url).query)
        self.assertEqual(query["uri"], ["https://example.com/a?x=1&y=two"])
        self.assertEqual(query["threatTypes"], list(THREATS))
        self.assertEqual(timeout, 8)
        self.assertNotIn(KEY, request.full_url + json.dumps(data))
        self.assertEqual(urllib.parse.urlsplit(request.full_url).netloc, "webrisk.googleapis.com")

    def test_threats_block(self):
        for threat in THREATS:
            with self.subTest(threat=threat):
                body = json.dumps({"threat": {"threatTypes": [threat], "expireTime": "2099-01-01T00:00:00Z"}}).encode()
                status, result = GoogleLookup(KEY, Transport(body)).check("https://example.com/")
                self.assertEqual((status, result["verdict"], result["complete"]), (200, "block", True))

    def test_bad_responses_never_allow(self):
        bodies = [b"", b"null", b"[]", b"true", b'{"error":{}}', b'{"threat":null}',
                  b'{"threat":{}}', b'{"threat":{"threatTypes":[]}}', b'{"extra":0}',
                  b'{"threat":{},"threat":{}}', b'{} garbage', b'{"x":NaN}', b'\xff',
                  b" " * 65537, b'[' * 2000]
        for kinds, expiry in [(["UNKNOWN"], "2099-01-01T00:00:00Z"), ([1], "2099-01-01T00:00:00Z"),
                              (["MALWARE"], "bad"), (["MALWARE"], "2000-01-01T00:00:00Z"),
                              (["MALWARE"], "2099-01-01"), (["MALWARE", "MALWARE"], "2099-01-01T00:00:00Z")]:
            bodies.append(json.dumps({"threat": {"threatTypes": kinds, "expireTime": expiry}}).encode())
        for body in bodies:
            with self.subTest(body=body[:50]):
                status, result = GoogleLookup(KEY, Transport(body)).check("https://example.com/")
                self.assertEqual((status, result["complete"], result["verdict"]), (503, False, "review"))

    def test_http_failures_and_secret_redaction(self):
        for status in [301, 302, 400, 401, 403, 429, 500, 503]:
            with self.subTest(status=status):
                error = urllib.error.HTTPError("https://google.invalid/?key=" + KEY, status, KEY, {}, io.BytesIO(KEY.encode()))
                code, data = GoogleLookup(KEY, Transport(error=error)).check("https://example.com/")
                self.assertEqual(code, 503)
                self.assertFalse(data["complete"])
                self.assertNotIn(KEY, json.dumps(data))
                self.assertEqual(data["evidence"]["upstream_status"], status)

    def test_network_failures(self):
        for error in [TimeoutError(KEY), socket.timeout(KEY), OSError(KEY), urllib.error.URLError(KEY)]:
            code, data = GoogleLookup(KEY, Transport(error=error)).check("https://example.com/")
            self.assertEqual(code, 503)
            self.assertNotIn(KEY, json.dumps(data))

    def test_wrong_mime_and_success_status(self):
        for transport in [Transport(status=204), Transport(content_type="text/html")]:
            code, data = GoogleLookup(KEY, transport).check("https://example.com/")
            self.assertEqual((code, data["verdict"]), (503, "review"))

    def test_no_redirect(self):
        self.assertIsNone(NoRedirect().redirect_request(None, None, 302, "", {}, "http://127.0.0.1/"))

    def test_reject_targets_before_upstream(self):
        for target in [None, 3, "", "https://a/" + "a" * 4096, "ftp://example.com", "http://127.0.0.1",
                       "http://[::1]", "http://169.254.169.254", "http://localhost", "http://a.localhost",
                       "https://u:p@example.com", "https://example.com:0", "https://example.com:65536",
                       "https://example.com/\n", "https://example.com\\evil", "https://example.com:bad"]:
            with self.subTest(target=str(target)[:60]):
                transport = Transport()
                code, data = GoogleLookup(KEY, transport).check(target)
                self.assertEqual((code, data["complete"], len(transport.calls)), (400, False, 0))

    def test_key_validation(self):
        for key in ["", "x", KEY + "\n", "a" * 257]:
            with self.assertRaises(ValueError):
                GoogleLookup(key)


class HTTPTests(unittest.TestCase):
    def setUp(self):
        self.transport = Transport()
        self.server = Server(("127.0.0.1", 0), GoogleLookup(KEY, self.transport))
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.url = "http://127.0.0.1:%d" % self.server.server_port
        self.client = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def request(self, body, path="/v1/check", content_type="application/json"):
        req = urllib.request.Request(self.url + path, data=body, headers={"Content-Type": content_type})
        try:
            response = self.client.open(req, timeout=3)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.loads(response.read())

    def test_real_http_roundtrip(self):
        status, data = self.request(b'{"target":"https://example.com/"}')
        self.assertEqual((status, data["complete"], data["verdict"]), (200, True, "allow"))
        self.assertEqual(len(self.transport.calls), 1)

    def test_bad_requests_do_not_spend_quota(self):
        for raw in [b"null", b"[]", b"{}", b"{", b"{} " * 3000,
                    b'{"target":"https://example.com","key":"ignored"}',
                    b'{"target":"https://example.com","target":"https://example.org"}']:
            self.assertEqual(self.request(raw)[0], 400)
        self.assertEqual(self.request(b'{}', content_type="text/plain")[0], 400)
        self.assertEqual(self.request(b'{}', path="/other")[0], 404)
        self.assertEqual(len(self.transport.calls), 0)

    def test_health_not_upstream_evidence(self):
        with self.client.open(self.url + "/healthz", timeout=3) as response:
            self.assertFalse(json.loads(response.read())["upstream_verified"])
        self.assertEqual(len(self.transport.calls), 0)

    def test_upstream_failure_over_http(self):
        self.transport.error = TimeoutError("sensitive-target")
        status, data = self.request(b'{"target":"https://example.com/"}')
        self.assertEqual((status, data["verdict"], data["complete"]), (503, "review", False))

    def test_duplicate_length_and_chunking_rejected(self):
        for headers in [b"Content-Length: 2\r\nContent-Length: 2\r\n", b"Transfer-Encoding: chunked\r\n"]:
            with socket.create_connection(self.server.server_address, timeout=3) as sock:
                sock.sendall(b"POST /v1/check HTTP/1.0\r\nContent-Type: application/json\r\n" + headers + b"\r\n{}")
                self.assertIn(b" 400 ", sock.recv(1024))
        self.assertEqual(len(self.transport.calls), 0)

    def test_capacity_is_bounded(self):
        for _ in range(8):
            self.assertTrue(self.server.slots.acquire(False))
        try:
            with socket.create_connection(self.server.server_address, timeout=3) as sock:
                self.assertEqual(sock.recv(1), b"")
        finally:
            for _ in range(8):
                self.server.slots.release()


if __name__ == "__main__":
    unittest.main(verbosity=2)
