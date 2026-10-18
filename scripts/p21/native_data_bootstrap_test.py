"""Exercise the shipped root data worker with real native MySQL and Redis."""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
import time
from catalog import catalog
from native_configuration_test import verify_configuration

repo = Path(__file__).resolve().parents[2]
binary, source = sys.argv[1:3]
assert os.geteuid() == 0
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()


def sql(database, query):
    return subprocess.check_output(['mysql', '-h127.0.0.1', '-ugojet_test', '-NB', database,
        '-e', query], env=os.environ | {'MYSQL_PWD': 'p21-ci-only'}, text=True).strip()


with tempfile.TemporaryDirectory(prefix='gojet-data-') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    release = root / 'release'
    (release / 'bin').mkdir(parents=True)
    shutil.copytree(repo / 'migrations', release / 'migrations')
    shutil.copytree(repo / 'installer', release / 'installer')
    shutil.copyfile(binary, release / 'bin/platformapi')
    (release / 'bin/platformapi').chmod(0o755)
    (release / 'migration-catalog.json').write_text(json.dumps(catalog(release / 'migrations')))
    plan = dict(schema='gojet.root-data-bootstrap.v1', release=str(release), source=source,
        version='10.0.0-rc.1', catalog_sha256=sha(release / 'migration-catalog.json'),
        platformapi_sha256=sha(release / 'bin/platformapi'),
        database=dict(host='127.0.0.1', port=3306, name='gojet_p21_data_success', user='gojet_test', password='p21-ci-only'),
        redis=dict(host='127.0.0.1', port=6379, db=4, password=''),
        admin=dict(email='initial@p21.test', display_name='Initial administrator', password=secrets.token_urlsafe(32)),
        admin_cipher=dict(id='p21-ci', key_hex=secrets.token_hex(32)), origin='https://admin.p21.test')

    def job(name, request=None):
        directory = root / name
        directory.mkdir(mode=0o700)
        request_path = directory / 'request.json'
        request_path.write_text(json.dumps(plan if request is None else request))
        request_path.chmod(0o600)
        return directory

    def call(directory, nonroot=False):
        command = ['php8.3', str(release / 'installer/bin/data-bootstrap.php'), str(directory)]
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else []) + command,
            capture_output=True, timeout=100, cwd=root, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'})

    def refused(result):
        assert result.returncode == 1 and not result.stdout, 'Expected refusal'
        assert result.stderr == b'Root data bootstrap refused; inspect private journal and verify recovery.\n', 'Unexpected refusal diagnostic: '+repr(result.stderr)

    permissions = job('permissions')
    (permissions / 'request.json').chmod(0o644)
    refused(call(permissions))
    assert not (permissions / 'journal.json').exists()
    linked = job('linked')
    (linked / 'request.json').unlink()
    (linked / 'request.json').symlink_to(permissions / 'request.json')
    refused(call(linked))
    for name, field, value in [('hash', 'catalog_sha256', '0'*64), ('identity', 'source', '0'*40), ('version', 'version', 'wrong')]:
        changed = copy.deepcopy(plan)
        changed[field] = value
        directory = job(name, changed)
        refused(call(directory))
        assert not (directory / 'journal.json').exists()
    locked = job('locked')
    with (locked / 'worker.lock').open('wb') as handle:
        os.chmod(handle.name, 0o600)
        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        refused(call(locked))
        assert not (locked / 'journal.json').exists()
    success = job('success')
    refused(call(success, nonroot=True))
    assert sql(plan['database']['name'], 'SHOW TABLES') == ''
    result = call(success)
    assert result.returncode == 0 and not result.stderr, 'Native data bootstrap failed'
    assert json.loads(result.stdout) == dict(data_complete=True, already_applied=False, installation_complete=False)
    assert json.loads(call(success).stdout) == dict(data_complete=True, already_applied=True, installation_complete=False)
    assert sql(plan['database']['name'], "SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
    assert sql(plan['database']['name'], 'SELECT COUNT(*) FROM admin_administrators') == '1'
    assert sql(plan['database']['name'], "SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.bootstrap' AND result='success'") == '1'
    verify_configuration(repo, root, binary, plan)
    # The actual non-root operationsmonitor must select the same Redis DB as API/redirect.
    monitor_binary = sys.argv[3]
    monitor_env = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin',
        'GOJET_MYSQL_DSN': 'gojet_test:p21-ci-only@tcp(127.0.0.1:3306)/gojet_p21_data_success?parseTime=true',
        'GOJET_REDIS_ADDR': '127.0.0.1:6379', 'GOJET_RISK_PROVIDER_NAME': 'native-config-probe',
        'GOJET_RISK_PROVIDER_ENDPOINT': 'https://unused.invalid', 'GOJET_RISK_POLICY_VERSION': 'native-config-probe'}
    def redis_clients():
        output = subprocess.check_output(['redis-cli', '--raw', 'CLIENT', 'LIST'], text=True)
        return {row['id']: row for line in output.splitlines()
                if (row := dict(item.split('=', 1) for item in line.split()))['cmd'] != 'client|list'}
    for configured, expected in [(None, '0'), ('4', '4')]:
        env = monitor_env.copy()
        if configured is not None:
            env['GOJET_REDIS_DB'] = configured
        before_clients = redis_clients()
        with tempfile.TemporaryFile() as logs:
            process = subprocess.Popen(['runuser', '-u', 'nobody', '--', monitor_binary],
                env=env, stdout=logs, stderr=logs, cwd='/tmp')
            try:
                for _ in range(100):
                    assert process.poll() is None, 'Operationsmonitor exited before readiness'
                    logs.seek(0)
                    if b'operationsmonitor started' in logs.read():
                        break
                    time.sleep(.05)
                else:
                    raise AssertionError('Operationsmonitor did not start')
                clients = redis_clients()
                new = [row for identity, row in clients.items() if identity not in before_clients]
                assert new and all(row['db'] == expected for row in new), 'Wrong actual Redis database'
            finally:
                process.terminate()
                process.wait(timeout=10)
    for value in ['-1', 'invalid', '999999999999999999999999999999999999']:
        result = subprocess.run(['runuser', '-u', 'nobody', '--', monitor_binary, '--once'],
            env=monitor_env | {'GOJET_REDIS_DB': value}, capture_output=True, timeout=10, cwd='/tmp')
        assert result.returncode != 0 and b'invalid GOJET_REDIS_DB' in result.stdout
        assert b'p21-ci-only' not in result.stdout + result.stderr
    # Real post-migration failure: deliberately unavailable local Redis endpoint.
    failed_plan = copy.deepcopy(plan)
    failed_plan['database']['name'] = 'gojet_p21_data_failure'
    failed_plan['redis']['port'] = 1
    failed = job('failed', failed_plan)
    refused(call(failed))
    journal = json.loads((failed / 'journal.json').read_text())
    assert journal['phase'] == 'failed' and journal['failed_at'] == 'admin-started'
    assert sql(failed_plan['database']['name'], "SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
    assert sql(failed_plan['database']['name'], 'SELECT COUNT(*) FROM admin_administrators') == '0'
    before = (failed / 'journal.json').read_bytes()
    refused(call(failed))
    failed_plan['redis']['port'] = 6379
    (failed / 'request.json').write_text(json.dumps(failed_plan))
    refused(call(failed))
    assert (failed / 'journal.json').read_bytes() == before
    # Freeze writes in this disposable MySQL instance, then kill the actual worker
    # while its real CREATE TABLE is blocked. No fabricated progress journal.
    root_env = os.environ | {'MYSQL_PWD': 'root'}
    root_mysql = ['mysql', '-h127.0.0.1', '-uroot', '-NB']
    subprocess.run(root_mysql + ['-e', "CREATE DATABASE gojet_p21_data_interrupted; GRANT ALL ON gojet_p21_data_interrupted.* TO 'gojet_test'@'127.0.0.1';"],
        env=root_env, check=True, capture_output=True, timeout=10)
    interrupted_plan = copy.deepcopy(plan)
    interrupted_plan['database']['name'] = 'gojet_p21_data_interrupted'
    interrupted = job('interrupted', interrupted_plan)
    holder = subprocess.Popen(root_mysql + ['--unbuffered'], env=root_env,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True)
    worker = None
    try:
        holder.stdin.write("FLUSH TABLES WITH READ LOCK; SELECT 1;\n")
        holder.stdin.flush()
        assert holder.stdout.readline().strip() == '1'
        worker = subprocess.Popen(['php8.3', str(release / 'installer/bin/data-bootstrap.php'), str(interrupted)],
            cwd=root, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'}, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        deadline = time.monotonic() + 6
        while True:
            waiting = subprocess.check_output(root_mysql + ['-e', "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE DB='gojet_p21_data_interrupted' AND STATE='Waiting for global read lock'"],
                env=root_env, text=True, timeout=3).strip()
            if waiting == '1':
                break
            assert worker.poll() is None and time.monotonic() < deadline, 'Worker did not reach blocked native DDL'
            time.sleep(0.05)
        marker = interrupted / 'journal.json'
        before = marker.read_bytes()
        assert json.loads(before)['phase'] == 'migrating'
        worker.kill()
        stdout, stderr = worker.communicate(timeout=5)
        assert worker.returncode == -9 and not stdout and not stderr
    finally:
        if worker is not None and worker.poll() is None:
            worker.kill()
            worker.communicate(timeout=5)
        holder.stdin.close()
        holder.wait(timeout=5)
        holder.stdout.close()
    refused(call(interrupted))
    assert marker.read_bytes() == before
    assert 'admin_administrators' not in sql('gojet_p21_data_interrupted', 'SHOW TABLES')
    for directory in [success, failed, interrupted]:
        raw = (directory / 'journal.json').read_text()
        assert plan['admin']['password'] not in raw and plan['admin_cipher']['key_hex'] not in raw
        assert (directory / 'journal.json').stat().st_mode & 0o777 == 0o600
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(source=source, native_data_bootstrap=True, migrations_applied=31,
    inherited_admin_created=True, repeat_noop=True, private_request_enforced=True,
    root_only=True, immutable_input_rejections=True, native_identity_checked=True,
    typed_configuration_rejected_unsafe_input=True, typed_eight_service_pid1_roundtrip=True,
    actual_platformapi_generated_configuration=True, inherited_session_boundaries_preserved=True,
    operationsmonitor_native_redis_db_selection=True, operationsmonitor_invalid_redis_db_rejected=True,
    job_lock_rejected=True, real_post_migration_failure=True, failed_retry_blocked=True,
    changed_request_retry_blocked=True, actual_worker_sigkill=True, native_ddl_wait_observed=True, interrupted_retry_blocked=True,
    secrets_not_in_journal=True, final_installation_lock_written=False, installation_complete=False)))
