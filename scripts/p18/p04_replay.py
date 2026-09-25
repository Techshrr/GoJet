"""Verify a retained replay of the original P04 source; never re-sign history."""
import hashlib
import io
import json
import urllib.request
import zipfile

SOURCE = '659e25e3c5e263ffb3dd74cde953e812b75a7439'
ORIGINAL = {
    'reviewed_pre_sign_commit': SOURCE,
    'workflow_run_id': 32392744860,
    'artifact_id': 9415518410,
    'artifact_digest': 'sha256:5f6b4ec5be87d866b07599e8bd32d75171a81523d29dd86441a524bf33cbc7bb',
    'required_tests': '10/10',
}
REPLAY = {
    'workflow_run_id': 35754013183,
    'workflow_head': '281519b3ede0aeba179e3e069772a184bf54bcff',
    'artifact_id': 10708499392,
    'artifact_digest': 'sha256:a7f219b2f3a3f71dbd0a9d620d70a7a99f56083f0a8bd8c6ec3d0d9d02501143',
    'source_commit': SOURCE,
    'reason': 'original artifact retention expired; original run is too old to rerun',
    'archive_verified': True,
    'merge_authoritative': False,
}


def verify_archive(data):
    if 'sha256:' + hashlib.sha256(data).hexdigest() != REPLAY['artifact_digest']:
        raise RuntimeError('P04 replay archive digest mismatch')
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        names = archive.namelist()
        if len(names) != len(set(names)):
            raise RuntimeError('P04 replay duplicate archive paths')
        source = json.loads(archive.read('P04/source.json'))
        index = json.loads(archive.read('P04/evidence-index.json'))
        if source.get('implementation_commit') != SOURCE or index.get('implementation_commit') != SOURCE:
            raise RuntimeError('P04 replay source mismatch')
        if index.get('results') != {'passed': 10, 'failed': 0, 'total': 10}:
            raise RuntimeError('P04 replay result summary mismatch')
        for entry in index['files']:
            path = entry['path']
            if not path.startswith('artifacts/v10/'):
                raise RuntimeError('P04 replay indexed path mismatch')
            content = archive.read(path.removeprefix('artifacts/v10/'))
            if hashlib.sha256(content).hexdigest() != entry['sha256']:
                raise RuntimeError('P04 replay indexed file digest mismatch')
        for number in range(1, 11):
            case = f'P04-T{number:03d}'
            result = json.loads(archive.read(f'P04/results/{case}.json'))
            if result.get('case_id') != case or result.get('status') != 'PASS' or result.get('errors') != []:
                raise RuntimeError('P04 replay case result mismatch')


class PublicDownloadRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is not None:
            redirected.remove_header('Authorization')
        return redirected


def bind(repository, token):
    if repository != 'Techshrr/GoJet':
        raise RuntimeError('P04 replay repository mismatch')
    base = f'https://api.github.com/repos/{repository}'
    opener = urllib.request.build_opener(PublicDownloadRedirect())
    def read(path, limit=4 * 1024 * 1024):
        request = urllib.request.Request(base + path, headers={
            'Accept': 'application/vnd.github+json', 'Authorization': f'Bearer {token}',
            'X-GitHub-Api-Version': '2022-11-28'})
        with opener.open(request, timeout=60) as response:
            data = response.read(limit + 1)
        if len(data) > limit:
            raise RuntimeError('P04 replay response exceeds size limit')
        return data
    run = json.loads(read(f'/actions/runs/{REPLAY["workflow_run_id"]}'))
    artifact = json.loads(read(f'/actions/artifacts/{REPLAY["artifact_id"]}'))
    if not (run.get('head_sha') == REPLAY['workflow_head']
            and run.get('path') == '.github/workflows/p04-historical-replay.yml'
            and run.get('status') == 'completed' and run.get('conclusion') == 'success'
            and artifact.get('expired') is False
            and artifact.get('digest') == REPLAY['artifact_digest']
            and artifact.get('workflow_run', {}).get('id') == REPLAY['workflow_run_id']
            and artifact.get('workflow_run', {}).get('head_sha') == REPLAY['workflow_head']):
        raise RuntimeError('P04 replay live metadata mismatch')
    verify_archive(read(f'/actions/artifacts/{REPLAY["artifact_id"]}/zip'))
    return {**ORIGINAL, 'retained_replay': dict(REPLAY)}
