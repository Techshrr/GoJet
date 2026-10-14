"""T049: pre-sign proof, or a direct review-only child's verified closure.

This program never writes a signature or manufactures accountable review.
"""
import json
import re
import subprocess

from common import ROOT, HEAD, emit, fail_if_errors
from evidence_coherence import inspect as inspect_coherence
from evidence_index import valid_case
from release_sources import api_context, list_complete
from t028_sources import require, digest, verified_archive, member, download_archive
from validate_contract import PENDING, SIGNED


def review_fields(review):
    patterns = {
        'head': r'Reviewed pre-sign implementation SHA: `([0-9a-f]{40})`',
        'run': r'Pre-sign T049 closure run: `([0-9]+)`',
        'artifact': r'Pre-sign T049 closure artifact: `([0-9]+)`',
        'digest': r'Pre-sign T049 closure digest: `(sha256:[0-9a-f]{64})`',
    }
    fields = {}
    for key, pattern in patterns.items():
        matches = re.findall(pattern, review)
        require(len(matches) == 1, 'missing/ambiguous review field: ' + key)
        fields[key] = matches[0]
    require('Evidence disposition: `P20-T001..P20-T048 PASS`' in review
            and 'P0/P1/DECISION REQUIRED: `0/0/0`' in review, 'review disposition incomplete')
    return fields


def review_only(root, head, parent):
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=root, text=True).strip()
    parents = git('rev-list', '--parents', '-n', '1', head).split()
    require(parents == [head, parent], 'signed revision is not a direct single-parent child')
    require(git('diff', '--name-only', parent, head).splitlines() == ['artifacts/v10/P20/review.md'],
            'signed child includes implementation or evidence changes')


def verify_pre_sign(root, fields):
    base, headers, api = api_context()
    run_id, artifact_id = int(fields['run']), int(fields['artifact'])
    run = api(base + f'/actions/runs/{run_id}')
    require(run['head_sha'] == fields['head'] and run['path'] == '.github/workflows/p20-closure.yml'
            and run['head_repository']['full_name'] == 'Techshrr/GoJet'
            and run['status'] == 'completed' and run['conclusion'] == 'success', 'invalid pre-sign closure run')
    rows = list_complete(api, base + f'/actions/runs/{run_id}/artifacts', 'artifacts')
    matches = [a for a in rows if a['id'] == artifact_id]
    require(len(matches) == 1, 'pre-sign artifact not in reviewed run')
    artifact = matches[0]
    require(artifact['digest'] == fields['digest'] and artifact['workflow_run']['id'] == run_id
            and artifact['name'] == 'gojet-v10-p20-closure-' + fields['head'], 'pre-sign artifact binding mismatch')
    raw = download_archive(base + f'/actions/artifacts/{artifact_id}/zip', headers)
    with verified_archive(raw, artifact, fields['head']) as archive:
        proof = json.loads(member(archive, 'P20', 'closure/P20-T049.json'))
    require(valid_case(proof, 'P20-T049', fields['head']), 'pre-sign closure not PASS')
    details = proof['details']
    require(details['phase'] == 'pre-sign' and details['review_phase'] == 'pending'
            and details['merge_authoritative'] is False and details['review_only_signed_child'] is False
            and details['evidence_range'] == 'P20-T001..P20-T048'
            and details['defects'] == {'p0': 0, 'p1': 0, 'decision_required': 0}, 'invalid pre-sign disposition')
    path = root / 'artifacts/v10/P20/runtime/closure-sources/pre-sign.zip'
    path.write_bytes(raw)
    return {'head_sha': fields['head'], 'run_id': run_id, 'artifact_id': artifact_id,
            'archive_sha256': digest(raw)}


def inspect(root, head):
    expected = inspect_coherence(root, head)
    raw = (root / 'artifacts/v10/P20/coherence/P20-T048.json').read_bytes()
    coherence = json.loads(raw)
    require(valid_case(coherence, 'P20-T048', head) and coherence['details'] == expected,
            'T048 does not reproduce from original archives')
    review = (root / 'artifacts/v10/P20/review.md').read_text()
    status = re.findall(r'^Status: \*\*[^\n]+\*\*$', review, re.MULTILINE)
    require(status in ([PENDING], [SIGNED]), 'invalid accountable review status')
    signed = status == [SIGNED]
    predecessor = None
    if signed:
        fields = review_fields(review)
        review_only(root, head, fields['head'])
        predecessor = verify_pre_sign(root, fields)
    return {'formal_p20_t049_claim': True, 'phase': 'signed' if signed else 'pre-sign',
            'review_phase': 'signed' if signed else 'pending',
            'review_only_signed_child': signed, 'merge_authoritative': signed,
            'evidence_range': 'P20-T001..P20-T048', 't048_sha256': digest(raw),
            'matrix_count': expected['matrix_count'], 'matrix': expected['matrix'],
            'gate_ledger_sha256': expected['gate_ledger_sha256'],
            'collection_sha256': expected['collection_sha256'], 'defects': expected['defects'],
            'pre_sign': predecessor, 'p21_p22_completion_claim': False,
            'production_deployment_claim': False}


def main():
    errors, details = [], {'merge_authoritative': False}
    try:
        details = inspect(ROOT, HEAD)
    except (ValueError, KeyError, TypeError, OSError, subprocess.CalledProcessError) as error:
        errors.append('T049: ' + str(error))
    fail_if_errors([emit('P20-T049', 'closure', 'Accountable exact-head Whole Product closure', errors, details)])


if __name__ == '__main__':
    main()
