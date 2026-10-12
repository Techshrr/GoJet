"""Original T020 SMTP/mail and T023 Admin audit to native log correlation.

This supplemental proof does not alone close the full eight-service T037 oracle.
"""
import hashlib
import json
import os
from pathlib import Path
import time

from common import HEAD, ROOT
from t022_case import mysql, q, predecessor


def fingerprint(kind, value):
    return hashlib.sha256((kind + ':' + str(value)).encode()).hexdigest()


def require(ok, message):
    if not ok:
        raise ValueError(message)


def records(path):
    raw = path.read_bytes()
    # Active process writers may be between write boundaries at snapshot time.
    if raw and not raw.endswith(b'\n'):
        raw = raw[:raw.rfind(b'\n') + 1]
    return raw, [json.loads(line) for line in raw.splitlines() if line]


def inspect_live():
    t20, t23 = predecessor('P20-T020')['details'], predecessor('P20-T023')['details']
    require(t20['workspace_id'] == t23['workspace_id'] and t20['user_id'] == t23['user_id'], 'business identity mismatch')
    require(t20['t020_full_lifecycle_proven'] and t23['audit_append_only'], 'native predecessor authority missing')
    ws, uid = q(t20['workspace_id']), q(t20['user_id'])
    jobs = mysql(f"SELECT j.id,j.resource_id,j.status,j.attempt_count FROM mail_jobs j JOIN support_ticket_messages m ON m.id=j.resource_id JOIN support_tickets t ON t.id=m.ticket_id WHERE j.template_key='support-ticket-reply' AND j.resource_type='ticket_message' AND m.kind='support_reply' AND t.workspace_id={ws} AND t.requester_user_id={uid}").splitlines()
    require(len(jobs) == 1, 'correlated durable mail job is missing or ambiguous')
    job, resource, status, attempts = jobs[0].split('\t')
    require(status == 'sent' and int(attempts) >= 1, 'mail outcome not durably sent')
    correlation = 'p20-t023-quarantine-' + HEAD[:12]
    audit = mysql(f"SELECT id,actor_id,resource_id,result FROM admin_audit_events WHERE action='admin.file.quarantine' AND request_correlation_id={q(correlation)}").splitlines()
    require(len(audit) == 1, 'durable correlated Admin audit missing or ambiguous')
    audit_id, actor, file_id, result = audit[0].split('\t')
    require(actor == t23['administrator_id'] and int(file_id) == int(t23['file_id']) and result == 'success', 'audit actor/resource/outcome mismatch')
    receiver = Path(os.environ['GOJET_P20_LOG_RECEIVER_OUT'])
    local_mail = Path('/tmp/gojet-p20/t020-mailworker.log')
    local_api = ROOT / 'artifacts/v10/P20/runtime/t009/platformapi.log'
    deadline = time.monotonic() + 5
    while True:
        snapshot, received = records(receiver)
        mail_bytes, mail = records(local_mail)
        api_bytes, api = records(local_api)
        mail_match = lambda row: row.get('service_id') == 'mailworker' and row.get('msg') == 'mail job completion persisted' and row.get('mail_job_fingerprint') == fingerprint('mail_job', job) and row.get('resource_fingerprint') == fingerprint('mail_resource', resource) and row.get('status') == status and row.get('attempt_count') == int(attempts)
        api_match = lambda row: row.get('service_id') == 'platformapi' and row.get('msg') == 'http request finished' and row.get('correlation_fingerprint') == fingerprint('correlation', correlation)
        originals = [row for row in mail if mail_match(row)] + [row for row in api if api_match(row)]
        if any(mail_match(row) for row in mail) and any(api_match(row) for row in api):
            if all(any({k:v for k,v in row.items() if k not in ('time','source_time')} == {k:v for k,v in original.items() if k != 'time'} and row.get('source_time') == original['time'] for row in received) for original in originals):
                break
        require(time.monotonic() < deadline, 'native log/receiver correlation incomplete')
        time.sleep(.1)
    for raw in (snapshot, mail_bytes, api_bytes):
        require(b'@example.test' not in raw and b'gojet_admin_session=' not in raw and b'Bearer ' not in raw, 'private material in native logs')
        for key, value in os.environ.items():
            if key.startswith('GOJET_') and any(k in key for k in ('SECRET', 'TOKEN', 'PASSWORD', 'DSN')) and len(value) >= 8:
                require(value.encode() not in raw, 'configured private material in native logs')
    directory = ROOT / 'artifacts/v10/P20/runtime/t037-business'
    directory.mkdir(parents=True, exist_ok=True)
    members = {'receiver.jsonl': snapshot, 'mailworker.jsonl': mail_bytes, 'platformapi.jsonl': api_bytes}
    for name, raw in members.items():
        (directory / name).write_bytes(raw)
    evidence = {'implementation_commit': HEAD, 'status': 'PASS', 'formal_p20_t037_claim': False,
        'scope': 'real T020 committed mail/SMTP and T023 authenticated Admin audit to native producer/receiver logs',
        'mail_job_fingerprint': fingerprint('mail_job', job), 'mail_resource_fingerprint': fingerprint('mail_resource', resource),
        'mail_status': status, 'mail_attempt_count': int(attempts),
        'audit_fingerprint': fingerprint('audit', audit_id), 'correlation_fingerprint': fingerprint('correlation', correlation),
        'resource_fingerprint': fingerprint('file', file_id), 'actor_fingerprint': fingerprint('administrator', actor),
        'correlated_original_records': len(originals),
        'files': {name: 'sha256:' + hashlib.sha256(raw).hexdigest() for name, raw in members.items()},
        'remaining': 'full eight-service business/operations coverage and formal same-head guards'}
    (directory / 'proof.json').write_text(json.dumps(evidence, indent=2) + '\n')
    print('PASS: real mail/SMTP and Admin audit correlate with original native and receiver logs')


if __name__ == '__main__':
    inspect_live()
