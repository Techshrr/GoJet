"""Recover the published-plan/absent-receipt boundary; never delete a real barrier."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess


def verify_preparation_recovery(root, release, original_binding, state, authority):
    binding = root/'recovery-web-configuration.json'
    destination = root/'recovery-prepared'
    value = json.loads(original_binding.read_text())
    value['destination'] = str(destination)
    binding.write_text(json.dumps(value)); binding.chmod(0o600)
    started = Path(str(binding)+'.started.json')
    ready = Path(str(binding)+'.ready.json')
    lock = Path(str(binding)+'.worker.lock')
    recover = ['php8.3', str(release/'installer/bin/recover-web-preparation.php'), str(binding), str(state)]
    worker = ['php8.3', str(release/'installer/bin/run-web-preparation.php'), str(binding), str(state)]

    def call(digest, success=False, nonroot=False):
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+recover+[digest],
            capture_output=True, timeout=120)
        if not success:
            assert result.returncode == 1 and not result.stdout
            assert result.stderr == b'Preparation recovery refused; preserve private records and inspect the published plan.\n'
            return
        assert result.returncode == 0 and not result.stderr, 'Published preparation recovery failed'
        return json.loads(result.stdout)

    call('0'*64)  # Missing started record and plan cannot be recovered or regenerated.
    assert not started.exists() and not ready.exists() and not destination.exists()
    # Deliberately construct exactly the durable boundary reached after plan publication,
    # before ready receipt publication. This is a boundary fixture, not a SIGKILL claim.
    code = 'require $argv[1]."/installer/src/InstallationPreparation.php";'
    code += '$b=GoJet\\Installer\\ApplyRequest::read($argv[2],0);'
    code += 'GoJet\\Installer\\ApplyRequest::write($argv[2].".started.json",["schema"=>1,"binding_sha256"=>hash("sha256",json_encode($b,JSON_THROW_ON_ERROR)),"state_sha256"=>hash("sha256",$argv[3])]);'
    fixture = subprocess.run(['php8.3', '-r', code, str(release), str(binding), str(state)], capture_output=True, timeout=120)
    assert fixture.returncode == 0 and not fixture.stdout and not fixture.stderr
    barrier = started.read_bytes()
    call('0'*64)
    assert started.read_bytes() == barrier and not ready.exists() and not destination.exists()
    code = 'require $argv[1]."/installer/src/InstallationPreparation.php";'
    code += 'GoJet\\Installer\\InstallationPreparation::createFromFrozenWebDraft($argv[2],$argv[3]);'
    fixture = subprocess.run(['php8.3', '-r', code, str(release), str(binding), str(state)], capture_output=True, timeout=120)
    assert fixture.returncode == 0 and not fixture.stdout and not fixture.stderr
    seal = destination/'seal.json'
    digest = hashlib.sha256(seal.read_bytes()).hexdigest()
    protected = [binding, started, state/'state.json', state/'configuration-draft.json']
    protected += [p for p in destination.rglob('*') if p.is_file()]
    original = {p: p.read_bytes() for p in protected}
    assert not ready.exists()
    # Normal retry stays closed; only explicit recovery may publish the missing receipt.
    result = subprocess.run(worker, capture_output=True, timeout=120)
    assert result.returncode == 1 and not result.stdout and not ready.exists()
    call(digest, nonroot=True)
    call('not-a-digest')
    call('0'*64)
    # Shared lock prevents recovery racing a normal worker and vice versa.
    with lock.open('r+b') as held:
        fcntl.flock(held, fcntl.LOCK_EX | fcntl.LOCK_NB)
        call(digest)
        assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    assert lock.stat().st_uid == 0 and lock.stat().st_mode & 0o777 == 0o600
    linked = root/'recovery-lock-hardlink'
    os.link(lock, linked)
    try:
        call(digest)
    finally:
        linked.unlink()
    lock.chmod(0o644)
    try:
        call(digest)
    finally:
        lock.chmod(0o600)
    for path in [started, seal, destination/'environment/platformapi.env']:
        path.write_bytes(original[path]+b' ')
        try:
            # A whitespace-only started edit decodes identically; change its identity instead.
            if path == started:
                path.write_text('{}')
            call(digest)
            assert not ready.exists()
        finally:
            path.write_bytes(original[path])
    draft = state/'configuration-draft.json'
    altered = json.loads(original[draft]); altered['admin']['display_name'] += ' changed'
    draft.write_text(json.dumps(altered))
    try:
        call(digest)
        assert not ready.exists()
    finally:
        draft.write_bytes(original[draft])
    for path in [authority/'web-apply.json', authority/'configuration-approval.json',
                 authority/'installed.lock', destination/'data/journal.json',
                 state/'apply-request.json', ready]:
        assert not path.exists() and not path.is_symlink()
        path.write_text('fixture barrier'); path.chmod(0o600)
        try:
            call(digest)
            assert path.read_text() == 'fixture barrier'
            if path != ready:
                assert not ready.exists()
        finally:
            path.unlink()  # Only the injected boundary fixture is removed.
    assert all(p.read_bytes() == raw for p, raw in original.items())
    assert call(digest, success=True) == dict(prepared=True, recovered=True, already_prepared=False, installation_complete=False)
    receipt = ready.read_bytes()
    assert ready.stat().st_uid == 0 and ready.stat().st_mode & 0o777 == 0o600
    assert call(digest, success=True) == dict(prepared=True, recovered=True, already_prepared=True, installation_complete=False)
    repeated = subprocess.run(worker, capture_output=True, timeout=120)
    assert repeated.returncode == 0 and not repeated.stderr
    assert json.loads(repeated.stdout) == dict(prepared=True, already_prepared=True, installation_complete=False)
    assert ready.read_bytes() == receipt and all(p.read_bytes() == raw for p, raw in original.items())
    assert not (state/'apply-approval.json').exists() and not (state/'apply-request.json').exists()
    assert not (authority/'configuration-approval.json').exists() and not (authority/'web-apply.json').exists()
    assert not (destination/'data/journal.json').exists()
