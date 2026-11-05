"""Real unsupported PHP and MariaDB against the unchanged, verified candidate.

Run only on the disposable native CI host. No fake version output, PDO doubles,
production hooks, candidate rebuild, or installation completion claims.
"""
import copy
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO/'scripts/p21'))
from archive import verify_archive

ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'}


def execute(argv, *, data=None, env=ENV, timeout=60):
    return subprocess.run([str(a) for a in argv], input=data, capture_output=True,
                          text=True, env=env, timeout=timeout)


def successful(argv, **kwargs):
    result = execute(argv, **kwargs)
    if result.returncode:
        # Do not dump SQL, credentials or full environment in a failure.
        raise RuntimeError('Fixture command failed: '+str(argv[0]))
    return result.stdout.strip()


def php81_command(fixtures):
    base = fixtures/'php81'
    modules = base/'usr/lib/php/20210902'
    command = [str(base/'usr/bin/php8.1'), '-n', '-d', 'extension_dir='+str(modules)]
    for name in ('posix', 'pdo', 'mysqlnd', 'pdo_mysql'):
        command += ['-d', 'extension='+name]
    identity = json.loads(successful(command+['-r',
        'echo json_encode(["version"=>PHP_VERSION,"posix"=>function_exists("posix_geteuid"),'
        '"mysql"=>extension_loaded("pdo_mysql"),"openssl"=>extension_loaded("openssl")]);']))
    assert identity['version'].startswith('8.1.')
    assert all(identity[name] is True for name in ('posix', 'mysql', 'openssl'))
    return command, identity


def main():
    assert os.geteuid() == 0 and os.environ.get('P21_DISPOSABLE_NATIVE_CI') == '1'
    archive, fixtures, output = (Path(x).resolve() for x in sys.argv[1:4])
    authority = json.loads((REPO/'artifacts/v10/P21/candidate/validation.json').read_bytes())
    source = authority['source']
    verified = verify_archive(archive, authority['archive']['sha256'], source)
    provenance = json.loads((fixtures/'provenance.json').read_bytes())
    assert provenance['signed_ubuntu_apt_metadata'] is True
    php81, identity = php81_command(fixtures)
    with tempfile.TemporaryDirectory(prefix='gojet-version-matrix-') as temporary:
        root = Path(temporary); root.chmod(0o755)
        release = root/'release'; release.mkdir(mode=0o755)
        with tarfile.open(archive, 'r:gz') as tar:
            tar.extractall(release, filter='data')
        assert hashlib.sha256((release/'manifest.json').read_bytes()).hexdigest() == authority['archive']['manifest_sha256']
        account = pwd.getpwnam('gojet'); storage = root/'storage'
        for path in (storage, storage/'quarantine', storage/'published'):
            path.mkdir(mode=0o700); os.chown(path, account.pw_uid, account.pw_gid)
        plan = dict(release=str(release), source=source, manifest_sha256=authority['archive']['manifest_sha256'],
            database=dict(port=3306, name='gojet_p21_preflight', user='gojet_preflight', password='p21-ci-only'),
            redis=dict(port=6379, db=4, password=''), storage=str(storage), clamav_socket='/run/clamav/clamd.ctl')
        receipts = Path('/var/lib/gojet-installer-authority')
        assert not (receipts/'installed.lock').exists()
        original_receipts = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                             for p in receipts.rglob('*') if p.is_file()}

        def call(expected=None, config=plan, php=('php8.3',)):
            result = execute([*php, release/'installer/bin/preflight.php'], data=json.dumps(config), timeout=100)
            assert not result.stderr and result.returncode == (0 if expected is None else 2), 'Preflight invocation failed'
            report = json.loads(result.stdout)
            assert report['source'] == source and report['installation_complete'] is False
            assert len(report['checks']) == 8 and report['ready'] is (expected is None)
            assert all(value is (name != expected) for name, value in report['checks'].items()), 'Unexpected failed dependency'
            assert config['database']['password'] not in result.stdout
            assert not (receipts/'installed.lock').exists()
            assert original_receipts == {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                                        for p in receipts.rglob('*') if p.is_file()}
            return report['checks']

        baseline = call()
        # Unsupported CLI still has every required extension; no missing-extension
        # or unavailable-service failure may masquerade as version rejection.
        call('php', php=php81)
        call()
        target = Path('/usr/sbin/php-fpm8.3')
        backup = target.with_name(target.name+'.p21-version-matrix-original')
        assert target.is_file() and not target.is_symlink() and not backup.exists()
        original = target.stat(); original_hash = hashlib.sha256(target.read_bytes()).hexdigest()
        fpm_pid = successful(['systemctl', 'show', 'php8.3-fpm.service', '--property=MainPID', '--value'])
        assert int(fpm_pid) > 0
        wrong_fpm = fixtures/'php81/usr/sbin/php-fpm8.1'
        wrong_version = successful([wrong_fpm, '-n', '-v']).splitlines()[0]
        assert wrong_version.startswith('PHP 8.1.') and 'fpm-fcgi' in wrong_version
        target.rename(backup)
        try:
            shutil.copyfile(wrong_fpm, target); target.chmod(0o755)
            assert successful([target, '-v']).splitlines()[0] == wrong_version
            call('php')
            assert successful(['systemctl', 'show', 'php8.3-fpm.service', '--property=MainPID', '--value']) == fpm_pid
        finally:
            target.unlink(missing_ok=True); backup.rename(target)
        restored = target.stat()
        assert (restored.st_dev, restored.st_ino, restored.st_mode) == (original.st_dev, original.st_ino, original.st_mode)
        assert hashlib.sha256(target.read_bytes()).hexdigest() == original_hash
        call()

        maria = fixtures/'mariadb/usr'
        private = root/'database'; private.mkdir(mode=0o700)
        datadir, socket_path = private/'data', private/'maria.sock'
        env = ENV | {'LD_LIBRARY_PATH': str(maria/'lib/x86_64-linux-gnu')}
        init = [maria/'bin/mariadb-install-db', '--no-defaults', '--basedir='+str(maria),
                '--datadir='+str(datadir), '--auth-root-authentication-method=normal',
                '--skip-test-db', '--skip-name-resolve', '--user=root']
        successful(init, env=env, timeout=90)
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', 0)); port = probe.getsockname()[1]
        server_log = (private/'server.log').open('w')
        server = subprocess.Popen([str(maria/'sbin/mariadbd'), '--no-defaults', '--user=root',
            '--basedir='+str(maria), '--datadir='+str(datadir), '--socket='+str(socket_path),
            '--port='+str(port), '--bind-address=127.0.0.1', '--skip-name-resolve',
            '--pid-file='+str(private/'maria.pid'), '--skip-log-bin'],
            env=env, stdout=server_log, stderr=server_log)
        try:
            admin = ['mysql', '--no-defaults', '--protocol=socket', '--socket='+str(socket_path), '-uroot', '-NB']
            deadline = time.monotonic()+30
            while time.monotonic() < deadline:
                assert server.poll() is None, 'Isolated unsupported database exited'
                result = execute(admin, data='SELECT VERSION();', timeout=5)
                if result.returncode == 0: break
                time.sleep(.2)
            else: raise RuntimeError('Isolated unsupported database readiness deadline')
            database_version = result.stdout.strip()
            assert database_version.startswith('10.11.') and 'MariaDB' in database_version
            password = secrets.token_hex(24)
            successful(admin, data="CREATE DATABASE gojet_wrong_version; CREATE USER 'version_probe'@'127.0.0.1' IDENTIFIED BY '"+password+"'; GRANT ALL ON gojet_wrong_version.* TO 'version_probe'@'127.0.0.1';")
            candidate = copy.deepcopy(plan)
            candidate['database'] = dict(port=port, name='gojet_wrong_version', user='version_probe', password=password)
            # Prove real PDO connectivity, authentication and the non-version
            # conditions succeed on this same endpoint before its rejection.
            code = '$p=json_decode(stream_get_contents(STDIN),true); $d=$p["database"];'
            code += '$db=new PDO("mysql:host=127.0.0.1;port=".$d["port"].";dbname=".$d["name"],$d["user"],$d["password"]);'
            code += 'echo json_encode($db->query("SELECT VERSION(), @@autocommit, @@log_bin, @@log_bin_trust_function_creators")->fetch(PDO::FETCH_NUM));'
            row = json.loads(successful(['php8.3', '-r', code], data=json.dumps(candidate)))
            assert row[0] == database_version and int(row[1]) == 1 and int(row[2]) == 0
            call('mysql', config=candidate)
            assert successful(admin, data='SELECT COUNT(*) FROM information_schema.tables WHERE table_schema="gojet_wrong_version";') == '0'
            call()
        finally:
            server.terminate()
            try: server.wait(timeout=15)
            except subprocess.TimeoutExpired: server.kill(); server.wait(timeout=5)
            server_log.close()
        assert not list(root.rglob('installed.lock'))
    report = dict(schema='gojet.native-dependency-version-matrix.v1', candidate_source=source,
        test_source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=REPO,text=True).strip(),
        archive_sha256=verified['archive_sha256'], manifest_sha256=authority['archive']['manifest_sha256'],
        baseline=baseline, real_unsupported_cli=identity, real_unsupported_fpm=wrong_version,
        real_unsupported_database=database_version, php_cli_rejected=True, php_fpm_rejected=True,
        unsupported_database_rejected=True, all_other_checks_remain_healthy=True,
        database_connection_and_settings_verified=True, unsupported_database_not_migrated=True,
        fpm_inode_mode_bytes_and_live_pid_preserved=True, original_dependencies_recovered=True,
        installation_authority_unchanged=True, final_lock_written=False, version_outputs_simulated=False,
        candidate_rebuilt=False, production_deployment=False, fixture_provenance=provenance)
    output.parent.mkdir(parents=True,exist_ok=True)
    output.write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(report))


if __name__ == '__main__': main()
