"""Eight real executables and local forwarding; not business/audit proof."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

SERVICES = {
    'redirectengine': 'services/redirectengine/cmd/server',
    'analyticsworker': 'services/analyticsworker/cmd/worker',
    'analyticsreconciler': 'services/analyticsreconciler/cmd/reconciler',
    'platformapi': 'services/platformapi/cmd/server',
    'mailworker': 'services/platformapi/cmd/mailworker',
    'fileworker': 'services/platformapi/cmd/fileworker',
    'operationsmonitor': 'services/platformapi/cmd/operationsmonitor',
}


def main():
    directory = Path('artifacts/v10/P20/runtime/t037-defect')
    directory.mkdir(parents=True, exist_ok=True)
    env = {k: v for k, v in os.environ.items() if not k.startswith('GOJET_')}
    rows = []
    receiver_checks = {}
    with tempfile.TemporaryDirectory() as tmp:
        os.chmod(tmp, 0o700)
        receiver = str(Path(tmp) / 'logreceiver')
        subprocess.run(['go', 'build', '-o', receiver, './services/logreceiver/cmd/server'], check=True)
        env['GOJET_LOG_SOCKET'] = str(Path(tmp) / 'logs.sock')
        output = directory / 'logreceiver.jsonl'
        with output.open('wb') as sink:
            process = subprocess.Popen([receiver], env=env, stdout=sink, stderr=subprocess.PIPE)
            try:
                deadline = time.monotonic() + 10
                while True:
                    health = subprocess.run([receiver, '--check'], env=env, capture_output=True)
                    if health.returncode == 0:
                        break
                    assert process.poll() is None and time.monotonic() < deadline, 'receiver not ready'
                    time.sleep(.05)
                receiver_checks['health'] = True
                assert Path(env['GOJET_LOG_SOCKET']).stat().st_mode & 0o777 == 0o600
                receiver_checks['private_socket'] = True
                for service, source in SERVICES.items():
                    executable = str(Path(tmp) / service)
                    subprocess.run(['go', 'build', '-o', executable, './' + source], check=True)
                    result = subprocess.run([executable], env=env, capture_output=True, timeout=10)
                    assert result.returncode == 1, (service, result.returncode)
                    assert not result.stderr, service + ': unexpected unstructured stderr'
                    events = [json.loads(line) for line in result.stdout.splitlines()]
                    assert len(events) == 1 and events[0]['service_id'] == service and events[0]['level'] == 'ERROR'
                    assert 'forwarding unavailable' not in events[0]['msg']
                    (directory / (service + '.jsonl')).write_bytes(result.stdout)
                    rows.append({'service': service, 'exit_code': result.returncode, 'records': len(events)})
            finally:
                process.terminate()
                _, stderr = process.communicate(timeout=5)
                assert process.returncode == 0 and not stderr, 'receiver shutdown failed'
        collected = [json.loads(line) for line in output.read_bytes().splitlines()]
        for service in SERVICES:
            forwarded = [e for e in collected if e['service_id'] == service]
            original = json.loads((directory / (service + '.jsonl')).read_bytes())
            assert len(forwarded) == 1 and forwarded[0]['source_time'] == original['time']
            assert {k: v for k, v in forwarded[0].items() if k not in ('time', 'source_time')} == {k: v for k, v in original.items() if k != 'time'}
        assert [e['msg'] for e in collected if e['service_id'] == 'logreceiver'] == ['logreceiver started', 'logreceiver stopped']
        receiver_checks['seven_original_records_received'] = True
        receiver_checks['clean_stop'] = True
        assert not Path(env['GOJET_LOG_SOCKET']).exists(), 'socket not removed on shutdown'
        health = subprocess.run([receiver, '--check'], env=env, capture_output=True)
        assert health.returncode == 1, 'stopped receiver falsely healthy'
        receiver_checks['stopped_health_denied'] = True
        rows.append({'service': 'logreceiver', 'exit_code': process.returncode, 'records': 2})
    (directory / 'manifest.json').write_text(json.dumps({
        'implementation_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
        'actual_processes': rows, 'receiver_checks': receiver_checks, 'formal_p20_t037_claim': False,
        'scope': 'eight native executables, actual Unix socket forwarding, quarantine-error regression',
        'missing': ['business/operations/audit correlation'],
    }, indent=2) + '\n')


if __name__ == '__main__':
    main()
