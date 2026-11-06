"""Complete exact-commit Actions discovery shared by evidence collectors."""
import urllib.parse
import urllib.request
import urllib.error
import json
import time
from http.client import IncompleteRead


def github_json(url, headers, *, opener=urllib.request.urlopen, sleep=time.sleep,
                clock=time.time, max_wait=1200):
    """Retry throttling and bounded transport failures; invalid evidence stays fatal."""
    deadline = clock() + max_wait
    transport_failures = 0
    while True:
        try:
            with opener(urllib.request.Request(url, headers=headers), timeout=30) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            body = exc.read(8192).decode('utf-8', errors='replace').lower()
            rate_limited = exc.code == 429 or (exc.code == 403 and (
                exc.headers.get('X-RateLimit-Remaining') == '0'
                or 'secondary rate limit' in body or 'api rate limit exceeded' in body))
            if not rate_limited:
                raise
            try:
                delay = float(exc.headers.get('Retry-After', '60'))
                if exc.headers.get('X-RateLimit-Remaining') == '0':
                    delay = max(delay, float(exc.headers.get('X-RateLimit-Reset', '0')) - clock() + 1)
            except ValueError:
                delay = 60
            delay = max(1, delay)
            if clock() + delay > deadline:
                raise RuntimeError('GitHub API throttling exceeded evidence retry budget') from None
            print(f'GitHub API throttled; retry in {int(delay)} seconds', flush=True)
            while delay > 0:
                interval = min(delay, 60)
                sleep(interval)
                delay -= interval
        except (IncompleteRead, TimeoutError, urllib.error.URLError) as exc:
            # urllib wraps connect timeouts; response reads can raise directly.
            # Do not retry certificate, DNS, permission or other URL failures.
            if isinstance(exc, urllib.error.URLError) and not isinstance(exc.reason, TimeoutError):
                raise
            # Restart the same GET, discarding any partial body. Timeouts and
            # truncation share one bounded retry allowance and the rate-limit deadline.
            transport_failures += 1
            delay = 2 ** (transport_failures - 1)
            if transport_failures >= 3 or clock() + delay > deadline:
                raise
            reason = 'response truncated' if isinstance(exc, IncompleteRead) else 'request timed out'
            print(f'GitHub API {reason}; retry in {delay} seconds', flush=True)
            sleep(delay)


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


def regression_runs(api, repository, head_sha):
    """Trusted same-head PR or manually dispatched regression producers.

    Historical closure callers retain workflow_runs(event='pull_request').
    Current regression collectors may use either execution entry point, with
    repository, SHA and event checked again after the API query.
    """
    return [row for row in workflow_runs(api, repository, head_sha)
            if row.get('head_sha') == head_sha
            and row.get('head_repository', {}).get('full_name') == repository
            and row.get('event') in ('pull_request', 'workflow_dispatch')]
