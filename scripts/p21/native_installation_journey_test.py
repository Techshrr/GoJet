"""Continuous fresh entry -> real browser -> root timers -> same-origin result -> retirement.

Invoked only inside the disposable native CI fixture. No state/approval/completion injection.
"""
import copy
import hashlib
import json
import os
from pathlib import Path
import selectors
import socket
import subprocess
import time
from native_prepared_services_test import SERVICES


def verify_journey(root, release, source, entry, origin, token, profile):
    authority = Path('/var/lib/gojet-installer-authority')
    state, prepared = entry/'state', entry/'prepared'
    binding = entry/'binding.json'
    marker = authority/'installed.lock'
    public = Path('/var/lib/gojet-installation-result/result.json')
    product = Path('/etc/nginx/conf.d/gojet.conf')
    default_pool = Path('/etc/php/8.3/fpm/pool.d/www.conf')
    default_raw = default_pool.read_bytes()
    kinds = ['preparation', 'preflight', 'apply']
    records = [authority/name for name in ['web-apply.json', 'configuration-approval.json',
        'web-apply-started.json', 'preflight.json', 'apply-progress.json', 'installed.lock',
        'retirement-started.json', 'retirement-ready.json']]
    records += [authority/(kind+'-schedule-'+stage+'.json') for kind in kinds for stage in ['started', 'ready']]
    units = [Path('/etc/systemd/system')/('gojet-'+name+'.service') for name in SERVICES]
    schedules = [Path('/etc/systemd/system')/('gojet-installer-'+kind+'.'+suffix)
                 for kind in kinds for suffix in ['service', 'timer']]
    assert all(not p.exists() and not p.is_symlink() for p in [*records, *units, *schedules, public, product, prepared])
    draft = copy.deepcopy(profile)
    del draft['tls']; del draft['configuration']['secrets']
    for name in ['root', 'clamav_socket']: del draft['configuration']['files'][name]
    draft['configuration']['origin'] = origin
    draft['configuration']['database']['name'] = 'gojet_p21_journey'
    draft['configuration']['redis']['db'] = 7
    repo = Path(__file__).resolve().parents[2]
    scheduler_bytes = {}

    def sql(query):
        return subprocess.check_output(['mysql', '-h127.0.0.1', '-ugojet_prepared', '-NB',
            'gojet_p21_journey', '-e', query], text=True, timeout=15,
            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'MYSQL_PWD': 'p21-ci-only'}).strip()

    def command(name, *args):
        result = subprocess.run(['php8.3', str(release/'installer/bin'/name), *map(str, args)],
            capture_output=True, timeout=180)
        assert result.returncode == 0 and not result.stderr, 'Continuous installation command failed: '+name
        return json.loads(result.stdout)

    def ctl(*args):
        return subprocess.run(['systemctl', *args], check=True, capture_output=True, text=True, timeout=45).stdout.strip()

    def status(kind):
        raw = ctl('show', 'gojet-installer-'+kind+'.service', '--property=ActiveState,Result,ExecMainStatus')
        return dict(line.split('=', 1) for line in raw.splitlines())

    def wait_for(check, label, seconds=180):
        deadline = time.monotonic()+seconds
        while time.monotonic() < deadline:
            if check(): return
            assert time.monotonic() < deadline, label+' timed out'
            time.sleep(0.5)
        raise AssertionError(label+' timed out')

    def finished(kind, receipt):
        value = status(kind)
        assert value['ActiveState'] != 'failed', 'Continuous '+kind+' worker failed; no replay attempted'
        if not receipt.exists() or value['ActiveState'] != 'inactive': return False
        assert value['Result'] == 'success' and value['ExecMainStatus'] == '0'
        return True

    def install(kind, *args):
        result = command('install-web-'+kind+'-schedule.php', *args)
        assert result == dict(scheduler_enabled=True, installation_complete=False)
        receipt = json.loads((authority/(kind+'-schedule-started.json')).read_text())
        for name, expected in receipt['units'].items():
            path = Path('/etc/systemd/system')/name
            raw = path.read_bytes()
            assert hashlib.sha256(raw).hexdigest() == expected
            scheduler_bytes[path] = raw
        assert ctl('is-enabled', 'gojet-installer-'+kind+'.timer') == 'enabled'

    assert sql('SHOW TABLES') == ''
    install('preparation', binding, state)
    assert not prepared.exists() and not (state/'configuration-draft.json').exists()
    process = subprocess.Popen([os.environ['P21_NODE'], str(repo/'scripts/p21/installer_journey_browser_test.mjs')],
        cwd=repo, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, bufsize=0)

    def send(value):
        process.stdin.write((json.dumps(value)+'\n').encode()); process.stdin.flush()

    def expect(event, seconds=90):
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            assert selector.select(timeout=seconds), 'Continuous browser timed out at '+event
            assert process.stdout.readline() == (event+'\n').encode(), 'Continuous browser failed at '+event

    try:
        send(dict(origin=origin, token=token, draft=draft))
        expect('drafted')
        assert json.loads((state/'configuration-draft.json').read_text()) == draft
        assert not (state/'apply-request.json').exists() and sql('SHOW TABLES') == ''
        wait_for(lambda: finished('preparation', Path(str(binding)+'.ready.json')), 'Scheduled preparation')
        assert not (prepared/'data/journal.json').exists() and not marker.exists()
        assert not (authority/'configuration-approval.json').exists()
        seal_hash = hashlib.sha256((prepared/'seal.json').read_bytes()).hexdigest()
        sealed = {p: p.read_bytes() for p in prepared.rglob('*') if p.is_file()}
        assert command('approve-web-configuration.php', prepared, state)['web_apply_provisioned'] is True
        assert command('authorize-entry-handover.php', prepared, entry)['already_applied'] is False
        install('preflight'); install('apply')
        def checked():
            if not finished('preflight', authority/'preflight.json'): return False
            value = json.loads((authority/'preflight.json').read_text())
            assert value['checks'] is not None and len(value['checks']) == 8 and all(value['checks'].values())
            return True
        wait_for(checked, 'Scheduled preflight')
        send(dict(root_approved=True)); expect('approved')
        assert json.loads((state/'state.json').read_text())['step'] == 4
        assert not (state/'apply-request.json').exists() and not (authority/'web-apply-started.json').exists()
        assert not (prepared/'data/journal.json').exists() and sql('SHOW TABLES') == ''
        send(dict(allow_submission=True)); expect('submitted')
        assert json.loads((state/'apply-request.json').read_text()) == json.loads((state/'apply-approval.json').read_text())
        wait_for(lambda: finished('apply', marker), 'Scheduled installation', 300)
        expect('completed', 90)
        assert json.loads(marker.read_text())['source'] == source
        assert json.loads(marker.read_text())['seal_sha256'] == seal_hash
        assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
        assert sql('SELECT COUNT(*) FROM admin_administrators') == '1'
        assert sql("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.bootstrap' AND result='success'") == '1'
        assert all(p.read_bytes() == raw for p, raw in sealed.items()), 'Execution changed the sealed plan'
        # Let any already-running observation finish. Do not stop the workers to manufacture retirement readiness.
        wait_for(lambda: all(status(kind)['ActiveState'] == 'inactive' for kind in kinds), 'Idle completed workers')
        retained = {p: p.read_bytes() for p in [*records, *state.iterdir(), prepared/'data/journal.json',
            prepared/'services.json', prepared/'site.json', public, *scheduler_bytes]
            if p.is_file()}
        result = command('retire-installer.php', prepared, entry/'web.json')
        assert result == dict(installer_retired=True, already_applied=False, installation_complete=True)
        assert all(p.read_bytes() == raw for p, raw in retained.items())
        assert default_pool.read_bytes() == default_raw
        for kind in kinds:
            timer = 'gojet-installer-'+kind+'.timer'
            assert ctl('show', timer, '--property=ActiveState', '--value') == 'inactive'
            assert ctl('show', timer, '--property=UnitFileState', '--value') == 'disabled'
        with socket.socket(socket.AF_UNIX) as probe:
            assert probe.connect_ex(str(entry/'fpm.sock')) != 0
        send(dict(installer_retired=True))
        stdout, stderr = process.communicate(timeout=60)
        assert process.returncode == 0 and not stdout and not stderr, 'Continuous browser post-retirement verification failed'
        evidence = json.loads((repo/'artifacts/v10/P21/payload/browser-journey/result.json').read_text())
        evidence.update(source=source, seal_sha256=seal_hash, empty_database_at_start=True,
            root_preparation_timer=True, root_apply_timer=True, root_approval_did_not_execute=True,
            migrations_applied=31, administrators_created=1, three_schedulers_retired=True,
            installer_socket_closed=True, private_records_preserved=True)
        (repo/'artifacts/v10/P21/payload/browser-journey/result.json').write_text(json.dumps(evidence)+'\n')
        # Cleanup is only for this successful disposable fixture, never an installer command.
        for path, raw in scheduler_bytes.items():
            assert path.read_bytes() == raw
            path.unlink()
        assert product.read_bytes() == (prepared/'nginx.conf').read_bytes()
        product.unlink(); ctl('reload', 'nginx.service')
        for path in units:
            assert path.read_bytes() == (prepared/'units'/path.name).read_bytes()
            ctl('stop', path.name); ctl('disable', path.name); path.unlink()
        ctl('daemon-reload')
        for path in records: path.unlink()
        assert json.loads(public.read_text())['result_id'] == hashlib.sha256(('gojet.installation-result.v1\n'+seal_hash).encode()).hexdigest()
        public.unlink()
    finally:
        if process.poll() is None:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=5)
        for pipe in [process.stdin, process.stdout, process.stderr]:
            if pipe is not None: pipe.close()
