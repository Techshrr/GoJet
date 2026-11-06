"""Read-only browser result observer; services-ready state is an explicit boundary fixture."""
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import pwd
import selectors
import subprocess


@contextmanager
def observe_result(root, release, prepared, entry, origin, token):
    authority = Path('/var/lib/gojet-installer-authority')
    state = entry/'state'
    approval = authority/'configuration-approval.json'
    assert not approval.exists()
    account = pwd.getpwnam('gojet-installer')
    code = 'require $argv[1]."/installer/src/InstallState.php"; $token=stream_get_contents(STDIN);'
    code += '$s=new GoJet\\Installer\\InstallState($argv[2],hash("sha256",$token)); $a=$s->authenticate($token);'
    code += 'foreach(["environment","data","site","admin"] as $step) {$r=$s->check($a["session"],$a["csrf"],$step,fn()=>true);$a["csrf"]=$r["csrf"];}'
    result = subprocess.run(['runuser', '-u', 'gojet-installer', '--', 'php8.3', '-r', code, str(release), str(state)],
        input=token.encode(), capture_output=True, timeout=30)
    assert result.returncode == 0 and not result.stdout and not result.stderr
    seal_hash = hashlib.sha256((prepared/'seal.json').read_bytes()).hexdigest()
    code = 'require $argv[1]."/installer/src/ConfigurationApproval.php"; GoJet\\Installer\\ConfigurationApproval::publish($argv[2],$argv[3],str_repeat("0",64),$argv[4],(int)$argv[5]);'
    result = subprocess.run(['php8.3', '-r', code, str(release), str(authority), str(state), seal_hash, str(account.pw_gid)],
        capture_output=True, timeout=30)
    assert result.returncode == 0 and not result.stdout and not result.stderr
    approval_raw = approval.read_bytes()
    result_id = hashlib.sha256(('gojet.installation-result.v1\n'+seal_hash).encode()).hexdigest()
    repo = Path(__file__).resolve().parents[2]
    process = subprocess.Popen([os.environ['P21_NODE'], str(repo/'scripts/p21/installer_result_browser_test.mjs')],
        cwd=repo, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    try:
        process.stdin.write(json.dumps(dict(origin=origin, token=token, result_id=result_id)))
        process.stdin.close(); process.stdin = None
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            assert selector.select(timeout=90), 'Browser result observer readiness timeout'
            assert process.stdout.readline() == 'ready\n', 'Browser result observer initialization failed'
        def wait_result():
            stdout, stderr = process.communicate(timeout=60)
            assert process.returncode == 0 and not stdout and not stderr, 'Native browser completion verification failed'
        yield wait_result
    finally:
        if process.poll() is None:
            process.terminate()
            try: process.wait(timeout=10)
            except subprocess.TimeoutExpired: process.kill(); process.wait(timeout=5)
        for pipe in [process.stdin, process.stdout, process.stderr]:
            if pipe is not None: pipe.close()
        assert approval.read_bytes() == approval_raw
        approval.unlink()
