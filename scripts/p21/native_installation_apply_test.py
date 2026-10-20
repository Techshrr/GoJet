"""Single shipped root command from empty native DB through completion, with failure/resume."""
import copy
import fcntl
import hashlib
import json
from pathlib import Path
import socket
import subprocess
from native_prepared_services_test import SERVICES
from native_web_apply_test import prepare_web_apply


def verify_apply(root, release, source, digest, request):
    destination = root/'applied'
    profile = copy.deepcopy(request)
    profile['configuration']['database']['name'] = 'gojet_p21_applied'
    profile['configuration']['redis']['db'] = 6
    marker = Path('/var/lib/gojet-installer-authority/installed.lock')
    target = Path('/etc/nginx/conf.d/gojet.conf')
    assert not marker.exists() and not target.exists()
    prepare = subprocess.run(['php8.3', str(release/'installer/bin/prepare-installation.php'), str(destination),
        str(release), digest, source], input=json.dumps(profile).encode(), capture_output=True, timeout=90)
    assert prepare.returncode == 0 and not prepare.stderr
    command = ['php8.3', str(release/'installer/bin/apply-installation.php'), str(destination)]

    def sql(query):
        return subprocess.check_output(['mysql', '-h127.0.0.1', '-ugojet_prepared', '-NB', 'gojet_p21_applied', '-e', query],
            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'MYSQL_PWD': 'p21-ci-only'}, text=True, timeout=15).strip()

    def call(ok=True, nonroot=False):
        r = subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command, capture_output=True, timeout=240)
        if ok:
            assert r.returncode == 0 and not r.stderr, 'Single-command native apply failed'
            return json.loads(r.stdout)
        assert r.returncode == 1 and not r.stdout
        assert r.stderr == b'Installation apply refused; inspect private phase journals before recovery.\n'

    units = ['gojet-'+name+'.service' for name in SERVICES]
    def ctl(*args):
        return subprocess.run(['systemctl', *args], capture_output=True, timeout=45)

    try:
        assert sql('SHOW TABLES') == ''
        call(False, True)
        for name in ['installation-apply.lock', 'service-activation.lock']:
            path = marker.parent/name
            with path.open('ab') as lock:
                path.chmod(0o600)
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                call(False)
            assert sql('SHOW TABLES') == '' and not (destination/'data/journal.json').exists()
        # Fail the actual API only after the same command has bootstrapped the empty DB.
        with socket.socket() as probe:
            probe.settimeout(1)
            assert probe.connect_ex(('127.0.0.1', 8081)) != 0, 'Prior fixture left an active API listener'
        with socket.socket() as blocker:
            # Reuse TIME_WAIT addresses from the previous real API, never an active listener.
            blocker.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            blocker.bind(('127.0.0.1', 8081)); blocker.listen()
            call(False)
            assert json.loads((destination/'data/journal.json').read_text())['phase'] == 'data-complete'
            assert json.loads((destination/'services.json').read_text())['phase'] == 'failed'
            assert not marker.exists() and not (destination/'site.json').exists()
            assert blocker.getsockname()[1] == 8081
        saved = (destination/'data/journal.json').read_bytes()
        failed = (destination/'services.json').read_bytes()
        call(False)
        assert (destination/'services.json').read_bytes() == failed
        assert (destination/'data/journal.json').read_bytes() == saved
        assert sql('SELECT COUNT(*) FROM admin_administrators') == '1'
        recovery = subprocess.run(['php8.3', str(release/'installer/bin/recover-services.php'), str(destination)], capture_output=True, timeout=120)
        assert recovery.returncode == 0 and not recovery.stderr
        first = prepare_web_apply(root, release, destination, source)
        assert first == dict(source=source, installation_complete=True, already_applied=False)
        assert (destination/'data/journal.json').read_bytes() == saved
        assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
        assert sql("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.bootstrap' AND result='success'") == '1'
        raw = marker.read_bytes()
        state = json.loads(raw)
        assert state['source'] == source and state['seal_sha256'] == hashlib.sha256((destination/'seal.json').read_bytes()).hexdigest()
        assert marker.stat().st_uid == 0 and marker.stat().st_mode & 0o777 == 0o600
        repeat = call()
        assert repeat == dict(source=source, installation_complete=True, already_applied=True)
        assert marker.read_bytes() == raw
        # A different sealed job cannot adopt this installation's completion authority.
        wrong = subprocess.run(command[:-1]+[str(root/'prepared')], capture_output=True, timeout=120)
        assert wrong.returncode == 1 and not wrong.stdout and marker.read_bytes() == raw
        for name in ['data/journal.json', 'services.json', 'site.json']:
            journal = (destination/name).read_text()
            assert profile['admin']['password'] not in journal
            assert profile['configuration']['database']['password'] not in journal
    finally:
        # Root mailbox artifacts belong solely to this disposable fixture.
        binding = marker.parent/'web-apply.json'
        if binding.exists():
            assert json.loads(binding.read_text())['prepared'] == str(destination)
            receipt = marker.parent/'web-apply-started.json'
            if receipt.exists():
                receipt.unlink()
            binding.unlink()
        # Remove only this fixture's exact owned site/units and its final marker.
        if marker.exists():
            assert json.loads(marker.read_text())['source'] == source
            marker.unlink()
        if target.exists():
            assert target.read_bytes() == (destination/'nginx.conf').read_bytes()
            target.unlink()
            assert ctl('reload', 'nginx.service').returncode == 0
        for unit in units:
            path = Path('/etc/systemd/system')/unit
            if path.exists():
                assert path.read_bytes() == (destination/'units'/unit).read_bytes()
                assert ctl('stop', unit).returncode == 0
                assert ctl('disable', unit).returncode == 0
                path.unlink()
        assert ctl('daemon-reload').returncode == 0
        ctl('reset-failed', *units)
