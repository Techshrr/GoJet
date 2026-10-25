"""Invalid frozen configuration -> retired root attempt -> fresh authenticated submission."""
import copy
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import subprocess


def verify_configuration_restart(root, release, original_binding, original_state, authority):
    account = pwd.getpwnam('gojet-installer')
    state = root/'invalid-configuration-state'
    state.mkdir(mode=0o700); os.chown(state, account.pw_uid, account.pw_gid)
    binding = root/'invalid-configuration-binding.json'
    value = json.loads(original_binding.read_text()); value['destination'] = str(root/'invalid-configuration-plan')
    binding.write_text(json.dumps(value)); binding.chmod(0o600)
    draft = json.loads((original_state/'configuration-draft.json').read_text())
    bad = copy.deepcopy(draft); bad['admin']['email'] = 'not-an-email'
    token = 'native-configuration-restart-token-0123456789'
    bootstrap = hashlib.sha256(token.encode()).hexdigest()
    successor = root/'replacement-attempt'
    retired = Path(str(binding)+'.retired.json')
    completed = Path(str(binding)+'.replacement-ready.json')
    started = Path(str(binding)+'.started.json')
    worker_lock = Path(str(binding)+'.worker.lock')
    worker = ['php8.3', str(release/'installer/bin/run-web-preparation.php'), str(binding), str(state)]
    restart = ['php8.3', str(release/'installer/bin/restart-web-configuration.php'), str(binding), str(state), str(successor)]

    def as_installer(code, directory, payload):
        return subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', '-r', code,
            str(release), str(directory)], input=json.dumps(payload).encode(), capture_output=True, timeout=120)

    setup = 'require $argv[1]."/installer/src/InstallState.php";$v=json_decode(stream_get_contents(STDIN),true);'
    setup += '$s=new GoJet\\Installer\\InstallState($argv[2],hash("sha256",$v["token"]));$s->initialize();'
    setup += '$a=$s->authenticate($v["token"]);$s->submitConfiguration($a["session"],$a["csrf"],$v["draft"]);echo json_encode($a);'
    result = as_installer(setup, state, dict(token=token, draft=bad))
    assert result.returncode == 0 and not result.stderr
    old_auth = json.loads(result.stdout)
    result = subprocess.run(worker, capture_output=True, timeout=120)
    assert result.returncode == 1 and not result.stdout and started.exists()
    assert not Path(value['destination']).exists() and not Path(str(binding)+'.ready.json').exists()
    frozen = state/'configuration-draft.json'
    digest = hashlib.sha256(frozen.read_bytes()).hexdigest()
    preserved = {p: p.read_bytes() for p in [binding, started, frozen, state/'state.json']}

    def call(expected=digest, success=False, nonroot=False):
        result = subprocess.run((['runuser', '-u', 'gojet-installer', '--'] if nonroot else [])+restart+[expected, bootstrap],
            capture_output=True, timeout=120)
        if success:
            assert result.returncode == 0 and not result.stderr, 'Configuration restart failed'
            return json.loads(result.stdout)
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Configuration restart refused; preserve old attempts and inspect private restart records.\n'

    call(nonroot=True); call('0'*64)
    existing_plan_restart = restart[:2]+[str(original_binding), str(original_state), str(root/'published-plan-restart'),
        hashlib.sha256((original_state/'configuration-draft.json').read_bytes()).hexdigest(), bootstrap]
    existing_plan = subprocess.run(existing_plan_restart, capture_output=True, timeout=120)
    assert existing_plan.returncode == 1 and not existing_plan.stdout
    assert not Path(str(original_binding)+'.retired.json').exists() and not (root/'published-plan-restart').exists()
    with worker_lock.open('r+b') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        call()
    # Every conflict remains untouched; no retirement is published before validation succeeds.
    for path in [authority/'web-apply.json', authority/'configuration-approval.json', authority/'installed.lock',
                 Path(value['destination']), successor, Path(str(binding)+'.ready.json')]:
        assert not path.exists() and not path.is_symlink()
        path.symlink_to('/nonexistent-configuration-restart-fixture')
        try:
            call()
            assert path.is_symlink() and not retired.exists() and not completed.exists()
        finally:
            path.unlink()
    # A restart interrupted immediately after retirement cannot reopen either attempt.
    retired.write_text('fixture interrupted retirement'); retired.chmod(0o600)
    try:
        call()
        assert retired.read_text() == 'fixture interrupted retirement' and not successor.exists()
        assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    finally:
        retired.unlink()  # Only the deliberately injected interruption fixture.
    assert call(success=True) == dict(restarted=True, previous_attempt_retired=True,
        binding=str(successor/'binding.json'), state=str(successor/'state'), prepared=str(successor/'prepared'),
        installation_complete=False)
    assert all(p.read_bytes() == raw for p, raw in preserved.items())
    assert successor.stat().st_uid == 0 and successor.stat().st_mode & 0o777 == 0o755
    new_state = successor/'state'
    assert new_state.stat().st_uid == account.pw_uid and new_state.stat().st_mode & 0o777 == 0o700
    for p in [retired, completed, successor/'binding.json']:
        assert p.stat().st_uid == 0 and p.stat().st_mode & 0o777 == 0o600 and p.stat().st_nlink == 1
    current = json.loads((new_state/'state.json').read_text())
    assert current['step'] == 0 and current['status'] == 'ready' and current['session'] is None and current['csrf'] is None
    assert not (new_state/'configuration-draft.json').exists() and not (successor/'prepared').exists()
    tombstone = retired.read_bytes(); publication = completed.read_bytes()
    call()  # Restart is exclusive; no silently created third attempt.
    assert retired.read_bytes() == tombstone and completed.read_bytes() == publication
    assert subprocess.run(worker, capture_output=True, timeout=120).returncode == 1
    render = ['php8.3', str(release/'installer/bin/render-web-preparation-schedule.php'), str(binding), str(state)]
    assert subprocess.run(render, capture_output=True, timeout=120).returncode == 1
    recovery = ['php8.3', str(release/'installer/bin/recover-web-preparation.php'), str(binding), str(state), '0'*64]
    assert subprocess.run(recovery, capture_output=True, timeout=120).returncode == 1
    next_worker = worker[:-2]+[str(successor/'binding.json'), str(new_state)]
    waiting = subprocess.run(next_worker, capture_output=True, timeout=120)
    assert waiting.returncode == 0 and not waiting.stderr
    assert json.loads(waiting.stdout) == dict(prepared=False, waiting_for_configuration=True, installation_complete=False)
    # The former session cannot read or submit against the newly initialized state.
    deny = 'require $argv[1]."/installer/src/InstallState.php";$v=json_decode(stream_get_contents(STDIN),true);'
    deny += '$s=new GoJet\\Installer\\InstallState($argv[2],hash("sha256",$v["token"]));'
    deny += 'try{$s->inspect($v["session"]);exit(2);}catch(Throwable){}'
    deny += 'try{$s->submitConfiguration($v["session"],$v["csrf"],$v["draft"]);exit(3);}catch(Throwable){}'
    denied = as_installer(deny, new_state, dict(token=token, session=old_auth['session'], csrf=old_auth['csrf'], draft=draft))
    assert denied.returncode == 0 and not denied.stdout and not denied.stderr
    assert not (new_state/'configuration-draft.json').exists()
    # Corrected data still requires fresh authentication and the normal frozen-draft preparation path.
    submit = 'require $argv[1]."/installer/src/InstallState.php";$v=json_decode(stream_get_contents(STDIN),true);'
    submit += '$s=new GoJet\\Installer\\InstallState($argv[2],hash("sha256",$v["token"]));$a=$s->authenticate($v["token"]);'
    submit += '$s->submitConfiguration($a["session"],$a["csrf"],$v["draft"]);'
    result = as_installer(submit, new_state, dict(token=token, draft=draft))
    assert result.returncode == 0 and not result.stdout and not result.stderr
    prepared = subprocess.run(next_worker, capture_output=True, timeout=120)
    assert prepared.returncode == 0 and not prepared.stderr
    assert json.loads(prepared.stdout) == dict(prepared=True, already_prepared=False, installation_complete=False)
    proof = json.loads((successor/'prepared/web-draft.json').read_text())
    assert proof['state_sha256'] == hashlib.sha256(str(new_state).encode()).hexdigest()
    assert proof['draft_sha256'] == hashlib.sha256((new_state/'configuration-draft.json').read_bytes()).hexdigest()
    assert all(p.read_bytes() == raw for p, raw in preserved.items())
    assert not (new_state/'apply-approval.json').exists() and not (new_state/'apply-request.json').exists()
    assert not (authority/'web-apply.json').exists() and not (authority/'configuration-approval.json').exists()
    assert not (successor/'prepared/data/journal.json').exists()
