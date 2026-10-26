"""Replacement publication prefixes; old attempts remain retired and byte-identical."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import subprocess


def verify_restart_publication_recovery(release, binding, old_state, successor, digest, bootstrap, preserved):
    account = pwd.getpwnam('gojet-installer')
    retired = Path(str(binding)+'.retired.json')
    ready = Path(str(binding)+'.replacement-ready.json')
    original = {p: p.read_bytes() for p in [retired, ready, successor/'binding.json', successor/'state/state.json']}
    state = successor/'state'
    command = ['php8.3', str(release/'installer/bin/recover-configuration-restart.php'),
               str(binding), str(old_state), str(successor), digest, bootstrap]
    ordinary = ['php8.3', str(release/'installer/bin/restart-web-configuration.php'), *command[2:]]
    worker = ['php8.3', str(release/'installer/bin/run-web-preparation.php'), str(binding), str(old_state)]

    def snapshot():
        paths = [retired, ready, successor, *successor.rglob('*'), *preserved]
        return {str(p): (p.lstat().st_mode, p.lstat().st_uid, p.lstat().st_gid,
            os.readlink(p) if p.is_symlink() else hashlib.sha256(p.read_bytes()).hexdigest() if p.is_file() else None)
            for p in paths if p.exists() or p.is_symlink()}

    def call(ok=True, args=None, nonroot=False):
        before = snapshot()
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+(args or command),
            capture_output=True, timeout=120)
        assert result.returncode == (0 if ok else 1), 'Replacement publication recovery result mismatch'
        if ok:
            assert not result.stderr
            value = json.loads(result.stdout)
            assert value['recovered'] is True and value['installation_complete'] is False
            return value
        assert not result.stdout and snapshot() == before

    call(False, nonroot=True)
    for prefix in range(10):
        # Only this disposable, never-used successor is reconstructed as a boundary fixture.
        assert all(p.read_bytes() == raw for p, raw in original.items())
        assert (state/'state.lock').read_bytes() == b''
        ready.unlink(); shutil.rmtree(successor)
        if prefix >= 1: successor.mkdir(mode=0o700)
        if prefix >= 2:
            (successor/'binding.json').write_bytes(original[successor/'binding.json'])
            (successor/'binding.json').chmod(0o600)
        if prefix >= 3: state.mkdir(mode=0o700)
        if prefix >= 4: (state/'state.lock').touch(mode=0o600)
        if prefix >= 5:
            (state/'state.json').write_bytes(original[state/'state.json']); (state/'state.json').chmod(0o600)
        if prefix == 6: os.chown(state/'state.json', account.pw_uid, 0)
        if prefix >= 7:
            for p in [state/'state.json', state/'state.lock', state]: os.chown(p, account.pw_uid, account.pw_gid)
            if prefix != 9:
                ready.write_bytes(original[ready]); ready.chmod(0o600)
        if prefix >= 8: successor.chmod(0o755)
        if prefix == 0:
            retired.write_text('{}'); call(False); retired.write_bytes(original[retired])
            with Path(str(binding)+'.worker.lock').open('r+b') as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB); call(False)
            missing = successor.parent/'wrong-replacement-target'
            call(False, command[:4]+[str(missing), digest, bootstrap]); assert not missing.exists()
        if prefix == 2:
            path = successor/'binding.json'; raw = path.read_bytes()
            path.write_text('{}'); call(False); path.write_bytes(raw)
            path.chmod(0o644); call(False); path.chmod(0o600)
            path.unlink(); path.symlink_to(binding); call(False); path.unlink()
            path.write_bytes(raw); path.chmod(0o600)
        if prefix == 7:
            # A busy successor preparation worker must be preserved, including its lock inode.
            lock = successor/'binding.json.worker.lock'; lock.touch(mode=0o600)
            with lock.open('r+b') as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB); call(False)
        if prefix == 8:
            for path in [successor/'fpm-switch-started.json', successor/'prepared', state/'configuration-draft.json']:
                path.symlink_to('/nonexistent-replacement-recovery-fixture'); call(False); path.unlink()
            path = state/'state.json'; raw = path.read_bytes()
            path.unlink(); call(False)
            path.write_bytes(raw); path.chmod(0o600); os.chown(path, account.pw_uid, account.pw_gid)
            changed = json.loads(raw); changed['attempts'] = 1
            path.write_text(json.dumps(changed)); call(False); path.write_bytes(raw)
        call(False, command[:-2]+['0'*64, bootstrap])
        call(False, ordinary)
        assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
        assert call()['already_applied'] is (prefix == 8)
        assert all(p.read_bytes() == raw for p, raw in original.items())
        assert all(p.read_bytes() == raw for p, raw in preserved.items())
        assert successor.stat().st_mode & 0o7777 == 0o755
        assert all(p.stat().st_uid == account.pw_uid and p.stat().st_gid == account.pw_gid
                   for p in [state, state/'state.json', state/'state.lock'])
        assert not (state/'configuration-draft.json').exists() and not (successor/'prepared').exists()
    before = snapshot(); assert call()['already_applied'] is True; assert snapshot() == before
