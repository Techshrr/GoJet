"""Start missing exact-head producers once; never rerun a failed test silently."""
import json
import os
import urllib.request
import urllib.error
from common import ROOT, HEAD
from release_matrix import validate_inventory, select_latest, REPOSITORY
from release_sources import api_context
from t028_sources import require


def main():
    base, headers, api = api_context()
    from ci.actions import workflow_runs
    ref = os.environ['GITHUB_REF_NAME']
    require(os.environ.get('GITHUB_REF_TYPE') == 'branch', 'closure requires an immutable-in-session branch head')
    runs = workflow_runs(api, REPOSITORY, HEAD)
    failures = []
    for workflow in validate_inventory(ROOT):
        existing = select_latest(runs, workflow, HEAD)
        if existing:
            print(f'Reuse {workflow}: run {existing["id"]} ({existing["status"]}/{existing["conclusion"]})', flush=True)
            continue
        current = api(base + '/git/ref/heads/' + ref)
        require(current['object']['sha'] == HEAD, 'branch moved during matrix launch')
        request = urllib.request.Request(base + '/actions/workflows/' + workflow + '/dispatches',
            data=json.dumps({'ref': ref}).encode(), headers={**headers, 'Content-Type': 'application/json'}, method='POST')
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                require(response.status == 204, 'producer dispatch rejected: ' + workflow)
            print('Started ' + workflow, flush=True)
        except urllib.error.HTTPError as error:
            # Finish independent launches; report names/status only, never tokens.
            failures.append(f'{workflow}: HTTP {error.code}')
    require(not failures, 'matrix launch incomplete: ' + ', '.join(failures))


if __name__ == '__main__':
    main()
