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
    entry = root/'handover-entry'
    entry_site = Path('/etc/nginx/conf.d/gojet-installer.conf')
    saved_site = Path(str(entry_site)+'.handover')
    pool = Path('/etc/php/8.3/fpm/pool.d/gojet-installer.conf')
    authority = Path('/var/lib/gojet-installer-authority')
    entry_owned = {}
    entry_raw = None
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
        # Provision the shipped persistent Installer on the SAME TLS origin as the product.
        seal = json.loads((prepared/'seal.json').read_text())
        cert = next(p for p, meta in seal['tls_files'].items() if not meta['private'])
        key = next(p for p, meta in seal['tls_files'].items() if meta['private'])
        entry_request = dict(schema='gojet.web-entry.v1', origin=profile['origin'], release=str(release),
            manifest_sha256=seal['manifest_sha256'], source=seal['source'],
            bootstrap_sha256=hashlib.sha256(b'native-handover-fixture-token-0123456789').hexdigest(),
            tls=dict(certificate=cert, private_key=key, http_port=18080),
            files={k: profile['files'][k] for k in ['root', 'clamav_socket']})
        result = subprocess.run(['php8.3', str(release/'installer/bin/provision-web-entry.php'), str(entry)],
            input=json.dumps(entry_request), capture_output=True, text=True, timeout=120)
        assert result.returncode == 0 and not result.stderr, 'Same-origin entry provisioning failed'
        entry_raw = entry_site.read_bytes()
        for p in [pool, authority/'web-entry-started.json', authority/'web-entry-ready.json']:
            entry_owned[p] = p.read_bytes()
        assert curl('/install/')[0] == 401
        refused(call())  # No root handover authority: the existing listener remains protected.
        assert entry_site.read_bytes() == entry_raw and not (prepared/'site.json').exists()
        authorization = ['php8.3', str(release/'installer/bin/authorize-entry-handover.php'), str(prepared), str(entry)]
        def authorize(ok=True, nonroot=False):
            result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+authorization,
                capture_output=True, timeout=120)
            if ok:
                assert result.returncode == 0 and not result.stderr, 'Root entry handover authorization failed'
                return json.loads(result.stdout)
            assert result.returncode == 1 and not result.stdout
            assert result.stderr == b'Entry handover authorization refused; inspect the sealed plan and original entry receipts.\n'
        authorize(False, True)
        with (authority/'service-activation.lock').open('r+b') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            authorize(False)
        assert authorize()['already_applied'] is False
        assert authorize()['already_applied'] is True
        handover = prepared/'entry-handover.json'
        handover_raw = handover.read_bytes()
        for mutation in ['permissions', 'binding', 'vhost', 'saved-link']:
            if mutation == 'permissions': handover.chmod(0o644)
            if mutation == 'binding': handover.write_bytes(b'{}')
            if mutation == 'vhost': entry_site.write_bytes(entry_raw+b'\n# foreign change\n')
            if mutation == 'saved-link': saved_site.symlink_to('/nonexistent-handover-fixture')
            try:
                refused(call())
                assert not target.exists() and not (prepared/'site.json').exists()
            finally:
                handover.write_bytes(handover_raw); handover.chmod(0o600)
                entry_site.write_bytes(entry_raw)
                if saved_site.is_symlink(): saved_site.unlink()
        # An independent HTTP listener must survive even after our HTTPS vhost is authorized.
        conflict.write_text('server { listen 18080; server_name other.invalid; return 204; }\n')
        try:
            refused(call())
            assert conflict.exists() and entry_site.read_bytes() == entry_raw and not saved_site.exists()
            assert not target.exists() and json.loads((prepared/'site.json').read_text())['phase'] == 'failed'
        finally:
            conflict.unlink()
        recover()
        # Construct publication boundaries on either side of the atomic entry rename.
        # These two cases are boundary fixtures; the later reload test kills a real worker.
        for parked in [False, True]:
            boundary = json.loads((prepared/'site.json').read_text())
            boundary.update(phase='publishing', cleanup_complete=False)
            (prepared/'site.json').write_text(json.dumps(boundary))
            if parked: entry_site.rename(saved_site)
            refused(call())
            recover()
            assert entry_site.read_bytes() == entry_raw and not saved_site.exists() and not target.exists()
        initial_recoveries = json.loads((prepared/'site.json').read_text())['recovery_count']
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
        assert state['phase'] == 'recovered' and state['recovered_from'] == 'failed' and state['recovery_count'] == initial_recoveries+1
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
        assert saved_site.read_bytes() == entry_raw and not entry_site.exists()
        saved_site.write_bytes(entry_raw+b'\n# foreign parked entry\n')
        try:
            recover(False)
            assert target.read_bytes() == expected and (prepared/'site.json').read_bytes() == interrupted
        finally:
            saved_site.write_bytes(entry_raw)
        # A newly created entry path must never be overwritten by restoration.
        entry_site.symlink_to('/nonexistent-handover-fixture')
        try:
            recover(False)
            assert entry_site.is_symlink() and target.read_bytes() == expected
        finally:
            entry_site.unlink()
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
        assert state['phase'] == 'recovered' and state['recovered_from'] == 'reloading' and state['recovery_count'] == initial_recoveries+2
        assert not target.exists()
        deadline = time.monotonic()+10
        while curl('/install/')[0] != 401:
            assert time.monotonic() < deadline, 'Recovered original Installer unavailable'
            time.sleep(.1)
        assert entry_site.read_bytes() == entry_raw and not saved_site.exists()
        ctl('restart', 'nginx.service')
        assert curl('/install/')[0] == 401
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
        assert saved_site.read_bytes() == entry_raw and not entry_site.exists()
        ctl('restart', 'nginx.service')
        assert curl('/')[0] == 200 and curl('/install/')[0] == 404
        assert handover.read_bytes() == handover_raw
        assert all(p.read_bytes() == raw for p, raw in entry_owned.items())
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
        for p in [entry_site, saved_site]:
            if p.exists() or p.is_symlink():
                assert entry_raw is not None and not p.is_symlink() and p.read_bytes() == entry_raw
                p.unlink()
        for p, raw in entry_owned.items():
            assert p.read_bytes() == raw
            p.unlink()
        if entry_owned: ctl('reload', 'php8.3-fpm.service')
        subprocess.run(['nginx', '-t'], check=True, capture_output=True, timeout=15)
        ctl('reload', 'nginx.service')
