"""Exercise shipped site activation, real reload failure, and TLS to actual daemons."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time


def verify_site(root, release, prepared, profile):
    target = Path('/etc/nginx/conf.d/gojet.conf')
    conflict = Path('/etc/nginx/conf.d/p21-existing.conf')
    override_dir = Path('/etc/systemd/system/nginx.service.d')
    override = override_dir/'p21-reload-failure.conf'
    assert not target.exists() and not target.is_symlink() and not conflict.exists() and not override.exists()
    command = ['php8.3', str(release/'installer/bin/activate-site.php'), str(prepared)]
    expected = (prepared/'nginx.conf').read_bytes()
    data_before = (prepared/'data/journal.json').read_bytes()
    services_before = (prepared/'services.json').read_bytes()

    def call(nonroot=False):
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            capture_output=True, timeout=120, cwd=root, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'})

    def refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Site activation refused; inspect private journal and verify recovery.\n'

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
        finally:
            override.unlink()
            ctl('daemon-reload')
            subprocess.run(['nginx', '-t'], check=True, capture_output=True, timeout=15)
            ctl('reload', 'nginx.service')
        refused(call())  # Restored reload command alone does not clear the failed authority.
        assert (prepared/'data/journal.json').read_bytes() == data_before
        assert (prepared/'services.json').read_bytes() == services_before
        # Explicit verified operator recovery in this disposable fixture only.
        (prepared/'site.json').unlink()
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
        repeated = call()
        assert repeated.returncode == 0 and not repeated.stderr
        assert json.loads(repeated.stdout) == dict(site_configured=True, already_applied=True, installation_complete=False)
        journal = (prepared/'site.json').read_text()
        assert profile['database']['password'] not in journal and profile['secrets']['admin'] not in journal
        assert not Path('/var/lib/gojet-installer-authority/installed.lock').exists()
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
