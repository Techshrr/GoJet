"""Real persistent Ubuntu FPM pool replacement; isolated Nginx keeps the same HTTPS URL."""
from contextlib import contextmanager
import fcntl
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
def switched_configuration_endpoint(root, release, old_state, state, authority, predecessor, old_token, token, old_session):
    target = Path('/etc/php/8.3/fpm/pool.d/gojet-installer.conf')
    assert not target.exists() and not target.is_symlink(), 'Never replace an existing operator pool in CI'
    base = root/'persistent-browser-replacement'; base.mkdir(mode=0o755)
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0)); port = sock.getsockname()[1]
    origin = f'https://localhost:{port}'
    settings = dict(origin=origin, release=str(release), state=str(old_state),
        bootstrap_sha256=hashlib.sha256(old_token.encode()).hexdigest(),
        certificate=str(root/'certificate.pem'), private_key=str(root/'private.key'),
        socket=str(base/'fpm.sock'), completion=str(authority/'installed.lock'))
    request = dict(previous=settings, predecessor=str(predecessor), binding=str(state.parent/'binding.json'),
        bootstrap_sha256=hashlib.sha256(token.encode()).hexdigest())
    switch = ['php8.3', str(release/'installer/bin/switch-web-configuration.php')]
    started, ready = state.parent/'fpm-switch-started.json', state.parent/'fpm-switch-ready.json'

    def command(args, **kwargs):
        result = subprocess.run(args, capture_output=True, text=True, timeout=120, **kwargs)
        assert result.returncode == 0, 'Persistent FPM fixture command failed'
        return result

    def call(success=False, nonroot=False, payload=None):
        args = (['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+switch
        result = subprocess.run(args, input=json.dumps(request if payload is None else payload),
            capture_output=True, text=True, timeout=120)
        if success:
            assert result.returncode == 0 and not result.stderr, 'Persistent FPM switch failed'
            return json.loads(result.stdout)
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == 'Installer FPM switch refused; preserve switch receipts and inspect the active pool.\n'

    render = 'require $argv[1]."/installer/src/NativeWebConfig.php";'
    render += 'echo json_encode(GoJet\\Installer\\NativeWebConfig::render(json_decode(stream_get_contents(STDIN),true)));'
    configs = json.loads(command(['php8.3', '-r', render, str(release)], input=json.dumps(settings)).stdout)
    nginx = base/'nginx.conf'
    nginx.write_text('user www-data;\npid '+str(base/'nginx.pid')+';\nerror_log '+str(base/'nginx.log')+';\nevents {}\nhttp { access_log off;\n'+configs['nginx']+'}\n')
    command(['nginx', '-t', '-c', str(nginx), '-p', str(base)])
    with os.fdopen(os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as output:
        output.write(configs['fpm'])
    process = None
    context = ssl.create_default_context(cafile=str(root/'certificate.pem'))

    def http_status(cookie=None, bootstrap=None):
        headers = {'Accept': 'application/json', 'Origin': origin}
        if cookie: headers['Cookie'] = '__Secure-gojet_install='+cookie
        data = None
        if bootstrap is not None:
            headers['Content-Type'] = 'application/json'; data = json.dumps(dict(bootstrap_token=bootstrap)).encode()
        req = urllib.request.Request(origin+'/install/', headers=headers, data=data)
        try:
            with urllib.request.urlopen(req, context=context, timeout=3) as response: return response.status
        except urllib.error.HTTPError as response:
            with response: return response.code

    def wait_status(cookie, expected):
        for _ in range(100):
            try:
                if http_status(cookie) == expected: return
            except (OSError, urllib.error.URLError): pass
            time.sleep(0.1)
        raise AssertionError('Persistent FPM endpoint did not reach expected session boundary')

    try:
        command(['php-fpm8.3', '-t'])
        command(['systemctl', 'reload', 'php8.3-fpm.service'])
        process = subprocess.Popen(['nginx', '-c', str(nginx), '-p', str(base), '-g', 'daemon off;'],
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        wait_status(old_session, 200)  # The same URL initially serves the frozen old attempt.
        original = target.read_bytes()
        call(nonroot=True)
        wrong = dict(request, previous=dict(settings, state=str(state)))
        call(payload=wrong)
        for path in [authority/'installed.lock', authority/'web-apply.json']:
            assert not path.exists() and not path.is_symlink()
            path.symlink_to('/nonexistent-switch-fixture')
            try: call()
            finally: path.unlink()
        target.write_bytes(original+b'; operator edit\n')
        call(); assert target.read_bytes() == original+b'; operator edit\n' and not started.exists()
        target.write_bytes(original)
        # Existing foreign aliases and pool overrides are never overwritten.
        linked = base/'pool-hardlink'; os.link(target, linked)
        try: call()
        finally: linked.unlink()
        other = target.parent/'gojet-conflict-fixture.conf'
        assert not other.exists()
        other.write_text('[gojet-installer]\npm.max_children = 3\n'); other.chmod(0o644)
        try: call()
        finally: other.unlink()
        assert not started.exists() and not ready.exists() and target.read_bytes() == original
        # The global lock is created by the first valid request, but does not authorize mutation.
        lock = authority/'fpm-switch.lock'
        with lock.open('r+b') as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB); call()
        assert call(success=True) == dict(fpm_switched=True, already_applied=False, installation_complete=False)
        replaced = target.read_bytes(); records = {p: p.read_bytes() for p in [started, ready]}
        assert replaced != original and str(state).encode() in replaced
        assert target.stat().st_mode & 0o777 == 0o600 and target.stat().st_uid == 0
        wait_status(old_session, 401)
        assert http_status(bootstrap=old_token) == 401
        assert call(success=True)['already_applied'] is True
        assert all(p.read_bytes() == raw for p, raw in records.items())
        # Explicit boundary fixture: durable replacement, missing reload-success receipt.
        # This is not a claim of a SIGKILL or power-loss experiment.
        ready.unlink()
        target.write_bytes(replaced+b'; foreign modification\n')
        call(); assert not ready.exists() and started.read_bytes() == records[started]
        target.write_bytes(replaced)
        assert call(success=True)['already_applied'] is False
        assert target.read_bytes() == replaced and started.read_bytes() == records[started]
        # A fresh master must load the persisted pool, not transient test arguments.
        command(['systemctl', 'restart', 'php8.3-fpm.service'])
        wait_status(None, 401)
        assert http_status(bootstrap=old_token) == 401
        assert http_status(bootstrap=token) == 200
        assert not (state/'configuration-draft.json').exists()
        yield origin
        assert target.read_bytes() == replaced and not (authority/'web-apply.json').exists()
    finally:
        if process is not None:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=5)
        if target.exists(): target.unlink()  # Only this fixture's exclusive pool, on a disposable CI host.
        command(['php-fpm8.3', '-t'])
        command(['systemctl', 'reload', 'php8.3-fpm.service'])
