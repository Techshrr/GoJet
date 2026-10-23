"""Real browser submission against shipped TLS/FPM; never edits installer state or mailbox."""
import hashlib
import json
import os
from pathlib import Path
import socket
import ssl
import subprocess
import time
import urllib.error
import urllib.request


def submit_browser(root, release, state, authority, token):
    repo = Path(__file__).resolve().parents[2]
    base = root/'browser-submit'
    base.mkdir(mode=0o755)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    origin = f'https://localhost:{port}'
    settings = dict(origin=origin, release=str(release), state=str(state),
        bootstrap_sha256=hashlib.sha256(token.encode()).hexdigest(),
        certificate=str(root/'certificate.pem'), private_key=str(root/'private.key'),
        socket=str(base/'fpm.sock'), completion=str(authority/'installed.lock'))
    render = 'require '+json.dumps(str(release/'installer/src/NativeWebConfig.php'))+';'
    render += 'echo json_encode(GoJet\\Installer\\NativeWebConfig::render(json_decode(stream_get_contents(STDIN),true)),JSON_THROW_ON_ERROR);'
    def command(args, **kwargs):
        result = subprocess.run(args, capture_output=True, text=True, timeout=120, **kwargs)
        assert result.returncode == 0, 'Native browser submission command failed'
        return result
    configs = json.loads(command(['php8.3', '-r', render], input=json.dumps(settings)).stdout)
    fpm, nginx = base/'fpm.conf', base/'nginx.conf'
    fpm.write_text('[global]\npid = '+str(base/'fpm.pid')+'\nerror_log = '+str(base/'fpm.log')+'\n'+configs['fpm'])
    nginx.write_text('user www-data;\npid '+str(base/'nginx.pid')+';\nerror_log '+str(base/'nginx.log')+';\nevents {}\nhttp { access_log off;\n'+configs['nginx']+'}\n')
    command(['php-fpm8.3', '-t', '-y', str(fpm)])
    command(['nginx', '-t', '-c', str(nginx), '-p', str(base)])
    processes = []
    try:
        processes.append(subprocess.Popen(['php-fpm8.3', '-F', '-y', str(fpm)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
        processes.append(subprocess.Popen(['nginx', '-c', str(nginx), '-p', str(base), '-g', 'daemon off;'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
        context = ssl.create_default_context(cafile=str(root/'certificate.pem'))
        for _ in range(100):
            try:
                with urllib.request.urlopen(origin+'/install/', context=context, timeout=2) as response:
                    if response.status == 200:
                        break
            except (OSError, urllib.error.URLError):
                pass
            time.sleep(0.1)
        else:
            raise AssertionError('Native browser endpoint unavailable')
        assert not (state/'apply-request.json').exists()
        result = command([os.environ['P21_NODE'], str(repo/'scripts/p21/installer_apply_browser_test.mjs')],
            input=json.dumps(dict(origin=origin, token=token)), cwd=repo)
        credentials = json.loads(result.stdout)
        assert set(credentials) == {'session', 'csrf'}
        assert (state/'apply-request.json').exists()
        return credentials
    finally:
        for process in reversed(processes):
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait(timeout=5)
