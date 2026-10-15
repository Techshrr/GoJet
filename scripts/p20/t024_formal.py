"""Bind real integrated lifecycle to separate frozen P17 network authority."""
import json
from common import HEAD, ROOT, emit, fail_if_errors, sha256_file


def run_case():
    errors = []
    details = {'formal_p20_t024_claim': False, 'next_case_unlocked': False}
    try:
        source = ROOT / 'artifacts/v10/P20/integration/P20-T024.json'
        real = json.loads(source.read_text())
        assert real['status'] == 'PASS' and real['errors'] == []
        assert real['implementation_commit'] == HEAD
        d = real['details']
        for key in ('real_p15_session', 't023_evidence_bound', 'management_regression_passed',
                    'real_api_key_scope_verified', 'real_api_key_rate_verified',
                    'real_api_key_expiry_verified', 'secret_once_verified', 'audit_secret_safe',
                    'webhook_producer_passed', 'producer_duplicate_idempotent',
                    'webhook_outbound_passed', 'receiver_signature_verified',
                    'receiver_rotation_verified', 'worker_restart_verified',
                    'outbound_audit_correlated', 'real_operationsmonitor', 'real_retry_clock'):
            assert d.get(key) is True, key
        for key in ('mock_authority', 'test_header_authority', 'secret_material_recorded'):
            assert d.get(key) is False, key
        refs = []
        for number in range(25, 30):
            case = f'P17-T{number:03d}'
            path = ROOT / 'artifacts/v10/P17/webhooks' / (case + '.json')
            proof = json.loads(path.read_text())
            assert proof['node'] == 'P17' and proof['case'] == case
            assert proof['status'] == 'PASS' and proof['exact_head'] == HEAD
            assert proof['contract_authority'] == '30174f40df28678360f644b8fed79736906b0ea0'
            assert proof['checks'] and all(v is True for v in proof['checks'].values())
            assert proof['evidence_policy'] and all(v is False for v in proof['evidence_policy'].values())
            assert proof['environment']['mysql_version'] and proof['environment']['redis_version']
            refs.append({'case': case, 'sha256': sha256_file(path),
                         'path': str(path.relative_to(ROOT))})
        # Keep original runtime result separately; never conceal its partial status.
        snapshot = source.with_name('P20-T024-runtime.json')
        snapshot.write_bytes(source.read_bytes())
        details.update(d)
        details.update(formal_p20_t024_claim=True, availability_only=False,
                       next_case_unlocked=False, promotion_requires_same_head_gates=True,
                       runtime_evidence_sha256=sha256_file(snapshot), predecessor_evidence=refs,
                       network_authority='Frozen P17 production guard with deterministic DNS/socket fixtures; separate from real public HTTPS delivery',
                       payment_callback_authority_separate=True)
    except Exception as exc:
        errors.append('T024 formal binding failed: ' + type(exc).__name__)
        details['formal_p20_t024_claim'] = False
    return emit('P20-T024', 'integration', 'API key and outbound webhook integrated lifecycle', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
