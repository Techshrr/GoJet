"""Real installed payload upgrade/restore, with SIGKILL only in a disposable tool copy.

The release binaries are the compiled payload; the candidate fixture appends DDL
and changes static bytes. This is a transition test, not a new binary release or
owner-controlled production acceptance. No fault hook is shipped in PHP.
"""
import hashlib
from http.client import HTTPSConnection
import json
import os
from pathlib import Path
import shutil
import signal
import ssl
import subprocess

from catalog import catalog
from archive import encode
from native_prepared_services_test import SERVICES


def verify_upgrade(root, release, prepared, source, profile):
    authority = Path('/var/lib/gojet-upgrade-authority')
    gate = Path('/etc/nginx/gojet-upgrade-gate.conf')
    assert not authority.exists() and not authority.is_symlink()
    assert not gate.exists() and not gate.is_symlink()
    home = root/'upgrade-jobs'
    home.mkdir(mode=0o700)
    tools = home/'fault-tools'
    shutil.copytree(release/'installer', tools)
    implementation = tools/'src/NativeUpgrade.php'
    code = implementation.read_text()
    hook = "        UpgradeIO::record($job.'/state.json',$state);"
    assert hook in code
    code = code.replace(hook, hook+"\n        if (getenv('P21_STOP_PHASE')===$phase) { posix_kill(getmypid(),9); }", 1)
    pending = "                UpgradeIO::record($pending,['job'=>$job,'plan_sha256'=>$expectedPlan]);"
    assert pending in code
    code = code.replace(pending, pending+"\n                if (getenv('P21_STOP_PHASE')==='prepared') { posix_kill(getmypid(),9); }")
    implementation.write_text(code)
    configuration = profile['configuration']
    dbname = configuration['database']['name']
    administrator = dict(host='127.0.0.1', port=3306, name=dbname, user='root',
                         password=os.environ['P21_MYSQL_ADMIN_PASSWORD'])
    env = dict(PATH='/usr/sbin:/usr/bin:/sbin:/bin')
    digest = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
    targets = {Path('/etc/nginx/conf.d/gojet.conf'): (prepared/'nginx.conf').read_bytes()}
    targets.update({Path('/etc/systemd/system')/('gojet-'+n+'.service'):
                    (prepared/'units'/('gojet-'+n+'.service')).read_bytes() for n in SERVICES})
    marker = Path('/var/lib/gojet-installer-authority/installed.lock')
    installed = marker.read_bytes()
    storage = Path(configuration['files']['root'])
    evidence = storage/'published/upgrade-evidence'
    evidence.write_bytes(b'original\x00\xff\xe4\xb8\xad\xe6\x96\x87')
    evidence.chmod(0o640)
    os.chown(evidence, storage.stat().st_uid, storage.stat().st_gid)
    original = evidence.read_bytes()
    session_cookie = None

    def administrator_session(login=False):
        nonlocal session_cookie
        context=ssl.create_default_context(cafile=profile['tls']['certificate'])
        connection=HTTPSConnection('localhost',18443,context=context,timeout=10)
        try:
            headers={'Origin':configuration['origin'],'Content-Type':'application/json'}
            if login:
                body=json.dumps({k:profile['admin'][k] for k in ['email','password']})
                connection.request('POST','/api/admin/auth/login',body,headers)
            else:
                headers['Cookie']=session_cookie
                connection.request('GET','/api/admin/auth/session',headers=headers)
            response=connection.getresponse(); body=json.loads(response.read())
            assert response.status==200, 'Existing administrator/session stopped working'
            if login:
                session_cookie=response.getheader('Set-Cookie').split(';',1)[0]
            else:
                assert body['administrator']['email']==profile['admin']['email']
        finally:
            connection.close()

    def redis(*args):
        return subprocess.check_output(['redis-cli','--raw','-h','127.0.0.1','-p',str(configuration['redis']['port']),
            '-n',str(configuration['redis']['db']),*args], text=True, timeout=10,
            env=env | ({'REDISCLI_AUTH':configuration['redis']['password']} if configuration['redis']['password'] else {})).strip()

    def sql(query, selected=True):
        return subprocess.check_output(['mysql', '-h127.0.0.1', '-uroot', '-NB',
            *([dbname] if selected else []), '-e', query], text=True, timeout=30,
            env=env | {'MYSQL_PWD': administrator['password']}).strip()

    def ctl(*args):
        subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=60)

    def http(path='/', token=None, forwarded=False):
        # Do not trust environment proxies or disable certificate verification.
        args = ['curl', '--silent', '--show-error', '--noproxy', '*', '--max-time', '10',
                '--cacert', profile['tls']['certificate'], '--resolve', 'localhost:18443:127.0.0.1',
                '-o', '/dev/null', '-w', '%{http_code}']
        if token is not None:
            args += ['-H', 'X-GoJet-Upgrade-Probe: '+token]
        if forwarded:
            args += ['-H', 'X-Forwarded-For: 127.0.0.1', '-H', 'X-Real-IP: 127.0.0.1']
        return subprocess.check_output(args+[configuration['origin']+path], text=True, timeout=15)

    def candidate(name, failing=False):
        dest = root/name
        shutil.copytree(release, dest)
        migration = 'CREATE TABLE gojet_upgrade_added (id INT PRIMARY KEY) ENGINE=InnoDB;\n'
        if failing:
            migration += "SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='disposable migration failure';\n"
        (dest/'migrations/000032_upgrade_fixture.sql').write_text(migration)
        (dest/'migration-catalog.json').write_bytes(encode(catalog(dest/'migrations')))
        with (dest/'static/site/index.html').open('ab') as stream:
            stream.write(b'\n<!-- isolated upgrade candidate -->\n')
        m = json.loads((dest/'manifest.json').read_text())
        for path in [dest/'migrations/000032_upgrade_fixture.sql', dest/'migration-catalog.json', dest/'static/site/index.html']:
            m['files'][str(path.relative_to(dest))] = dict(bytes=path.stat().st_size, mode=0o644, sha256=digest(path))
        (dest/'manifest.json').write_bytes(encode(m))
        names = sorted([*m['files'], 'manifest.json', 'sbom.cdx.json'])
        (dest/'SHA256SUMS').write_text(''.join(digest(dest/n)+'  '+n+'\n' for n in names))
        return dest

    def prepare(name, dest):
        job = home/name
        command = ['php8.3', str(release/'installer/bin/prepare-upgrade.php'), str(prepared),
                   str(dest), digest(dest/'manifest.json'), source, str(job)]
        r = subprocess.run(command, input=json.dumps(administrator).encode(), capture_output=True, timeout=180, env=env)
        assert r.returncode == 0 and not r.stderr, 'Upgrade preparation failed: '+name
        result = json.loads(r.stdout)
        assert result['prepared'] and not result['upgrade_complete']
        return job, result['plan_sha256']

    def execute(job, binding, recovery=False, stop=None, refused=False):
        name = 'recover-upgrade.php' if recovery else 'run-upgrade.php'
        command = ['php8.3', str((tools if stop else release/'installer')/'bin'/name), str(job), binding]
        r = subprocess.run(command, capture_output=True, timeout=240, env=env | ({'P21_STOP_PHASE': stop} if stop else {}))
        if stop:
            assert r.returncode == -signal.SIGKILL and not r.stdout, 'Fault checkpoint not reached: '+stop
            return
        if refused:
            assert r.returncode == 1 and not r.stdout
            assert r.stderr == b'Upgrade operation refused; preserve the private plan and recovery state.\n'
            return
        assert r.returncode == 0 and not r.stderr, 'Upgrade execution/recovery failed: '+job.name+' phase='+json.loads((job/'state.json').read_text())['phase']
        return json.loads(r.stdout)

    def restored():
        assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '31'
        assert sql("SHOW TABLES LIKE 'gojet_upgrade_added'") == ''
        assert sql('SELECT HEX(value) FROM gojet_upgrade_evidence WHERE id=1') == '00FF0102'
        assert evidence.read_bytes() == original and evidence.stat().st_mode & 0o777 == 0o640
        assert not (storage/'published/candidate-only').exists()
        assert http() == '200' and not (authority/'pending.json').exists()
        assert marker.read_bytes() == installed

    stopped = []
    try:
        administrator_session(login=True)
        sql('CREATE TABLE gojet_upgrade_evidence(id INT PRIMARY KEY,value LONGBLOB) ENGINE=InnoDB; '
            'INSERT INTO gojet_upgrade_evidence VALUES(1,0x00FF0102)')
        good = candidate('upgrade-candidate')
        bad = candidate('upgrade-failing-candidate', True)
        # A real DDL failure leaves the candidate table and failed migration journal.
        job, binding = prepare('ddl-failure', bad)
        execute(job, binding, refused=True)
        assert json.loads((job/'state.json').read_text())['phase'] == 'migrating'
        assert sql("SELECT status FROM gojet_schema_migrations WHERE id=32") == 'failed'
        assert sql("SHOW TABLES LIKE 'gojet_upgrade_added'") == 'gojet_upgrade_added'
        # Recovery must depend on the sealed old package, not broken candidate bytes.
        with (bad/'static/site/index.html').open('ab') as stream:
            stream.write(b'corrupt failed candidate')
        execute(job, binding, recovery=True)
        restored()

        for phase in ['prepared', 'stopping', 'adopting', 'backing-up', 'backed-up',
                      'rehearsing', 'migrating', 'switching', 'starting', 'verifying']:
            job, binding = prepare('kill-'+phase, good)
            execute(job, binding, stop=phase)
            assert json.loads((job/'state.json').read_text())['phase'] == phase
            assert (authority/'pending.json').exists()
            # Ordinary retry never quietly resumes an interrupted transaction.
            if phase != 'prepared':
                execute(job, binding, refused=True)
            if phase in ['backed-up', 'verifying']:
                assert http() == http(forwarded=True) == http(token='0'*64) == '503'
                assert http(token=(authority/'probe-token').read_text()) == '200'
            if phase in ['migrating', 'switching', 'starting', 'verifying']:
                sql('UPDATE gojet_upgrade_evidence SET value=0xDEADBEEF WHERE id=1')
                evidence.write_bytes(b'candidate modified existing object')
                (storage/'published/candidate-only').write_bytes(b'candidate-only')
                assert redis('SET','risk:link:987654321:'+('a'*64),'candidate projection')=='OK'
                assert redis('SET','gojet:upgrade:unrelated-fixture','retain','EX','600')=='OK'
            if phase == 'verifying':
                execute(job, binding, recovery=True, stop='restoring')
                sql('DROP DATABASE `'+dbname+'`', selected=False)
                # Interrupted DROP/import is recovered from the same original backup.
            if phase == 'switching':
                execute(job, binding, recovery=True, stop='recovery-committed')
                # Once committed, recovery cleanup must retain subsequent writes.
                sql('INSERT INTO gojet_upgrade_evidence VALUES(2,0xAA)')
                execute(job, binding, recovery=True, stop='recovered')
                assert sql('SELECT HEX(value) FROM gojet_upgrade_evidence WHERE id=2') == 'AA'
            result = execute(job, binding, recovery=True)
            assert result['recovered'] and not result['upgrade_complete']
            restored()
            if phase in ['migrating', 'switching', 'starting', 'verifying']:
                assert redis('EXISTS','risk:link:987654321:'+('a'*64))=='0'
                assert redis('GET','gojet:upgrade:unrelated-fixture')=='retain'
            stopped.append(phase)

        job, binding = prepare('commit-upgrade', good)
        execute(job, binding, stop='committed')
        assert sql("SELECT COUNT(*) FROM gojet_schema_migrations WHERE status='applied'") == '32'
        sql('INSERT INTO gojet_upgrade_evidence VALUES(3,0xBB)')
        evidence.write_bytes(b'new release committed data')
        execute(job, binding, recovery=True, stop='complete')
        result = execute(job, binding, recovery=True)
        assert result['upgrade_complete'] and not result['recovered']
        assert sql('SELECT HEX(value) FROM gojet_upgrade_evidence WHERE id=3') == 'BB'
        assert evidence.read_bytes() == b'new release committed data'
        assert (authority/'active').resolve() == job/'after'
        assert http() == '200' and not (authority/'pending.json').exists()
        assert execute(job, binding)['upgrade_complete']
        administrator_session()
        assert sql("SELECT COUNT(*) FROM admin_audit_events WHERE action='admin.auth.login' AND result='success'")=='1'
        assert marker.read_bytes() == installed
        return dict(native_upgrade=True, native_database_required_recovery=True,
                    interrupted_phases=stopped+['restoring', 'recovery-committed', 'recovered', 'committed', 'complete'],
                    real_partial_ddl_recovered=True, missing_schema_recovered=True,
                    maintenance_requires_private_capability=True, loopback_proxy_bypass_refused=True,
                    committed_data_preserved=True, first_install_authority_unchanged=True,
                    existing_administrator_session_preserved=True, login_audit_preserved=True,
                    stale_risk_projections_removed=True, unrelated_redis_data_preserved=True,
                    candidate_uses_same_compiled_binaries=True, production_acceptance=False)
    finally:
        # Only fixtures created here are cleaned; never upload plans/credentials or SQL.
        ctl('stop', *['gojet-'+n+'.service' for n in SERVICES])
        for target, raw in targets.items():
            if target.is_symlink():
                assert str(target.readlink()).startswith(str(authority/'active')+'/')
                target.unlink()
                target.write_bytes(raw)
                target.chmod(0o644)
            else:
                assert target.read_bytes() == raw
        if gate.exists():
            gate.unlink()
        if authority.exists():
            shutil.rmtree(authority)
        ctl('daemon-reload')
        ctl('reload', 'nginx.service')
