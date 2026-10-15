"""T047: explicit G0-G10 dispositions backed by current release evidence."""
import json
from common import ROOT, HEAD, emit, fail_if_errors
from release_sources import inspect as inspect_sources
from t028_sources import require, digest
from traceability_cases import INTEGRATIONS

GATES = {
    'G0': ('scope-traceability', (1, 2, 3, 4, 6, 7, 8)),
    'G1': ('native-architecture', (6, 37)),
    'G2': ('design-system', (34, 35)),
    'G3': ('functional-api', tuple(range(9, 31)) + tuple(range(37, 47))),
    'G4': ('browser-responsive', (27, 30, 31, 34, 35)),
    'G5': ('accessibility', (35,)),
    'G6': ('security', tuple(range(16, 30)) + (35, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46)),
    'G7': ('seo', (30, 31, 32, 33)),
    'G8': ('visual', (34, 35)),
    'G9': ('performance', (36,)),
    'G10': ('full-stack-p0', tuple(range(9, 27)) + (37, 38)),
}


def inspect(root, head, sources=None):
    sources = sources or inspect_sources(root, head)
    inherited = {r['node']: r for r in sources['authority']['integrations']}
    for node in ('P01', 'P03'):
        row = inherited[node]
        require(row['integration'] == INTEGRATIONS[node] and row['in_head_ancestry'] is True
                and row['github_commit_exists'] is True, 'missing live Gate authority: ' + node)
    rows = {}
    for gate, (name, numbers) in GATES.items():
        evidence = {f'P20-T{n:03d}': sources['bindings'][f'P20-T{n:03d}'] for n in numbers}
        rows[gate] = {'name': name, 'disposition': 'PASS', 'evidence': evidence,
                      'implementation_commit': head, 'conditional_pass': False}
    rows['G1'].update(disposition='LIVE_BOUND_CARRY_FORWARD', inherited_authority=inherited['P01'],
                      current_baseline=sources['matrix']['p01-engineering.yml'],
                      later_execution_stages=['P21', 'P22'],
                      later_capabilities=['CAP-NATIVE-INSTALL', 'CAP-NATIVE-ONLY-RELEASE'],
                      later_owned_completion_claim=False)
    rows['G2'].update(disposition='LIVE_BOUND_COMPLETE', inherited_authority=inherited['P03'],
                      current_baseline=sources['matrix']['p03-design-system.yml'])
    return {'formal_p20_t047_claim': True, 'gates': rows, 'gate_count': 11,
            'p20_execution_pass_count': 9, 'outside_p20': ['G11', 'G12', 'G13'],
            'collection_sha256': sources['collection_sha256'],
            'production_deployment_claim': False, 'p21_p22_completion_claim': False}


def main():
    errors, details = [], {}
    try:
        details = inspect(ROOT, HEAD)
        path = ROOT / 'artifacts/v10/P20/gates/gate-ledger.json'
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(details, indent=2) + '\n')
        details['ledger_sha256'] = digest(path.read_bytes())
    except (ValueError, KeyError, TypeError, OSError) as error:
        errors.append('T047: ' + str(error))
    fail_if_errors([emit('P20-T047', 'gates', 'G0-G10 release-candidate Gate ledger', errors, details)])


if __name__ == '__main__':
    main()
