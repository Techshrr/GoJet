"""Byte-identical original P20 guard jobs; only dependency/permissions added."""
from pathlib import Path
from t036_guards import GUARDS, expected_job
from t028_sources import require, digest


def verify(root):
    workflow = (root / '.github/workflows/p20-t037-observability.yml').read_text()
    hashes = {}
    for key in GUARDS:
        begin, end = f'  # BEGIN CANONICAL {key}\n', f'  # END CANONICAL {key}\n'
        require(workflow.count(begin) == workflow.count(end) == 1, 'canonical guard missing/duplicated')
        body = workflow.split(begin)[1].split(end)[0]
        expected = expected_job(root,key).replace('    needs: dispatch\n','    needs: native\n',1)
        require(body == expected, 'canonical guard modified')
        hashes[key] = digest(body.encode())
    return hashes


if __name__ == '__main__':
    print(verify(Path(__file__).resolve().parents[2]))
