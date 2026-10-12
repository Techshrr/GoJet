"""Actual executable failure logs; deliberately not eight-service T037 proof."""
import json
import os
from pathlib import Path
import subprocess
import tempfile

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
    with tempfile.TemporaryDirectory() as tmp:
        for service, source in SERVICES.items():
            executable = str(Path(tmp) / service)
            subprocess.run(['go', 'build', '-o', executable, './' + source], check=True)
            result = subprocess.run([executable], env=env, capture_output=True, timeout=10)
            assert result.returncode == 1, (service, result.returncode)
            assert not result.stderr, service + ': unexpected unstructured stderr'
            events = [json.loads(line) for line in result.stdout.splitlines()]
            assert events and all(e['service_id'] == service and e['level'] == 'ERROR' for e in events)
            (directory / (service + '.jsonl')).write_bytes(result.stdout)
            rows.append({'service': service, 'exit_code': result.returncode, 'records': len(events)})
    (directory / 'manifest.json').write_text(json.dumps({
        'implementation_commit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip(),
        'actual_processes': rows, 'formal_p20_t037_claim': False,
        'scope': 'seven native startup failures and separate real quarantine-error regression',
        'missing': ['logreceiver', 'business/operations/audit correlation'],
    }, indent=2) + '\n')


if __name__ == '__main__':
    main()
