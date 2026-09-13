"""External test infrastructure only; run behind a public HTTPS reverse proxy."""
import hashlib
import hmac
import json
import os
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Receiver:
    def __init__(self, token):
        if len(token) < 32:
            raise ValueError('receiver control token must have at least 32 characters')
        self.token = token
        self.runs = {}
        self.lock = threading.Lock()

    def request(self, method, path, headers, body):
        with self.lock:
            now = time.time()
            self.runs = {k: v for k, v in self.runs.items() if v['expires'] > now}
            match = re.fullmatch(r'/(runs|deliver)/([a-zA-Z0-9_-]{8,80})', path)
            if not match:
                return 404, {}
            surface, run_id = match.groups()
            if surface == 'runs':
                if not hmac.compare_digest(headers.get('Authorization', ''), 'Bearer ' + self.token):
                    return 401, {}
                run = self.runs.get(run_id)
                if method == 'POST':
                    if run is not None or len(self.runs) >= 16:
                        return 409, {}
                    data = json.loads(body)
                    if (not isinstance(data.get('secret'), str) or not 16 <= len(data['secret']) <= 256
                            or not isinstance(data.get('workspace_id'), str)
                            or not 1 <= len(data['workspace_id']) <= 64
                            or type(data.get('fail_first', 1)) is not int
                            or not 0 <= data.get('fail_first', 1) <= 2):
                        return 422, {}
                    self.runs[run_id] = {'secret': data['secret'], 'workspace': data['workspace_id'],
                                         'fail_first': data.get('fail_first', 1), 'expires': now + 1800,
                                         'deliveries': {}, 'invalid': 0}
                    return 201, {'configured': True}
                if run is None:
                    return 404, {}
                if method == 'DELETE':
                    del self.runs[run_id]
                    return 200, {'deleted': True}
                if method == 'PATCH':
                    data = json.loads(body)
                    if not isinstance(data.get('secret'), str) or not 16 <= len(data['secret']) <= 256:
                        return 422, {}
                    run['secret'] = data['secret']
                    return 200, {'rotated': True}
                if method == 'GET':
                    return 200, {'invalid_count': run['invalid'], 'delivery_count': len(run['deliveries']),
                                 'accepted_count': sum(x['accepted'] for x in run['deliveries'].values()),
                                 'attempt_count': sum(x['attempts'] for x in run['deliveries'].values()),
                                 'deliveries': [{'delivery_id': k, 'attempts': v['attempts'],
                                                 'accepted': v['accepted'], 'body_sha256': v['digest']}
                                                for k, v in run['deliveries'].items()]}
                return 405, {}
            if method != 'POST':
                return 405, {}
            run = self.runs.get(run_id)
            if run is None:
                return 404, {}
            delivery = headers.get('X-GoJet-Delivery', '')
            timestamp = headers.get('X-GoJet-Timestamp', '')
            if (not re.fullmatch(r'whd_[A-Za-z0-9_-]{1,60}', delivery)
                    or not timestamp.isdigit() or len(timestamp) > 12
                    or abs(now - int(timestamp)) > 300
                    or headers.get('X-GoJet-Idempotency-Key') != delivery):
                run['invalid'] += 1
                return 401, {}
            message = (delivery + '\n' + timestamp + '\n').encode() + body
            expected = 'v1=' + hmac.new(run['secret'].encode(), message, hashlib.sha256).hexdigest()
            if not hmac.compare_digest(expected, headers.get('X-GoJet-Signature', '')):
                run['invalid'] += 1
                return 401, {}
            envelope = json.loads(body)
            if (envelope.get('workspace_id') != run['workspace']
                    or envelope.get('type') != headers.get('X-GoJet-Event')):
                return 422, {}
            digest = hashlib.sha256(body).hexdigest()
            entry = run['deliveries'].get(delivery)
            if entry is None:
                if len(run['deliveries']) >= 16:
                    return 429, {}
                entry = {'digest': digest, 'attempts': 0, 'accepted': False}
                run['deliveries'][delivery] = entry
            if entry['digest'] != digest:
                return 409, {}
            entry['attempts'] += 1
            if entry['attempts'] <= run['fail_first']:
                return 503, {'retry': True}
            entry['accepted'] = True
            return 200, {'accepted': True}


def handler(receiver):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def handle_request(self):
            self.connection.settimeout(10)
            try:
                length = int(self.headers.get('Content-Length', '0'))
                if length < 0 or length > 256 * 1024 or self.headers.get('Transfer-Encoding'):
                    status, data = 413, {}
                else:
                    status, data = receiver.request(self.command, self.path, self.headers,
                                                    self.rfile.read(length))
            except (ValueError, TypeError, AttributeError):
                status, data = 400, {}
            except (TimeoutError, OSError):
                return
            payload = json.dumps(data).encode()
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(payload)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('X-Content-Type-Options', 'nosniff')
            self.end_headers()
            self.wfile.write(payload)

        do_GET = do_POST = do_PATCH = do_DELETE = handle_request
    return Handler


if __name__ == '__main__':
    receiver = Receiver(os.environ['P20_RECEIVER_CONTROL_TOKEN'])
    ThreadingHTTPServer(('127.0.0.1', int(os.environ.get('P20_RECEIVER_PORT', '18884'))),
                        handler(receiver)).serve_forever()
