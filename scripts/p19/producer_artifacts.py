"""Bind P19 evidence to one successful producer attempt, including after retries."""
from datetime import datetime


def select_artifact(rows, name, run, head):
    if (run.get('status') != 'completed' or run.get('conclusion') != 'success'
            or run.get('head_sha') != head):
        raise ValueError('P19 producer is not successful on the exact head')
    if not isinstance(rows, list) or len(rows) >= 100:
        raise ValueError('P19 artifact listing is incomplete')
    # A rerun keeps previous uploads. A sole stale upload is also inadmissible.
    start = datetime.fromisoformat(run['run_started_at'].replace('Z', '+00:00'))
    matches = [item for item in rows
               if item.get('name') == name and item.get('expired') is False
               and datetime.fromisoformat(item['created_at'].replace('Z', '+00:00')) >= start]
    if len(matches) != 1:
        raise ValueError('P19 current-attempt artifact is missing or ambiguous: ' + name)
    item = matches[0]
    if (item.get('workflow_run', {}).get('id') != run['id']
            or item.get('workflow_run', {}).get('head_sha') != head):
        raise ValueError('P19 artifact run or head mismatch')
    return item
