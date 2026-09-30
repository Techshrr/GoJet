"""Bind the executed P0 sequence and its durable resource relationships."""
import hashlib
import json
from datetime import datetime

from common import HEAD, ROOT, emit, fail_if_errors, sha256_file
from t022_case import mysql, q

STAGES = ['register', 'verify', 'login', 'link', 'redirect', 'analytics', 'QR',
          'file', 'text', 'bio', 'domain', 'ticket', 'billing', 'notification', 'admin']
RESOURCE_KEYS = ['link_id', 'source_link_id', 'click_event_id', 'qr_id', 'file_id',
                 'text_id', 'bio_id', 'domain_id', 'administrator_id']


def require(ok, message):
    if not ok:
        raise ValueError(message)


def validate_chain(records, head):
    require(len(records) == len(STAGES), 'P0 sequence incomplete')
    user = records[0]['details'].get('user_id')
    workspace = records[0]['details'].get('workspace_id')
    require(bool(user) and bool(workspace), 'registration identity missing')
    previous = None
    for number, record in enumerate(records, 9):
        require(record.get('case') == f'P20-T{number:03d}', 'P0 sequence out of order')
        require(record.get('implementation_commit') == head and record.get('status') == 'PASS'
                and record.get('errors') == [], 'P0 evidence is not exact-head PASS')
        d = record['details']
        require(d.get('mock_authority') is False and d.get('test_header_authority') is not True,
                'P0 mock or test-header authority detected')
        require(d.get('secret_material_recorded') is False, 'P0 redaction unproven')
        require(d.get('workspace_id') == workspace, 'Workspace correlation lost')
        if number != 14:  # Analytics binds the same link and Workspace, not a login session.
            require(d.get('user_id') == user, 'user correlation lost')
        stamp = datetime.fromisoformat(record['generated_at'].replace('Z', '+00:00'))
        require(previous is None or stamp >= previous, 'P0 timestamps out of order')
        previous = stamp
    link = records[3]['details'].get('link_id')
    require(bool(link) and records[4]['details'].get('link_id') == link
            and records[5]['details'].get('link_id') == link
            and records[6]['details'].get('source_link_id') == link, 'link/redirect/analytics/QR correlation lost')
    require(records[7]['details'].get('file_id') == records[14]['details'].get('file_id')
            and bool(records[7]['details'].get('file_id')), 'Admin file correlation lost')
    return user, workspace


def run_case():
    details = {'formal_p20_t026_claim': False, 'next_case_unlocked': False,
               'promotion_requires_same_head_gates': True, 'mock_authority': False,
               'test_header_authority': False, 'secret_material_recorded': False}
    errors = []
    try:
        prior_path = ROOT / 'artifacts/v10/P20/integration/P20-T025.json'
        prior = json.loads(prior_path.read_text())
        require(prior.get('implementation_commit') == HEAD and prior.get('status') == 'PASS'
                and prior.get('errors') == [] and prior['details'].get('formal_p20_t025_claim') is True,
                'formal T025 prerequisite missing')
        paths = [ROOT / f'artifacts/v10/P20/p0/P20-T{n:03d}.json' for n in range(9, 24)]
        records = [json.loads(path.read_text()) for path in paths]
        user, workspace = validate_chain(records, HEAD)
        ws, uid = q(workspace), q(user)
        # Select only the correlated business flow, never fixture rows from other tenants.
        tickets = mysql(f"SELECT DISTINCT t.id FROM support_tickets t JOIN support_ticket_messages m ON m.ticket_id=t.id WHERE t.workspace_id={ws} AND t.requester_user_id={uid} AND m.kind='support_reply'").splitlines()
        paid = mysql(f"SELECT o.id,n.id FROM billing_orders o JOIN workspace_notifications n ON n.resource_id=o.id AND n.workspace_id=o.workspace_id WHERE o.workspace_id={ws} AND o.status='paid' AND n.category='billing' AND n.event_key='payment_succeeded' AND n.recipient_user_id={uid}").splitlines()
        require(len(tickets) == 1 and len(paid) == 1, 'durable ticket/payment/notification chain ambiguous or missing')
        order, notification = paid[0].split('\t')
        # Stable fingerprints permit cross-case correlation without exposing opaque billing identifiers.
        fingerprint = lambda kind, value: hashlib.sha256((kind + ':' + value).encode()).hexdigest()
        resources = {'ticket': fingerprint('ticket', tickets[0]), 'order': fingerprint('order', order),
                     'notification': fingerprint('notification', notification)}
        timeline = []
        for stage, record, path in zip(STAGES, records, paths):
            d = record['details']
            row = {'stage': stage, 'case': record['case'], 'generated_at': record['generated_at'],
                   'user_id': user, 'workspace_id': workspace,
                   'source': str(path.relative_to(ROOT)), 'sha256': sha256_file(path),
                   'resources': {key: d[key] for key in RESOURCE_KEYS if d.get(key) is not None}}
            if stage == 'ticket': row['resources']['ticket_sha256'] = resources['ticket']
            if stage in ('billing', 'notification'): row['resources']['order_sha256'] = resources['order']
            if stage == 'notification': row['resources']['notification_sha256'] = resources['notification']
            timeline.append(row)
        details.update(formal_p20_t026_claim=True, user_id=user, workspace_id=workspace,
                       stage_count=len(timeline), timeline=timeline,
                       t025_sha256=sha256_file(prior_path), durable_relationships_verified=True)
    except (OSError, ValueError, KeyError, TypeError):
        errors.append('T026 sequence or durable correlation validation failed')
    return emit('P20-T026', 'p0', 'Correlated whole-product P0 timeline', errors, details)


if __name__ == '__main__':
    fail_if_errors([run_case()])
