"""Require byte-identical canonical guard jobs in the scoped T036 workflow.

The original P20 workflows are not on main yet and cannot be dispatched there.
Only read-only permissions and the prerequisite dependency are added to the jobs.
"""
from pathlib import Path
from t028_sources import require, digest

GUARDS = {
    'contract': ('p20-whole-product-verification.yml', 'P20 contract freeze and whole-product baseline guard'),
    'candidate': ('p20-candidate-freeze.yml', 'P20 T001-T008 candidate, traceability and evidence freeze'),
}


def expected_job(root, key):
    workflow = GUARDS[key][0]
    canonical = (root / '.github/workflows' / workflow).read_text().split('\njobs:\n', 1)[1]
    require(canonical.startswith('  ' + key + ':\n'), 'unexpected canonical guard structure')
    return canonical.replace('  ' + key + ':\n', '  ' + key + ':\n    needs: dispatch\n    permissions:\n      contents: read\n', 1)


def verify(root):
    text = (root / '.github/workflows/p20-t036-performance.yml').read_text()
    result = {}
    for key in GUARDS:
        begin, end = f'  # BEGIN CANONICAL {key}\n', f'  # END CANONICAL {key}\n'
        require(text.count(begin) == text.count(end) == 1, 'missing/duplicate canonical guard')
        body = text.split(begin)[1].split(end)[0]
        require(body == expected_job(root, key), 'canonical guard steps changed: ' + key)
        result[key] = digest(body.encode())
    return result


if __name__ == '__main__':
    print(verify(Path(__file__).resolve().parents[2]))
