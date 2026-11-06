"""Disposable root TLS fixture for T024; real P09 probes, no install/apply claim.

Only the two starting phases are fixtures. Health comes from the shipped native
helper as the same unprivileged user as platformapi. Unchecked P21 dependencies
remain false; this test must never manufacture whole-install readiness.
"""
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import selectors
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]


def command(args, **kwargs):
    result = subprocess.run(args, capture_output=True, text=True, timeout=20, **kwargs)
    if result.returncode:
        raise RuntimeError('T024 native command failed: '+Path(args[0]).name)
    return result.stdout


def main():
    assert os.geteuid() == 0, 'T024 fixture requires disposable runner root'
    cfg = json.load(sys.stdin)
    runner = pwd.getpwuid(cfg['uid'])
    assert runner.pw_uid > 0
    account = pwd.getpwnam('gojet-installer')
    processes = []
    browser = None
    with tempfile.TemporaryDirectory(prefix='gojet-p09-tls-') as temporary:
        base = Path(temporary)
        base.chmod(0o755)
        release = base/'release'
        shutil.copytree(ROOT/'installer', release/'installer')
        css = release/'frontend/packages/tokens/generated'
        css.mkdir(parents=True)
        shutil.copy2(ROOT/'frontend/packages/tokens/generated/tokens.css', css/'tokens.css')
        cert, key = base/'cert.pem', base/'key.pem'
        command(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                 '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1',
                 '-keyout', str(key), '-out', str(cert)])
        tls = ssl.create_default_context(cafile=str(cert))

        def php(file, code, data):
            program = 'require '+json.dumps(str(release/'installer/src'/file))+';'
            program += '$v=json_decode(stream_get_contents(STDIN),true,16,JSON_THROW_ON_ERROR);'+code
            args = ['php8.3', '-r', program]
            return command(args, input=json.dumps(data), cwd=base)

        def request(origin, route='/install/'):
            try:
                response = urllib.request.urlopen(urllib.request.Request(origin+route,
                    headers={'Accept': 'application/json'}), context=tls, timeout=3)
            except urllib.error.HTTPError as error:
                response = error
            with response:
                return response.status, json.load(response)

        fixtures = {}
        servers = []
        ports = set()
        try:
            for phase in ['environment', 'services']:
                directory = base/phase
                directory.mkdir(mode=0o755)
                state, authority = directory/'private', directory/'authority'
                state.mkdir(mode=0o700)
                authority.mkdir(mode=0o755)
                token = secrets.token_hex(32)
                digest = hashlib.sha256(token.encode()).hexdigest()
                method = 'initialize' if phase == 'environment' else 'initializePreparedApply'
                # Prepared phase is explicit test setup, not an approved/sealed installation.
                php('InstallState.php', '(new GoJet\\Installer\\InstallState($v[0],$v[1]))->'+method+'();',
                    [str(state), digest])
                for member in state.iterdir():
                    os.chown(member, account.pw_uid, account.pw_gid)
                os.chown(state, account.pw_uid, account.pw_gid)
                while True:
                    with socket.socket() as sock:
                        sock.bind(('127.0.0.1', 0))
                        port = sock.getsockname()[1]
                    if port not in ports:
                        ports.add(port)
                        break
                origin = f'https://127.0.0.1:{port}'
                settings = dict(origin=origin, release=str(release), state=str(state), bootstrap_sha256=digest,
                    certificate=str(cert), private_key=str(key), socket=str(directory/'fpm.sock'),
                    completion=str(authority/'installed.lock'))
                configs = json.loads(php('NativeWebConfig.php',
                    'echo json_encode(GoJet\\Installer\\NativeWebConfig::render($v),JSON_THROW_ON_ERROR);', settings))
                fpm = directory/'fpm.conf'
                fpm.write_text('[global]\npid = '+str(directory/'fpm.pid')+'\nerror_log = '+str(directory/'fpm.log')+'\n'+configs['fpm'])
                command(['php-fpm8.3', '-t', '-y', str(fpm)])
                processes.append(subprocess.Popen(['php-fpm8.3', '-F', '-y', str(fpm)],
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
                servers.append(configs['nginx'])
                fixtures[phase] = dict(origin=origin, token=token, state=state, authority=authority)
            nginx = base/'nginx.conf'
            nginx.write_text('user www-data;\npid '+str(base/'nginx.pid')+';\nerror_log '+str(base/'nginx.log')+';\n'
                'events {}\nhttp { access_log off;\n'+''.join(servers)+'}\n')
            command(['nginx', '-t', '-c', str(nginx), '-p', str(base)])
            processes.append(subprocess.Popen(['nginx', '-c', str(nginx), '-p', str(base), '-g', 'daemon off;'],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
            for fixture in fixtures.values():
                for attempt in range(100):
                    try:
                        status, body = request(fixture['origin'])
                        assert status == 401 and body == {'error': 'Installer authentication required'}
                        break
                    except OSError:
                        if any(p.poll() is not None for p in processes):
                            raise RuntimeError('T024 native web process exited')
                        time.sleep(.1)
                else:
                    raise RuntimeError('T024 certificate-verified TLS readiness failed')
            service = fixtures['services']
            storage = Path(cfg['environment']['GOJET_FILE_STORAGE_ROOT'])
            binary = cfg['environment']['GOJET_FILE_PREFLIGHT_BIN']
            probe_env = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', **cfg['environment']}
            # Reserve a loopback port without listening: deterministically refused connections.
            with socket.socket() as fault:
                fault.bind(('127.0.0.1', 0))
                down = '127.0.0.1:'+str(fault.getsockname()[1])

                def observe(mode):
                    if mode == 'missing':
                        php('PreflightSnapshot.php', 'GoJet\\Installer\\PreflightSnapshot::invalidate($v);', str(service['authority']))
                        return None
                    started = int(time.time())
                    original_mode = storage.stat().st_mode & 0o777
                    try:
                        if mode == 'unsafe':
                            storage.chmod(0o755)
                        env = probe_env | ({'GOJET_CLAMAV_ADDRESS': down} if mode == 'down' else {})
                        result = subprocess.run(['runuser', '-u', runner.pw_name, '--', binary],
                            env=env, cwd=base, capture_output=True, text=True, timeout=15)
                        assert result.returncode in (0, 2) and not result.stderr
                        health = json.loads(result.stdout)
                        storage_ok = health['storage']['state'] == 'healthy' and health['storage']['writable'] is True
                        clamav_ok = health['clamav']['state'] == 'healthy' and bool(health['clamav'].get('engine_version')) and bool(health['clamav'].get('signature_version'))
                        assert (storage_ok, clamav_ok) == {'healthy': (True, True), 'down': (True, False),
                            'unsafe': (False, True), 'stale': (True, True)}[mode], 'Unexpected real P09 probe result'
                        assert health['ready'] == (storage_ok and clamav_ok)
                        assert result.returncode == (0 if health['ready'] else 2)
                    finally:
                        storage.chmod(original_mode)
                    checks = dict.fromkeys(['package', 'systemd', 'nginx', 'php', 'mysql', 'redis'], False)
                    checks.update(storage=storage_ok, clamav=clamav_ok)
                    return json.loads(php('PreflightSnapshot.php',
                        'echo json_encode(GoJet\\Installer\\PreflightSnapshot::publish($v[0],$v[1],$v[2],$v[3]),JSON_THROW_ON_ERROR);',
                        [str(service['authority']), str(service['state']), started-(121 if mode == 'stale' else 0), checks]))

                observe('healthy')
                child_env = os.environ | cfg['environment'] | {'CHROME_BIN': cfg['chrome']}
                browser = subprocess.Popen(['runuser', '-u', runner.pw_name, '--', cfg['node'],
                    str(ROOT/'scripts/p09/browser_installer.mjs')], cwd=ROOT, env=child_env,
                    stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=sys.stderr, text=True)

                def send(value):
                    browser.stdin.write(json.dumps(value)+'\n')
                    browser.stdin.flush()

                send({phase: {key: value[key] for key in ['origin', 'token']} for phase, value in fixtures.items()})
                selector = selectors.DefaultSelector()
                selector.register(browser.stdout, selectors.EVENT_READ)
                evidence = None
                deadline = time.monotonic()+120
                try:
                    while time.monotonic() < deadline:
                        if not selector.select(timeout=1):
                            continue
                        line = browser.stdout.readline()
                        if not line:
                            break
                        message = json.loads(line)
                        operation = message['op']
                        if operation == 'done':
                            evidence = message['evidence']
                            break
                        assert operation in ['healthy', 'down', 'unsafe', 'stale', 'missing', 'complete']
                        if operation == 'complete':
                            for fixture in fixtures.values():
                                (fixture['authority']/'installed.lock').touch(mode=0o644)
                            send({'closed': True})
                        else:
                            send(observe(operation))
                    assert evidence is not None, 'T024 browser did not produce evidence'
                    assert browser.wait(timeout=10) == 0
                finally:
                    selector.close()
            for phase, fixture in fixtures.items():
                state = json.loads((fixture['state']/'state.json').read_text())
                assert state['step'] == (0 if phase == 'environment' else 4) and state['status'] == 'ready'
                assert not (fixture['state']/'apply-request.json').exists()
                assert not (fixture['state']/'configuration-draft.json').exists()
            print(json.dumps(evidence | {'certificate_verified_tls': True, 'real_unprivileged_filepreflight': True,
                'starting_phases_are_fixtures': True, 'unchecked_p21_dependencies_are_false': True,
                'privileged_apply_verified': False, 'state_not_advanced': True}))
        finally:
            if browser is not None and browser.poll() is None:
                browser.terminate()
                try:
                    browser.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    browser.kill()
                    browser.wait(timeout=5)
            for process in reversed(processes):
                process.terminate()
            for process in reversed(processes):
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=5)


if __name__ == '__main__':
    main()
