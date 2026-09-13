"""Vercel entrypoint; shared receiver protocol with Redis compare-and-set state."""
import hmac
import json
import os
import urllib.request
from urllib.parse import urlsplit, parse_qs
from http.server import BaseHTTPRequestHandler

from webhook_receiver import Receiver


CAS = """
local current = redis.call('GET', KEYS[1]) or ''
if current ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'EX', 1800)
return 1
"""


def redis_command(command):
    url = os.environ['UPSTASH_REDIS_REST_URL'].rstrip('/')
    if not url.startswith('https://'):
        raise ValueError('HTTPS storage required')
    request = urllib.request.Request(url, data=json.dumps(command).encode(), method='POST',
                                     headers={'Authorization': 'Bearer ' + os.environ['UPSTASH_REDIS_REST_TOKEN'],
                                              'Content-Type': 'application/json'})
    with urllib.request.urlopen(request, timeout=5) as response:
        result = json.load(response)
    if 'error' in result:
        raise ValueError('state command failed')
    return result['result']


def dispatch(method, path, headers, body, command=redis_command):
    required = ('P20_RECEIVER_CONTROL_TOKEN', 'UPSTASH_REDIS_REST_URL', 'UPSTASH_REDIS_REST_TOKEN')
    configured = all(os.environ.get(name) for name in required)
    if path == '/healthz' and method == 'GET':
        return (200 if configured else 503), {'configured': configured, 'formal_p20_t024_claim': False}
    if not configured:
        return 503, {'error': 'receiver_not_configured'}
    token = os.environ['P20_RECEIVER_CONTROL_TOKEN']
    if path.startswith('/runs/') and not hmac.compare_digest(headers.get('Authorization', ''), 'Bearer ' + token):
        return 401, {}
    key = 'gojet:p20:receiver:v1'
    for _ in range(5):
        previous = command(['GET', key]) or ''
        receiver = Receiver(token)
        receiver.runs = json.loads(previous) if previous else {}
        status, result = receiver.request(method, path, headers, body)
        current = json.dumps(receiver.runs, separators=(',', ':'), sort_keys=True)
        if command(['EVAL', CAS, 1, key, previous, current]) == 1:
            return status, result
    return 503, {'error': 'state_contention'}


class handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def respond(self):
        try:
            length = int(self.headers.get('Content-Length', '0'))
            if length < 0 or length > 256 * 1024 or self.headers.get('Transfer-Encoding'):
                status, result = 413, {}
            else:
                parsed = urlsplit(self.path)
                paths = parse_qs(parsed.query).get("receiver_path", [parsed.path])
                if len(paths) != 1:
                    raise ValueError("ambiguous receiver path")
                status, result = dispatch(self.command, paths[0], self.headers, self.rfile.read(length))
        except (ValueError, TypeError, AttributeError):
            status, result = 400, {}
        except Exception:
            # Dependency details may contain credentials; never return or log them.
            status, result = 503, {'error': 'receiver_dependency_unavailable'}
        payload = json.dumps(result).encode()
        self.send_response(status)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(payload)))
        self.send_header('Cache-Control', 'no-store')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.end_headers()
        self.wfile.write(payload)

    do_GET = do_POST = do_PATCH = do_DELETE = respond
