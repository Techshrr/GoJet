"""Explicit scheduler publication recovery on real PID1, with constructed boundaries."""
import fcntl
import json
from pathlib import Path
import subprocess
import time


def verify_schedule_recovery(release, kind, arguments=()):
    authority = Path('/var/lib/gojet-installer-authority')
    suffix = kind
    if kind == 'preparation' and len(arguments) == 3:
        import hashlib
        suffix += '-'+hashlib.sha256(str(arguments[0]).encode()).hexdigest()
    service, timer = ['gojet-installer-'+suffix+'.'+ext for ext in ['service', 'timer']]
    paths = [Path('/etc/systemd/system')/name for name in [service, timer]]
    started, ready = [authority/(suffix+'-schedule-'+stage+'.json') for stage in ['started', 'ready']]
    original = {p: p.read_bytes() for p in [*paths, started, ready]}
    binding = Path(arguments[0]) if kind == 'preparation' else authority/'web-apply.json'
    binding_raw = binding.read_bytes()
    recover = ['php8.3', str(release/'installer/bin/recover-web-schedule.php'), kind, *map(str, arguments)]

    def ctl(*args):
        return subprocess.check_output(['systemctl', *args], text=True, timeout=45, stderr=subprocess.PIPE).strip()

    def idle():
        ctl('stop', timer)
        deadline = time.monotonic()+120
        while ctl('show', service, '--property=ActiveState', '--value') != 'inactive':
            assert time.monotonic() < deadline, 'Scheduler worker did not finish before fixture setup'
            time.sleep(.2)

    def call(ok=True, nonroot=False):
        before = {p: (p.read_bytes() if not p.is_symlink() else p.readlink())
                  for p in [*paths, started, ready, binding] if p.exists() or p.is_symlink()}
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+recover,
            capture_output=True, text=True, timeout=180)
        assert result.returncode == (0 if ok else 1), 'Scheduler publication recovery result mismatch: '+kind
        if ok:
            assert not result.stderr
            return json.loads(result.stdout)
        assert not result.stdout and result.stderr == 'Scheduler recovery refused; preserve original receipts and inspect unit identity and worker status.\n'
        assert {p: (p.read_bytes() if not p.is_symlink() else p.readlink())
                for p in [*paths, started, ready, binding] if p.exists() or p.is_symlink()} == before

    idle(); call(False, nonroot=True)
    for count in [0, 1, 2]:
        idle(); ctl('disable', timer)
        ready.unlink()
        for p in paths[count:]:
            assert p.read_bytes() == original[p]; p.unlink()
        ctl('daemon-reload')
        if count == 0:
            # Missing units cannot be reconstructed from a changed binding, even if units render identically.
            binding.write_bytes(binding_raw+b'\n'); call(False); binding.write_bytes(binding_raw)
            started.write_text('{}'); call(False); started.write_bytes(original[started])
            paths[0].symlink_to('/nonexistent-scheduler-recovery-fixture'); call(False); paths[0].unlink()
            lock = authority/'scheduler-publication.lock'
            with lock.open('r+b') as held:
                fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB); call(False)
        if count == 1:
            paths[0].write_bytes(original[paths[0]]+b'# changed\n'); call(False); paths[0].write_bytes(original[paths[0]])
            paths[0].chmod(0o666); call(False); paths[0].chmod(0o644)
        assert call() == dict(scheduler_enabled=True, already_applied=False, installation_complete=False)
        assert all(p.read_bytes() == raw for p, raw in original.items())
        assert ctl('is-enabled', timer) == 'enabled' and ctl('is-active', timer) == 'active'
    # Receipt lost after enable: idle preparation/apply keep their live timer.
    # Preflight actually runs probes, so use its completed inactive boundary without racing a tick.
    if kind == 'preflight': idle()
    ready.unlink()
    assert call()['already_applied'] is False
    # Completed repeat is read-only; reject a deliberate operator stop rather than reversing it.
    idle(); call(False)
    ctl('start', timer)
    if kind != 'preflight': assert call()['already_applied'] is True
    assert all(p.read_bytes() == raw for p, raw in original.items())
    assert binding.read_bytes() == binding_raw
