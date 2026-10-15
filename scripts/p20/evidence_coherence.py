"""T048: original archive and exact-head Gate/defect coherence admission."""
import json
from common import ROOT, HEAD, emit, fail_if_errors
from release_sources import inspect as inspect_sources
from gate_ledger import inspect as inspect_gates
from evidence_index import valid_case, unsafe_secret
from t028_sources import require, digest


def inspect(root, head):
    sources = inspect_sources(root, head)
    expected = inspect_gates(root, head, sources)
    directory = root / 'artifacts/v10/P20'
    ledger_raw = (directory / 'gates/gate-ledger.json').read_bytes()
    require(json.loads(ledger_raw) == expected, 'Gate ledger does not reproduce')
    expected['ledger_sha256'] = digest(ledger_raw)
    gate_raw = (directory / 'gates/P20-T047.json').read_bytes()
    gate = json.loads(gate_raw)
    require(valid_case(gate, 'P20-T047', head) and gate['details'] == expected
            and not unsafe_secret(gate), 'invalid T047 formal evidence')
    defect_raw = (directory / 'defect-ledger.json').read_bytes()
    defects = json.loads(defect_raw)
    require(defects['schema'] == 'gojet.p20-defect-ledger.v1' and defects['node'] == 'P20'
            and defects['status'] == 'ACTIVE' and isinstance(defects['open'], list)
            and defects['decision_required'] == [], 'invalid defect ledger')
    require(all(r['severity'] not in ('P0', 'P1') and r.get('disposition')
                and not r.get('gate_hard_failure') for r in defects['open']), 'open blocking defect')
    t008 = sources['cases']['P20-T008']['details']
    require(t008['ledger_sha256'] == digest(defect_raw)
            and [t008[k] for k in ('p0_open', 'p1_open', 'decision_required')] == [0, 0, 0],
            'defect ledger changed since native candidate admission')
    return {'formal_p20_t048_claim': True, 'case_range': 'P20-T001..P20-T047',
            'case_count': 47, 'cases': sources['bindings'], 't047_sha256': digest(gate_raw),
            'gate_ledger_sha256': digest(ledger_raw), 'defect_ledger_sha256': digest(defect_raw),
            'matrix': sources['matrix'], 'matrix_count': len(sources['matrix']),
            'collection_sha256': sources['collection_sha256'], 'secret_safe': True,
            'same_exact_head': True, 'defects': {'p0': 0, 'p1': 0, 'decision_required': 0},
            'merge_authoritative': False}


def main():
    errors, details = [], {}
    try:
        details = inspect(ROOT, HEAD)
    except (ValueError, KeyError, TypeError, OSError) as error:
        errors.append('T048: ' + str(error))
    fail_if_errors([emit('P20-T048', 'coherence', 'Exact-head release-candidate evidence coherence', errors, details)])


if __name__ == '__main__':
    main()
