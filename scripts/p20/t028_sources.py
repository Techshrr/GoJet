"""Exact-head Actions sources for T028; section PASS never replaces raw evidence."""
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import zipfile
import urllib.error
import urllib.request

# workflow file, successful job (only when historical closure is a separate job),
# section artifact, raw artifact, raw node.
SOURCES = {
    'destination': ('p16-evidence.yml', None, 'p20-t028-destination-', 'gojet-v10-p16-evidence-', 'P16'),
    'domain': ('p06-integration.yml', None, 'p20-t028-domain-', 'gojet-v10-p06-integration-', 'P06'),
    'files': ('p09-evidence.yml', None, 'p20-t028-files-', 'gojet-v10-p09-evidence-', 'P09'),
    'admin': ('p17-evidence.yml', None, 'p20-t028-admin-', 'gojet-v10-p17-evidence-', 'P17'),
    'auth': ('p15-authentication-oauth-account.yml', 'P15 contract guard and T028 coherence',
             'p20-t028-auth-', 'p15-t028-evidence-coherence-', 'P15'),
    'support': ('p14-support-tickets-mail.yml', 'P14 contract freeze',
                'p20-t028-support-', 'gojet-v10-p14-evidence-', 'P14'),
    'entitlement': ('p13-evidence.yml', None, 'p20-t028-entitlement-', 'gojet-v10-p13-evidence-', 'P13'),
}
EXTRA = {
    'domain-browser': ('p06-browser.yml', 'gojet-v10-p06-browser-', 'P06',
                       ['results/P06-T023.json']),
    't027': ('p12-browser.yml', 'gojet-v10-p12-browser-', 'P20',
             ['runtime/t027/workspace-rbac.jsonl', 'runtime/t027/native-manifest.json',
              'runtime/t027/workspace-browser.json', 'runtime/t027/workspace-browser.jsonl',
              'consistency/P20-T027.json']),
}
GATES = ['p20-whole-product-verification.yml', 'p20-candidate-freeze.yml',
         'p12-evidence.yml', 'p20-p0-support.yml']


def digest(raw):
    return 'sha256:' + hashlib.sha256(raw).hexdigest()


def select_artifact(rows, name, run, job=None):
    matches = [a for a in rows if a.get('name') == name and a.get('expired') is False]
    if len(matches) > 1:
        # Retrying a failed producer retains the earlier attempt's upload.
        # Bind duplicates to the admitted job/run attempt, never an older green.
        from datetime import datetime
        boundary = (job or {}).get('started_at') or run.get('run_started_at')
        require(bool(boundary), 'artifact attempt boundary missing')
        start = datetime.fromisoformat(boundary.replace('Z', '+00:00'))
        matches = [a for a in matches if datetime.fromisoformat(a['created_at'].replace('Z', '+00:00')) >= start]
    require(len(matches) == 1, 'missing/ambiguous artifact: ' + name)
    require(matches[0].get('workflow_run', {}).get('id') == run['id'], 'artifact run mismatch')
    return matches[0]


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def member(archive, node, relative):
    """Read a unique known suffix; never extract untrusted archive paths."""
    wanted = PurePosixPath(relative)
    require(not wanted.is_absolute() and '..' not in wanted.parts, 'unsafe source path')
    candidates = {str(wanted), f'{node}/{wanted}', f'artifacts/v10/{node}/{wanted}'}
    matches = [info for info in archive.infolist() if info.filename in candidates]
    require(len(matches) == 1 and not matches[0].is_dir(), 'missing/ambiguous source: ' + relative)
    require(matches[0].file_size <= 32 * 1024 * 1024, 'oversized evidence file')
    return archive.read(matches[0])


def verified_archive(raw, artifact, head):
    require(artifact.get('expired') is False and artifact.get('digest') == digest(raw),
            'expired artifact or archive digest mismatch')
    require(artifact.get('workflow_run', {}).get('head_sha') == head, 'artifact head mismatch')
    return zipfile.ZipFile(io.BytesIO(raw))


def select_run(runs, workflow, head, repository):
    candidates = [r for r in runs if r.get('path') == '.github/workflows/' + workflow
                  and r.get('head_sha') == head and r.get('event') in ('pull_request', 'workflow_dispatch')
                  and r.get('head_repository', {}).get('full_name') == repository]
    return max(candidates, key=lambda r: (r['id'], r.get('run_attempt', 1))) if candidates else None


def successful(run, job_name, jobs):
    if job_name:
        selected = [j for j in jobs if j.get('name') == job_name]
        require(len(selected) <= 1, 'ambiguous producer job')
        if not selected or selected[0].get('status') != 'completed':
            return False
        require(selected[0].get('conclusion') == 'success', 'required producer job failed: ' + job_name)
        return True
    if run.get('status') != 'completed':
        return False
    require(run.get('conclusion') == 'success', 'required producer failed: ' + run['path'])
    return True


def download_archive(url, headers):
    """Keep GitHub authorization on the API host, not its signed redirect."""
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, hdrs, newurl):
            return None
    request = urllib.request.Request(url, headers=headers)
    try:
        response = urllib.request.build_opener(NoRedirect).open(request, timeout=60)
    except urllib.error.HTTPError as error:
        require(error.code == 302, 'artifact download failed')
        location = error.headers['Location']
        require(location.startswith('https://'), 'insecure artifact redirect')
        response = urllib.request.urlopen(location, timeout=120)
    with response:
        return response.read()


def collect(root: Path, head: str):
    import os
    import sys
    import time
    sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
    from ci.actions import github_json, workflow_runs
    repository = os.environ['GITHUB_REPOSITORY']
    headers = {'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
               'Accept': 'application/vnd.github+json'}
    api = lambda url: github_json(url, headers)
    base = 'https://api.github.com/repos/' + repository
    requirements = {v[0]: v[1] for v in SOURCES.values()}
    requirements.update({v[0]: None for v in EXTRA.values()})
    requirements.update({name: None for name in GATES})
    deadline = time.monotonic() + 150 * 60
    while True:
        # Same-head manually dispatched repairs are valid producers too. Keep
        # repository/path/SHA checks in select_run; never select by branch alone.
        runs = workflow_runs(api, repository, head)
        selected, pending = {}, []
        for workflow, job in requirements.items():
            run = select_run(runs, workflow, head, repository)
            jobs = api(base + f'/actions/runs/{run["id"]}/attempts/{run.get("run_attempt", 1)}/jobs?per_page=100')['jobs'] if run and job else []
            if run and successful(run, job, jobs):
                selected[workflow] = {'run': run, 'job': next((j for j in jobs if j['name'] == job), None)}
            else:
                pending.append(workflow)
        if not pending:
            break
        require(time.monotonic() < deadline, 'timed out waiting for ' + ', '.join(pending))
        print('T028 waiting: ' + ', '.join(pending), flush=True)
        time.sleep(45)

    source_root = root / 'artifacts/v10/P20/runtime/t028/sources'
    # Never admit files left over from checkout or an earlier attempt.
    require(not source_root.exists(), 'T028 source directory must start empty')
    source_root.mkdir(parents=True)
    provenance, archives = {}, {}

    def archive(workflow, prefix):
        key = prefix + head
        if key in archives:
            return archives[key]
        run = selected[workflow]['run']
        rows = api(base + f'/actions/runs/{run["id"]}/artifacts?per_page=100')['artifacts']
        require(len(rows) < 100, 'artifact listing completeness unproven')
        artifact = select_artifact(rows, key, run, selected[workflow]['job'])
        raw = download_archive(base + f'/actions/artifacts/{artifact["id"]}/zip', headers)
        archives[key] = verified_archive(raw, artifact, head)
        provenance[key] = {'workflow': workflow, 'run_id': run['id'],
                           'run_attempt': run.get('run_attempt', 1), 'head_sha': head,
                           'job_id': (selected[workflow]['job'] or {}).get('id'),
                           'artifact_id': artifact['id'], 'archive_sha256': digest(raw)}
        return archives[key]

    files = {}
    def save(path, raw, artifact_name):
        require(path.startswith('artifacts/v10/') and '..' not in PurePosixPath(path).parts,
                'unsafe destination')
        require(path not in files, 'duplicate evidence source')
        destination = source_root / path
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(raw)
        files[path] = {'sha256': digest(raw), 'artifact': artifact_name + head}

    for section, (workflow, _, section_prefix, raw_prefix, node) in SOURCES.items():
        raw_section = member(archive(workflow, section_prefix), 'P20', f'P20-T028-{section}.json')
        result = json.loads(raw_section)
        require(result.get('case') == 'P20-T028-' + section and result.get('status') == 'PASS'
                and result.get('implementation_commit') == head and result.get('errors') == [],
                'section evidence mismatch')
        save(f'artifacts/v10/P20/consistency/P20-T028-{section}.json', raw_section, section_prefix)
        for ref in result['details']['source_evidence']:
            prefix = f'artifacts/v10/{node}/'
            require(ref['path'].startswith(prefix), 'section references wrong authority')
            raw = member(archive(workflow, raw_prefix), node, ref['path'][len(prefix):])
            require(digest(raw) == ref['sha256'], 'section source digest mismatch')
            save(ref['path'], raw, raw_prefix)
        if section == 'admin':
            relative = 'cases/P17-T033.json'
            save(f'artifacts/v10/{node}/{relative}', member(archive(workflow, raw_prefix), node, relative), raw_prefix)
    for workflow, prefix, node, paths in EXTRA.values():
        for relative in paths:
            save(f'artifacts/v10/{node}/{relative}', member(archive(workflow, prefix), node, relative), prefix)
    manifest = {'implementation_commit': head, 'artifacts': provenance, 'files': files,
                'gates': {name: {'run_id': selected[name]['run']['id'], 'head_sha': head,
                                 'conclusion': 'success'} for name in GATES}}
    (source_root.parent / 'collection.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(f'T028 collected {len(files)} digest-bound source files from {len(archives)} archives', flush=True)


if __name__ == '__main__':
    from common import ROOT, HEAD
    collect(ROOT, HEAD)
