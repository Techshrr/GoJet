"""Real systemd EnvironmentFile consumption and root-only atomic generation."""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile

repo = Path(__file__).resolve().parents[2]
services = json.loads((repo/'artifacts/v10/P21/test-plan.json').read_text())['services']
assert os.geteuid() == 0
with tempfile.TemporaryDirectory(prefix='gojet-environment-') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    shutil.copytree(repo/'installer', root/'tools')
    secret = secrets.token_urlsafe(20) + ' spaces "quote" \\path $HOME `id` %n # comment ; 中文 '
    values = {name: {'GOJET_MYSQL_DSN':secret, 'GOJET_REDIS_PASSWORD':'', 'GOJET_LOG_SOCKET':'/run/gojet-log/collector.sock'} for name in services}
    destination = root/'environment'
    command = ['php8.3', str(root/'tools/bin/create-environment.php')]
    def call(data=values, target=destination, nonroot=False):
        return subprocess.run((['runuser','-u','nobody','--'] if nonroot else [])+command+[str(target)],
            input=json.dumps(data).encode(), capture_output=True, timeout=15, cwd=root,
            env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin'})
    def refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Service environment creation refused.\n'
    refused(call(nonroot=True))
    for key, value in [('LD_PRELOAD','bad'), ('GOJET_TEST_AUTH_ENABLED','1'), ('GOJET_P20_BROWSER_PROBE','1'),
                       ('GOJET_MYSQL_DSN','line1\nLD_PRELOAD=bad'), ('GOJET_MYSQL_DSN','x\0y')]:
        bad = copy.deepcopy(values)
        bad['platformapi'][key] = value
        refused(call(bad))
        assert not destination.exists()
    bad = copy.deepcopy(values)
    del bad['mailworker']
    refused(call(bad))
    assert not destination.exists()
    with (root/'.environment.lock').open('wb') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        refused(call())
    result = call()
    assert result.returncode == 0 and not result.stderr
    assert json.loads(result.stdout) == dict(created=True,files=8,installation_complete=False)
    assert secret.encode() not in result.stdout
    assert destination.stat().st_mode & 0o777 == 0o700
    assert {p.name for p in destination.iterdir()} == {name+'.env' for name in services}
    before = {p.name:p.read_bytes() for p in destination.iterdir()}
    refused(call())
    assert before == {p.name:p.read_bytes() for p in destination.iterdir()}
    link = root/'linked'
    link.symlink_to(destination, target_is_directory=True)
    refused(call(target=link))
    # PID1 reads the private EnvironmentFile, then executes a real non-root probe.
    probe = root/'probe.py'
    probe.write_text('import os,hashlib,sys\nassert os.geteuid()!=0\nassert hashlib.sha256(os.environ["GOJET_MYSQL_DSN"].encode()).hexdigest()==sys.argv[1]\nassert os.environ["GOJET_REDIS_PASSWORD"]==""\nprint("verified")\n')
    expected = hashlib.sha256(secret.encode()).hexdigest()
    for name in services:
        path = destination/(name+'.env')
        stat = path.stat()
        assert stat.st_uid == 0 and stat.st_nlink == 1 and stat.st_mode & 0o777 == 0o600
        unreadable = subprocess.run(['runuser','-u','gojet','--','cat',str(path)], capture_output=True)
        assert unreadable.returncode != 0 and not unreadable.stdout
        unit = 'p21-env-'+name+'-'+secrets.token_hex(4)
        check = subprocess.run(['systemd-run','--quiet','--wait','--pipe','--collect','--unit='+unit,
            '--property=User=gojet','--property=EnvironmentFile='+str(path),
            '/usr/bin/python3',str(probe),expected], capture_output=True, timeout=30)
        assert check.returncode == 0 and check.stdout.strip() == b'verified', 'systemd EnvironmentFile roundtrip failed'
    assert not list(root.glob('.environment-*'))
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(eight_environment_files=True, real_systemd_roundtrip=True,
    nonroot_execution=True, root_private_permissions=True, special_characters_preserved=True,
    injection_test_flags_rejected=True, missing_service_rejected=True, lock_contention_rejected=True,
    existing_destination_preserved=True, symlink_destination_rejected=True,
    secrets_not_emitted=True, production_services_started=False, installation_complete=False)))
