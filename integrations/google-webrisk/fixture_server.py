"""CI fixture only. Not copied to the service installation directory."""
import io
import json
from email.message import Message
from pathlib import Path
import sys
import urllib.parse

from adapter import GoogleLookup, Server


class Response(io.BytesIO):
    status = 200
    def __init__(self, body, status=200):
        super().__init__(body)
        self.status = status
        self.headers = Message()
        self.headers["Content-Type"] = "application/json"


class FakeGoogle:
    def open(self, request, timeout):
        target = urllib.parse.parse_qs(urllib.parse.urlsplit(request.full_url).query)["uri"][0]
        if target.endswith("/malware"):
            return Response(json.dumps({"threat": {"threatTypes": ["MALWARE"], "expireTime": "2099-01-01T00:00:00Z"}}).encode())
        if target.endswith("/quota"):
            return Response(b'{"error":{"code":429}}', 429)
        if target.endswith("/malformed"):
            return Response(b'{"threat":null}')
        return Response(b'{}')


if __name__ == "__main__":
    server = Server(("127.0.0.1", 0), GoogleLookup("fixture-key-not-a-real-credential", FakeGoogle()))
    Path(sys.argv[1]).write_text(str(server.server_port))
    server.serve_forever()
