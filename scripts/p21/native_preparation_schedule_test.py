"""Real systemd preparation timer; only disposable fixture artifacts are removed."""
from contextlib import contextmanager
import json
from pathlib import Path
import subprocess
import time


@contextmanager
def preparation_schedule(release, binding, state, destination, authority):
    render = ['php8.3', str(release/'installer/bin/render-web-preparation-schedule.php'), str(binding), str(state)]
    install = ['php8.3', str(release/'installer/bin/install-web-preparation-schedule.php'), str(binding), str(state)]
    names = ['gojet-installer-preparation.service', 'gojet-installer-preparation.timer']
    paths = [Path('/etc/systemd/system')/name for name in names]
    started = authority/'preparation-schedule-started.json'
    ready = authority/'preparation-schedule-ready.json'
    attempt = Path(str(binding)+'.started.json')
    prepared = Path(str(binding)+'.ready.json')
    marker = authority/'installed.lock'
    assert all(not p.exists() and not p.is_symlink() for p in [*paths, started, ready, attempt, prepared, marker])

    def run(argv, success=True):
        result = subprocess.run(argv, capture_output=True, timeout=180)
        assert result.returncode == (0 if success else 1), 'Preparation scheduler command result mismatch'
        if success:
            assert not result.stderr
        else:
            assert not result.stdout
        return result

    def ctl(*args):
        return subprocess.run(['systemctl', *args], capture_output=True, check=True, timeout=45)

    run(['runuser', '-u', 'gojet-installer', '--']+render, False)
    run(['runuser', '-u', 'gojet-installer', '--']+install, False)
    units = json.loads(run(render).stdout)
    assert list(units) == names
    assert f'ExecStart=/usr/bin/php8.3 {release}/installer/bin/run-web-preparation.php {binding} {state}' in units[names[0]]
    assert 'run-web-apply' not in units[names[0]] and 'approve-web-configuration' not in units[names[0]]
    # Reject unsafe literal systemd argument paths before any publication.
    run(render[:-1]+[str(state)+'/%n'], False)
    original = binding.read_bytes()
    binding.chmod(0o644)
    try:
        run(install, False)
        assert not started.exists() and all(not p.exists() for p in paths)
    finally:
        binding.chmod(0o600)
    assert binding.read_bytes() == original
    for conflict in ['file', 'link', 'receipt']:
        artifact = started if conflict == 'receipt' else paths[0]
        if conflict == 'link':
            artifact.symlink_to('/nonexistent-gojet-preparation-fixture')
        else:
            artifact.write_text('fixture conflict'); artifact.chmod(0o600)
        try:
            run(install, False)
            assert not ready.exists() and not paths[1].exists()
            if conflict == 'link':
                assert artifact.is_symlink()
            else:
                assert artifact.read_text() == 'fixture conflict'
        finally:
            artifact.unlink()
    installed = False
    try:
        result = run(install)
        installed = True
        assert json.loads(result.stdout) == dict(scheduler_enabled=True, installation_complete=False)
        assert ctl('is-enabled', names[1]).stdout.strip() == b'enabled'
        assert ctl('is-active', names[1]).stdout.strip() == b'active'
        for p in paths:
            assert p.read_text() == units[p.name] and p.stat().st_uid == 0 and p.stat().st_mode & 0o777 == 0o644
        for p in [started, ready]:
            assert p.stat().st_uid == 0 and p.stat().st_mode & 0o777 == 0o600
        receipt = started.read_bytes()
        run(install, False)
        assert started.read_bytes() == receipt
        # Before submission, even an explicit start is skipped without creating a plan.
        ctl('start', names[0])
        assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
        assert not attempt.exists() and not prepared.exists() and not destination.exists()

        def wait_prepared():
            deadline = time.monotonic()+180
            while time.monotonic() < deadline:
                if prepared.exists() and ctl('show', names[0], '--property=ActiveState', '--value').stdout.strip() == b'inactive':
                    assert ctl('show', names[0], '--property=Result', '--value').stdout.strip() == b'success'
                    return
                if ctl('show', names[0], '--property=ActiveState', '--value').stdout.strip() == b'failed':
                    raise AssertionError('Real preparation service failed; no retry performed')
                time.sleep(0.5)
            raise AssertionError('Real preparation timer did not finish')
        yield wait_prepared
        ctl('stop', names[1], names[0])
        snapshots = {p: p.read_bytes() for p in [attempt, prepared, destination/'seal.json']}
        ctl('start', names[0])
        assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
        assert all(p.read_bytes() == raw for p, raw in snapshots.items())
        assert not (state/'apply-approval.json').exists() and not (state/'apply-request.json').exists()
        assert not (authority/'configuration-approval.json').exists() and not (authority/'web-apply.json').exists()
    finally:
        # No operator files: these names were absent and generated solely in this fixture.
        if installed or any(p.exists() for p in paths):
            ctl('disable', '--now', names[1]); ctl('stop', names[0])
            for p in paths:
                if p.exists():
                    assert p.read_text() == units[p.name]
                    p.unlink()
            ctl('daemon-reload')
