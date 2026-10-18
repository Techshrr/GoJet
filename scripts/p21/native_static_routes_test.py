"""Read the actual verified archive and request its compiled pages/assets through Nginx."""
import json
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
        config = base/'nginx.conf'
        config.write_text('user www-data;\npid '+str(base/'nginx.pid')+';\n'
                          'error_log '+str(base/'error.log')+';\nevents {}\nhttp {\n'
                          'include /etc/nginx/mime.types;\naccess_log off;\nserver {\n'
                          f'listen 127.0.0.1:{port};\n'+fragment+'\nlocation / { return 404; }\n}\n}\n')
        checked = subprocess.run(['nginx', '-t', '-c', str(config)], capture_output=True, text=True)
        assert checked.returncode == 0, checked.stderr
        process = subprocess.Popen(['nginx', '-c', str(config), '-g', 'daemon off;'],
                                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        def request(path):
            try:
                response = urllib.request.urlopen(f'http://127.0.0.1:{port}'+path, timeout=5)
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
                         '/manifest.json', '/.env']:
                assert request(path)[0] == 404, path
        finally:
            process.terminate()
            process.wait(timeout=10)
    print(json.dumps(dict(source=source, actual_compiled_archive=True, four_static_apps=True,
                         exact_asset_bytes=True, assets_checked=len(assets), private_shell_headers=True,
                         inherited_docs_statuses=True, private_paths_not_served=True,
                         full_product_proxy=False, production_deployment=False)))


if __name__ == '__main__':
    main()
