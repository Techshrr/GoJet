#!/usr/bin/env python3
"""Google Lookup API -> GoJet semantic provider, Python 3.12 stdlib only.

No target fetching, query-string credentials, request logging, redirects or proxy
inheritance. HTTP is bound to loopback; the supplied Nginx config owns TLS/ACLs.
"""
import ipaddress
import http.client
import functools
import json
import os
import re
import socket
import ssl
import threading
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

VERSION = "1.0.0"
API = "https://webrisk.googleapis.com/v1/uris:search"
THREATS = ("MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE")
BODY_LIMIT = 8192
RESPONSE_LIMIT = 65536


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class IPv4HTTPS(urllib.request.HTTPSHandler):
    def https_open(self, request):
        # Binding an IPv4 source makes create_connection skip IPv6 candidates.
        # This matches the explicitly IPv4-restricted Google key on the host.
        connection = functools.partial(http.client.HTTPSConnection, source_address=("0.0.0.0", 0))
        return self.do_open(connection, request, context=self._context)


def opener():
    return urllib.request.build_opener(
        urllib.request.ProxyHandler({}), NoRedirect(),
        IPv4HTTPS(context=ssl.create_default_context()))


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("duplicate key")
            result[key] = value
        return result
    def invalid_constant(_):
        raise ValueError("invalid constant")
    return json.loads(raw, object_pairs_hook=pairs, parse_constant=invalid_constant)


def valid_target(target):
    if not isinstance(target, str) or not 1 <= len(target.encode("utf-8")) <= 4096:
        return False
    if any(ord(c) < 33 or ord(c) == 127 for c in target) or "\\" in target:
        return False
    try:
        u = urllib.parse.urlsplit(target)
        if u.scheme not in ("http", "https") or not u.hostname or u.username is not None or u.password is not None:
            return False
        if u.port is not None and not 1 <= u.port <= 65535:
            return False
        host = u.hostname.lower().rstrip(".")
        if host == "localhost" or host.endswith(".localhost"):
            return False
        try:
            if not ipaddress.ip_address(host).is_global:
                return False
        except ValueError:
            if not re.fullmatch(r"[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?", host):
                return False
        return True
    except (ValueError, UnicodeError):
        return False


def reply(verdict, signal, complete=True, **evidence):
    return {"complete": complete, "verdict": verdict, "signal_code": signal,
            "evidence": {"provider": "google-web-risk", "adapter_version": VERSION, **evidence}}


def unavailable(category, upstream_status=None):
    evidence = {"failure_category": category}
    if upstream_status is not None:
        evidence["upstream_status"] = upstream_status
    # Non-2xx maps to ProviderUnavailable in the immutable GoJet client.
    return 503, reply("review", "google-webrisk-unavailable", complete=False, **evidence)


class GoogleLookup:
    def __init__(self, key, transport=None):
        if not re.fullmatch(r"[A-Za-z0-9_-]{20,256}", key):
            raise ValueError("invalid API key format")
        self.key = key
        self.transport = transport if transport is not None else opener()

    def check(self, target):
        if not valid_target(target):
            return 400, reply("review", "invalid-target", complete=False)
        query = urllib.parse.urlencode([("uri", target)] + [("threatTypes", t) for t in THREATS])
        request = urllib.request.Request(API + "?" + query, headers={
            "X-Goog-Api-Key": self.key, "Accept": "application/json",
            "User-Agent": "GoJet-WebRisk/" + VERSION}, method="GET")
        try:
            with self.transport.open(request, timeout=8) as response:
                if response.status != 200:
                    return unavailable("http-status", response.status)
                if response.headers.get_content_type() != "application/json":
                    return unavailable("content-type")
                raw = response.read(RESPONSE_LIMIT + 1)
                if len(raw) > RESPONSE_LIMIT:
                    return unavailable("response-too-large")
            data = strict_json(raw)
        except urllib.error.HTTPError as error:
            # Never expose Google's error text, request URL or supplied key.
            status = error.code
            error.close()
            return unavailable("http-status", status)
        except (urllib.error.URLError, TimeoutError, socket.timeout, ssl.SSLError, OSError, http.client.HTTPException):
            return unavailable("transport")
        except (ValueError, UnicodeError, RecursionError):
            return unavailable("malformed-response")
        if data == {}:
            return 200, reply("allow", "google-webrisk-no-match", threat_types=[])
        if not isinstance(data, dict) or set(data) != {"threat"} or not isinstance(data["threat"], dict):
            return unavailable("malformed-response")
        threat = data["threat"]
        kinds, expiry = threat.get("threatTypes"), threat.get("expireTime")
        if (set(threat) != {"threatTypes", "expireTime"} or not isinstance(kinds, list)
                or not kinds or len(kinds) > len(THREATS)
                or any(not isinstance(t, str) or t not in THREATS for t in kinds)
                or len(set(kinds)) != len(kinds) or not isinstance(expiry, str)):
            return unavailable("malformed-response")
        try:
            expires = datetime.fromisoformat(expiry.replace("Z", "+00:00"))
            if expires.utcoffset() is None or expires <= datetime.now(timezone.utc):
                return unavailable("expired-response")
        except (ValueError, OverflowError):
            return unavailable("malformed-response")
        return 200, reply("block", "google-webrisk-threat", threat_types=sorted(kinds), expire_time=expiry)


class Handler(BaseHTTPRequestHandler):
    server_version = "GoJet-WebRisk"
    sys_version = ""
    protocol_version = "HTTP/1.0"

    def setup(self):
        self.request.settimeout(12)
        super().setup()

    def log_message(self, *args):
        pass  # Request targets and exceptions must not enter journal logs.

    def respond(self, status, payload):
        raw = json.dumps(payload, separators=(",", ":")).encode()
        try:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.send_header("Cache-Control", "no-store")
            self.send_header("Connection", "close")
            self.end_headers()
            self.wfile.write(raw)
        except (OSError, TimeoutError):
            pass

    def do_GET(self):
        if self.path == "/healthz":
            self.respond(200, {"adapter_ready": True, "upstream_verified": False, "version": VERSION})
        else:
            self.respond(404, {"error": "not-found"})

    def do_POST(self):
        if self.path != "/v1/check":
            self.respond(404, {"error": "not-found"})
            return
        lengths = self.headers.get_all("Content-Length", [])
        if (self.headers.get("Transfer-Encoding") is not None or len(lengths) != 1
                or not lengths[0].isdigit() or not 1 <= int(lengths[0]) <= BODY_LIMIT
                or self.headers.get_content_type() != "application/json"):
            self.respond(400, reply("review", "invalid-request", complete=False))
            return
        try:
            raw = self.rfile.read(int(lengths[0]))
            if len(raw) != int(lengths[0]):
                raise ValueError("partial request")
            data = strict_json(raw)
            if not isinstance(data, dict) or set(data) != {"target"}:
                raise ValueError("invalid fields")
            status, payload = self.server.lookup.check(data["target"])
            self.respond(status, payload)
        except (ValueError, UnicodeError, RecursionError):
            self.respond(400, reply("review", "invalid-request", complete=False))
        except (OSError, TimeoutError):
            self.respond(408, reply("review", "request-timeout", complete=False))


class Server(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 8

    def __init__(self, address, lookup):
        self.lookup = lookup
        self.slots = threading.BoundedSemaphore(8)
        super().__init__(address, Handler)

    def process_request(self, request, address):
        if not self.slots.acquire(blocking=False):
            self.shutdown_request(request)
            return
        try:
            super().process_request(request, address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request, address):
        try:
            super().process_request_thread(request, address)
        finally:
            self.slots.release()

    def handle_error(self, request, client_address):
        pass  # Do not emit exception reprs that may contain a target or credential.


def main():
    credentials = os.environ.get("CREDENTIALS_DIRECTORY")
    if not credentials:
        raise ValueError("systemd credential required")
    key = (Path(credentials) / "api-key").read_text().strip()
    Server(("127.0.0.1", 9089), GoogleLookup(key)).serve_forever(poll_interval=0.5)


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Web Risk adapter stopped; inspect service and credential configuration.") from None
