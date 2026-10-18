"""Disposable Ubuntu CI only: actual Nginx descriptors and shipped logrotate policy."""
import gzip
import grp
import json
import os
from pathlib import Path
import pwd
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
LOGS = Path('/var/log/gojet/nginx')
CONF = Path('/etc/nginx/conf.d/gojet-p21-logrotate.conf')


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True)


def main():
    assert os.geteuid() == 0, 'Disposable root CI fixture required'
    assert not CONF.exists() and not LOGS.exists(), 'Refuse existing product log configuration'
    run('systemd-tmpfiles', '--create', str(ROOT/'deploy/tmpfiles/gojet.conf'))
    for directory in [LOGS.parent, LOGS]:
        stat = directory.stat()
        assert stat.st_uid == 0 and stat.st_gid == grp.getgrnam('www-data').gr_gid
        assert stat.st_mode & 0o777 == 0o750
        run('runuser', '-u', 'www-data', '--', 'test', '-x', str(directory))
        assert subprocess.run(['runuser', '-u', 'www-data', '--', 'test', '-w', str(directory)]).returncode != 0
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    access = LOGS/'access.log'
    with tempfile.TemporaryDirectory(prefix='gojet-logrotate-') as tmp:
        state = str(Path(tmp)/'rotation.state')
        policy = Path(tmp)/'gojet-nginx'
        shutil.copyfile(ROOT/'deploy/logrotate/gojet-nginx', policy)
        policy.chmod(0o644)
        CONF.write_text(f'''server {{
    listen 127.0.0.1:{port};
    access_log {access} combined;
    error_log {LOGS}/error.log warn;
    location / {{ return 200 "native log rotation\\n"; }}
}}
''')
        try:
            run('nginx', '-t')
            run('systemctl', 'restart', 'nginx.service')
            def request(marker):
                with urllib.request.urlopen(f'http://127.0.0.1:{port}/{marker}', timeout=5) as response:
                    assert response.status == 200
            def wait_for(path, marker):
                for _ in range(100):
                    if path.exists() and marker in path.read_text():
                        return
                    time.sleep(0.05)
                raise AssertionError('Nginx did not reopen its active log')
            request('before-rotation')
            wait_for(access, 'before-rotation')
            inode = access.stat().st_ino
            run('logrotate', '--debug', '--state', state, str(policy))
            # Reproduce the original production configuration defect in this disposable fixture.
            for directory in [LOGS.parent, LOGS]:
                os.chown(directory, 0, grp.getgrnam('adm').gr_gid)
            run('logrotate', '--force', '--state', state, str(policy))
            rotated = LOGS/'access.log.1'
            assert rotated.stat().st_ino == inode
            assert not (LOGS/'access.log.1.gz').exists(), 'First rotation must delay compression'
            # Worker reopen must actually fail before testing the corrected configuration.
            for _ in range(100):
                errors = (LOGS/'error.log').read_text()
                if str(access) in errors and '(13: Permission denied)' in errors:
                    break
                time.sleep(0.05)
            else:
                raise AssertionError('Expected real worker traversal failure was not observed')
            request('blocked-worker-reopen')
            wait_for(rotated, 'blocked-worker-reopen')
            assert 'blocked-worker-reopen' not in access.read_text()
            for directory in [LOGS.parent, LOGS]:
                os.chown(directory, 0, grp.getgrnam('www-data').gr_gid)
            run('systemctl', 'kill', '--kill-whom=main', '--signal=USR1', 'nginx.service')
            # USR1 is asynchronous: requests may briefly still reach the old descriptor.
            for index in range(100):
                marker = f'after-rotation-{index}'
                request(marker)
                if marker in access.read_text():
                    break
                time.sleep(0.05)
            else:
                raise AssertionError('Live Nginx kept the retired descriptor')
            retired = rotated.read_bytes()
            request('stable-new-descriptor')
            wait_for(access, 'stable-new-descriptor')
            assert rotated.read_bytes() == retired
            stat = access.stat()
            assert stat.st_ino != inode and stat.st_mode & 0o777 == 0o640
            assert stat.st_uid == pwd.getpwnam('www-data').pw_uid
            assert stat.st_gid == grp.getgrnam('adm').gr_gid
            denied = subprocess.run(['runuser', '-u', 'gojet', '--', 'cat', str(access)], capture_output=True)
            assert denied.returncode != 0
            run('logrotate', '--force', '--state', state, str(policy))
            assert gzip.decompress((LOGS/'access.log.2.gz').read_bytes()) == retired
            run('systemctl', 'stop', 'nginx.service')
            # Stopped servers need no signal; empty/missing logs are valid.
            run('logrotate', '--force', '--state', state, str(policy))
            for path in LOGS.iterdir():
                path.unlink()
            run('logrotate', '--force', '--state', state, str(policy))
        finally:
            run('systemctl', 'stop', 'nginx.service')
            CONF.unlink(missing_ok=True)
            shutil.rmtree(LOGS)
    print(json.dumps(dict(native_nginx_rotation=True, descriptor_reopened=True,
                         delayed_compression_exact=True, root_owned_directories=True,
                         private_log_permissions=True, gojet_read_denied=True,
                         worker_traversal_failure_reproduced=True, permissions_restored_reopen=True,
                         stopped_empty_missing_logs_supported=True,
                         copytruncate_used=False, production_deployment=False)))


if __name__ == '__main__':
    main()
