"""Published-prefix recovery fixtures; never a claim of machine power-loss testing."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import time
from urllib.parse import urlsplit
import subprocess


def verify_entry_publication_recovery(release, entry, request, owned):
    authority = Path('/var/lib/gojet-installer-authority')
    pool = Path('/etc/php/8.3/fpm/pool.d/gojet-installer.conf')
    site = Path('/etc/nginx/conf.d/gojet-installer.conf')
    ready, started = authority/'web-entry-ready.json', authority/'web-entry-started.json'
    state = entry/'state'
    account = pwd.getpwnam('gojet-installer')
    state_raw = (state/'state.json').read_bytes()
    root_before = started.read_bytes()
    command = ['php8.3', str(release/'installer/bin/recover-web-entry.php'), str(entry)]

    def call(ok=True, value=None, nonroot=False):
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+command,
            input=json.dumps(request if value is None else value), capture_output=True, text=True, timeout=120)
        assert result.returncode == (0 if ok else 1), 'Entry publication recovery result mismatch'
        if ok:
            assert not result.stderr
            return json.loads(result.stdout)
        assert not result.stdout and result.stderr == 'Installer entry recovery refused; preserve root receipts and inspect owned web configuration.\n'

    def ctl(*args):
        subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=45)

    def snapshot():
        return {str(p): (p.lstat().st_mode, p.lstat().st_uid, p.lstat().st_gid,
            os.readlink(p) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None)
            for p in [started, ready, pool, site, entry, *entry.rglob('*')] if p.exists() or p.is_symlink()}

    def denied(value=None, nonroot=False):
        before = snapshot(); call(False, value, nonroot); assert snapshot() == before

    denied(nonroot=True)
    for prefix in range(10):
        # Roll back only this fixture's verified, never-used entry to a publication boundary.
        assert site.read_bytes() == owned[site] and pool.read_bytes() == owned[pool]
        site.unlink(); pool.unlink(); ready.unlink()
        ctl('reload', 'nginx.service'); ctl('restart', 'php8.3-fpm.service')
        deadline = time.monotonic()+10
        while True:
            with socket.socket() as probe:
                try: probe.bind(('0.0.0.0', urlsplit(request['origin']).port)); break
                except OSError:
                    assert time.monotonic() < deadline, 'Previous fixture listener did not close'
            time.sleep(.1)
        assert (state/'state.json').read_bytes() == state_raw and (state/'state.lock').read_bytes() == b''
        shutil.rmtree(entry)
        if prefix >= 1: entry.mkdir(mode=0o700)
        for index, name in enumerate(['binding.json', 'web.json'], 2):
            if prefix >= index:
                (entry/name).write_bytes(owned[entry/name]); (entry/name).chmod(0o600)
        if prefix >= 4: state.mkdir(mode=0o700)
        if prefix >= 5:
            (state/'state.lock').touch(mode=0o600)
        if prefix >= 6:
            (state/'state.json').write_bytes(state_raw); (state/'state.json').chmod(0o600)
        if prefix == 7: os.chown(state/'state.json', account.pw_uid, 0)  # Interrupted chown/chgrp sequence.
        if prefix >= 8:
            for p in [state/'state.json', state/'state.lock', state]: os.chown(p, account.pw_uid, account.pw_gid)
            entry.chmod(0o755)
        if prefix >= 9:
            pool.write_bytes(owned[pool]); pool.chmod(0o600)
        if prefix == 3:
            original = (entry/'web.json').read_bytes()
            (entry/'web.json').write_text('{}'); denied(); (entry/'web.json').write_bytes(original)
            (entry/'web.json').chmod(0o644); denied(); (entry/'web.json').chmod(0o600)
            (entry/'web.json').unlink(); (entry/'web.json').symlink_to(entry/'binding.json'); denied()
            (entry/'web.json').unlink(); (entry/'web.json').write_bytes(original); (entry/'web.json').chmod(0o600)
        if prefix == 8:
            (state/'state.json').unlink(); denied()  # Published state cannot be reconstructed.
            (state/'state.json').write_bytes(state_raw); (state/'state.json').chmod(0o600)
            os.chown(state/'state.json', account.pw_uid, account.pw_gid)
            changed = json.loads(state_raw); changed['status'] = 'checking'
            (state/'state.json').write_text(json.dumps(changed)); denied(); (state/'state.json').write_bytes(state_raw)
        if prefix == 9:
            pool.write_bytes(owned[pool]+b'; changed\n'); denied(); pool.write_bytes(owned[pool])
        denied(dict(request, bootstrap_sha256='0'*64))
        before = snapshot()
        ordinary = subprocess.run(['php8.3', str(release/'installer/bin/provision-web-entry.php'), str(entry)],
            input=json.dumps(request), capture_output=True, text=True, timeout=120)
        assert ordinary.returncode == 1 and snapshot() == before
        assert call() == dict(entry_ready=True, already_applied=False, installation_complete=False)
        assert started.read_bytes() == root_before and ready.read_bytes() == owned[ready]
        assert all(p.read_bytes() == raw for p, raw in owned.items())
        assert (state/'state.json').read_bytes() == state_raw
        assert (state/'state.lock').read_bytes() == b''
        assert all(p.stat().st_uid == account.pw_uid and p.stat().st_gid == account.pw_gid
                   for p in [state, state/'state.json', state/'state.lock'])
    assert call()['already_applied'] is True
