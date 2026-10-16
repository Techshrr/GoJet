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
from catalog import catalog

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
        command = ['php8.3', str(repo / 'installer/bin/data-bootstrap.php'), str(directory)]
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else []) + command,
            capture_output=True, timeout=100)

    def refused(result):
        assert result.returncode == 1 and not result.stdout, 'Expected refusal'
        assert result.stderr == b'Root data bootstrap refused; inspect private journal and verify recovery.\n', 'Unredacted diagnostic'

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
    # Explicit interrupted-journal fixture; does not claim a process-kill test.
    interrupted = job('interrupted')
    marker = interrupted / 'journal.json'
    marker.write_text(json.dumps(dict(schema=1, source=source,
        request_sha256=sha(interrupted / 'request.json'), phase='admin-started')))
    marker.chmod(0o600)
    refused(call(interrupted))
    for directory in [success, failed, interrupted]:
        raw = (directory / 'journal.json').read_text()
        assert plan['admin']['password'] not in raw and plan['admin_cipher']['key_hex'] not in raw
        assert (directory / 'journal.json').stat().st_mode & 0o777 == 0o600
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(source=source, native_data_bootstrap=True, migrations_applied=31,
    inherited_admin_created=True, repeat_noop=True, private_request_enforced=True,
    root_only=True, immutable_input_rejections=True, native_identity_checked=True,
    job_lock_rejected=True, real_post_migration_failure=True, failed_retry_blocked=True,
    changed_request_retry_blocked=True, interrupted_journal_fixture_blocked=True,
    secrets_not_in_journal=True, final_installation_lock_written=False, installation_complete=False)))
