"""Real Links -> operationsmonitor -> authorized HTTPS receiver evidence."""
import hashlib
import http.client
import json
import os
import secrets
import subprocess
import time
from datetime import datetime

from t022_case import HEAD, http_json, mysql, scalar, q


class OutboundVerificationError(RuntimeError):
    pass


def verify_outbound(workspace, auth_headers):
    stage = 'configuration'
    worker = None
    hook_id = None
    receiver_created = False
    run_id = 'p20-' + HEAD[:12] + '-' + secrets.token_hex(8)
    control = os.environ.get('P20_WEBHOOK_RECEIVER_TOKEN', '')
    path = f'/api/workspaces/{workspace}/webhooks'

    def check(ok):
        if not ok:
            raise ValueError('outbound assertion failed')

    def receiver(method, expected, data=None):
        conn = http.client.HTTPSConnection('test.gojet.cc', timeout=20)
        try:
            conn.request(method, '/runs/' + run_id,
                         body=json.dumps(data).encode() if data is not None else None,
                         headers={'Authorization': 'Bearer ' + control, 'Content-Type': 'application/json'})
            response = conn.getresponse()
            check(response.status == expected)
            return json.loads(response.read())
        finally:
            conn.close()

    def headers():
        status, _, me = http_json('GET', '/api/me', headers=auth_headers)
        check(status == 200 and bool(me.get('csrf_token')))
        return {**auth_headers, 'Origin': os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],
                'X-CSRF-Token': me['csrf_token']}

    def stop():
        nonlocal worker
        if worker is not None:
            worker.terminate()
            try:
                worker.wait(timeout=10)
            except subprocess.TimeoutExpired:
                worker.kill()
                worker.wait(timeout=5)
            worker = None

    def start():
        nonlocal worker
        env = os.environ.copy()
        env.update(GOJET_WEBHOOK_DELIVERY_ENABLED='1', GOJET_RISK_PROVIDER_NAME='p20-unused',
                   GOJET_RISK_PROVIDER_ENDPOINT='http://127.0.0.1:9',
                   GOJET_RISK_POLICY_VERSION='p20-webhook-verification',
                   GOJET_OPSMONITOR_INTERVAL='2s')
        # This process uses the real producer, safe HTTP client, lease and clock.
        # Unrelated risk inspection has no authorized external provider here.
        worker = subprocess.Popen(['/tmp/gojet-p20-operationsmonitor'], env=env,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

    def deliveries():
        status, _, result = http_json('GET', path + '/' + hook_id + '/deliveries', headers=auth_headers)
        check(status == 200)
        return result['deliveries']

    def wait_for(predicate, timeout=130):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            check(worker is not None and worker.poll() is None)
            rows = deliveries()
            found = predicate(rows)
            if found:
                return found
            time.sleep(2)
        raise TimeoutError('outbound observation deadline')

    try:
        check(len(control) >= 32)
        # Separate prior queue-only events even on timestamp precision boundaries.
        time.sleep(1.1)
        stage = 'create_webhook'
        status, _, created = http_json('POST', path, {
            'name': 'P20 real outbound verification',
            'endpoint_url': 'https://test.gojet.cc/deliver/' + run_id,
            'events': ['link.created'],
        }, headers())
        check(status == 201)
        hook_id = created['webhook']['id']
        secret_values = [created['secret']]
        receiver('POST', 201, {'secret': secret_values[0], 'workspace_id': workspace, 'fail_first': 1})
        receiver_created = True
        # No active endpoint other than this explicitly authorized receiver.
        check(int(scalar("SELECT COUNT(*) FROM workspace_webhooks WHERE status='active' "
                         f'AND id<>{q(hook_id)}')) == 0)
        proofs = []
        for index in range(2):
            stage = 'rotate_secret' if index else 'first_product_event'
            if index:
                status, _, rotated = http_json('POST', path + '/' + hook_id + '/rotate-secret', {}, headers())
                check(status == 200 and rotated['secret'] != secret_values[0])
                secret_values.append(rotated['secret'])
                receiver('PATCH', 200, {'secret': rotated['secret']})
            status, _, link = http_json('POST', f'/api/workspaces/{workspace}/links', {
                'hostname': 'gojet.cc', 'domain_kind': 'official',
                'code': 'p20out' + secrets.token_hex(8), 'title': 'P20 outbound verification',
                'primary_destination': 'https://example.com/p20-product-event',
                'redirect_status': 302, 'routing': [], 'ab': [], 'utm': {},
                'access': {}, 'one_time': False, 'change_reason': 'P20 outbound verification',
            }, headers())
            check(status == 201)
            link_id = int(link['id'])
            audit_id = int(scalar(f"SELECT id FROM link_audit_events WHERE workspace_id={q(workspace)} "
                                  f"AND link_id={link_id} AND action='link.create' AND result='success'"))
            event_id = 'link-audit-' + str(audit_id)
            stage = 'first_attempt_' + str(index)
            start()
            first = wait_for(lambda rows: next((r for r in rows if r['event_id'] == event_id
                                                and r['attempts'] >= 1), None))
            stop()
            check(first['status'] == 'retrying' and first['attempts'] == 1
                  and first['last_status_code'] == 503)
            delay = (datetime.fromisoformat(first['next_attempt_at'].replace('Z', '+00:00'))
                     - datetime.fromisoformat(first['last_attempt_at'].replace('Z', '+00:00'))).total_seconds()
            check(delay >= 60)
            stage = 'real_backoff_after_restart_' + str(index)
            start()
            done = wait_for(lambda rows: next((r for r in rows if r['event_id'] == event_id
                                               and r['status'] == 'delivered'), None))
            time.sleep(5)  # Additional real producer iterations cannot duplicate it.
            stop()
            check(done['id'] == first['id'] and done['attempts'] == 2 and done['last_status_code'] == 200)
            raw = scalar('SELECT CONVERT(body USING utf8mb4) FROM workspace_webhook_deliveries '
                         f'WHERE id={q(done["id"])}')
            envelope = json.loads(raw)
            check(envelope['data'] == {'link_id': link_id, 'version': int(link['version'])})
            report = receiver('GET', 200)
            observed = [r for r in report['deliveries'] if r['delivery_id'] == done['id']]
            check(len(observed) == 1 and observed[0]['attempts'] == 2 and observed[0]['accepted'])
            check(observed[0]['body_sha256'] == done['body_sha256'] == hashlib.sha256(raw.encode()).hexdigest())
            check(report['invalid_count'] == 0 and report['accepted_count'] == index + 1)
            check(len(deliveries()) == index + 1)
            proofs.append({'delivery_id': done['id'], 'event_id': event_id, 'link_id': link_id,
                           'attempts': 2, 'retry_delay_seconds': delay,
                           'body_sha256': done['body_sha256'], 'receiver_accepted_once': True})
        stage = 'audit_and_secret_boundary'
        audit = mysql('SELECT action,actor_id,result,metadata_json FROM workspace_audit_events '
                      f'WHERE workspace_id={q(workspace)} AND resource_id IN '
                      '(' + ','.join(q(p['delivery_id']) for p in proofs) + ') ORDER BY id')
        check(audit.count('webhook.delivery.retrying') == 2 and audit.count('webhook.delivery.delivered') == 2)
        check(all('operationsmonitor' in row for row in audit.splitlines()))
        status, _, listed = http_json('GET', path, headers=auth_headers)
        check(status == 200 and all(s not in json.dumps(listed) + audit + json.dumps(proofs) for s in secret_values))
        return {'webhook_outbound_passed': True, 'outbound_delivery_attempted': True,
                'real_operationsmonitor': True, 'real_retry_clock': True, 'worker_restart_verified': True,
                'receiver_signature_verified': True, 'receiver_rotation_verified': True,
                'outbound_audit_correlated': True, 'outbound_deliveries': proofs,
                'formal_p20_t024_claim': False}
    except Exception as exc:
        # Stage/type only; never response bodies, request headers or secrets.
        raise OutboundVerificationError('outbound stage ' + stage + ': ' + type(exc).__name__) from None
    finally:
        stop()
        try:
            if hook_id:
                status, _, body = http_json('POST', path + '/' + hook_id + '/disable', {}, headers())
                check(status == 200 and body['webhook']['status'] == 'disabled')
        finally:
            if receiver_created:
                receiver('DELETE', 200)
