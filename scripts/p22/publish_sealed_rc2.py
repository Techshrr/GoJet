"""Publish only the byte-identical, previously accepted RC bundle.

No application build, migration, deployment, or external-service lookup occurs.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import urllib.error
import urllib.parse
import urllib.request
import zipfile

REPO = 'Techshrr/GoJet'
SOURCE = 'ce305a9207fd2fe46396e7cf433137c48ab5aec1'
TAG = 'v10.0.0-rc.2'
NAME = 'GoJet-Native-10.0.0-rc.2-ce305a9-Full.zip'
DIGEST = 'f16c2e6a851b506c6b08bb54ba78942edaf0618f55a22e913e32d6ca65acfb60'
ROOT = Path(__file__).resolve().parents[2]
API = 'https://api.github.com/repos/' + REPO


def sha(data):
    return hashlib.sha256(data).hexdigest()


def api(path, data=None, method=None):
    headers = {'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
               'Accept': 'application/vnd.github+json', 'Content-Type': 'application/json',
               'X-GitHub-Api-Version': '2022-11-28'}
    request = urllib.request.Request(API + path, headers=headers,
                                     data=json.dumps(data).encode() if data is not None else None,
                                     method=method)
    with urllib.request.urlopen(request, timeout=60) as response:
        return json.load(response)


def artifact(artifact_id, run_id, source, digest, member):
    run = api('/actions/runs/' + str(run_id))
    assert run['head_sha'] == source and run['conclusion'] == 'success'
    meta = api('/actions/artifacts/' + str(artifact_id))
    assert not meta['expired'] and meta['workflow_run']['id'] == run_id
    assert meta['digest'] == 'sha256:' + digest
    # Never forward the GitHub token to the artifact storage redirect.
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *args, **kwargs):
            return None
    request = urllib.request.Request(API + '/actions/artifacts/' + str(artifact_id) + '/zip',
                                    headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN']})
    try:
        urllib.request.build_opener(NoRedirect()).open(request, timeout=60)
        raise RuntimeError('Expected artifact storage redirect')
    except urllib.error.HTTPError as error:
        if error.code != 302:
            raise
        url = error.headers['Location']
    assert urllib.parse.urlsplit(url).scheme == 'https'
    with urllib.request.urlopen(url, timeout=180) as response:
        raw = response.read()
    assert sha(raw) == digest
    with zipfile.ZipFile(io.BytesIO(raw)) as z:
        return z.read(member)


def assemble(native, risk, output):
    assert sha(native) == '60c1a29b0a331a8a78a956984e105ba2ba8c72d4be3065ec55cf177e6a32141e'
    assert sha(risk) == 'e6045d32ce89af0d8640a3d069fb849251a0f5acdd414cf224654fdc308d6db0'
    with zipfile.ZipFile(io.BytesIO(native)) as z:
        prefix = z.namelist()[0].split('/')[0] + '/'
        assert all(n.startswith(prefix) and '..' not in Path(n).parts for n in z.namelist())
        files = {n[len(prefix):]: z.read(n) for n in z.namelist()}
    for line in files['SHA256SUMS'].decode().splitlines():
        digest, name = line.split('  ', 1)
        assert sha(files[name]) == digest
    files['NATIVE-SHA256SUMS'] = files.pop('SHA256SUMS')
    acceptance = json.loads(files['acceptance.json'])
    assert acceptance['native_ci_passed'] and acceptance['source'] == SOURCE
    assert acceptance['verification_source'] == '9913180dfed3acd017f401fc4caffa010bfae133'
    for name in ['README-ZH.md', 'BUNDLE.json', 'evidence/webrisk-local-tests.json',
                 'verification-fixes-9913180.patch']:
        files[name] = (ROOT / 'scripts/p22/release-overlay' / name).read_bytes()
    files['integrations/GoJet-WebRisk-1.0.0.zip'] = risk
    files['SHA256SUMS'] = ''.join(sha(data) + '  ' + name + '\n'
                                  for name, data in sorted(files.items())).encode()
    output.mkdir(parents=True, exist_ok=True)
    archive = output / NAME
    with zipfile.ZipFile(archive, 'w', compression=zipfile.ZIP_DEFLATED) as z:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(NAME[:-4] + '/' + name, date_time=(2026, 11, 8, 0, 0, 0))
            info.external_attr = 0o100644 << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(info, data)
    assert sha(archive.read_bytes()) == DIGEST, 'Sealed bundle byte identity mismatch'
    checksum = output / (NAME + '.sha256')
    checksum.write_text(DIGEST + '  ' + NAME + '\n')
    return [archive, checksum]


def publish(paths):
    # Preserve existing releases and assets; never replace a conflicting upload.
    try:
        release = api('/releases/tags/' + TAG)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        release = None
    try:
        ref = api('/git/ref/tags/' + TAG)
        assert ref['object']['type'] == 'commit' and ref['object']['sha'] == SOURCE
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
        assert release is None
        api('/git/refs', {'ref': 'refs/tags/' + TAG, 'sha': SOURCE}, 'POST')
    body = (ROOT / 'docs/p22/rc2-release-notes.md').read_text()
    title = 'GoJet 10.0.0-rc.2 — 完整原生安装候选包'
    if release is None:
        release = api('/releases', {'tag_name': TAG, 'target_commitish': SOURCE,
                      'name': title, 'body': body, 'draft': True, 'prerelease': True,
                      'make_latest': 'false'}, 'POST')
    assert release['prerelease'] and release['body'] == body and release['name'] == title
    existing = {a['name']: a for a in api('/releases/' + str(release['id']) + '/assets')}
    assert not (set(existing) - {p.name for p in paths})
    for path in paths:
        raw = path.read_bytes()
        digest = 'sha256:' + sha(raw)
        if path.name in existing:
            asset = existing[path.name]
        else:
            assert release['draft'], 'Do not modify an already published release'
            url = release['upload_url'].split('{')[0]
            assert urllib.parse.urlsplit(url).netloc == 'uploads.github.com'
            url += '?' + urllib.parse.urlencode({'name': path.name})
            request = urllib.request.Request(url, data=raw, method='POST', headers={
                'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
                'Content-Type': 'application/octet-stream'})
            with urllib.request.urlopen(request, timeout=180) as response:
                asset = json.load(response)
        assert asset['size'] == len(raw) and asset['digest'] == digest
        assert asset['state'] == 'uploaded'
    if release['draft']:
        release = api('/releases/' + str(release['id']), {'draft': False,
                      'prerelease': True, 'make_latest': 'false'}, 'PATCH')
    assert not release['draft'] and release['prerelease']
    print(json.dumps({'published': True, 'url': release['html_url'],
                      'sha256': DIGEST, 'production_deployed': False}))


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--local-native', type=Path)
    parser.add_argument('--local-risk', type=Path)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--publish', action='store_true')
    args = parser.parse_args()
    if args.local_native or args.local_risk:
        assert args.local_native and args.local_risk and not args.publish
        native, risk = args.local_native.read_bytes(), args.local_risk.read_bytes()
    else:
        assert os.environ.get('GITHUB_REPOSITORY') == REPO
        native = artifact(13617965772, 42481392530,
                          '9913180dfed3acd017f401fc4caffa010bfae133',
                          '61dc7fae676c900fbfa5f55d2e43068d444a0d60157f5a6d18c5e8088451b7c6',
                          'GoJet-Native-10.0.0-rc.2-ce305a9.zip')
        risk = artifact(13565551491, 42350398388,
                        'c98d42848f405d0bfe3364b6b918b8070b2f4870',
                        '04cd7047956deb3f475146c3b20a4119ee42c62f6a90afb2fbf7f5b19623c7ae',
                        'GoJet-WebRisk-1.0.0.zip')
    paths = assemble(native, risk, args.output)
    print('Sealed bundle SHA-256 verified: ' + DIGEST)
    if args.publish:
        publish(paths)
