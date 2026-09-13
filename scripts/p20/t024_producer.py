"""Real HTTP product event to durable queue, explicitly without delivery proof."""
import json
import os
import subprocess

from t022_case import HEAD, http_json, scalar, q


def verify_producer(workspace, auth_headers):
    def check(value):
        if not value:
            raise ValueError('production webhook queue assertion failed')

    def headers():
        status, _, me = http_json('GET', '/api/me', headers=auth_headers)
        check(status == 200 and bool(me.get('csrf_token')))
        return {**auth_headers, 'Origin': os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],
                'X-CSRF-Token': me['csrf_token']}

    path = f'/api/workspaces/{workspace}/webhooks'
    # Resolve and store an ordinary valid public URL; this queue-only probe
    # never invokes delivery or makes an HTTP request to that endpoint.
    status, _, created = http_json('POST', path, {
        'name': 'P20 production queue verification',
        'endpoint_url': 'https://example.com/p20-queue-only',
        'events': ['link.created'],
    }, headers())
    check(status == 201)
    webhook_id = created['webhook']['id']
    status, _, link = http_json('POST', f'/api/workspaces/{workspace}/links', {
        'hostname': 'gojet.cc', 'domain_kind': 'official',
        'code': 'p20wh' + HEAD[:12], 'title': 'P20 queue verification',
        'primary_destination': 'https://example.com/p20-product-event',
        'redirect_status': 302, 'routing': [], 'ab': [], 'utm': {},
        'access': {}, 'one_time': False, 'change_reason': 'P20 queue verification',
    }, headers())
    check(status == 201)
    link_id = int(link['id'])
    audit_id = int(scalar(f"SELECT id FROM link_audit_events WHERE workspace_id={q(workspace)} "
                          f"AND link_id={link_id} AND action='link.create' AND result='success'"))
    def reconcile():
        result = subprocess.run(['go', 'run', './scripts/p20/webhook_producer_probe'],
                                capture_output=True, text=True, timeout=90)
        check(result.returncode == 0)
        return int(result.stdout.strip())

    check(reconcile() == 1)
    check(reconcile() == 0)
    status, _, response = http_json('GET', path + '/' + webhook_id + '/deliveries', headers=auth_headers)
    deliveries = response.get('deliveries', [])
    check(status == 200 and len(deliveries) == 1)
    row = deliveries[0]
    check(row['event_id'] == 'link-audit-' + str(audit_id)
          and row['event_type'] == 'link.created' and row['attempts'] == 0)
    raw = scalar(f'SELECT CONVERT(body USING utf8mb4) FROM workspace_webhook_deliveries '
                 f'WHERE webhook_id={q(webhook_id)} AND event_id={q(row["event_id"])}')
    envelope = json.loads(raw)
    check(envelope['data'] == {'link_id': link_id, 'version': int(link['version'])}
          and envelope['workspace_id'] == workspace)
    check(created['secret'] not in raw)
    # Queue-only evidence must never leave a non-test recipient active when
    # subsequent verification starts the real operationsmonitor.
    status, _, disabled = http_json('POST', path + '/' + webhook_id + '/disable', {}, headers())
    check(status == 200 and disabled['webhook']['status'] == 'disabled')
    return {'webhook_producer_passed': True, 'real_link_http_create': True,
            'committed_link_audit_bound': True, 'producer_duplicate_idempotent': True,
            'webhook_queue_count': 1, 'webhook_payload_minimal': True,
            'outbound_delivery_attempted': False, 'formal_p20_t024_claim': False}
