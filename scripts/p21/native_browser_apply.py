"""Real browser submission against shipped TLS/FPM; never edits installer state or mailbox."""
from contextlib import contextmanager
import selectors
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


@contextmanager
def browser_endpoint(root, release, state, authority, token, name):
    base = root/name
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
                request = urllib.request.Request(origin+'/install/', headers={'Accept': 'application/json'})
                with urllib.request.urlopen(request, context=context, timeout=2):
                    raise AssertionError('Unauthenticated installer unexpectedly accepted')
            except urllib.error.HTTPError as response:
                with response:
                    if response.code == 401 and json.loads(response.read()) == {'error': 'Installer authentication required'}:
                        break
            except (OSError, urllib.error.URLError):
                pass
            if any(process.poll() is not None for process in processes):
                raise AssertionError('Native browser endpoint process exited')
            time.sleep(0.1)
        else:
            raise AssertionError('Native browser endpoint unavailable')
        yield origin
    finally:
        for process in reversed(processes):
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill(); process.wait(timeout=5)


def submit_browser(root, release, state, authority, token):
    repo = Path(__file__).resolve().parents[2]
    with browser_endpoint(root, release, state, authority, token, 'browser-submit') as origin:
        assert not (state/'apply-request.json').exists()
        result = subprocess.run([os.environ['P21_NODE'], str(repo/'scripts/p21/installer_apply_browser_test.mjs')],
            input=json.dumps(dict(origin=origin, token=token)), cwd=repo,
            capture_output=True, text=True, timeout=120)
        assert result.returncode == 0, 'Native browser submission failed'
        credentials = json.loads(result.stdout)
        assert set(credentials) == {'session', 'csrf'}
        assert (state/'apply-request.json').exists()
        return credentials


@contextmanager
def observe_browser(root, release, state, authority, token):
    """Keep real desktop/mobile pages open around native timer execution; no state fixtures."""
    repo = Path(__file__).resolve().parents[2]
    with browser_endpoint(root, release, state, authority, token, 'browser-observe') as origin:
        process = subprocess.Popen([os.environ['P21_NODE'], str(repo/'scripts/p21/installer_progress_browser_test.mjs')],
            cwd=repo, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            process.stdin.write(json.dumps(dict(origin=origin, token=token)))
            process.stdin.close(); process.stdin = None
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                assert selector.select(timeout=60), 'Browser observer readiness timeout'
                assert process.stdout.readline() == 'ready\n', 'Browser observer failed before native execution'
            yield
            stdout, stderr = process.communicate(timeout=60)
            assert process.returncode == 0 and not stdout and not stderr, 'Browser native progress/closure verification failed'
        finally:
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait(timeout=5)
            for pipe in [process.stdin, process.stdout, process.stderr]:
                if pipe is not None:
                    pipe.close()
