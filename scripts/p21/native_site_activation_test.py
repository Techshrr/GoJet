"""Exercise shipped site activation, real reload failure, and TLS to actual daemons."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time
from native_installation_completion_test import verify_completion


def verify_site(root, release, prepared, profile):
    target = Path('/etc/nginx/conf.d/gojet.conf')
    conflict = Path('/etc/nginx/conf.d/p21-existing.conf')
    override_dir = Path('/etc/systemd/system/nginx.service.d')
    override = override_dir/'p21-reload-failure.conf'
    assert not target.exists() and not target.is_symlink() and not conflict.exists() and not override.exists()
    command = ['php8.3', str(release/'installer/bin/activate-site.php'), str(prepared)]
    recovery = ['php8.3', str(release/'installer/bin/recover-site.php'), str(prepared)]
    expected = (prepared/'nginx.conf').read_bytes()
    data_before = (prepared/'data/journal.json').read_bytes()
    services_before = (prepared/'services.json').read_bytes()

    def call(nonroot=False):
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            capture_output=True, timeout=120, cwd=root, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'})

    def refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Site activation refused; inspect private journal and verify recovery.\n'

    def recover(ok=True, nonroot=False):
        result = subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+recovery,
            capture_output=True, timeout=120, cwd=root)
        if ok:
            assert result.returncode == 0 and not result.stderr, 'Root site recovery failed'
            assert json.loads(result.stdout)['site_recovered'] is True
        else:
            assert result.returncode == 1 and not result.stdout
            assert result.stderr == b'Site recovery refused; inspect private journal and verify recovery.\n'
        return result

    def ctl(*args):
        subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=30)

    def curl(path, tls=True, headers=()):
        port = 18443 if tls else 18080
        args = ['curl', '--silent', '--show-error', '--noproxy', '*', '--max-time', '5',
            '--resolve', 'localhost:'+str(port)+':127.0.0.1', '-D', str(root/'site-response.headers'),
            '-o', str(root/'site-response.body'), '-w', '%{http_code}']
        if tls:
            args += ['--cacert', str(root/'certificate.pem')]
        for header in headers:
            args += ['-H', header]
        result = subprocess.run(args+['https://localhost:18443'+path if tls else 'http://localhost:18080'+path],
            capture_output=True, timeout=8)
        if result.returncode:
            return 0, b'', ''
        return int(result.stdout), (root/'site-response.body').read_bytes(), (root/'site-response.headers').read_text()

    try:
        refused(call(True))
        with Path('/var/lib/gojet-installer-authority/service-activation.lock').open('r+b') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            refused(call())
        target.write_text('# Existing operator site\n')
        target.chmod(0o644)
        refused(call())
        assert target.read_text() == '# Existing operator site\n'
        target.unlink()
        # Inline listen syntax must not bypass collision detection. Do not reload this fixture.
        conflict.write_text('server { listen 18080; server_name other.invalid; return 204; }\n')
        refused(call())
        assert conflict.read_text() == 'server { listen 18080; server_name other.invalid; return 204; }\n'
        assert not target.exists() and not (prepared/'site.json').exists()
        conflict.unlink()
        # Real systemd reload failure injection, not a simulated HTTP/business response.
        override_dir.mkdir(mode=0o755, exist_ok=True)
        override.write_text('[Service]\nExecReload=\nExecReload=/usr/bin/false\n')
        ctl('daemon-reload')
        try:
            refused(call())
            state = json.loads((prepared/'site.json').read_text())
            assert state['phase'] == 'failed' and state['cleanup_complete'] is False
            assert not target.exists()  # Own file removed; rollback reload still deliberately fails.
            failed = (prepared/'site.json').read_bytes()
            refused(call())
            assert (prepared/'site.json').read_bytes() == failed
            recover(False)  # Restoration cannot succeed while the real reload command fails.
            assert json.loads((prepared/'site.json').read_text())['phase'] == 'recovering'
            refused(call())
        finally:
            override.unlink()
            ctl('daemon-reload')
            subprocess.run(['nginx', '-t'], check=True, capture_output=True, timeout=15)
            ctl('reload', 'nginx.service')
        refused(call())  # Restored reload command alone does not clear the failed authority.
        assert (prepared/'data/journal.json').read_bytes() == data_before
        assert (prepared/'services.json').read_bytes() == services_before
        recover(False, True)
        with Path('/var/lib/gojet-installer-authority/service-activation.lock').open('r+b') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            recover(False)
        recover()
        state = json.loads((prepared/'site.json').read_text())
        assert state['phase'] == 'recovered' and state['recovered_from'] == 'failed' and state['recovery_count'] == 1
        assert json.loads(recover().stdout)['already_applied'] is True
        # Interrupt the shipped root worker while a real PID1 reload is in progress.
        # The native reload still executes after a bounded delay, even if its caller dies.
        override.write_text('[Service]\nExecReload=\nExecReload=/usr/bin/sleep 3\nExecReload=/usr/sbin/nginx -s reload\n')
        ctl('daemon-reload')
        worker = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, cwd=root)
        try:
            deadline = time.monotonic()+15
            while True:
                assert worker.poll() is None, 'Site worker exited before interruption'
                state = json.loads((prepared/'site.json').read_text())
                native = subprocess.check_output(['systemctl', 'show', 'nginx.service', '--property=SubState', '--value'], text=True).strip()
                if state['phase'] == 'reloading' and native == 'reload':
                    break
                assert time.monotonic() < deadline, 'Actual reload not observed'
                time.sleep(.02)
            worker.kill()
            stdout, stderr = worker.communicate(timeout=40)
            assert worker.returncode == -9 and not stdout and not stderr
            # Wait for the actual native reload to finish, preserving the interrupted authority.
            deadline = time.monotonic()+10
            while curl('/')[0] != 200:
                assert time.monotonic() < deadline, 'Interrupted reload did not reach actual TLS site'
                time.sleep(.1)
        finally:
            if worker.poll() is None:
                worker.kill()
                worker.communicate(timeout=40)
            override.unlink()
            ctl('daemon-reload')
        interrupted = (prepared/'site.json').read_bytes()
        assert json.loads(interrupted)['phase'] == 'reloading'
        refused(call())
        assert (prepared/'site.json').read_bytes() == interrupted
        target.write_bytes(expected+b'\n# operator change\n')
        try:
            recover(False)
            assert target.read_bytes() == expected+b'\n# operator change\n'
            assert (prepared/'site.json').read_bytes() == interrupted
        finally:
            target.write_bytes(expected)
        marker = Path('/var/lib/gojet-installer-authority/installed.lock')
        marker.touch(mode=0o600)
        try:
            recover(False)
        finally:
            marker.unlink()
        recover()
        state = json.loads((prepared/'site.json').read_text())
        assert state['phase'] == 'recovered' and state['recovered_from'] == 'reloading' and state['recovery_count'] == 2
        assert not target.exists()
        deadline = time.monotonic()+10
        while curl('/')[0] != 0:
            assert time.monotonic() < deadline, 'Recovered site still accepts new connections'
            time.sleep(.1)
        assert (prepared/'data/journal.json').read_bytes() == data_before
        assert (prepared/'services.json').read_bytes() == services_before
        result = call()
        assert result.returncode == 0 and not result.stderr, 'Root site activation failed'
        assert json.loads(result.stdout) == dict(site_configured=True, already_applied=False, installation_complete=False)
        assert target.read_bytes() == expected and target.stat().st_uid == 0 and target.stat().st_mode & 0o777 == 0o644
        deadline = time.monotonic()+10
        while True:
            status, body, _ = curl('/')
            if status == 200:
                break
            assert time.monotonic() < deadline, 'Applied Nginx TLS site unavailable'
            time.sleep(.1)
        assert body == (release/'static/site/index.html').read_bytes()
        assert curl('/api/public/auth/providers')[0] == 200
        assert curl('/api/workspaces/1/files')[0] in [401, 403]
        assert curl('/p21absent')[0] == 404
        assert curl('/install')[0] == 404
        assert curl('/', headers=['Host: unrelated.invalid'])[0] == 421
        status, _, headers = curl('/login?next=%2Fapp', tls=False)
        assert status == 308 and ('location: '+profile['origin']+'/login?next=%2Fapp').lower() in headers.lower()
        recover(False)  # Never tear down an already successful site.
        repeated = call()
        assert repeated.returncode == 0 and not repeated.stderr
        assert json.loads(repeated.stdout) == dict(site_configured=True, already_applied=True, installation_complete=False)
        journal = (prepared/'site.json').read_text()
        assert profile['database']['password'] not in journal and profile['secrets']['admin'] not in journal
        assert not Path('/var/lib/gojet-installer-authority/installed.lock').exists()
        verify_completion(root, release, prepared)
    finally:
        if override.exists():
            override.unlink()
            ctl('daemon-reload')
        if conflict.exists():
            conflict.unlink()
        if target.exists():
            assert target.read_bytes() in [expected, b'# Existing operator site\n'], 'Refuse cleanup of foreign site'
            target.unlink()
        subprocess.run(['nginx', '-t'], check=True, capture_output=True, timeout=15)
        ctl('reload', 'nginx.service')
