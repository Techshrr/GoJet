"""Complete exact-commit Actions discovery shared by evidence collectors."""
import urllib.parse


def workflow_runs(api, repository, head_sha, *, event=None):
    runs = []
    for page in range(1, 11):
        params = {'head_sha': head_sha, 'per_page': 100, 'page': page}
        if event is not None:
            params['event'] = event
        query = urllib.parse.urlencode(params)
        payload = api(f'https://api.github.com/repos/{repository}/actions/runs?{query}')
        rows = payload['workflow_runs']
        if not isinstance(rows, list) or len(rows) > 100 or any(not isinstance(row, dict) for row in rows):
            raise RuntimeError('Malformed Actions workflow listing')
        runs.extend(rows)
        if len(rows) < 100:
            return runs
    raise RuntimeError('Actions listing reached the 1000-run search limit; completeness is unproven')
