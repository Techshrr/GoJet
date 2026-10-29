"""Bind the actual verified payload, typed configuration and native data bootstrap."""
import copy
import fcntl
import hashlib
import json
import os
import pwd
import re
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
from archive import verify_archive
from native_prepared_services_test import verify_services
from native_installation_apply_test import verify_apply
from native_web_entry_test import verify_web_entry

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

    # Validation-only mode has no dependency credentials and must never create an account.
    validation = [str(release/'bin/platformapi'), '--validate-bootstrap-admin']
    validation_input = json.dumps(request['admin'] | {'correlation_id': 'p21-validation-only'}).encode()
    checked = subprocess.run(validation, input=validation_input, capture_output=True, timeout=15, env=env)
    assert checked.returncode == 0 and checked.stdout == b'{"valid":true}\n' and not checked.stderr
    for argv, variables in [(validation+['unexpected'], env),
                            (['runuser', '-u', 'nobody', '--']+validation, env),
                            (validation, env | {'GOJET_TEST_AUTH_ENABLED': '1'})]:
        checked = subprocess.run(argv, input=validation_input, capture_output=True, timeout=15, env=variables)
        assert checked.returncode == 1 and not checked.stdout
        assert checked.stderr == b'initial administrator validation refused\n'
    assert sql('SHOW TABLES') == '' and not destination.exists()
    # These passed PHP's former nonempty/length checks but fail the native Admin authority.
    # Reject before publishing any prepared directory or touching the empty database.
    for email in ['not-an-email', '@example.test', 'admin@@example.test', 'admin@.example.test', 'admin@example.test.']:
        bad = copy.deepcopy(request)
        bad['admin']['email'] = email
        refused(prepare(bad), True)
        assert not destination.exists(), 'Invalid administrator published a plan'
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
    # Missing keys use root-generated material; malformed explicit values never select defaults.
    for invalid in [None, {}, {'key_id': 'incomplete'}]:
        bad = copy.deepcopy(request)
        bad['configuration']['secrets'] = invalid
        refused(prepare(bad), True)
        assert not destination.exists()
    automatic = copy.deepcopy(request)
    del automatic['configuration']['secrets']
    refused(prepare(automatic, nonroot=True), True)
    bad = copy.deepcopy(automatic)
    bad['admin']['email'] = 'not-an-email'
    refused(prepare(bad), True)
    assert not destination.exists()
    # Root-owned paths/identity never come from the future web form's untrusted draft.
    binding = root/'web-configuration-binding.json'
    binding_value = dict(schema='gojet.web-configuration-binding.v1', destination=str(destination),
        release=str(release), manifest_sha256=digest, source=source, tls=request['tls'],
        files={k: profile['files'][k] for k in ['root', 'clamav_socket']})
    binding.write_text(json.dumps(binding_value)); binding.chmod(0o600)
    draft = copy.deepcopy(automatic)
    del draft['tls']
    for name in ['root', 'clamav_socket']:
        del draft['configuration']['files'][name]

    def prepare_web(value=draft, nonroot=False, extra=False):
        command = ['php8.3', str(root/'tools/bin/prepare-web-configuration.php'), str(binding)]
        if extra:
            command.append('unexpected')
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            input=json.dumps(value).encode(), capture_output=True, timeout=60, cwd=root, env=env)

    def web_refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Web configuration preparation refused.\n'
        assert sql('SHOW TABLES') == '' and not destination.exists()

    web_refused(prepare_web(nonroot=True))
    web_refused(prepare_web(extra=True))
    for field, value in [('tls', request['tls']), ('destination', '/etc'), ('release', '/etc'),
                         ('manifest_sha256', digest), ('source', source), ('command', '/bin/sh')]:
        bad = copy.deepcopy(draft); bad[field] = value
        web_refused(prepare_web(bad))
    bad = copy.deepcopy(draft); bad['configuration']['secrets'] = profile['secrets']
    web_refused(prepare_web(bad))
    for field in ['root', 'clamav_socket']:
        bad = copy.deepcopy(draft); bad['configuration']['files'][field] = profile['files'][field]
        web_refused(prepare_web(bad))  # Even the correct path cannot be supplied by the client.
    bad = copy.deepcopy(draft); bad['admin']['roles'] = ['administrator']
    web_refused(prepare_web(bad))
    bad = copy.deepcopy(draft); bad['admin']['email'] = 'not-an-email'
    web_refused(prepare_web(bad))
    binding.chmod(0o644)
    web_refused(prepare_web())
    binding.chmod(0o600); os.chown(binding, account.pw_uid, account.pw_gid)
    web_refused(prepare_web())
    os.chown(binding, 0, 0)
    saved_binding = root/'saved-web-binding.json'
    binding.rename(saved_binding); binding.symlink_to(saved_binding)
    web_refused(prepare_web())
    binding.unlink(); saved_binding.rename(binding)
    for field in ['source', 'manifest_sha256']:
        bad = copy.deepcopy(binding_value); bad[field] = '0'*len(bad[field])
        binding.write_text(json.dumps(bad))
        web_refused(prepare_web())
    binding.write_text(json.dumps(binding_value))
    binding_before = binding.read_bytes()
    try:
        installer_account = pwd.getpwnam('gojet-installer')
    except KeyError:
        subprocess.run(['useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'gojet-installer'], check=True)
        installer_account = pwd.getpwnam('gojet-installer')
    frozen_state = root/'configuration-state'
    frozen_state.mkdir(mode=0o700)
    os.chown(frozen_state, installer_account.pw_uid, installer_account.pw_gid)
    publisher = root/'submit-configuration.php'
    publisher.write_text('''<?php
require_once $argv[1].'/installer/src/InstallState.php';
$token='native-configuration-fixture-token-0123456789';
$state=new GoJet\\Installer\\InstallState($argv[2],hash('sha256',$token));
$state->initialize(); $auth=$state->authenticate($token);
$state->submitConfiguration($auth['session'],$auth['csrf'],json_decode(stream_get_contents(STDIN),true,16,JSON_THROW_ON_ERROR));
''')
    publisher.chmod(0o644)
    submitted = subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', str(publisher), str(release), str(frozen_state)],
        input=json.dumps(draft).encode(), capture_output=True, timeout=30, env=env)
    assert submitted.returncode == 0 and not submitted.stdout and not submitted.stderr
    frozen_before = {p.name: p.read_bytes() for p in frozen_state.iterdir()}
    frozen_command = ['php8.3', str(root/'tools/bin/prepare-frozen-web-configuration.php'), str(binding), str(frozen_state)]
    denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+frozen_command, capture_output=True, timeout=30, env=env)
    assert denied.returncode == 1 and not denied.stdout and not destination.exists()
    result = subprocess.run(frozen_command, capture_output=True, timeout=60, env=env)
    assert frozen_before == {p.name: p.read_bytes() for p in frozen_state.iterdir()}, 'Root rewrote web-owned state'
    assert result.returncode == 0 and not result.stderr, 'Preparation of verified payload failed'
    assert json.loads(result.stdout) == dict(prepared=True, source=source, manifest_sha256=digest,
        environment_files=8, data_executed=False, installation_complete=False)
    assert sql('SHOW TABLES') == ''
    assert binding.read_bytes() == binding_before
    private_before = {str(p.relative_to(destination)): p.read_bytes()
                      for p in destination.rglob('*') if p.is_file()}
    refused(prepare(automatic), True)
    refused(prepare(), True)
    repeated = prepare_web()
    assert repeated.returncode == 1 and not repeated.stdout
    assert repeated.stderr == b'Web configuration preparation refused.\n'
    repeated = subprocess.run(frozen_command, capture_output=True, timeout=60, env=env)
    assert repeated.returncode == 1 and not repeated.stdout
    assert repeated.stderr == b'Frozen web configuration preparation refused.\n'
    assert private_before == {str(p.relative_to(destination)): p.read_bytes()
                              for p in destination.rglob('*') if p.is_file()}, 'Retry rotated sealed keys'
    # Read only root-private generated values for binding assertions; never print them.
    environment = (destination/'environment/platformapi.env').read_text()
    names = dict(admin='GOJET_ADMIN_TOTP_KEY_HEX', csrf='GOJET_AUTH_CSRF_KEY_HEX',
        grant='GOJET_AUTH_GRANT_KEY_HEX', oauth='GOJET_OAUTH_KEY_HEX',
        file_public='GOJET_FILE_PUBLIC_AUTH_SECRET', text_public='GOJET_TEXT_PUBLIC_AUTH_SECRET')
    generated = {}
    for name, variable in names.items():
        matches = re.findall(r'^'+variable+r'="([a-f0-9]{64})"$', environment, re.M)
        assert len(matches) == 1, 'Generated service key missing or malformed'
        generated[name] = matches[0]
    assert len(set(generated.values())) == 6
    ids = re.findall(r'^GOJET_ADMIN_TOTP_KEY_ID="(install-[a-f0-9]{32})"$', environment, re.M)
    assert len(ids) == 1
    generated['key_id'] = ids[0]
    for secret in generated.values():
        assert secret.encode() not in result.stdout + result.stderr
    profile['secrets'] = generated
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
    verify_web_entry(root, release, source, digest, request)
    upgrade_report = verify_apply(root, release, source, digest, request)
    assert not list(root.rglob('installed.lock'))
print(json.dumps(dict(source=source, archive_sha256=archive_digest, manifest_sha256=digest,
    actual_release_preparation=True, single_command_native_apply=True, upgrade=upgrade_report,
    replacement_partial_publication_recovery=True, replacement_recovery_preserves_retirement=True,
    entry_partial_publication_recovery=True, scheduler_partial_publication_recovery=True,
    recovery_preserves_execution_barriers=True, native_initial_web_entry=True, initial_entry_browser_to_root_preparation=True,
    continuous_first_install_browser_to_retirement=True, continuous_installation_without_state_fixtures=True,
    initial_entry_survives_service_restart=True, initial_entry_missing_receipt_recovery=True,
    scheduled_root_executor=True, executor_idle_and_started_barriers=True,
    unprivileged_http_apply_mailbox=True, root_bound_web_apply=True, web_apply_replay_refused=True,
    browser_apply_submission_verified=True, browser_native_progress_and_closure=True, apply_failure_resume_preserves_data=True,
    apply_global_serialization=True, foreign_completion_refused=True, typed_configuration_data_binding=True, native_nginx_syntax=True,
    preparation_without_database_mutation=True, root_private_plan=True, source_identity_bound=True,
    existing_job_and_concurrent_preparation_rejected=True, tls_content_and_mode_changes_rejected=True,
    prepared_input_and_release_tampering_rejected=True, links_and_owner_changes_rejected=True,
    current_dependencies_checked_before_migration=True, stopped_dependencies_block_data_execution=True,
    changed_storage_permissions_block_data_execution=True, restored_dependencies_allow_execution=True,
    same_origin_browser_completion=True, invalid_browser_results_refused=True,
    root_completion_receipt_recovery=True, installer_pool_and_timer_retirement=True,
    retirement_repeat_and_interruption_recovery=True, retirement_preserves_private_records=True,
    same_origin_entry_handover=True, entry_handover_failure_restores_installer=True,
    entry_handover_sigkill_recovery=True, entry_handover_foreign_files_preserved=True,
    entry_handover_survives_nginx_restart=True,
    root_site_activation=True, real_site_worker_sigkill=True, explicit_site_recovery=True, changed_site_recovery_refused=True, existing_site_and_listener_preserved=True, real_reload_failure_barrier=True,
    trusted_tls_to_actual_api=True, applied_static_bytes_verified=True, site_repeat_noop=True,
    root_service_activation=True, existing_service_conflict_preserved=True, activation_global_lock=True,
    real_service_worker_sigkill=True, explicit_service_recovery=True, changed_unit_recovery_refused=True,
    real_api_port_failure_cleaned=True, failed_activation_retry_blocked=True, activation_repeat_noop=True,
    eight_packaged_units_started=True, eight_daemons_nonroot=True, eight_daemons_no_restart=True,
    custom_storage_service_sandbox_verified=True, real_api_and_redirect_reachable=True,
    native_migrations_applied=31, inherited_admin_created=True, invalid_admin_rejected_before_plan_or_migration=True,
    root_generated_service_keys=True, repeated_preparation_preserves_keys=True,
    root_bound_web_configuration_preparation=True, web_configuration_authority_fields_rejected=True,
    frozen_unprivileged_draft_prepared=True, root_preserves_web_state=True,
    root_draft_approval_and_unprivileged_adoption=True, browser_draft_to_real_root_preparation=True, browser_real_root_approval_acknowledgement=True, root_preparation_worker_wait_repeat_interruption=True, browser_draft_to_real_preparation_timer=True, published_preparation_recovery_preserves_plan=True, preparation_worker_recovery_serialization=True, invalid_configuration_replacement_preserves_old_attempt=True, replacement_requires_new_session=True, replacement_browser_and_root_timer=True, persistent_fpm_replacement_same_endpoint=True, replacement_survives_fpm_restart=True, fpm_switch_missing_receipt_recovery=True, repeat_noop=True,
    installation_complete=False, isolated_installation_completion_verified=True,
    final_installation_lock_written=True, live_tls_mismatch_blocks_completion=True, stopped_services_block_completion=True)))
