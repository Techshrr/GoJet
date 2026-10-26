"""Post-completion retirement of the real pool and published preparation timer."""
import fcntl
import json
from pathlib import Path
import socket
import subprocess


def verify_retirement(root, release, prepared, entry, scheduler_owned):
    authority = Path('/var/lib/gojet-installer-authority')
    pool = Path('/etc/php/8.3/fpm/pool.d/gojet-installer.conf')
    saved = Path(str(pool)+'.retired')
    command = ['php8.3', str(release/'installer/bin/retire-installer.php'), str(prepared), str(entry/'web.json')]
    started, ready = authority/'retirement-started.json', authority/'retirement-ready.json'
    immutable = {p: p.read_bytes() for p in [authority/'installed.lock', entry/'web.json', entry/'binding.json',
        entry/'state/state.json', prepared/'data/journal.json', prepared/'services.json', prepared/'site.json',
        Path('/var/lib/gojet-installation-result/result.json'), *scheduler_owned]}
    pool_raw = pool.read_bytes()
    default_pool = Path('/etc/php/8.3/fpm/pool.d/www.conf')
    default_raw = default_pool.read_bytes()
    def call(ok=True, nonroot=False):
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+command,
            capture_output=True, timeout=150)
        if ok:
            assert result.returncode == 0 and not result.stderr, 'Installer retirement failed'
            return json.loads(result.stdout)
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Installer retirement refused; preserve completion and retirement receipts and inspect owned resources.\n'
    call(False, True)
    # Completion alone never permits replacing a foreign pool or overriding a scheduler.
    pool.write_bytes(pool_raw+b'\n; operator change\n')
    try:
        call(False); assert not started.exists()
    finally: pool.write_bytes(pool_raw)
    saved.symlink_to('/nonexistent-retirement-fixture')
    try:
        call(False); assert saved.is_symlink() and not started.exists()
    finally: saved.unlink()
    service = Path('/etc/systemd/system/gojet-installer-preparation.service')
    service.write_bytes(scheduler_owned[service]+b'\n# operator change\n')
    try:
        call(False); assert not started.exists()
    finally: service.write_bytes(scheduler_owned[service])
    for name in ['service-activation.lock', 'fpm-switch.lock', 'scheduler-publication.lock']:
        with (authority/name).open('r+b') as held:
            fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
            call(False)
            assert not started.exists()
    assert call() == dict(installer_retired=True, already_applied=False, installation_complete=True)
    assert not pool.exists() and saved.read_bytes() == pool_raw
    for name in ['gojet-installer-preparation.timer', 'gojet-installer-preparation.service']:
        assert subprocess.check_output(['systemctl', 'show', name, '--property=ActiveState', '--value'], text=True).strip() == 'inactive'
    assert subprocess.check_output(['systemctl', 'show', 'gojet-installer-preparation.timer', '--property=UnitFileState', '--value'], text=True).strip() == 'disabled'
    with socket.socket(socket.AF_UNIX) as probe:
        assert probe.connect_ex(str(entry/'fpm.sock')) != 0
    receipt = ready.read_bytes()
    assert call()['already_applied'] is True
    # Explicit missing-success-receipt boundary, not a machine power-loss claim.
    ready.unlink()
    assert call()['already_applied'] is False and ready.read_bytes() == receipt
    # The same durable attempt can also resume with its original exact pool still active.
    ready.unlink(); saved.rename(pool)
    subprocess.run(['systemctl', 'reload', 'php8.3-fpm.service'], check=True, capture_output=True, timeout=45)
    assert call()['already_applied'] is False and ready.read_bytes() == receipt
    assert all(p.read_bytes() == raw for p, raw in immutable.items())
    assert default_pool.read_bytes() == default_raw
    subprocess.run(['systemctl', 'restart', 'php8.3-fpm.service'], check=True, capture_output=True, timeout=45)
    with socket.socket(socket.AF_UNIX) as probe:
        assert probe.connect_ex(str(entry/'fpm.sock')) != 0
    assert call()['already_applied'] is True
    result = subprocess.run(['curl', '--silent', '--show-error', '--noproxy', '*', '--cacert', str(root/'certificate.pem'),
        'https://localhost:18443/.well-known/gojet-installation-result'], capture_output=True, timeout=10)
    assert result.returncode == 0 and result.stdout == immutable[Path('/var/lib/gojet-installation-result/result.json')]
