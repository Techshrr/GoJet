"""Real native dependency failures against the compiled payload in a disposable runner."""
import copy
import hashlib
import json
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
from archive import verify_archive

repo = Path(__file__).resolve().parents[2]
archive, source = Path(sys.argv[1]).resolve(), sys.argv[2]
assert os.geteuid() == 0
verify_archive(archive, archive.with_name(archive.name+'.sha256').read_text().split()[0], source)
with tempfile.TemporaryDirectory(prefix='gojet-preflight-') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    release = root/'release'
    release.mkdir(mode=0o755)
    with tarfile.open(archive, 'r:gz') as tar:
        tar.extractall(release, filter='data')
    shutil.copytree(repo/'installer', root/'tools')
    storage = root/'storage'
    account = pwd.getpwnam('gojet')
    for directory in [storage, storage/'quarantine', storage/'published']:
        directory.mkdir(mode=0o700)
        os.chown(directory, account.pw_uid, account.pw_gid)
    plan = dict(release=str(release), source=source,
        manifest_sha256=hashlib.sha256((release/'manifest.json').read_bytes()).hexdigest(),
        database=dict(port=3306, name='gojet_p21_preflight', user='gojet_preflight', password='p21-ci-only'),
        redis=dict(port=6379, db=4, password=''), storage=str(storage), clamav_socket='/run/clamav/clamd.ctl')
    command = ['php8.3', str(root/'tools/bin/preflight.php')]

    def call(p=plan, nonroot=False):
        result = subprocess.run((['runuser','-u','nobody','--'] if nonroot else [])+command,
            input=json.dumps(p).encode(), capture_output=True, timeout=100, cwd=root,
            env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin'})
        if nonroot:
            assert result.returncode == 1 and not result.stdout and result.stderr == b'Native dependency preflight refused.\n'
            return None
        assert result.returncode in (0,2) and not result.stderr, 'Preflight diagnostic failure'
        report = json.loads(result.stdout)
        assert report['source'] == source and report['installation_complete'] is False
        assert plan['database']['password'].encode() not in result.stdout
        assert result.returncode == (0 if report['ready'] else 2)
        return report

    call(nonroot=True)
    baseline = call()
    deadline = time.monotonic() + 60
    while not baseline['checks']['clamav'] and time.monotonic() < deadline:
        time.sleep(1)
        baseline = call()
    if not baseline['ready']:
        diagnostic = subprocess.run(['runuser','-u','gojet','--',str(release/'bin/filepreflight')],
            capture_output=True, timeout=15, cwd=root, env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin',
                'GOJET_FILE_STORAGE_ROOT':str(storage), 'GOJET_CLAMAV_NETWORK':'unix',
                'GOJET_CLAMAV_ADDRESS':plan['clamav_socket']})
        try:
            health = json.loads(diagnostic.stdout)
            print(json.dumps({'native_file_probe': {key:health.get(key) for key in ['ready','status','storage','clamav']}}), file=sys.stderr)
        except (ValueError, TypeError):
            print('Native file probe returned no health JSON', file=sys.stderr)
        import socket
        try:
            with socket.socket(socket.AF_UNIX) as connection:
                connection.settimeout(3)
                connection.connect(plan['clamav_socket'])
                connection.sendall(b'zVERSION\x00')
                version = connection.recv(4096).decode('utf-8', 'replace').strip('\x00\r\n')
                print(json.dumps({'native_clamd_version_reply':version}), file=sys.stderr)
        except OSError as error:
            print(json.dumps({'native_clamd_socket_errno':error.errno}), file=sys.stderr)
    assert baseline['ready'], 'Native preflight failed checks: '+str(baseline['checks'])
    for name in ['database','redis']:
        bad = copy.deepcopy(plan)
        bad[name]['port'] = 1
        report = call(bad)
        assert not report['ready'] and report['checks']['mysql' if name == 'database' else name] is False
    bad = copy.deepcopy(plan)
    bad['database']['password'] = 'deliberately-wrong-ci-password'
    assert not call(bad)['checks']['mysql']
    bad = copy.deepcopy(plan)
    bad['redis']['password'] = 'deliberately-wrong-ci-password'
    assert not call(bad)['checks']['redis']
    bad = copy.deepcopy(plan)
    bad['clamav_socket'] = str(root/'absent-clamd.sock')
    report = call(bad)
    assert not report['ready'] and not report['checks']['clamav'] and report['checks']['storage']
    storage.chmod(0o755)
    try:
        report = call()
        assert not report['ready'] and not report['checks']['storage'] and report['checks']['clamav']
    finally:
        storage.chmod(0o700)
    for unit, check in [('nginx.service','nginx'), ('php8.3-fpm.service','php')]:
        subprocess.run(['systemctl','stop',unit], check=True, capture_output=True, timeout=30)
        try:
            report = call()
            assert not report['ready'] and not report['checks'][check]
        finally:
            subprocess.run(['systemctl','start',unit], check=True, capture_output=True, timeout=30)
    member = release/'bin/filepreflight'
    member.chmod(0o777)
    try:
        report = call()
        assert not report['ready'] and not report['checks']['package']
        assert not report['checks']['storage'] and not report['checks']['clamav']
    finally:
        member.chmod(0o755)
    assert call()['ready']
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(source=source, real_native_dependencies=True, baseline=baseline['checks'],
    stopped_nginx_fpm_rejected=True, unreachable_mysql_redis_rejected=True,
    bad_database_redis_credentials_rejected=True, missing_clamav_rejected=True,
    unsafe_storage_rejected=True, untrusted_helper_not_executed=True, root_only=True,
    dependencies_restored=True, final_lock_written=False, full_p21_t010_acceptance=False)))
