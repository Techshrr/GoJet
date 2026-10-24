"""Real browser/TLS submission -> root timer apply, plus adversarial HTTP-adapter checks."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import subprocess
import time
from native_browser_apply import submit_browser, observe_browser


def verify_preflight_schedule(release, authority, account):
    render = ['php8.3', str(release/'installer/bin/render-web-preflight-schedule.php')]
    assert subprocess.run(['runuser', '-u', 'gojet-installer', '--']+render, capture_output=True, timeout=30).returncode == 1
    result = subprocess.run(render, capture_output=True, timeout=120)
    assert result.returncode == 0 and not result.stderr
    units = json.loads(result.stdout)
    names = ['gojet-installer-preflight.service', 'gojet-installer-preflight.timer']
    assert list(units) == names
    assert f'ExecStart=/usr/bin/php8.3 {release}/installer/bin/refresh-web-preflight.php' in units[names[0]]
    assert 'run-web-apply' not in result.stdout.decode()
    paths = [Path('/etc/systemd/system')/name for name in names]
    assert all(not p.exists() and not p.is_symlink() for p in paths)
    marker = authority/'installed.lock'
    assert not marker.exists()
    snapshot = authority/'preflight.json'
    install = ['php8.3', str(release/'installer/bin/install-web-preflight-schedule.php')]
    started = authority/'preflight-schedule-started.json'
    ready = authority/'preflight-schedule-ready.json'
    assert not started.exists() and not ready.exists()
    denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+install, capture_output=True, timeout=30)
    assert denied.returncode == 1 and not started.exists() and all(not p.exists() for p in paths)
    # Existing operator configuration must remain byte-identical, without a provisioning receipt.
    paths[0].write_text('# existing operator unit\n'); paths[0].chmod(0o644)
    try:
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert paths[0].read_text() == '# existing operator unit\n' and not started.exists()
    finally:
        paths[0].unlink()
    # Dangling links and orphan ready receipts are also operator-owned conflicts.
    paths[0].symlink_to('/nonexistent-gojet-scheduler-fixture')
    try:
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert paths[0].is_symlink() and os.readlink(paths[0]) == '/nonexistent-gojet-scheduler-fixture'
        assert not started.exists() and not paths[1].exists()
    finally:
        paths[0].unlink()
    ready.write_text('fixture orphan ready receipt'); ready.chmod(0o600)
    try:
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert ready.read_text() == 'fixture orphan ready receipt'
        assert not started.exists() and all(not p.exists() for p in paths)
    finally:
        ready.unlink()
    # A simulated interrupted attempt blocks new writes; only this injected fixture is removed.
    started.write_text('fixture interruption barrier'); started.chmod(0o600)
    try:
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert started.read_text() == 'fixture interruption barrier' and all(not p.exists() for p in paths)
    finally:
        started.unlink()
    def ctl(*args):
        return subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=30)
    def observe(after, redis):
        deadline = time.monotonic()+100
        while time.monotonic() < deadline:
            try:
                value = json.loads(snapshot.read_text())
                if snapshot.stat().st_mtime_ns > after and value['checks'] is not None and value['checks']['redis'] is redis:
                    return value
            except (FileNotFoundError, json.JSONDecodeError):
                pass  # Refresh intentionally removes the old snapshot before probing.
            time.sleep(0.5)
        raise AssertionError('Native timer did not publish expected dependency observation')
    try:
        before = snapshot.stat().st_mtime_ns
        result = subprocess.run(install, capture_output=True, timeout=180)
        assert result.returncode == 0 and not result.stderr, 'Root scheduler provisioning failed'
        assert json.loads(result.stdout) == dict(scheduler_enabled=True, installation_complete=False)
        for p in paths:
            assert p.read_text() == units[p.name] and p.stat().st_uid == 0 and p.stat().st_mode & 0o777 == 0o644
        assert started.stat().st_uid == 0 and started.stat().st_mode & 0o777 == 0o600
        assert json.loads(ready.read_text()) == dict(schema=1, scheduler_enabled=True)
        receipt = started.read_bytes()
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert started.read_bytes() == receipt
        assert ctl('is-enabled', names[1]).stdout.strip() == b'enabled'
        assert all(observe(before, True)['checks'].values()), 'Scheduled preflight must verify every dependency'
        assert ctl('is-active', names[1]).stdout.strip() == b'active'
        # A subsequent tick, not a direct service invocation, must detect the real outage.
        ctl('stop', 'redis-server.service')
        try:
            failed = observe(snapshot.stat().st_mtime_ns, False)
            assert failed['checks']['package'] is True
        finally:
            ctl('start', 'redis-server.service')
        assert all(observe(snapshot.stat().st_mtime_ns, True)['checks'].values()), 'Recovered preflight must verify every dependency'
        ctl('stop', names[1], names[0])
        original = snapshot.read_bytes()
        # Fixture completion lock proves the service condition skips even explicit starts.
        with marker.open('x') as f:
            f.write('')
        try:
            ctl('start', names[0])
            assert snapshot.read_bytes() == original
            assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
        finally:
            marker.unlink()
    finally:
        ctl('disable', '--now', names[1])
        ctl('stop', names[0])
        for p in paths:
            if p.exists():
                assert p.read_text() == units[p.name]
                p.unlink()
        ctl('daemon-reload')


def diagnose_scheduled_file_probe(release, destination, unit):
    """CI-only reproduction; emit enums/booleans, never raw helper or PAM output."""
    plan = json.loads((destination/'dependencies.json').read_text())
    properties = [line for line in unit.splitlines() if line.split('=', 1)[0] in {
        'User', 'Group', 'UMask', 'NoNewPrivileges', 'ProtectHome', 'ProtectKernelTunables',
        'ProtectKernelModules', 'ProtectControlGroups', 'RestrictSUIDSGID'}]
    command = ['/usr/bin/systemd-run', '--quiet', '--wait', '--pipe', '--collect']
    for value in properties:
        command += ['--property='+value]
    command += ['/usr/bin/env', '-i', 'PATH=/usr/sbin:/usr/bin:/sbin:/bin',
        'GOJET_FILE_STORAGE_ROOT='+plan['storage'], 'GOJET_CLAMAV_NETWORK=unix',
        'GOJET_CLAMAV_ADDRESS='+plan['clamav_socket'], '/usr/sbin/runuser', '-u', 'gojet', '--']
    results = {}
    for name, executable in [('identity', '/usr/bin/id'), ('health', str(release/'bin/filepreflight'))]:
        result = subprocess.run(command+[executable], capture_output=True, text=True, timeout=30)
        evidence = {'exit': result.returncode, 'stderr_present': bool(result.stderr)}
        evidence['errors'] = [label for label, phrase in {
            'groups': 'cannot set groups', 'execute': 'failed to execute',
            'permission': 'Permission denied', 'operation': 'Operation not permitted',
            'pam': 'pam', 'namespace': 'mount namespacing',
        }.items() if phrase in result.stderr]
        if name == 'identity':
            evidence['gojet_uid'] = result.stdout.startswith('uid='+str(pwd.getpwnam('gojet').pw_uid)+'(')
        else:
            try:
                report = json.loads(result.stdout)
                for key in ['storage', 'clamav']:
                    state = report.get(key, {}).get('state')
                    evidence[key] = state if state in {'healthy', 'unavailable', 'permission_error', 'stale', 'indeterminate'} else 'invalid'
            except (ValueError, AttributeError):
                evidence['health_json'] = False
        results[name] = evidence
    return results


def run_scheduled_apply(release, authority, account, state, destination, source):
    """Actual PID1 timer -> shipped root worker; no direct worker call for the successful apply."""
    render = ['php8.3', str(release/'installer/bin/render-web-apply-schedule.php')]
    install = ['php8.3', str(release/'installer/bin/install-web-apply-schedule.php')]
    names = ['gojet-installer-apply.service', 'gojet-installer-apply.timer']
    paths = [Path('/etc/systemd/system')/name for name in names]
    started = authority/'apply-schedule-started.json'
    ready = authority/'apply-schedule-ready.json'
    receipt = authority/'web-apply-started.json'
    marker = authority/'installed.lock'
    assert not any(p.exists() or p.is_symlink() for p in [*paths, started, ready, receipt, marker])
    denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+render, capture_output=True, timeout=30)
    assert denied.returncode == 1
    rendered = subprocess.run(render, capture_output=True, timeout=120)
    assert rendered.returncode == 0 and not rendered.stderr
    units = json.loads(rendered.stdout)
    assert list(units) == names
    assert f'ExecStart=/usr/bin/php8.3 {release}/installer/bin/run-web-apply.php' in units[names[0]]
    assert f'ConditionPathExists={state}/apply-request.json' in units[names[0]]
    assert f'ConditionPathExists=!{receipt}' in units[names[0]]
    assert f'ConditionPathExists=!{marker}' in units[names[0]]
    assert 'Restart=' not in units[names[0]] and 'KillMode=control-group' in units[names[0]]
    for command in [install+['unexpected'], ['runuser', '-u', 'gojet-installer', '--']+install]:
        assert subprocess.run(command, capture_output=True, timeout=30).returncode == 1
        assert not started.exists() and all(not p.exists() for p in paths)
    # Refuse existing operator files and interruption evidence without enabling a worker.
    paths[0].write_text('# operator executor unit\n'); paths[0].chmod(0o644)
    try:
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert paths[0].read_text() == '# operator executor unit\n' and not started.exists()
    finally:
        paths[0].unlink()
    for record in [started, ready]:
        record.write_text('injected provisioning barrier'); record.chmod(0o600)
        try:
            assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
            assert record.read_text() == 'injected provisioning barrier' and all(not p.exists() for p in paths)
        finally:
            record.unlink()
    request = state/'apply-request.json'
    saved_request = state/'apply-request.saved-fixture'
    data_before = (destination/'data/journal.json').read_bytes()
    services_before = (destination/'services.json').read_bytes()
    def ctl(*args):
        return subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=30)
    request.rename(saved_request)
    try:
        installed = subprocess.run(install, capture_output=True, timeout=180)
        assert installed.returncode == 0 and not installed.stderr, 'Root apply scheduler provisioning failed'
        assert json.loads(installed.stdout) == dict(scheduler_enabled=True, installation_complete=False)
        for path in paths:
            assert path.read_text() == units[path.name] and path.stat().st_uid == 0 and path.stat().st_mode & 0o777 == 0o644
        assert started.stat().st_uid == 0 and started.stat().st_mode & 0o777 == 0o600
        assert json.loads(ready.read_text()) == dict(schema=1, scheduler_enabled=True)
        provision_receipt = started.read_bytes()
        assert subprocess.run(install, capture_output=True, timeout=120).returncode == 1
        assert started.read_bytes() == provision_receipt
        assert ctl('is-enabled', names[1]).stdout.strip() == b'enabled'
        assert ctl('is-active', names[1]).stdout.strip() == b'active'
        ctl('start', names[0])
        assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
        assert not receipt.exists() and not marker.exists()
        # A partial/failed execution receipt prevents a timer retry even with a valid mailbox.
        receipt.write_text('injected execution barrier'); receipt.chmod(0o600)
        saved_request.rename(request)
        try:
            ctl('start', names[0])
            assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
            assert receipt.read_text() == 'injected execution barrier'
            assert (destination/'data/journal.json').read_bytes() == data_before
            assert (destination/'services.json').read_bytes() == services_before
            assert not marker.exists()
        finally:
            receipt.unlink()  # Only this injected barrier, before any actual executor invocation.
        deadline = time.monotonic()+300
        while time.monotonic() < deadline:
            status = ctl('show', names[0], '--property=ActiveState,Result,ExecMainStatus').stdout.decode()
            if 'ActiveState=failed' in status:
                # Publish finite diagnostic fields only; raw journals and configuration stay private.
                evidence = {'systemd': dict(line.split('=', 1) for line in status.splitlines() if '=' in line)}
                for label, path in [('execution', authority/'apply-progress.json'), ('data', destination/'data/journal.json'),
                                    ('services', destination/'services.json'), ('site', destination/'site.json')]:
                    if path.exists():
                        value = json.loads(path.read_text())
                        phase = value.get('phase')
                        evidence[label] = phase if phase in {'starting', 'data', 'services', 'site', 'health',
                            'data-complete', 'failed', 'recovered', 'services-ready', 'site-configured',
                            'publishing', 'reloading'} else 'unavailable'
                journal = subprocess.run(['journalctl', '-u', names[0], '-n', '40', '--no-pager', '-o', 'cat'],
                                         capture_output=True, text=True, timeout=15).stdout
                evidence['dependencies'] = [name for line in journal.splitlines()
                    if line.startswith('Web apply dependency failures: ')
                    for name in line.removeprefix('Web apply dependency failures: ').split(',')
                    if name in {'package', 'systemd', 'nginx', 'php', 'mysql', 'redis', 'storage', 'clamav'}][:8]
                evidence['locations'] = re.findall(r'^Web apply failure location: ([A-Za-z]+\.php:[0-9]+)$', journal, re.M)[:4]
                evidence['file_probe'] = diagnose_scheduled_file_probe(release, destination, units[names[0]])
                raise AssertionError('Scheduled root installation failed: '+json.dumps(evidence, sort_keys=True))
            if marker.exists() and 'ActiveState=inactive' in status:
                assert 'Result=success' in status and 'ExecMainStatus=0' in status
                break
            time.sleep(0.5)
        else:
            raise AssertionError('Root timer did not complete the approved installation')
        assert json.loads(marker.read_text())['source'] == source
        original = receipt.read_bytes()
        ctl('start', names[0])
        assert ctl('show', names[0], '--property=ConditionResult', '--value').stdout.strip() == b'no'
        assert receipt.read_bytes() == original
        return dict(source=source, installation_complete=True, already_applied=False)
    finally:
        ctl('disable', '--now', names[1])
        ctl('stop', names[0])
        if saved_request.exists():
            saved_request.rename(request)
        for path in paths:
            if path.exists():
                assert path.read_text() == units[path.name]
                path.unlink()
        ctl('daemon-reload')


def prepare_web_apply(root, release, destination, source):
    try:
        account = pwd.getpwnam('gojet-installer')
    except KeyError:
        subprocess.run(['useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'gojet-installer'], check=True)
        account = pwd.getpwnam('gojet-installer')
    authority = Path('/var/lib/gojet-installer-authority')
    for name in ['web-apply.json', 'web-apply-started.json']:
        assert not (authority/name).exists()
    state = root/'web-apply-state'
    token = 'native-configuration-fixture-token-0123456789'
    digest = hashlib.sha256(token.encode()).hexdigest()
    provision = ['php8.3', str(release/'installer/bin/approve-web-configuration.php'), str(destination), str(state)]
    before_approval = {p.name: p.read_bytes() for p in state.iterdir()}
    denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+provision, capture_output=True, timeout=30)
    assert denied.returncode == 1 and not (authority/'web-apply.json').exists()
    draft_path = state/'configuration-draft.json'
    original_draft = draft_path.read_bytes()
    draft_path.write_bytes(original_draft+b' ')
    assert subprocess.run(provision, capture_output=True, timeout=120).returncode == 1
    assert not (authority/'web-apply.json').exists()
    draft_path.write_bytes(original_draft)
    result = subprocess.run(provision, capture_output=True, timeout=120)
    assert result.returncode == 0 and not result.stderr, 'Web apply provisioning failed'
    assert json.loads(result.stdout) == dict(source=source, web_apply_provisioned=True, installation_complete=False)
    assert before_approval == {p.name: p.read_bytes() for p in state.iterdir()}, 'Root approval rewrote web state'
    binding = (authority/'web-apply.json').read_bytes()
    assert (authority/'web-apply.json').stat().st_uid == 0
    assert state.stat().st_uid == account.pw_uid
    again = subprocess.run(provision, capture_output=True, timeout=120)
    assert again.returncode == 1 and (authority/'web-apply.json').read_bytes() == binding
    adopter = root/'adopt-configuration.php'
    adopter.write_text("""<?php
require_once $argv[1].'/installer/src/HttpAccess.php';
$token=stream_get_contents(STDIN);
try {
$s=new GoJet\\Installer\\InstallState($argv[2],hash('sha256',$token));
$a=$s->authenticate($token);
$server=['REQUEST_URI'=>'/install/configuration/approve','REQUEST_METHOD'=>'POST',
    'HTTPS'=>'on','HTTP_HOST'=>'localhost:18443','HTTP_ORIGIN'=>'https://localhost:18443',
    'CONTENT_TYPE'=>'application/json'];
$r=GoJet\\Installer\\HttpAccess::handle($server,['__Secure-gojet_install'=>$a['session']],
    json_encode(['csrf'=>$a['csrf']]),$argv[2],hash('sha256',$token),
    'https://localhost:18443',$argv[3].'/installed.lock');
if($r[0]!==200||$r[2]['step']!=='services'||$r[2]['status']!=='ready') {exit(1);}
} catch(Throwable) {exit(1);}
""")
    adopter.chmod(0o644)
    def adopt():
        return subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', str(adopter), str(release), str(state), str(authority)],
            input=token.encode(), capture_output=True, timeout=20)
    approval_path = authority/'configuration-approval.json'
    assert approval_path.stat().st_uid == 0 and approval_path.stat().st_mode & 0o777 == 0o640
    approval_path.chmod(0o666)
    assert adopt().returncode == 1 and not (state/'apply-approval.json').exists()
    approval_path.chmod(0o640)
    approval_bytes = approval_path.read_bytes()
    for field in ['state_sha256', 'draft_sha256']:
        changed = json.loads(approval_bytes); changed[field] = '0'*64
        approval_path.write_text(json.dumps(changed))
        assert adopt().returncode == 1 and not (state/'apply-approval.json').exists()
    approval_path.write_bytes(approval_bytes)
    # Inject only the local file left by an interrupted adoption; never delete a real apply receipt.
    interrupted = state/'apply-approval.json'
    interrupted.write_text(json.dumps(json.loads(binding)['approval'])); interrupted.chmod(0o600)
    os.chown(interrupted, account.pw_uid, account.pw_gid)
    interrupted_bytes = interrupted.read_bytes()
    assert adopt().returncode == 1 and interrupted.read_bytes() == interrupted_bytes
    assert json.loads((state/'state.json').read_text())['status'] == 'checking'
    interrupted.unlink()
    adopted = adopt()
    assert adopted.returncode == 0 and not adopted.stdout and not adopted.stderr
    assert not (state/'apply-request.json').exists()
    assert (state/'apply-approval.json').stat().st_uid == account.pw_uid
    state_approval = (state/'apply-approval.json').read_bytes()
    assert adopt().returncode == 1 and (state/'apply-approval.json').read_bytes() == state_approval
    # Invoke the actual shipped adapter as the actual unprivileged installer account.
    # Trusted TLS variables are supplied by this test, not proof of a TLS/browser request.
    adapter = root/'web-apply-adapter.php'
    adapter.write_text('''<?php
require_once $argv[1].'/installer/src/HttpAccess.php';
$r=json_decode(stream_get_contents(STDIN),true,16,JSON_THROW_ON_ERROR);
$s=['REQUEST_URI'=>$r['uri'],'REQUEST_METHOD'=>$r['method'],'HTTP_ACCEPT'=>'application/json','HTTPS'=>'on','HTTP_HOST'=>'localhost:18443',
'HTTP_ORIGIN'=>'https://localhost:18443','CONTENT_TYPE'=>'application/json'];
echo json_encode(GoJet\\Installer\\HttpAccess::handle($s,$r['cookies'],json_encode($r['body']),
$argv[2],$argv[3],'https://localhost:18443','/var/lib/gojet-installer-authority/installed.lock'));
''')
    adapter.chmod(0o644)

    def http(uri, body, cookies=None, method="POST"):
        r = subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', str(adapter), str(release), str(state), digest],
            input=json.dumps(dict(uri=uri, body=body, cookies=cookies or {}, method=method)).encode(), capture_output=True, timeout=15)
        assert r.returncode == 0 and not r.stderr, 'Unprivileged HTTP adapter failed'
        return json.loads(r.stdout)

    assert http('/install/services', {'csrf': '0'*64})[0] == 401
    auth = http('/install', {'bootstrap_token': token})
    assert auth[0] == 200 and auth[2]['step'] == 'services'
    cookie = auth[1]['Set-Cookie'].split(';', 1)[0].split('=', 1)[1]
    cookies = {'__Secure-gojet_install': cookie}
    csrf = auth[2]['csrf']
    def status(c=cookies):
        return http('/install/services', {}, c, method='GET')

    assert status({})[0] == 401
    assert status()[2]['preflight']['status'] == 'unavailable'
    refresh = ['php8.3', str(release/'installer/bin/refresh-web-preflight.php')]
    snapshot = authority/'preflight.json'
    assert subprocess.run(['runuser', '-u', 'gojet-installer', '--']+refresh, capture_output=True, timeout=30).returncode == 1
    assert not snapshot.exists()

    def publish():
        r = subprocess.run(refresh, capture_output=True, timeout=120)
        assert r.returncode == 0 and not r.stderr, 'Root preflight refresh failed'
        return json.loads(r.stdout)

    observed = publish()
    assert observed['status'] == 'ready' and observed['ready'] is True
    assert observed['installation_complete'] is False and all(observed['checks'].values())
    assert status()[2]['preflight'] == observed
    assert snapshot.stat().st_uid == 0 and snapshot.stat().st_mode & 0o777 == 0o644
    before = (state/'state.json').read_bytes()
    good = snapshot.read_bytes()
    # Bounded read rejects unsafe nodes, wrong binding, expiry and forged result shapes.
    value = json.loads(good)
    for key, bad in [('checked_at', 1), ('checked_at', 4102444800), ('state_sha256', '0'*64), ('checks', {'mysql': True})]:
        altered = dict(value); altered[key] = bad
        snapshot.write_text(json.dumps(altered))
        assert status()[2]['preflight']['ready'] is False
    snapshot.write_bytes(good); snapshot.chmod(0o666)
    assert status()[2]['preflight']['status'] == 'unavailable'
    snapshot.chmod(0o644)
    snapshot.unlink(); snapshot.symlink_to(authority/'web-apply.json')
    assert status()[2]['preflight']['status'] == 'unavailable'
    snapshot.unlink(); snapshot.write_bytes(good); snapshot.chmod(0o644)
    os.chown(snapshot, account.pw_uid, account.pw_gid)
    assert status()[2]['preflight']['status'] == 'unavailable'
    os.chown(snapshot, 0, 0)
    # A real stopped Redis service must replace green with an authenticated failure.
    subprocess.run(['systemctl', 'stop', 'redis-server.service'], check=True)
    try:
        failed = publish()
        assert failed['status'] == 'failed' and failed['checks']['redis'] is False and failed['ready'] is False
        assert status()[2]['preflight'] == failed
    finally:
        subprocess.run(['systemctl', 'start', 'redis-server.service'], check=True)
    assert publish()['ready'] is True
    assert (state/'state.json').read_bytes() == before
    assert not (state/'apply-request.json').exists()
    # Seal mismatch cannot retain a prior green result or disclose sealed configuration.
    seal_path = destination/'seal.json'; seal_bytes = seal_path.read_bytes()
    try:
        seal_path.write_bytes(seal_bytes+b' ')
        assert publish()['status'] == 'unavailable'
        assert status()[2]['preflight']['ready'] is False
    finally:
        seal_path.write_bytes(seal_bytes)
    assert publish()['ready'] is True
    response = json.dumps(status())
    assert str(destination) not in response and str(state) not in response and 'p21-ci-only' not in response
    verify_preflight_schedule(release, authority, account)
    assert (state/'state.json').read_bytes() == before
    assert not (state/'apply-request.json').exists()
    assert http('/install/services', {'csrf': '0'*64}, cookies)[0] == 403
    assert http('/install/services', {'csrf': csrf, 'prepared': '/etc'}, cookies)[0] == 400
    assert not (state/'apply-request.json').exists()
    worker = ['php8.3', str(release/'installer/bin/run-web-apply.php')]
    assert subprocess.run(worker, capture_output=True, timeout=30).returncode == 1
    assert not (authority/'web-apply-started.json').exists()
    browser_auth = submit_browser(root, release, state, authority, token)
    csrf = browser_auth['csrf']
    cookies = {'__Secure-gojet_install': browser_auth['session']}
    assert status(cookies)[2]['status'] == 'checking'
    request = state/'apply-request.json'
    raw = request.read_bytes()
    assert request.stat().st_uid == account.pw_uid and request.stat().st_mode & 0o777 == 0o600
    assert http('/install/services', {'csrf': csrf}, cookies)[0] == 403 and request.read_bytes() == raw
    rotated = http('/install', {'bootstrap_token': token})
    cookie2 = rotated[1]['Set-Cookie'].split(';', 1)[0].split('=', 1)[1]
    assert http('/install/services', {'csrf': rotated[2]['csrf']}, {'__Secure-gojet_install': cookie2})[0] == 409
    assert subprocess.run(['runuser', '-u', 'gojet-installer', '--']+worker, capture_output=True, timeout=30).returncode == 1
    # Exercise the shipped publisher and authenticated reader with explicit observation fixtures.
    progress = authority/'apply-progress.json'
    publisher = root/'publish-apply-progress.php'
    publisher.write_text("<?php require_once $argv[1].'/installer/src/ApplyProgress.php'; "
                         "echo json_encode(GoJet\\Installer\\ApplyProgress::publish($argv[2],$argv[3],$argv[4],$argv[5]));")
    publisher.chmod(0o644)
    publish_command = ['php8.3', str(publisher), str(release), str(authority), str(state)]
    assert subprocess.run(['runuser', '-u', 'gojet-installer', '--']+publish_command+['data', 'running'],
                          capture_output=True, timeout=15).returncode != 0
    assert not progress.exists()
    current_cookies = {'__Secure-gojet_install': cookie2}
    for phase in ['starting', 'data', 'services', 'site', 'health']:
        r = subprocess.run(publish_command+[phase, 'running'], capture_output=True, timeout=15)
        assert r.returncode == 0 and not r.stderr
        observed = json.loads(r.stdout)
        assert observed['phase'] == phase and observed['status'] == 'running'
        assert observed['installation_complete'] is False
        assert status(current_cookies)[2]['execution'] == observed
    assert status({})[0] == 401
    good_progress = progress.read_bytes()
    value = json.loads(good_progress)
    for key, bad, expected in [('updated_at', 1, 'stale'), ('updated_at', 4102444800, 'unavailable'),
                               ('state_sha256', '0'*64, 'unavailable'), ('phase', '/etc/private', 'unavailable'),
                               ('status', 'complete', 'unavailable')]:
        altered = dict(value); altered[key] = bad
        progress.write_text(json.dumps(altered))
        observation = status(current_cookies)[2]['execution']
        assert observation['status'] == expected and observation['phase'] is None
        assert observation['installation_complete'] is False
    progress.write_bytes(good_progress); progress.chmod(0o666)
    assert status(current_cookies)[2]['execution']['status'] == 'unavailable'
    progress.chmod(0o644); os.chown(progress, account.pw_uid, account.pw_gid)
    assert status(current_cookies)[2]['execution']['status'] == 'unavailable'
    os.chown(progress, 0, 0)
    progress.unlink(); progress.symlink_to(authority/'web-apply.json')
    assert status(current_cookies)[2]['execution']['status'] == 'unavailable'
    progress.unlink()
    r = subprocess.run(publish_command+['services', 'failed'], capture_output=True, timeout=15)
    assert r.returncode == 0 and not r.stderr
    assert status(current_cookies)[2]['execution']['status'] == 'failed'
    # The earlier replay consumed this CSRF: stale authority is rejected before step ordering.
    assert http('/install/services', {'csrf': rotated[2]['csrf']}, current_cookies)[0] == 403
    reauth = http('/install', {'bootstrap_token': token})
    fresh_cookie = reauth[1]['Set-Cookie'].split(';', 1)[0].split('=', 1)[1]
    assert http('/install/services', {'csrf': reauth[2]['csrf']}, {'__Secure-gojet_install': fresh_cookie})[0] == 409
    assert request.read_bytes() == raw and not (authority/'web-apply-started.json').exists()
    progress.unlink()  # Remove only observation fixtures before the real worker invocation.
    # A forged digest or unsafe mailbox never starts privileged work.
    forged = json.loads(raw); forged['seal_sha256'] = '0'*64
    request.write_text(json.dumps(forged))
    assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    assert not (authority/'web-apply-started.json').exists()
    request.write_bytes(raw)
    request.chmod(0o644)
    assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    assert not (authority/'web-apply-started.json').exists()
    request.chmod(0o600)
    request.unlink(); request.symlink_to(state/'apply-approval.json')
    assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    request.unlink(); request.write_bytes(raw); request.chmod(0o600); os.chown(request, account.pw_uid, account.pw_gid)
    assert not (authority/'web-apply-started.json').exists()
    # A durable started receipt blocks execution even without a final installed lock.
    receipt_path = authority/'web-apply-started.json'
    receipt_path.write_text(json.dumps(json.loads(raw))); receipt_path.chmod(0o600)
    data_before = (destination/'data/journal.json').read_bytes()
    services_before = (destination/'services.json').read_bytes()
    assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    assert (destination/'data/journal.json').read_bytes() == data_before
    assert (destination/'services.json').read_bytes() == services_before
    assert not (destination/'site.json').exists()
    receipt_path.unlink()  # Only the deliberately injected fixture receipt, before any real apply.
    with observe_browser(root, release, state, authority, token):
        result = run_scheduled_apply(release, authority, account, state, destination, source)
    assert result == dict(source=source, installation_complete=True, already_applied=False)
    final_progress = json.loads(progress.read_text())
    assert final_progress['phase'] == 'health' and final_progress['status'] == 'running'
    assert progress.stat().st_uid == 0 and progress.stat().st_mode & 0o777 == 0o644
    assert subprocess.run(publish_command+['health', 'running'], capture_output=True, timeout=15).returncode != 0
    receipt = (authority/'web-apply-started.json').read_bytes()
    assert (authority/'web-apply-started.json').stat().st_uid == 0
    assert subprocess.run(worker, capture_output=True, timeout=30).returncode == 1
    assert (authority/'web-apply-started.json').read_bytes() == receipt
    assert http('/install/services', {'csrf': csrf}, cookies)[0] == 404
    assert status()[0] == 404
    assert subprocess.run(refresh, capture_output=True, timeout=30).returncode == 1
    return result
