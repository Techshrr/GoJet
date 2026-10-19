"""Actual eight-daemon startup from sealed unit bytes, only in disposable native CI."""
import fcntl
import json
import os
from pathlib import Path
import pwd
import secrets
import socket
import subprocess
import time
import urllib.error
import urllib.request


SERVICES = ['logreceiver', 'redirectengine', 'analyticsworker', 'analyticsreconciler',
            'platformapi', 'mailworker', 'fileworker', 'operationsmonitor']


def verify_services(root, release, prepared, storage, profile):
    assert os.geteuid() == 0
    units = ['gojet-'+name+'.service' for name in SERVICES]
    account = pwd.getpwnam('gojet')
    installed = []

    def systemctl(*args, check=True):
        return subprocess.run(['systemctl', *args], check=check, capture_output=True, text=True, timeout=45)

    def status(unit):
        output = systemctl('show', unit, '--property=ActiveState,SubState,MainPID,NRestarts,User,Group,ProtectSystem,NoNewPrivileges').stdout
        return dict(line.split('=', 1) for line in output.splitlines() if '=' in line)

    def http(port, path):
        try:
            response = urllib.request.urlopen('http://127.0.0.1:'+str(port)+path, timeout=3)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status

    # Never replace an operator unit or interfere with an existing installation.
    for unit in units:
        for directory in ['/etc/systemd/system', '/run/systemd/system', '/usr/lib/systemd/system']:
            path = Path(directory)/unit
            assert not path.exists() and not path.is_symlink(), 'Existing GoJet unit blocks isolated fixture'
        assert systemctl('is-active', unit, check=False).stdout.strip() not in ['active', 'activating']
    authority = Path('/var/lib/gojet-installer-authority')
    authority.mkdir(mode=0o755, exist_ok=True)
    assert authority.stat().st_uid == 0 and authority.stat().st_mode & 0o022 == 0
    assert not (authority/'installed.lock').exists()
    command = ['php8.3', str(release/'installer/bin/activate-services.php'), str(prepared)]

    def activate(nonroot=False):
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            capture_output=True, timeout=150, cwd=root, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'})

    def refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Service activation refused; inspect private journal and verify recovery.\n'

    refused(activate(True))
    marker = authority/'installed.lock'
    marker.touch(mode=0o600)
    try:
        refused(activate())
    finally:
        marker.unlink()
    with (authority/'service-activation.lock').open('wb') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        refused(activate())
    conflict = Path('/etc/systemd/system/gojet-platformapi.service')
    conflict.write_text('[Unit]\nDescription=Existing operator authority\n')
    conflict.chmod(0o644)
    try:
        refused(activate())
        assert conflict.read_text() == '[Unit]\nDescription=Existing operator authority\n'
    finally:
        conflict.unlink()
        systemctl('daemon-reload')
    assert not (prepared/'services.json').exists()
    journal = (prepared/'data/journal.json').read_bytes()
    # A real occupied API port causes the actual API binary to fail during activation.
    blocker = socket.socket()
    blocker.bind(('127.0.0.1', 8081))
    blocker.listen()
    try:
        refused(activate())
        state = json.loads((prepared/'services.json').read_text())
        assert state['phase'] == 'failed' and state['cleanup_complete'] is True
        failed = (prepared/'services.json').read_bytes()
        refused(activate())
        assert (prepared/'services.json').read_bytes() == failed
        assert blocker.getsockname()[1] == 8081  # The unrelated port owner was not stopped.
    finally:
        blocker.close()
    for unit in units:
        assert not (Path('/etc/systemd/system')/unit).exists()
        assert systemctl('is-active', unit, check=False).stdout.strip() not in ['active', 'activating']
        assert not (Path('/etc/systemd/system/multi-user.target.wants')/unit).is_symlink()
    assert (prepared/'data/journal.json').read_bytes() == journal
    # Explicit operator recovery in the disposable fixture after verifying complete cleanup.
    # Production has no automatic failed-state reset; database state is never rolled back here.
    (prepared/'services.json').unlink()
    try:
        result = activate()
        assert result.returncode == 0 and not result.stderr, 'Root native activation failed'
        assert json.loads(result.stdout) == dict(services_ready=True, already_applied=False, installation_complete=False)
        installed = [Path('/etc/systemd/system')/unit for unit in units]
        repeat = activate()
        assert repeat.returncode == 0 and not repeat.stderr
        assert json.loads(repeat.stdout) == dict(services_ready=True, already_applied=True, installation_complete=False)
        for unit in units:
            assert systemctl('is-enabled', unit).stdout.strip() == 'enabled'
        expected = {name: name+(' listening' if name in ['platformapi', 'redirectengine'] else ' started') for name in SERVICES}
        deadline = time.monotonic()+20
        while True:
            logs = subprocess.check_output(['journalctl', *sum((['-u', unit] for unit in units), []), '--no-pager', '-o', 'cat'], text=True)
            ready = set()
            for line in logs.splitlines():
                try:
                    row = json.loads(line)
                except ValueError:
                    continue
                name = row.get('service_id')
                if name in expected and row.get('msg') == expected[name]:
                    ready.add(name)
            if ready == set(SERVICES):
                break
            assert time.monotonic() < deadline, 'Missing daemon startup events: '+','.join(sorted(set(SERVICES)-ready))
            time.sleep(.2)
        assert http(8081, '/api/public/auth/providers') == 200
        assert http(8081, '/api/workspaces/1/files') in [401, 403]
        # A nonexistent short code traverses the actual redirect handler and returns404.
        assert http(8080, '/p21absent') == 404
        assert http(8080, '/healthz') == 204
        time.sleep(3)
        for unit in units:
            state = status(unit)
            assert state['ActiveState'] == 'active' and state['SubState'] == 'running', 'Daemon not running: '+unit
            assert state['NRestarts'] == '0', 'Daemon restarted: '+unit
            assert state['User'] == 'gojet' and state['Group'] == 'gojet'
            assert state['ProtectSystem'] == 'strict' and state['NoNewPrivileges'] == 'yes'
            pid = int(state['MainPID'])
            assert pid > 1
            process = Path('/proc')/str(pid)
            uids = next(line for line in (process/'status').read_text().splitlines() if line.startswith('Uid:')).split()[1:]
            assert all(int(uid) == account.pw_uid for uid in uids)
            assert (process/'exe').resolve() == release/'bin'/unit.removeprefix('gojet-').removesuffix('.service')
            if unit in ['gojet-platformapi.service', 'gojet-fileworker.service']:
                # Enter the actual service's mount namespace without changing its hardening.
                probe = storage/('.p21-write-'+secrets.token_hex(8))
                code = 'import os,pathlib,sys; assert os.geteuid()==int(sys.argv[2]); p=pathlib.Path(sys.argv[1]); p.write_bytes(b"native"); p.unlink()'
                check = subprocess.run(['nsenter', '--target', str(pid), '--mount', '--setuid', str(account.pw_uid),
                    '--setgid', str(account.pw_gid), '/usr/bin/python3', '-c', code, str(probe), str(account.pw_uid)],
                    capture_output=True, timeout=10)
                assert check.returncode == 0, 'Configured storage inaccessible inside actual service sandbox'
        for secret in [profile['database']['password'], *profile['secrets'].values()]:
            assert secret not in logs, 'Private configuration leaked into journal'
        assert not list(root.rglob('installed.lock'))
    finally:
        if installed:
            systemctl('stop', *units, check=False)
            systemctl('disable', *units, check=False)
            for path in installed:
                path.unlink()
            systemctl('daemon-reload')
            systemctl('reset-failed', *units, check=False)
    for unit in units:
        assert systemctl('is-active', unit, check=False).stdout.strip() not in ['active', 'activating']
