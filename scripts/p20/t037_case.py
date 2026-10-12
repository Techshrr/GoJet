"""Eight native service records bound to actual SQL/event/audit authorities."""
import hashlib
import json
import os
from pathlib import Path

SERVICES = {'redirectengine', 'analyticsworker', 'analyticsreconciler', 'platformapi',
            'mailworker', 'fileworker', 'operationsmonitor', 'logreceiver'}


def require(ok, reason):
    if not ok:
        raise ValueError(reason)


def digest(raw):
    return 'sha256:' + hashlib.sha256(raw).hexdigest()


def fingerprint(kind, value):
    return hashlib.sha256((kind + ':' + str(value)).encode()).hexdigest()


def inspect(directory, head):
    manifest = json.loads((directory / 'manifest.json').read_bytes())
    require(manifest['implementation_commit'] == head, 'mixed source SHA')
    expected = {'receiver.jsonl', 'bindings.json', 'business-proof.json'} | {
        x + '.jsonl' for x in SERVICES - {'logreceiver', 'analyticsreconciler'}} | {
        'analyticsreconciler-1.jsonl', 'analyticsreconciler-2.jsonl'} | {
        f'P20-T{n:03d}.json' for n in (13, 14, 16, 20, 23, 24)}
    require(set(manifest['files']) == expected, 'source inventory incomplete')
    files = {}
    for name, sha in manifest['files'].items():
        raw = (directory / name).read_bytes()
        require(digest(raw) == sha, 'source digest mismatch')
        files[name] = raw
    parsed = {name: [json.loads(line) for line in raw.splitlines() if line]
              for name, raw in files.items() if name.endswith('.jsonl')}
    receiver = parsed['receiver.jsonl']
    require({r['service_id'] for r in receiver} == SERVICES, 'eight receiver identities missing')
    require(any(r['service_id'] == 'logreceiver' and r['msg'] == 'logreceiver started' for r in receiver), 'receiver lifecycle absent')
    for raw in (files[n] for n in parsed):
        require(all(s not in raw for s in (b'@example.test', b'gojet_admin_session=', b'Bearer ', b'P20-T009!Strong-Passphrase')), 'secret/PII leaked')
    for rows in parsed.values():
        for row in rows:
            require(row.get('service_id') in SERVICES and row.get('level') in ('INFO','WARN','ERROR') and row.get('time'), 'malformed native row')
            for key, value in row.items():
                if key.lower() in ('error','err','path','payload','body') or any(k in key.lower() for k in ('password','secret','token','credential','authorization','cookie','dsn','quarantine','file_content','verification_material','txt_record','payment_signature')):
                    require(value == '[redacted]', 'unsanitized log attribute')
    cases = {}
    for n in (13,14,16,20,23,24):
        p = json.loads(files[f'P20-T{n:03d}.json'])
        require(p['status'] == 'PASS' and p['implementation_commit'] == head and p['errors'] == [], 'failed/mixed original authority')
        cases[n] = p['details']
    bindings = json.loads(files['bindings.json'])
    business = json.loads(files['business-proof.json'])
    require(business['implementation_commit'] == head and business['status'] == 'PASS', 'mail/Admin business proof missing')
    require(business['actor_fingerprint'] == fingerprint('administrator', cases[23]['administrator_id']) and business['resource_fingerprint'] == fingerprint('file', cases[23]['file_id']), 'business proof actor/resource mismatch')
    selected = []
    def match(source, service, message, expected):
        rows = [r for r in parsed[source] if r['service_id'] == service and r['msg'] == message
                and all(r.get(k) == v for k,v in expected.items())]
        require(rows, service + ' durable identifiers missing')
        for original in rows:
            require(any(r.get('source_time') == original['time'] and
                        {k:v for k,v in r.items() if k not in ('time','source_time')} ==
                        {k:v for k,v in original.items() if k != 'time'} for r in receiver), 'receiver changed/dropped original record')
        selected.extend(rows)
    click = bindings['click']
    require(click['event_id'] == cases[13]['click_event_id'] and click['workspace_id'] == cases[13]['workspace_id'] and click['link_id'] == int(cases[13]['link_id']) and click['click_sequence'] == 1 and click['stream_id'], 'durable click authority mismatch')
    match('redirectengine.jsonl','redirectengine','redirect click committed',{k:click[k] for k in ('event_id','workspace_id','link_id','click_sequence')})
    match('analyticsworker.jsonl','analyticsworker','analytics event processed',{k:click[k] for k in ('event_id','link_id','click_sequence','stream_id')})
    for index, row in enumerate(bindings['reconciliations'], 1):
        require(row['source_event_total'] == row['aggregate_before'] == row['aggregate_after'] == 1 and row['repaired'] is False, 'reconciliation SQL state mismatch')
        match(f'analyticsreconciler-{index}.jsonl','analyticsreconciler','analytics reconciliation cycle',row)
    require(len(bindings['reconciliations']) == 2, 'reconciliation repeat missing')
    file = bindings['file']
    require(file['file_id'] == int(cases[16]['file_id']) == int(cases[23]['file_id']) and file['status'] == 'clean', 'file SQL authority mismatch')
    match('fileworker.jsonl','fileworker','file scan completed',{'file_id':file['file_id'],'attempt_id':file['attempt_id'],'generation':file['generation'],'verdict':'clean'})
    match('mailworker.jsonl','mailworker','mail job completion persisted',{'mail_job_fingerprint':business['mail_job_fingerprint'],'resource_fingerprint':business['mail_resource_fingerprint'],'status':business['mail_status'],'attempt_count':business['mail_attempt_count']})
    match('platformapi.jsonl','platformapi','http request finished',{'correlation_fingerprint':business['correlation_fingerprint']})
    require(cases[24]['outbound_audit_correlated'] and cases[24]['webhook_outbound_passed'], 'real outbound/audit authority missing')
    require(len(bindings['webhooks']) == len(cases[24]['outbound_deliveries']) == 2, 'outbound authority count mismatch')
    for row, original in zip(bindings['webhooks'], cases[24]['outbound_deliveries']):
        require(row['delivery_id'] == original['delivery_id'] and row['event_id'] == original['event_id'] and row['status'] == 'delivered' and row['attempts'] == original['attempts'] == 2 and row['retry_audits'] == row['delivered_audits'] == 1, 'webhook SQL/audit mismatch')
        for status, attempt in [('retrying',1),('delivered',2)]:
            match('operationsmonitor.jsonl','operationsmonitor','webhook delivery persisted',{'delivery_fingerprint':fingerprint('webhook_delivery',row['delivery_id']),'event_fingerprint':fingerprint('webhook_event',row['event_id']),'correlation_fingerprint':fingerprint('correlation','operationsmonitor-'+row['delivery_id']),'status':status,'attempt_count':attempt})
    return {'formal_p20_t037_claim': True, 'promotion_requires_same_head_gates': True,
            'service_count': len(SERVICES), 'correlated_native_records': len(selected),
            'forwarding_warning_count': sum('forwarding unavailable' in r['msg'] for rows in parsed.values() for r in rows),
            'collection_sha256': digest((directory / 'manifest.json').read_bytes()),
            'mock_authority': False, 'secret_material_recorded': False,
            'audit_authority_independent': True, 'next_case_unlocked': False}


def collect():
    from common import ROOT, HEAD
    from t022_case import mysql, q, predecessor
    source = ROOT / 'artifacts/v10/P20/runtime'
    destination = source / 't037-formal'
    destination.mkdir(parents=True, exist_ok=True)
    mapping = {'receiver.jsonl': Path(os.environ['GOJET_P20_LOG_RECEIVER_OUT']),
        'redirectengine.jsonl': source/'t013/redirectengine.log',
        'analyticsworker.jsonl': source/'t014/analyticsworker.jsonl',
        'analyticsreconciler-1.jsonl': source/'t014/analyticsreconciler-1.jsonl',
        'analyticsreconciler-2.jsonl': source/'t014/analyticsreconciler-2.jsonl',
        'fileworker.jsonl': source/'t016/fileworker.log',
        'mailworker.jsonl': Path('/tmp/gojet-p20/t020-mailworker.log'),
        'platformapi.jsonl': source/'t009/platformapi.log',
        'operationsmonitor.jsonl': source/'t037-business/operationsmonitor.jsonl',
        'business-proof.json': source/'t037-business/proof.json'}
    for n in (13,14,16,20,23,24): mapping[f'P20-T{n:03d}.json'] = ROOT/f'artifacts/v10/P20/{"integration" if n==24 else "p0"}/P20-T{n:03d}.json'
    files = {}
    for name,path in mapping.items():
        raw = path.read_bytes()
        if name.endswith('.jsonl') and raw and not raw.endswith(b'\n'): raw = raw[:raw.rfind(b'\n')+1]
        (destination/name).write_bytes(raw); files[name] = digest(raw)
    cases = {n:json.loads((destination/f'P20-T{n:03d}.json').read_bytes())['details'] for n in (13,16,24)}
    t13 = cases[13]
    click = mysql(f"SELECT event_id,workspace_id,link_id,click_sequence,published_stream_id FROM analytics_outbox WHERE event_id={q(t13['click_event_id'])}").split('\t')
    require(len(click)==5, 'click SQL row missing')
    bindings = {'click':dict(zip(('event_id','workspace_id','link_id','click_sequence','stream_id'),(click[0],click[1],int(click[2]),int(click[3]),click[4]))),'reconciliations':[],'webhooks':[]}
    for index in (1,2):
        rows=[json.loads(l) for l in (destination/f'analyticsreconciler-{index}.jsonl').read_bytes().splitlines()]
        runs=[r['run_id'] for r in rows if r['msg']=='analytics reconciliation cycle'];require(len(runs)==1,'ambiguous reconciliation')
        r=mysql(f"SELECT run_id,source_event_total,aggregate_total_before,aggregate_total_after,repaired FROM analytics_reconciliation_runs WHERE run_id={q(runs[0])}").split('\t')
        bindings['reconciliations'].append(dict(zip(('run_id','source_event_total','aggregate_before','aggregate_after','repaired'),(r[0],int(r[1]),int(r[2]),int(r[3]),bool(int(r[4]))))))
    r=mysql(f"SELECT file_id,id,generation,status FROM file_scan_attempts WHERE file_id={int(cases[16]['file_id'])} AND status='clean' ORDER BY id LIMIT 1").split('\t')
    bindings['file']=dict(zip(('file_id','attempt_id','generation','status'),(int(r[0]),int(r[1]),int(r[2]),r[3])))
    for item in cases[24]['outbound_deliveries']:
        r=mysql(f"SELECT id,event_id,status,attempts FROM workspace_webhook_deliveries WHERE id={q(item['delivery_id'])}").split('\t')
        row=dict(zip(('delivery_id','event_id','status','attempts'),(r[0],r[1],r[2],int(r[3]))))
        for status in ('retrying','delivered'):
            count=mysql(f"SELECT COUNT(*) FROM workspace_audit_events WHERE resource_id={q(r[0])} AND actor_id='operationsmonitor' AND action='webhook.delivery.{status}'")
            row['retry_audits' if status=='retrying' else 'delivered_audits']=int(count)
        bindings['webhooks'].append(row)
    raw=(json.dumps(bindings,indent=2)+'\n').encode();(destination/'bindings.json').write_bytes(raw);files['bindings.json']=digest(raw)
    (destination/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':files},indent=2)+'\n')
    return destination


def run_case():
    from common import HEAD, emit
    errors, details = [], {'formal_p20_t037_claim':False,'next_case_unlocked':False}
    try: details=inspect(collect(),HEAD)
    except ValueError as error: errors.append('T037: ' + str(error))
    except (KeyError,TypeError,OSError,IndexError) as error: errors.append('T037 native evidence invalid: ' + type(error).__name__)
    return emit('P20-T037','consistency','Eight-service observability and secret-redaction consistency',errors,details)


if __name__=='__main__':
    from common import fail_if_errors
    fail_if_errors([run_case()])
