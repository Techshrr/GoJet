"""Real PHP HTTP entrypoint tests; TLS server-variable mapping is a test fixture, not a TLS claim."""
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
import unittest
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
PHP = os.environ.get('PHP_BIN', 'php8.3')
ROUTES = ['', '/', '/environment', '/data', '/site', '/admin', '/services', '/health', '/complete']


@unittest.skipUnless(shutil.which(PHP), 'Native PHP 8.3 required')
class InstallerHTTP(unittest.TestCase):
    def test_public_entrypoint_boundary(self):
        with tempfile.TemporaryDirectory(prefix='gojet-http-') as temp:
            base = Path(temp)
            state = base / 'private'
            state.mkdir(mode=0o700)
            token = secrets.token_hex(32)
            env = os.environ | {'GOJET_INSTALL_STATE_DIR': str(state),
                'GOJET_INSTALL_BOOTSTRAP_SHA256': hashlib.sha256(token.encode()).hexdigest(),
                'GOJET_INSTALL_ORIGIN': 'https://installer.test', 'P21_TEST_TLS': 'on'}
            init = "require " + json.dumps(str(ROOT / 'installer/src/InstallState.php')) + ";"
            init += "(new GoJet\\Installer\\InstallState(getenv('GOJET_INSTALL_STATE_DIR'),getenv('GOJET_INSTALL_BOOTSTRAP_SHA256')))->initialize();"
            subprocess.run([PHP, '-r', init], env=env, check=True, capture_output=True)
            # Only this unshipped test router maps a simulated server TLS variable.
            # Production trusts HTTPS set by its Nginx/FPM configuration, not headers.
            router = base / 'router.php'
            router.write_text("<?php $_SERVER['HTTPS']=getenv('P21_TEST_TLS'); require " +
                              json.dumps(str(ROOT / 'installer/public/index.php')) + ';')
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', 0))
                port = sock.getsockname()[1]
            def request(path='/install/', data=None, headers=None, method=None):
                h = {'Host': 'installer.test', 'Origin': 'https://installer.test', 'Content-Type': 'application/json'}
                h.update(headers or {})
                req = urllib.request.Request(f'http://127.0.0.1:{port}'+path, data=data, headers=h, method=method)
                try:
                    response = urllib.request.urlopen(req, timeout=5)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    return response.status, response.headers, response.read().decode()
            process = subprocess.Popen([PHP, '-S', f'127.0.0.1:{port}', str(router)], env=env,
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                for _ in range(100):
                    try:
                        with socket.create_connection(('127.0.0.1', port), timeout=.1):
                            break
                    except OSError:
                        if process.poll() is not None:
                            self.fail('PHP HTTP server exited')
                        time.sleep(.05)
                for suffix in ROUTES:
                    self.assertEqual(request('/install'+suffix)[0], 401)
                payload = json.dumps({'bootstrap_token': token}).encode()
                self.assertEqual(request(data=payload, headers={'Origin': 'https://evil.test'})[0], 403)
                self.assertEqual(request(data=payload, headers={'Host': 'evil.test'})[0], 400)
                self.assertEqual(request(data=b'x'*4097)[0], 413)
                self.assertEqual(request(data=payload, headers={'Content-Type': 'text/plain'})[0], 415)
                self.assertEqual(request(data=b'{')[0], 400)
                self.assertEqual(request(method='PUT', data=payload)[0], 405)
                self.assertEqual(request('/install/?bootstrap_token=forbidden')[0], 404)
                status, headers, body = request(data=payload)
                self.assertEqual(status, 200)
                cookie = headers['Set-Cookie']
                for part in ['Path=/install;', 'Secure;', 'HttpOnly;', 'SameSite=Strict']:
                    self.assertIn(part, cookie)
                auth = json.loads(body)
                self.assertNotIn('session', auth)
                self.assertNotIn(token, body)
                self.assertEqual(len(auth['csrf']), 64)
                cookie_header = {'Cookie': cookie.split(';')[0]}
                for route in ['/install/', '/install/environment']:
                    status, headers, body = request(route, headers=cookie_header)
                    self.assertEqual(status, 200)
                    self.assertIn('data-shell="installer"', body)
                    self.assertEqual(headers['Cache-Control'], 'no-store')
                    self.assertEqual(headers['X-Robots-Tag'], 'noindex, nofollow')
                for route in ['/install/data', '/install/admin', '/install/complete']:
                    self.assertEqual(request(route, headers=cookie_header)[0], 409)
                self.assertEqual(request('/install/environment', data=b'{"success":true}', headers=cookie_header)[0], 503)
                self.assertEqual(json.loads((state/'state.json').read_text())['step'], 0)
                # A new authenticated session invalidates the old cookie.
                self.assertEqual(request(data=payload)[0], 200)
                self.assertEqual(request(headers=cookie_header)[0], 401)
                for _ in range(8):
                    self.assertEqual(request(data=b'{"bootstrap_token":"wrong"}')[0], 401)
                self.assertEqual(request(data=payload)[0], 429)
                durable = (state/'state.json').read_text()
                self.assertNotIn(token, durable)
                self.assertNotIn(auth['csrf'], durable)
                (state/'installed.lock').touch()
                for suffix in ROUTES:
                    self.assertEqual(request('/install'+suffix, headers=cookie_header)[0], 404)
                self.assertEqual(request(data=payload)[0], 404)
            finally:
                process.terminate()
                process.wait(timeout=5)
            # Forwarded headers cannot substitute for the trusted server TLS variable.
            env['P21_TEST_TLS'] = 'off'
            (state/'installed.lock').unlink()
            process = subprocess.Popen([PHP, '-S', f'127.0.0.1:{port}', str(router)], env=env,
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                for _ in range(100):
                    try:
                        result = request(headers={'X-Forwarded-Proto': 'https'})
                        break
                    except OSError:
                        time.sleep(.05)
                self.assertEqual(result[0], 400)
            finally:
                process.terminate()
                process.wait(timeout=5)


if __name__ == '__main__':
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(InstallerHTTP)
    result = unittest.TextTestRunner().run(suite)
    if result.skipped or not result.wasSuccessful():
        raise SystemExit(1)
    print(json.dumps({'real_php_http_entrypoint': True, 'authentication_cookie_rotation': True,
        'origin_and_size_enforced': True, 'route_order_enforced': True, 'bootstrap_rate_limit': True,
        'post_lock_404': True, 'forwarded_tls_spoof_rejected': True,
        'real_tls_and_browser_verified': False, 'privileged_apply_verified': False}))
