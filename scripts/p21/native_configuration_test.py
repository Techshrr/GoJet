"""Typed configuration to private files to actual PID1 and platformapi startup."""
import copy
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import subprocess
import time
import urllib.error
import urllib.request


def verify_configuration(repo, root, platform_binary, bootstrap):
    account = pwd.getpwnam('nobody')
    storage = root/'configured-storage'
    storage.mkdir(mode=0o700)
    os.chown(storage, account.pw_uid, account.pw_gid)
    p = dict(schema='gojet.native-configuration.v1', origin=bootstrap['origin'],
        database={k: v for k, v in bootstrap['database'].items() if k != 'host'},
        redis={k: v for k, v in bootstrap['redis'].items() if k != 'host'},
        secrets=dict(key_id=bootstrap['admin_cipher']['id'], admin=bootstrap['admin_cipher']['key_hex'],
                     **{k: secrets.token_hex(32) for k in ['csrf', 'grant', 'oauth', 'file_public', 'text_public']}),
        files=dict(root=str(storage), clamav_socket='/run/clamav/clamd.ctl', max_upload_bytes=1048576,
                   max_files=100, max_bytes=10485760, types={'pdf': ['application/pdf'], 'png': ['image/png']}),
        quotas=dict(text=100, bio=100, qr=100),
        smtp=dict(host='localhost', port=2525, **{'from': 'native@p21.test'}, username='', password=''),
        risk=dict(name='native-config-probe', endpoint='https://unused.invalid/check', policy_version='native-v1'),
        turnstile_secret=secrets.token_urlsafe(32), auth_rate=dict(limit=3, window_seconds=60))
    destination = root/'configured-environment'
    command = ['php8.3', str(repo/'installer/bin/configure-services.php'), str(destination)]
    def call(value, nonroot=False):
        return subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            input=json.dumps(value).encode(), capture_output=True, timeout=15, cwd=root,
            env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin'})
    def refused(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Native service configuration refused.\n'
        assert not destination.exists()
    refused(call(p, True))
    for section, name, value in [
        (None, 'origin', 'http://site.test'), (None, 'origin', 'https://site.test/path'),
        (None, 'GOJET_TEST_AUTH_ENABLED', '1'), ('database', 'port', True),
        ('database', 'name', 'db?multiStatements=true'), ('database', 'password', 'x\nBAD=1'),
        ('redis', 'db', -1), ('redis', 'db', 16), ('redis', 'port', p['database']['port']),
        ('secrets', 'csrf', p['secrets']['admin']), ('secrets', 'grant', '0'*64),
        ('files', 'root', '/tmp/../etc'), ('files', 'max_bytes', 1),
        ('files', 'types', {'php': ['text/plain;evil']}), ('files', 'max_upload_bytes', 1073741824),
        ('smtp', 'username', 'incomplete'), ('smtp', 'from', 'not-an-email'),
        ('risk', 'endpoint', 'https://user:secret@site.test/'), ('quotas', 'qr', 100001),
        ('auth_rate', 'limit', 0), ('auth_rate', 'limit', 1001), ('auth_rate', 'limit', True),
        ('auth_rate', 'window_seconds', 0), ('auth_rate', 'window_seconds', 86401)]:
        bad = copy.deepcopy(p)
        (bad if section is None else bad[section])[name] = value
        refused(call(bad))
    result = call(p)
    assert result.returncode == 0 and not result.stderr, 'Configuration creation refused'
    assert json.loads(result.stdout) == dict(created=True, files=8, installation_complete=False,
                                            configuration_validated=True, max_body_bytes=2097152)
    # Compile in the root test harness only to calculate nonsecret expectation digests.
    code = 'require '+json.dumps(str(repo/'installer/src/NativeConfiguration.php'))+';'
    code += 'echo json_encode(GoJet\\Installer\\NativeConfiguration::compile(json_decode(stream_get_contents(STDIN),true)));'
    compiled = subprocess.run(['php8.3', '-r', code], input=json.dumps(p).encode(), capture_output=True, check=True)
    config = json.loads(compiled.stdout)
    values = config['services']
    assert len(values) == 8
    for name in ['platformapi', 'redirectengine', 'analyticsworker', 'analyticsreconciler', 'operationsmonitor']:
        assert values[name]['GOJET_REDIS_DB'] == str(p['redis']['db'])
        assert values[name]['GOJET_MYSQL_DSN'] == values['platformapi']['GOJET_MYSQL_DSN']
    assert set(values['logreceiver']) == {'GOJET_LOG_SOCKET'}
    for name in ['mailworker', 'fileworker', 'operationsmonitor']:
        assert 'GOJET_ADMIN_TOTP_KEY_HEX' not in values[name] and 'GOJET_AUTH_CSRF_KEY_HEX' not in values[name]
    expected = root/'configured-expectations.json'
    expected.write_text(json.dumps({name: {k: hashlib.sha256(v.encode()).hexdigest() for k,v in row.items()}
                                    for name,row in values.items()}))
    expected.chmod(0o644)
    probe = root/'configured-probe.py'
    probe.write_text('import os,sys,json,hashlib\nassert os.geteuid()!=0\n'
                     'expected=json.load(open(sys.argv[1]))[sys.argv[2]]\n'
                     'actual={k:hashlib.sha256(v.encode()).hexdigest() for k,v in os.environ.items() if k.startswith("GOJET_")}\n'
                     'assert actual==expected\nprint("verified")\n')
    probe.chmod(0o644)
    for name in values:
        path = destination/(name+'.env')
        assert path.stat().st_uid == 0 and path.stat().st_mode & 0o777 == 0o600
        run = subprocess.run(['systemd-run', '--quiet', '--wait', '--pipe', '--collect',
            '--unit=p21-config-'+secrets.token_hex(5), '--property=User=nobody',
            '--property=EnvironmentFile='+str(path), '/usr/bin/python3', str(probe), str(expected), name],
            capture_output=True, timeout=20)
        assert run.returncode == 0 and run.stdout.strip() == b'verified', 'Typed environment PID1 mismatch'
    # Actual compiled Go API consumes the unmodified generated file under a non-root UID.
    unit = 'p21-config-api-'+secrets.token_hex(5)
    def request(path, data=None):
        try:
            req = urllib.request.Request('http://127.0.0.1:8081'+path, data=data,
                headers={'Content-Type': 'application/json', 'Origin': p['origin']})
            response = urllib.request.urlopen(req, timeout=5)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.read()
    try:
        subprocess.run(['systemd-run', '--quiet', '--unit='+unit, '--property=User=nobody',
            '--property=EnvironmentFile='+str(destination/'platformapi.env'), platform_binary],
            check=True, capture_output=True, timeout=10)
        last_status = None
        for _ in range(100):
            try:
                last_status = request('/api/public/auth/providers')[0]
                if last_status == 200:
                    break
            except OSError:
                pass
            time.sleep(.1)
        else:
            # Retain only source-owned startup event names and numeric process status.
            # Never emit raw journals, configuration, response bodies or driver errors.
            raw = subprocess.check_output(['journalctl', '-u', unit, '--no-pager', '-o', 'cat'], text=True)
            allowed = {'required configuration missing', 'open mysql', 'mysql unavailable', 'redis unavailable',
                'configure Links authentication authority', 'configure Domains authentication authority',
                'configure Analytics authentication authority', 'configure QR authentication authority',
                'configure files', 'configure Text Sharing', 'configure Bio', 'configure Workspace organization',
                'configure Billing, Payments and Entitlements', 'configure Support Tickets and Mail',
                'configure Authentication, OAuth and Account', 'configure Authentication rate protection',
                'configure Authentication challenge protection', 'configure Account settings', 'configure Admin OAuth',
                'configure Trust, Destination Risk and Abuse', 'configure Admin Access and Audit',
                'platformapi listening', 'platformapi failed'}
            events = []
            for line in raw.splitlines():
                try:
                    message = json.loads(line).get('msg')
                except (ValueError, AttributeError):
                    continue
                if message in allowed:
                    events.append(message)
            exit_status = subprocess.check_output(['systemctl', 'show', unit, '--property=ExecMainStatus', '--value'], text=True).strip()
            assert exit_status.isdigit()
            raise AssertionError('Actual API readiness failed: '+json.dumps(dict(http_status=last_status,
                exit_status=int(exit_status), startup_events=events)))
        for path in ['/api/workspaces/1/files', '/api/workspaces/1/text-shares', '/api/workspaces/1/bio-pages',
                     '/api/workspaces/1/domains', '/api/workspaces/1/qr-codes']:
            assert request(path)[0] in [401, 403], 'Generated configuration bypassed inherited authentication'
        login = json.dumps(dict(email='native-rate-probe@p21.test', password=secrets.token_urlsafe(24))).encode()
        statuses = [request('/api/auth/login', login)[0] for _ in range(4)]
        assert all(status in [400, 401, 403] for status in statuses[:3]) and statuses[3] == 429, 'Configured authentication rate limit not enforced'
        logs = subprocess.check_output(['journalctl', '-u', unit, '--no-pager', '-o', 'cat'], text=True)
        rows = [json.loads(line) for line in logs.splitlines() if line.startswith('{')]
        ready = [r for r in rows if r.get('msg') == 'platformapi listening']
        assert ready and all(ready[-1][k+'_enabled'] for k in ['analytics', 'files', 'text', 'bio', 'workspace',
                        'billing', 'support', 'auth', 'account', 'admin_oauth', 'trust', 'admin_access'])
        for secret in p['secrets'].values():
            if len(secret) == 64:
                assert secret not in logs and secret.encode() not in result.stdout
        assert p['database']['password'] not in logs
    finally:
        subprocess.run(['systemctl', 'stop', unit], capture_output=True, timeout=15)
        subprocess.run(['systemctl', 'reset-failed', unit], capture_output=True, timeout=10)
    assert not list(root.rglob('installed.lock'))
