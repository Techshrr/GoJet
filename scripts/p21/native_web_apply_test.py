"""Real unprivileged PHP HTTP-adapter mailbox -> root native apply (not a browser/TLS submission test)."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import subprocess


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
    token = 'p21-web-apply-bootstrap-fixture-only-0123456789'
    digest = hashlib.sha256(token.encode()).hexdigest()
    provision = ['php8.3', str(release/'installer/bin/provision-web-apply.php'), str(destination), str(state), digest]
    denied = subprocess.run(['runuser', '-u', 'gojet-installer', '--']+provision, capture_output=True, timeout=30)
    assert denied.returncode == 1 and not state.exists() and not (authority/'web-apply.json').exists()
    result = subprocess.run(provision, capture_output=True, timeout=120)
    assert result.returncode == 0 and not result.stderr, 'Web apply provisioning failed'
    assert json.loads(result.stdout) == dict(source=source, web_apply_provisioned=True, installation_complete=False)
    binding = (authority/'web-apply.json').read_bytes()
    assert (authority/'web-apply.json').stat().st_uid == 0
    assert state.stat().st_uid == account.pw_uid
    again = subprocess.run(provision, capture_output=True, timeout=120)
    assert again.returncode == 1 and (authority/'web-apply.json').read_bytes() == binding
    # Invoke the actual shipped adapter as the actual unprivileged installer account.
    # Trusted TLS variables are supplied by this test, not proof of a TLS/browser request.
    adapter = root/'web-apply-adapter.php'
    adapter.write_text('''<?php
require_once $argv[1].'/installer/src/HttpAccess.php';
$r=json_decode(stream_get_contents(STDIN),true,16,JSON_THROW_ON_ERROR);
$s=['REQUEST_URI'=>$r['uri'],'REQUEST_METHOD'=>'POST','HTTPS'=>'on','HTTP_HOST'=>'localhost:18443',
'HTTP_ORIGIN'=>'https://localhost:18443','CONTENT_TYPE'=>'application/json'];
echo json_encode(GoJet\\Installer\\HttpAccess::handle($s,$r['cookies'],json_encode($r['body']),
$argv[2],$argv[3],'https://localhost:18443','/var/lib/gojet-installer-authority/installed.lock'));
''')
    adapter.chmod(0o644)

    def http(uri, body, cookies=None):
        r = subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', str(adapter), str(release), str(state), digest],
            input=json.dumps(dict(uri=uri, body=body, cookies=cookies or {})).encode(), capture_output=True, timeout=15)
        assert r.returncode == 0 and not r.stderr, 'Unprivileged HTTP adapter failed'
        return json.loads(r.stdout)

    assert http('/install/services', {'csrf': '0'*64})[0] == 401
    auth = http('/install', {'bootstrap_token': token})
    assert auth[0] == 200 and auth[2]['step'] == 'services'
    cookie = auth[1]['Set-Cookie'].split(';', 1)[0].split('=', 1)[1]
    cookies = {'__Secure-gojet_install': cookie}
    csrf = auth[2]['csrf']
    assert http('/install/services', {'csrf': '0'*64}, cookies)[0] == 403
    assert http('/install/services', {'csrf': csrf, 'prepared': '/etc'}, cookies)[0] == 400
    assert not (state/'apply-request.json').exists()
    worker = ['php8.3', str(release/'installer/bin/run-web-apply.php')]
    assert subprocess.run(worker, capture_output=True, timeout=30).returncode == 1
    assert not (authority/'web-apply-started.json').exists()
    queued = http('/install/services', {'csrf': csrf}, cookies)
    assert queued[0] == 202 and queued[2] == dict(step='services', status='checking')
    request = state/'apply-request.json'
    raw = request.read_bytes()
    assert request.stat().st_uid == account.pw_uid and request.stat().st_mode & 0o777 == 0o600
    assert http('/install/services', {'csrf': csrf}, cookies)[0] == 403 and request.read_bytes() == raw
    rotated = http('/install', {'bootstrap_token': token})
    cookie2 = rotated[1]['Set-Cookie'].split(';', 1)[0].split('=', 1)[1]
    assert http('/install/services', {'csrf': rotated[2]['csrf']}, {'__Secure-gojet_install': cookie2})[0] == 409
    assert subprocess.run(['runuser', '-u', 'gojet-installer', '--']+worker, capture_output=True, timeout=30).returncode == 1
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
    result = subprocess.run(worker, capture_output=True, timeout=240)
    assert result.returncode == 0 and not result.stderr, 'Root mailbox apply failed'
    assert json.loads(result.stdout) == dict(source=source, installation_complete=True, already_applied=False)
    receipt = (authority/'web-apply-started.json').read_bytes()
    assert (authority/'web-apply-started.json').stat().st_uid == 0
    assert subprocess.run(worker, capture_output=True, timeout=30).returncode == 1
    assert (authority/'web-apply-started.json').read_bytes() == receipt
    assert http('/install/services', {'csrf': csrf}, cookies)[0] == 404
    return json.loads(result.stdout)
