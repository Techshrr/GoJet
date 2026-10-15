"""Live-bind original signed closure reports after ZIP retention expires.

This does not recreate a ZIP, rerun history, issue a signature, or assert that
expired artifact bytes are available. GitHub's original successful job log
retains the exact signed report and upload ID/digest attestation. Both are
validated against the independently inspected original records and retained
with current evidence. Historical oracle and signed reviews remain unchanged.
"""
import hashlib
import json
import re
import urllib.error
import urllib.request
from pathlib import Path

from scripts.ci.actions import github_json

AUTHORITIES = {
    'P17': {
        'head': '5818406072a131db1c7d8aa7bc5ef8a7adc8d51f', 'run': 33232541982,
        'job': 99047747744, 'artifact': 9709093486,
        'archive_digest': '72f8256b242c4412c82cfd4e69c653e4051dc2b7a951c10c9214c2db775805c1',
        'report_digest': 'b24cc33201e158117de5a776773c3b563a42c1804027cf61aca3c614890b36ed',
        'case': 'P17-T035', 'matrix_count': 66,
    },
    'P18': {
        'head': 'e8746159b02c729a877e3dcbd9655d415a5cc269', 'run': 33260817755,
        'job': 99122329425, 'artifact': 9717210947,
        'archive_digest': '3e403765409b3ab273be1c35a9d88b565505c416a47364d9a6f0339cc130efe4',
        'report_digest': 'a656623273ac200aa156fb6742c4cb0f8f978aeb1c0c6d3736d41d2f5884436e',
        'case': 'P18-T026', 'matrix_count': 10,
    },
}


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def inspect_log(raw, node):
    authority = AUTHORITIES[node]
    require(len(raw) <= 2 * 1024 * 1024, 'oversized original closure log')
    text = re.sub(r'^\d{4}-\d\d-\d\dT\S+Z ', '', raw.decode('utf-8-sig'), flags=re.M)
    upload = f"SHA256 digest of uploaded artifact zip is {authority['archive_digest']}"
    name = f"gojet-v10-{node.lower()}-closure-{authority['head']}"
    final = f"Artifact {name}.zip successfully finalized. Artifact ID {authority['artifact']}"
    require(text.splitlines().count(upload) == 1 and text.splitlines().count(final) == 1,
            'original artifact upload ID/digest attestation missing or ambiguous')
    reports = []
    for match in re.finditer(r'^\{', text, re.M):
        try:
            report, _ = json.JSONDecoder().raw_decode(text[match.start():])
        except ValueError:
            continue
        if report.get('node') == node and report.get('case') == authority['case']:
            reports.append(report)
    require(len(reports) == 1, 'original signed closure report missing or ambiguous')
    report = reports[0]
    canonical = json.dumps(report, sort_keys=True, separators=(',', ':')).encode()
    require(hashlib.sha256(canonical).hexdigest() == authority['report_digest'], 'original signed report digest mismatch')
    require(report['implementation_commit'] == authority['head'] and report['status'] == 'PASS'
            and report['errors'] == [] and report['phase'] == report['review_phase'] == 'signed'
            and report['review_only_signed_child'] is True and report['merge_authoritative'] is True
            and report['signing_required'] is False
            and report['defects'] == {'p0': 0, 'p1': 0, 'decision_required': 0}
            and report['affected_matrix']['complete'] is True
            and report['affected_matrix']['passed'] == report['affected_matrix']['required'] == authority['matrix_count'],
            'original closure authority incomplete')
    return report


def read_log(url, headers):
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args):
            return None
    request = urllib.request.Request(url, headers=headers)
    try:
        response = urllib.request.build_opener(NoRedirect).open(request, timeout=60)
    except urllib.error.HTTPError as error:
        require(error.code == 302, 'original job log unavailable')
        location = error.headers['Location']
        require(location.startswith('https://'), 'insecure job log redirect')
        # GitHub credentials must never reach the blob host.
        response = urllib.request.urlopen(location, timeout=60)
    with response:
        raw = response.read(2 * 1024 * 1024 + 1)
    return raw


def bind(node, repository, token, directory):
    require(repository == 'Techshrr/GoJet', 'untrusted historical repository')
    authority = AUTHORITIES[node]
    base = 'https://api.github.com/repos/' + repository
    headers = {'Accept': 'application/vnd.github+json', 'Authorization': 'Bearer ' + token}
    run = github_json(base + f'/actions/runs/{authority["run"]}', headers)
    job = github_json(base + f'/actions/jobs/{authority["job"]}', headers)
    require(run['head_sha'] == authority['head'] and run['status'] == 'completed'
            and run['conclusion'] == 'success'
            and run['path'] == f'.github/workflows/{node.lower()}-closure.yml'
            and run['head_repository']['full_name'] == repository, 'original closure run mismatch')
    require(job['run_id'] == authority['run'] and job['head_sha'] == authority['head']
            and job['status'] == 'completed' and job['conclusion'] == 'success', 'original closure job mismatch')
    raw = read_log(base + f'/actions/jobs/{authority["job"]}/logs', headers)
    report = inspect_log(raw, node)
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    (directory / f'{node}-original-job.log').write_bytes(raw)
    (directory / f'{node}-signed-report.json').write_text(json.dumps(report, indent=2) + '\n')
    binding = {'binding_mode': 'original_successful_job_log', 'original_archive_available': False,
               'original_run_id': authority['run'], 'original_job_id': authority['job'],
               'original_head': authority['head'], 'original_artifact_id': authority['artifact'],
               'original_archive_sha256': 'sha256:' + authority['archive_digest'],
               'original_report_sha256': 'sha256:' + authority['report_digest'],
               'original_job_log_sha256': 'sha256:' + hashlib.sha256(raw).hexdigest(),
               'new_signature_issued': False, 'historical_execution_reinterpreted': False}
    (directory / f'{node}-binding.json').write_text(json.dumps(binding, indent=2) + '\n')
    return binding
