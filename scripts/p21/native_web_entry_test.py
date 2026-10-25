"""Ship-owned first entry on real system services; browser publishes the initial draft."""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import socket
import subprocess
from native_browser_apply import submit_configuration_browser


def verify_web_entry(root, release, source, manifest, profile):
    authority = Path('/var/lib/gojet-installer-authority')
    directory = root/'initial-web-entry'
    state = directory/'state'
    pool = Path('/etc/php/8.3/fpm/pool.d/gojet-installer.conf')
    site = Path('/etc/nginx/conf.d/gojet-installer.conf')
    started, ready = authority/'web-entry-started.json', authority/'web-entry-ready.json'
    assert all(not p.exists() and not p.is_symlink() for p in [directory, pool, site, started, ready])
    token = 'native-initial-entry-fixture-token-0123456789'
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0)); port = probe.getsockname()[1]
    origin = f'https://localhost:{port}'
    request = dict(schema='gojet.web-entry.v1', origin=origin, release=str(release), manifest_sha256=manifest,
        source=source, bootstrap_sha256=hashlib.sha256(token.encode()).hexdigest(), tls=profile['tls'],
        files={k: profile['configuration']['files'][k] for k in ['root', 'clamav_socket']})
    command = ['php8.3', str(release/'installer/bin/provision-web-entry.php'), str(directory)]

    def call(success=False, nonroot=False, data=None):
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+command,
            input=json.dumps(request if data is None else data), capture_output=True, text=True, timeout=120)
        if success:
            assert result.returncode == 0 and not result.stderr, 'Native initial web entry provisioning failed'
            return json.loads(result.stdout)
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == 'Installer entry provisioning refused; preserve root receipts and inspect owned web configuration.\n'

    def ctl(*args):
        return subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=45)

    call(nonroot=True); call(data=dict(request, manifest_sha256='0'*64))
    call(data=dict(request, origin='https://localhost/path'))
    for conflict in [directory, pool, site, authority/'installed.lock']:
        conflict.symlink_to('/nonexistent-native-entry-fixture')
        try:
            call(); assert conflict.is_symlink() and not started.exists()
        finally: conflict.unlink()
    with socket.socket() as blocker:
        blocker.bind(('0.0.0.0', port)); blocker.listen()
        call(); assert not started.exists() and not directory.exists()
    for name in ['service-activation.lock', 'fpm-switch.lock']:
        lock = authority/name
        assert lock.exists()
        with lock.open('r+b') as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            call(); assert not started.exists()
    # Deliberately incomplete publication: no automatic reconstruction or state reset.
    started.write_text('{}'); started.chmod(0o600)
    try:
        call(); assert started.read_text() == '{}' and not directory.exists()
    finally: started.unlink()  # Only the deliberately injected boundary fixture.
    owned = {}
    try:
        assert call(success=True) == dict(entry_ready=True, already_applied=False, installation_complete=False)
        account = pwd.getpwnam('gojet-installer')
        assert directory.stat().st_uid == 0 and directory.stat().st_mode & 0o777 == 0o755
        assert state.stat().st_uid == account.pw_uid and state.stat().st_mode & 0o777 == 0o700
        for path, mode in [(pool, 0o600), (site, 0o644), (started, 0o600), (ready, 0o600),
                           (directory/'web.json', 0o600), (directory/'binding.json', 0o600)]:
            assert path.stat().st_uid == 0 and path.stat().st_mode & 0o777 == mode and path.stat().st_nlink == 1
            owned[path] = path.read_bytes()
        current = json.loads((state/'state.json').read_text())
        assert current['step'] == 0 and current['session'] is None and current['status'] == 'ready'
        assert not (directory/'prepared').exists() and not (state/'configuration-draft.json').exists()
        assert json.loads((directory/'web.json').read_text())['state'] == str(state)
        assert json.loads((directory/'binding.json').read_text())['destination'] == str(directory/'prepared')
        before = {p: p.read_bytes() for p in [state/'state.json', state/'state.lock']}
        assert call(success=True)['already_applied'] is True
        assert all(p.read_bytes() == raw for p, raw in owned.items())
        assert all(p.read_bytes() == raw for p, raw in before.items())
        # Boundary fixture after all immutable publications but before a success receipt.
        ready.unlink()
        pool.write_bytes(owned[pool]+b'; foreign pool modification\n')
        call(); assert not ready.exists() and pool.read_bytes() != owned[pool]
        pool.write_bytes(owned[pool])
        assert call(success=True)['already_applied'] is False
        assert all(p.read_bytes() == raw for p, raw in before.items())
        # Both actual daemons reload from persistent host configuration after restart.
        ctl('restart', 'php8.3-fpm.service'); ctl('restart', 'nginx.service')
        draft = copy.deepcopy(profile); del draft['tls']; del draft['configuration']['secrets']
        for name in ['root', 'clamav_socket']: del draft['configuration']['files'][name]
        submit_configuration_browser(root, release, state, authority, token, draft, initialized=True,
            evidence_name='browser-entry', endpoint_origin=origin)
        worker = ['php8.3', str(release/'installer/bin/run-web-preparation.php'), str(directory/'binding.json'), str(state)]
        result = subprocess.run(worker, capture_output=True, timeout=180)
        assert result.returncode == 0 and not result.stderr, 'Initial endpoint draft failed native root preparation'
        assert json.loads(result.stdout)['prepared'] is True
        assert not (directory/'prepared/data/journal.json').exists()
        assert not (authority/'web-apply.json').exists() and not (authority/'configuration-approval.json').exists()
        assert not (state/'apply-request.json').exists() and not (state/'apply-approval.json').exists()
        assert all(p.read_bytes() == raw for p, raw in owned.items())
    finally:
        # This fixture exclusively owns these nodes. Never remove changed operator configuration.
        for path in [site, pool]:
            if path in owned:
                assert path.read_bytes() == owned[path]
                path.unlink()
        if owned:
            ctl('reload', 'php8.3-fpm.service'); ctl('reload', 'nginx.service')
            for path in [ready, started]:
                assert path.read_bytes() == owned[path]
                path.unlink()
