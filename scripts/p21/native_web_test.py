"""Exercise rendered native Nginx + PHP-FPM with certificate-verified TLS; no product apply claim."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
PHP = 'php8.3'


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, **kwargs)
    if result.returncode:
        raise RuntimeError('Native command failed: '+args[0]+'; '+result.stderr[-2000:])
    return result


def main():
    if os.geteuid() != 0:
        raise RuntimeError('Native isolated fixture needs root to start an unprivileged FPM pool')
    account = pwd.getpwnam('gojet-installer')
    with tempfile.TemporaryDirectory(prefix='gojet-native-web-') as tmp:
        base = Path(tmp)
        base.chmod(0o755)
        release = base/'release'
        shutil.copytree(ROOT/'installer', release/'installer')
        css = release/'frontend/packages/tokens/generated'
        css.mkdir(parents=True)
        shutil.copy2(ROOT/'frontend/packages/tokens/generated/tokens.css', css/'tokens.css')
        private = base/'private'
        private.mkdir(mode=0o700)
        os.chown(private, account.pw_uid, account.pw_gid)
        authority = base/'authority'
        authority.mkdir(mode=0o755)
        completion = authority/'installed.lock'
        runtime = base/'runtime'
        runtime.mkdir(mode=0o755)
        cert, key = base/'test-cert.pem', base/'test-key.pem'
        command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                 '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1',
                 '-keyout', str(key), '-out', str(cert)])
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        origin = f'https://127.0.0.1:{port}'
        token = secrets.token_hex(32)
        settings = dict(origin=origin, release=str(release), state=str(private),
                        bootstrap_sha256=hashlib.sha256(token.encode()).hexdigest(),
                        certificate=str(cert), private_key=str(key), socket=str(runtime/'fpm.sock'), completion=str(completion))
        render = 'require '+json.dumps(str(release/'installer/src/NativeWebConfig.php'))+';'
        render += 'echo json_encode(GoJet\\Installer\\NativeWebConfig::render(json_decode(stream_get_contents(STDIN),true)),JSON_THROW_ON_ERROR);'
        configs = json.loads(command([PHP, '-r', render], input=json.dumps(settings)).stdout)
        # Reject directive injection, relative/traversal paths and public state before any file writes.
        for field, value in [('origin', 'https://localhost/evil'), ('release', '/opt/gojet;evil'),
                             ('state', str(release)+'/private'), ('socket', '/tmp/../socket'),
                             ('bootstrap_sha256', 'invalid'), ('completion', str(private/'installed.lock'))]:
            bad = subprocess.run([PHP, '-r', render], input=json.dumps(settings | {field: value}),
                                 capture_output=True, text=True)
            assert bad.returncode != 0, field
        init = 'require '+json.dumps(str(release/'installer/src/InstallState.php'))+';'
        init += '(new GoJet\\Installer\\InstallState(getenv("GOJET_INSTALL_STATE_DIR"),getenv("GOJET_INSTALL_BOOTSTRAP_SHA256")))->initialize();'
        command(['runuser', '-u', 'gojet-installer', '--', PHP, '-r', init], env=os.environ | {
            'GOJET_INSTALL_STATE_DIR': str(private), 'GOJET_INSTALL_BOOTSTRAP_SHA256': settings['bootstrap_sha256']})
        fpm = base/'fpm.conf'
        fpm.write_text('[global]\npid = '+str(runtime/'fpm.pid')+'\nerror_log = '+str(base/'fpm.log')+'\n'+configs['fpm'])
        nginx = base/'nginx.conf'
        nginx.write_text('user www-data;\npid '+str(runtime/'nginx.pid')+';\nerror_log '+str(base/'nginx.log')+';\nevents {}\nhttp { access_log off;\n'+configs['nginx']+'}\n')
        command(['php-fpm8.3', '-t', '-y', str(fpm)])
        command(['nginx', '-t', '-c', str(nginx), '-p', str(base)])
        context = ssl.create_default_context(cafile=str(cert))
        def request(path='/install/', data=None, headers=None):
            h = {'Origin': origin, 'Content-Type': 'application/json'}
            h.update(headers or {})
            req = urllib.request.Request(origin+path, data=data, headers=h)
            try:
                response = urllib.request.urlopen(req, context=context, timeout=5)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                return response.status, response.headers, response.read().decode()
        processes = []
        try:
            processes.append(subprocess.Popen(['php-fpm8.3', '-F', '-y', str(fpm)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
            processes.append(subprocess.Popen(['nginx', '-c', str(nginx), '-p', str(base), '-g', 'daemon off;'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
            for _ in range(100):
                try:
                    response = request()
                    if response[0] == 401:
                        break
                except OSError:
                    pass
                if any(p.poll() is not None for p in processes):
                    raise RuntimeError('Native web process exited')
                time.sleep(.1)
            else:
                raise RuntimeError('Native TLS entrypoint did not become ready')
            payload = json.dumps({'bootstrap_token': token}).encode()
            status, headers, body = request(data=payload)
            assert status == 200, (status, body)
            cookie = headers['Set-Cookie'].split(';')[0]
            for flag in ['Secure', 'HttpOnly', 'SameSite=Strict', 'Path=/install']:
                assert flag in headers['Set-Cookie'], flag
            status, headers, body = request('/install/environment', headers={'Cookie': cookie})
            assert status == 200 and 'data-shell="installer"' in body
            assert request('/install/complete', headers={'Cookie': cookie})[0] == 409
            assert request(data=payload, headers={'Origin': 'https://evil.test'})[0] == 403
            assert request('/install/index.php')[0] == 404
            assert request('/installer/src/InstallState.php')[0] == 404
            assert request('/private/state.json')[0] == 404
            assert request('/install/environment', data=b'{"success":true}', headers={'Cookie': cookie})[0] == 503
            assert request(data=b'x'*5000)[0] == 413
            large_draft_request = json.dumps({'csrf': '0'*5000, 'draft': {}}).encode()
            assert request('/install/configuration', data=large_draft_request, headers={'Cookie': cookie})[0] == 403
            assert request('/install/configuration', data=b'x'*32769, headers={'Cookie': cookie})[0] == 413
            assert request('/install/configuration/approve', data=large_draft_request, headers={'Cookie': cookie})[0] == 413
            assert not (private/'configuration-draft.json').exists()
            assert json.loads((private/'state.json').read_text())['step'] == 0
            assert (private/'state.json').stat().st_uid == account.pw_uid
            # Verify the actual FPM worker is unprivileged, not merely a rendered pool declaration.
            workers = command(['ps', '-eo', 'user=,args=']).stdout.splitlines()
            assert any(line.strip().startswith('gojet-i') and 'php-fpm: pool gojet-installer' in line for line in workers)
            browser = command([os.environ['P21_NODE'], str(ROOT/'scripts/p21/installer_browser_test.mjs')],
                input=json.dumps(dict(origin=origin, state=str(private), completion=str(completion), token=token,
                    uid=account.pw_uid, gid=account.pw_gid)), timeout=150)
            browser_evidence = json.loads(browser.stdout)
            assert browser_evidence['desktop_mobile_browser'] is True
            (ROOT/'artifacts/v10/P21/native-web/browser.json').write_text(json.dumps(browser_evidence)+'\n')
            completion.touch(mode=0o644)
            # Even code executing as the FPM service account cannot remove or
            # replace root's final authority, or mutate its containing directory.
            for command_line in [['rm', '-f', str(completion)],
                                 ['touch', str(authority/'replacement')],
                                 ['mv', str(authority), str(base/'moved-authority')]]:
                denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+command_line,
                                        capture_output=True)
                assert denied.returncode != 0, 'FPM account changed completion authority'
            # Resetting all writable PHP state still cannot reopen the installation.
            for file in private.iterdir():
                file.unlink()
            for route in ['', '/', '/environment', '/data', '/site', '/admin', '/services', '/health', '/complete', '/configuration', '/configuration/approve']:
                assert request('/install'+route, headers={'Cookie': cookie})[0] == 404
            assert request(data=payload)[0] == 404
        finally:
            for p in reversed(processes):
                p.terminate()
            for p in reversed(processes):
                p.wait(timeout=10)
        # Do not export configs, keys, session cookies or raw request/response bodies.
        print(json.dumps({'native_nginx_fpm': True, 'certificate_verified_tls': True,
            'render_injection_rejected': True, 'dedicated_unprivileged_fpm': True,
            'private_files_not_served': True, 'root_completion_cannot_be_removed': True,
            'writable_state_reset_cannot_reopen': True, 'request_boundaries': True, 'post_lock_404': True,
            'full_product_vhost_verified': False, 'privileged_apply_verified': False,
            'approved_plan_browser_verified': True, 'full_interactive_installer_verified': False}))


if __name__ == '__main__':
    main()
