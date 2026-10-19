"""Bind the actual verified payload, typed configuration and native data bootstrap."""
import copy
import fcntl
import hashlib
import json
import os
import pwd
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
from archive import verify_archive
from native_prepared_services_test import verify_services

repo = Path(__file__).resolve().parents[2]
archive, source = Path(sys.argv[1]).resolve(), sys.argv[2]
assert os.geteuid() == 0
archive_digest = archive.with_name(archive.name+'.sha256').read_text().split()[0]
verify_archive(archive, archive_digest, source)
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
env = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'}


def sql(query):
    return subprocess.check_output(['mysql', '-h127.0.0.1', '-ugojet_prepared', '-NB',
        'gojet_p21_prepared', '-e', query], env=env | {'MYSQL_PWD': 'p21-ci-only'}, text=True, timeout=15).strip()


with tempfile.TemporaryDirectory(prefix='gojet-preparation-', dir='/var/lib') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    release = root/'release'
    release.mkdir(mode=0o755)
    with tarfile.open(archive, 'r:gz') as tar:
        tar.extractall(release, filter='data')
    # Trust the checked-out tool before verifying any executable in the archive.
    shutil.copytree(repo/'installer', root/'tools')
    digest = sha(release/'manifest.json')
    certificate, key = root/'certificate.pem', root/'private.key'
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
        '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost',
        '-keyout', str(key), '-out', str(certificate)], check=True, capture_output=True, timeout=20)
    certificate.chmod(0o644)
    key.chmod(0o600)
    storage = root/'storage'
    account = pwd.getpwnam('gojet')
    for directory in [storage, storage/'quarantine', storage/'published']:
        directory.mkdir(mode=0o700)
        os.chown(directory, account.pw_uid, account.pw_gid)
    profile = dict(schema='gojet.native-configuration.v1', origin='https://localhost:18443',
        database=dict(port=3306, name='gojet_p21_prepared', user='gojet_prepared', password='p21-ci-only'),
        redis=dict(port=6379, db=5, password=''),
        secrets=dict(key_id='prepared-ci', **{k: secrets.token_hex(32) for k in
            ['admin', 'csrf', 'grant', 'oauth', 'file_public', 'text_public']}),
        files=dict(root=str(storage), clamav_socket='/run/clamav/clamd.ctl', max_upload_bytes=1048576,
            max_files=100, max_bytes=10485760, types={'pdf': ['application/pdf']}),
        quotas=dict(text=100, bio=100, qr=100),
        smtp=dict(host='localhost', port=2525, **{'from': 'native@p21.test'}, username='', password=''),
        risk=dict(name='prepared-probe', endpoint='https://unused.invalid/check', policy_version='native-v1'),
        turnstile_secret=secrets.token_urlsafe(32), auth_rate=dict(limit=3, window_seconds=60))
    request = dict(configuration=profile,
        admin=dict(email='prepared@p21.test', display_name='Prepared administrator', password=secrets.token_urlsafe(32)),
        tls=dict(certificate=str(certificate), private_key=str(key), http_port=18080))
    destination = root/'prepared'

    def prepare(value=request, manifest=digest, commit=source, nonroot=False):
        command = ['php8.3', str(root/'tools/bin/prepare-installation.php'), str(destination), str(release), manifest, commit]
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            input=json.dumps(value).encode(), capture_output=True, timeout=60, cwd=root, env=env)

    def execute(nonroot=False):
        command = ['php8.3', str(root/'tools/bin/run-prepared-data.php'), str(destination)]
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            capture_output=True, timeout=120, cwd=root, env=env)

    def refused(result, preparation=False):
        assert result.returncode == 1 and not result.stdout, 'Expected refusal before data execution'
        expected = (b'Installation preparation refused.\n' if preparation else
                    b'Prepared data execution refused; verify private inputs and recovery.\n')
        assert result.stderr == expected, 'Unexpected refusal diagnostic'
        assert sql('SHOW TABLES') == '', 'Refused input mutated database'
        assert not (destination/'data/journal.json').exists()

    refused(prepare(nonroot=True), True)
    refused(prepare(manifest='0'*64), True)
    refused(prepare(commit='0'*40), True)
    for value in [str(release), str(destination), str(root)]:
        bad = copy.deepcopy(request)
        bad['configuration']['files']['root'] = value
        refused(prepare(bad), True)
    bad = copy.deepcopy(request)
    bad['tls']['private_key'] = str(certificate)
    refused(prepare(bad), True)
    bad = copy.deepcopy(request)
    bad['configuration']['redis']['db'] = -1
    refused(prepare(bad), True)
    key.chmod(0o644)
    refused(prepare(), True)
    key.chmod(0o600)
    destination.symlink_to(root/'absent')
    refused(prepare(), True)
    destination.unlink()
    with (root/'.installation-preparation.lock').open('wb') as lock:
        os.chmod(lock.name, 0o600)
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        refused(prepare(), True)
    result = prepare()
    assert result.returncode == 0 and not result.stderr, 'Preparation of verified payload failed'
    assert json.loads(result.stdout) == dict(prepared=True, source=source, manifest_sha256=digest,
        environment_files=8, data_executed=False, installation_complete=False)
    assert sql('SHOW TABLES') == ''
    refused(prepare(), True)
    refused(execute(nonroot=True))
    for path in [destination, destination/'environment', destination/'data', destination/'units']:
        assert path.stat().st_uid == 0 and path.stat().st_mode & 0o7777 == 0o700
    for path in destination.rglob('*'):
        if path.is_file():
            assert path.stat().st_uid == 0 and path.stat().st_mode & 0o7777 == 0o600 and path.stat().st_nlink == 1
    prepared = json.loads((destination/'data/request.json').read_text())
    assert prepared['database'] == dict(host='127.0.0.1', **profile['database'])
    assert prepared['redis'] == dict(host='127.0.0.1', **profile['redis'])
    assert prepared['admin'] == request['admin'] and prepared['origin'] == profile['origin']
    assert prepared['admin_cipher'] == dict(id=profile['secrets']['key_id'], key_hex=profile['secrets']['admin'])
    dependencies = json.loads((destination/'dependencies.json').read_text())
    assert dependencies['database'] == profile['database'] and dependencies['redis'] == profile['redis']
    assert dependencies['storage'] == str(storage) and dependencies['source'] == source
    seal_before = (destination/'seal.json').read_bytes()
    # Validate the composed configuration with the real installed Nginx parser.
    nginx = root/'nginx.conf'
    nginx.write_text('events {}\nhttp { include /etc/nginx/mime.types; include '+str(destination/'nginx.conf')+'; }\n')
    check = subprocess.run(['nginx', '-t', '-c', str(nginx)], capture_output=True, timeout=15)
    assert check.returncode == 0, 'Prepared product Nginx configuration invalid'
    for path in [destination/'units/gojet-platformapi.service', destination/'dependencies.json', destination/'data/request.json', destination/'environment/platformapi.env',
                 destination/'nginx.conf', certificate, key, release/'deploy/sysusers/gojet.conf']:
        original = path.read_bytes()
        path.write_bytes(original+b'\n')
        refused(execute())
        path.write_bytes(original)
    # Same bytes with changed permissions, ownership, hardlinks or symlinks are unsafe.
    for path in [key, destination/'data/request.json', destination/'seal.json']:
        path.chmod(0o644)
        refused(execute())
        path.chmod(0o600)
    target = destination/'environment/platformapi.env'
    original = target.read_bytes()
    linked = root/'linked'
    os.link(target, linked)
    refused(execute())
    linked.unlink()
    os.chown(target, 65534, 65534)
    refused(execute())
    os.chown(target, 0, 0)
    target.rename(linked)
    target.symlink_to(linked)
    refused(execute())
    target.unlink()
    linked.rename(target)
    extra = destination/'environment/unexpected.env'
    extra.write_text('unexpected')
    refused(execute())
    extra.unlink()
    assert (destination/'seal.json').read_bytes() == seal_before
    # Changes after preparation must prevent any migration, even with an intact seal.
    storage.chmod(0o755)
    try:
        refused(execute())
    finally:
        storage.chmod(0o700)
    for unit in ['nginx.service', 'php8.3-fpm.service', 'redis-server.service']:
        subprocess.run(['systemctl', 'stop', unit], check=True, capture_output=True, timeout=30)
        try:
            refused(execute())
        finally:
            subprocess.run(['systemctl', 'start', unit], check=True, capture_output=True, timeout=30)
    assert (destination/'seal.json').read_bytes() == seal_before
    result = execute()
    assert result.returncode == 0 and not result.stderr, 'Prepared native data execution failed'
    assert json.loads(result.stdout) == dict(data_complete=True, already_applied=False, installation_complete=False)
    assert json.loads(execute().stdout) == dict(data_complete=True, already_applied=True, installation_complete=False)
    assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
    assert sql('SELECT COUNT(*) FROM admin_administrators') == '1'
    assert sql("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.bootstrap' AND result='success'") == '1'
    for secret in [request['admin']['password'], profile['database']['password'], *profile['secrets'].values()]:
        assert secret.encode() not in result.stdout+result.stderr
    journal = (destination/'data/journal.json').read_text()
    assert request['admin']['password'] not in journal and profile['secrets']['admin'] not in journal
    verify_services(root, release, destination, storage, profile)
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(source=source, archive_sha256=archive_digest, manifest_sha256=digest,
    actual_release_preparation=True, typed_configuration_data_binding=True, native_nginx_syntax=True,
    preparation_without_database_mutation=True, root_private_plan=True, source_identity_bound=True,
    existing_job_and_concurrent_preparation_rejected=True, tls_content_and_mode_changes_rejected=True,
    prepared_input_and_release_tampering_rejected=True, links_and_owner_changes_rejected=True,
    current_dependencies_checked_before_migration=True, stopped_dependencies_block_data_execution=True,
    changed_storage_permissions_block_data_execution=True, restored_dependencies_allow_execution=True,
    eight_packaged_units_started=True, eight_daemons_nonroot=True, eight_daemons_no_restart=True,
    custom_storage_service_sandbox_verified=True, real_api_and_redirect_reachable=True,
    native_migrations_applied=31, inherited_admin_created=True, repeat_noop=True,
    installation_complete=False, final_installation_lock_written=False)))
