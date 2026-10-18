"""Read the actual verified archive and request its compiled pages/assets through Nginx."""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
import os
from pathlib import Path
import re
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
from archive import digest, verify_archive
from static_routes import PREFIX


def main():
    archive = Path(sys.argv[1]).resolve()
    source = sys.argv[2]
    verify_archive(archive, digest(archive.read_bytes()), source)
    assert os.geteuid() == 0
    with tempfile.TemporaryDirectory(prefix='gojet-static-') as tmp:
        base = Path(tmp)
        base.chmod(0o755)
        release = base/'release'
        release.mkdir()
        # verify_archive above has rejected links/devices/traversal/duplicates.
        with tarfile.open(archive, 'r:gz') as tar:
            tar.extractall(release, filter='data')
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        fragment = (release/'deploy/nginx/static-apps.conf').read_text().replace(PREFIX, str(release))
        # Transport probe only: this is not a replacement for real Go business authority.
        class Probe(BaseHTTPRequestHandler):
            def do_GET(self):
                body = self.rfile.read(int(self.headers.get('Content-Length', '0')))
                result = json.dumps(dict(path=self.path, method=self.command,
                    body=body.decode(), upstream_port=self.server.server_port, headers=dict(self.headers))).encode()
                self.send_response(418)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Set-Cookie', 'probe=retained; HttpOnly')
                self.send_header('Cache-Control', 'no-store')
                self.end_headers()
                self.wfile.write(result)
            do_POST = do_GET
            def log_message(self, *args):
                pass
        upstream = ThreadingHTTPServer(('127.0.0.1', 0), Probe)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        redirect_upstream = ThreadingHTTPServer(('127.0.0.1', 0), Probe)
        threading.Thread(target=redirect_upstream.serve_forever, daemon=True).start()
        redirect = (release/'deploy/nginx/redirect-proxy.conf').read_text()
        redirect = redirect.replace('127.0.0.1:8080', '127.0.0.1:'+str(redirect_upstream.server_port))
        proxy = (release/'deploy/nginx/platform-proxy.conf').read_text()
        proxy = proxy.replace('127.0.0.1:8081', '127.0.0.1:'+str(upstream.server_port))
        config = base/'nginx.conf'
        config.write_text('user www-data;\npid '+str(base/'nginx.pid')+';\n'
                          'error_log '+str(base/'error.log')+';\nevents {}\nhttp {\n'
                          'include /etc/nginx/mime.types;\naccess_log off;\nserver {\n'
                          f'listen 127.0.0.1:{port};\n'+proxy+'\n'+fragment+'\n'+redirect+'\n}\n}\n')
        checked = subprocess.run(['nginx', '-t', '-c', str(config)], capture_output=True, text=True)
        assert checked.returncode == 0, checked.stderr
        process = subprocess.Popen(['nginx', '-c', str(config), '-g', 'daemon off;'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        def request(path, data=None, headers=None):
            try:
                req = urllib.request.Request(f'http://127.0.0.1:{port}'+path, data=data, headers=headers or {})
                response = urllib.request.urlopen(req, timeout=5)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                return response.status, response.headers, response.read()
        try:
            for _ in range(100):
                try:
                    if request('/')[0] == 200:
                        break
                except OSError:
                    pass
                assert process.poll() is None, 'Nginx exited'
                time.sleep(.05)
            else:
                raise AssertionError('Static entry not ready')
            for url, app in [('/', 'site'), ('/app', 'workspace'), ('/app/billing', 'workspace'),
                             ('/admin', 'admin'), ('/admin/users', 'admin')]:
                status, headers, body = request(url)
                assert status == 200 and body == (release/'static'/app/'index.html').read_bytes(), url
                if app != 'site':
                    assert headers['Cache-Control'] == 'no-store' and 'noindex' in headers['X-Robots-Tag']
            for route in ['login', 'register', 'verify-email', 'forgot-password', 'reset-password',
                          'social-registration', 'oauth/google/callback', 'linkunavailable', 'abuse/report']:
                status, headers, body = request('/'+route+'?state=opaque')
                assert status == 200 and body == (release/'static/site/index.html').read_bytes()
                assert headers['Cache-Control'] == 'no-store' and 'noindex' in headers['X-Robots-Tag']
            for route in ['/api/auth/login', '/api/public/auth/google/callback', '/f/sample', '/t/sample', '/p/sample', '/short-code']:
                for data in [None, b'payload-with-exact-bytes']:
                    path = route+'?opaque=a%2Fb&x=1&x=2'
                    status, headers, body = request(path, data, {'Cookie': 'session=opaque',
                        'Authorization': 'Bearer probe-only', 'X-Forwarded-For': '203.0.113.1',
                        'X-Real-IP': '203.0.113.2', 'X-Forwarded-Proto': 'https',
                        'X-Forwarded-Host': 'evil.test', 'X-Forwarded-Port': '9999',
                        'Forwarded': 'for=203.0.113.3;proto=https'})
                    assert status == 418 and headers['Set-Cookie'] == 'probe=retained; HttpOnly'
                    result = json.loads(body)
                    expected_port = redirect_upstream.server_port if route == '/short-code' else upstream.server_port
                    assert result['upstream_port'] == expected_port
                    assert result['path'] == path and result['body'] == (data or b'').decode()
                    assert result['method'] == ('POST' if data else 'GET')
                    h = {k.lower(): v for k, v in result['headers'].items()}
                    assert h['cookie'] == 'session=opaque' and h['authorization'] == 'Bearer probe-only'
                    assert h['x-forwarded-for'] == h['x-real-ip'] == '127.0.0.1'
                    assert h['x-forwarded-proto'] == 'http' and 'forwarded' not in h
                    assert h['x-forwarded-host'] == h['host'] == f'127.0.0.1:{port}'
                    assert h['x-forwarded-port'] == str(port)
            assets = re.findall(r'location = (/assets/[^ ]+) \{\n    alias ([^;]+);', fragment)
            assert assets, 'Missing compiled assets'
            for url, path in assets:
                status, headers, body = request(url)
                assert status == 200 and body == Path(path).read_bytes(), url
                if url.endswith('.js'):
                    assert 'javascript' in headers['Content-Type'], url
                if url.endswith('.css'):
                    assert 'text/css' in headers['Content-Type'], url
            docs = list((release/'static/docs').glob('*/index.html'))
            assert docs
            for page in docs:
                relative = page.relative_to(release/'static/docs').parent.as_posix()
                assert request('/docs/'+relative+'/')[2] == page.read_bytes()
            assert request('/docs/en/search')[0] == 400
            assert request('/docs/en/search?q=install')[0] == 200
            for path in ['/guides/legacy-deployment', '/docs/en/legacy-api']:
                assert request(path)[0] == 410
            for path in ['/assets/not-present.js', '/docs/not-present', '/installer/src/InstallState.php',
                         '/manifest.json', '/.env', '/healthz', '/too/many/segments']:
                assert request(path)[0] == 404, path
        finally:
            process.terminate()
            process.wait(timeout=10)
            upstream.shutdown()
            upstream.server_close()
            redirect_upstream.shutdown()
            redirect_upstream.server_close()
    print(json.dumps(dict(source=source, actual_compiled_archive=True, four_static_apps=True,
                         exact_asset_bytes=True, assets_checked=len(assets), private_shell_headers=True,
                         inherited_docs_statuses=True, private_paths_not_served=True,
                         auth_shell_routes=True, proxy_transport_verified=True, redirect_transport_verified=True,
                         forged_forwarding_headers_replaced=True, real_business_proxy_verified=False,
                         full_product_proxy=False, production_deployment=False)))


if __name__ == '__main__':
    main()
