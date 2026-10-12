"""Real support reply across notification, SMTP/mail and independent audit."""
import json
import os
from pathlib import Path
import time

from common import HEAD, ROOT, emit
from t022_case import http_json, mysql, predecessor, q, session_cookie
from t037_case import digest, fingerprint, require


def one(sql):
    rows = mysql(sql).splitlines()
    require(len(rows) == 1, 'cross-channel SQL authority missing or ambiguous')
    return json.loads(rows[0])


def collect():
    t20 = predecessor('P20-T020')
    t22 = predecessor('P20-T022')
    t37_path = ROOT/'artifacts/v10/P20/consistency/P20-T037.json'
    t37 = json.loads(t37_path.read_bytes())
    require(t37['status'] == 'PASS' and t37['errors'] == [] and t37['implementation_commit'] == HEAD,
            'same-head T037 authority missing')
    d20, d22 = t20['details'], t22['details']
    uid, ws = d20['user_id'], d20['workspace_id']
    require(uid == d22['user_id'] and ws == d22['workspace_id'] and d20['real_smtp_delivery'] and
            d20['native_mailworker'], 'cross-channel predecessor identity mismatch')
    correlation = 'p20-t020-admin-reply-' + HEAD[:12]
    message = one(f"SELECT JSON_OBJECT('id',m.id,'ticket_id',m.ticket_id,'actor_id',m.actor_id,'correlation_id',m.correlation_id,'workspace_id',t.workspace_id,'requester_user_id',t.requester_user_id) FROM support_ticket_messages m JOIN support_tickets t ON t.id=m.ticket_id WHERE m.kind='support_reply' AND m.actor_type='support' AND m.correlation_id={q(correlation)} AND t.workspace_id={q(ws)} AND t.requester_user_id={q(uid)}")
    mid, tid = message['id'], message['ticket_id']
    job = one(f"SELECT JSON_OBJECT('id',j.id,'resource_id',j.resource_id,'status',j.status,'attempt_count',j.attempt_count,'recipient_matches',j.recipient_value=wm.email,'recipient_kind',j.recipient_kind) FROM mail_jobs j JOIN workspace_memberships wm ON wm.workspace_id={q(ws)} AND wm.user_id={q(uid)} WHERE j.template_key='support-ticket-reply' AND j.resource_type='ticket_message' AND j.resource_id={q(mid)}")
    attempt = one(f"SELECT JSON_OBJECT('job_id',mail_job_id,'attempt_number',attempt_number,'status',status,'smtp_code',smtp_code) FROM mail_attempts WHERE mail_job_id={q(job['id'])} ORDER BY attempt_number DESC LIMIT 1")
    notification_sql = f"SELECT JSON_OBJECT('id',id,'workspace_id',workspace_id,'recipient_user_id',recipient_user_id,'category',category,'event_key',event_key,'dedupe_key',dedupe_key,'title',title,'summary',summary,'deep_link',deep_link,'resource_type',resource_type,'resource_id',resource_id,'read',read_at IS NOT NULL) FROM workspace_notifications WHERE dedupe_key={q('support:ticket_reply_received:'+tid+':'+mid)}"
    notification = one(notification_sql)
    audit_sql = f"SELECT JSON_OBJECT('id',id,'workspace_id',workspace_id,'actor_id',actor_id,'action',action,'resource_type',resource_type,'resource_id',resource_id,'correlation_id',request_correlation_id,'result',result,'metadata',metadata_json) FROM support_audit_events WHERE action='admin_ticket_support_reply' AND resource_id={q(mid)} AND request_correlation_id={q(correlation)}"
    audit = one(audit_sql)
    smtp_path = Path(os.environ['GOJET_TEST_P14_SMTP_STATE'])
    smtp = json.loads(smtp_path.read_bytes())
    native = json.loads((ROOT/'artifacts/v10/P20/runtime/t037-business/proof.json').read_bytes())
    require(native['mail_job_fingerprint'] == fingerprint('mail_job',job['id']) and
            native['mail_resource_fingerprint'] == fingerprint('mail_resource',mid), 'native mail belongs to another reply')

    # Real P15 credentials remain in memory; no cookie, token, email or body is archived.
    login_status, login_headers, login = http_json('POST','/api/auth/login',{
        'email':f'p20-t009-{HEAD[:12]}@example.test',
        'password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],
        'correlation_id':'p20-t038-login-'+HEAD[:12]})
    if login_status == 429:
        time.sleep(61)
        login_status, login_headers, login = http_json('POST','/api/auth/login',{
            'email':f'p20-t009-{HEAD[:12]}@example.test',
            'password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],
            'correlation_id':'p20-t038-login-retry-'+HEAD[:12]})
    cookie = session_cookie(login_headers)
    require(login_status == 200 and login.get('status') == 'authenticated' and cookie, 'real recipient login failed')
    headers = {'Cookie':'__Host-gojet_session='+cookie}
    status, _, me = http_json('GET','/api/me',headers=headers)
    require(status == 200 and me.get('user',{}).get('id') == uid, 'recipient session mismatch')
    list_path = f'/api/workspaces/{ws}/notifications?category=support&limit=100'
    status, _, page = http_json('GET',list_path,headers=headers)
    items = [x for x in page.get('items',[]) if x.get('id') == notification['id']]
    require(status == 200 and len(items) == 1, 'recipient notification unavailable')
    item = items[0]
    status, _, thread = http_json('GET','/api/support/tickets/'+tid,headers=headers)
    require(status == 200 and thread.get('ticket',{}).get('id') == tid and
            any(x.get('id') == mid for x in thread.get('messages',[])), 'deep-link resource authorization mismatch')
    require(all(x.get('kind') != 'internal_note' for x in thread.get('messages',[])), 'internal support note exposed')
    denials = {}
    for label, path, auth in [('anonymous_notification',list_path,{}),
            ('anonymous_ticket','/api/support/tickets/'+tid,{}),
            ('recipient_admin_mail','/api/admin/mail/queue',headers),
            ('recipient_admin_audit','/api/admin/audit',headers)]:
        code, _, _ = http_json('GET',path,headers=auth)
        require(code in (401,403), label+' not denied')
        denials[label] = code
    before = {'audit':audit, 'mail':job, 'smtp':smtp}
    changes = []
    for state in ('read','unread'):
        code, _, current = http_json('GET','/api/me',headers=headers)
        require(code == 200 and current.get('csrf_token'), 'recipient CSRF unavailable')
        code, _, changed = http_json('POST',f'/api/workspaces/{ws}/notifications/{notification["id"]}/{state}',headers={
            **headers,'Origin':os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],
            'X-CSRF-Token':current['csrf_token'],'X-Request-ID':'p20-t038-'+state+'-'+HEAD[:12]})
        require(code == 200 and changed.get('id') == notification['id'] and changed.get('read') == (state=='read'), 'recipient read-state mutation failed')
        persisted = one(notification_sql)
        require(bool(persisted['read']) == (state=='read'), 'notification read state not durable')
        changes.append({'state':state,'http_status':code,'durable_read':bool(persisted['read'])})
    after_audit = one(audit_sql)
    require(after_audit == before['audit'], 'notification mutation changed audit authority')
    current_job = one(f"SELECT JSON_OBJECT('status',status,'attempt_count',attempt_count) FROM mail_jobs WHERE id={q(job['id'])}")
    after_smtp = json.loads(smtp_path.read_bytes())
    require(current_job == {k:job[k] for k in ('status','attempt_count')} and after_smtp == smtp,
            'notification mutation changed mail/delivery authority')
    mail_count = int(mysql(f"SELECT COUNT(*) FROM mail_jobs WHERE resource_type='ticket_message' AND resource_id={q(mid)}"))
    require(mail_count == 1,
            'logical support reply duplicated mail')
    final_notification = one(notification_sql)
    proof = {'implementation_commit':HEAD,'message':message,'mail':job,'attempt':attempt,
             'notification':notification,'final_notification':final_notification,'audit':audit,
             'smtp':smtp,'native_mail_fingerprint':native['mail_job_fingerprint'],
             'api_notification':item,'denials':denials,'read_changes':changes,
             'after_audit':after_audit,'after_mail':current_job,'after_smtp':after_smtp,
             'mail_count':mail_count,'real_recipient_session':True,'authorized_thread':True}
    private = [cookie, os.environ['GOJET_P20_T009_LOGIN_FIXTURE'], me['user'].get('email','')]
    raw = (json.dumps(proof,indent=2)+'\n').encode()
    require(not any(v and v.encode() in raw for v in private), 'private material in cross-channel evidence')
    directory = ROOT/'artifacts/v10/P20/runtime/t038'
    directory.mkdir(parents=True,exist_ok=True)
    files = {'bindings.json':raw, 'P20-T020.json':json.dumps(t20).encode(),
             'P20-T022.json':json.dumps(t22).encode(), 'P20-T037.json':t37_path.read_bytes()}
    for name, data in files.items(): (directory/name).write_bytes(data)
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{k:digest(v) for k,v in files.items()}},indent=2)+'\n')
    return directory


def inspect(directory, head):
    manifest = json.loads((directory/'manifest.json').read_bytes())
    require(manifest['implementation_commit'] == head and set(manifest['files']) ==
            {'bindings.json','P20-T020.json','P20-T022.json','P20-T037.json'}, 'cross-channel source inventory/SHA mismatch')
    packets = {}
    for name, sha in manifest['files'].items():
        raw = (directory/name).read_bytes();require(digest(raw) == sha, 'cross-channel source digest mismatch')
        packets[name] = json.loads(raw)
    for n in (20,22,37):
        p = packets[f'P20-T{n:03d}.json']
        require(p['implementation_commit'] == head and p['status'] == 'PASS' and p['errors'] == [], 'cross-channel predecessor invalid')
    p = packets['bindings.json'];m,j,a,n = (p[k] for k in ('message','mail','audit','notification'))
    d20 = packets['P20-T020.json']['details']
    require(p['implementation_commit'] == head and m['workspace_id'] == n['workspace_id'] == a['workspace_id'] == d20['workspace_id'] and
            m['requester_user_id'] == n['recipient_user_id'] == d20['user_id'], 'cross-channel tenant/recipient mismatch')
    require(j['resource_id'] == a['resource_id'] == m['id'] and a['actor_id'] == m['actor_id'] and
            a['correlation_id'] == m['correlation_id'] == 'p20-t020-admin-reply-'+head[:12], 'reply/audit/mail references mismatch')
    require(a['action'] == 'admin_ticket_support_reply' and a['resource_type'] == 'ticket_message' and a['result'] == 'success' and
            a['metadata']['message_kind'] == 'support_reply', 'independent audit authority mismatch')
    require(j['recipient_kind'] == 'requester' and j['recipient_matches'] == 1 and j['status'] == 'sent' and j['attempt_count'] == 1,
            'authorized mail recipient/outcome mismatch')
    require(p['attempt']['job_id'] == j['id'] and p['attempt']['attempt_number'] == 1 and p['attempt']['status'] == 'sent' and
            p['smtp']['deliveries'] == d20['smtp_deliveries'] and p['smtp']['deliveries'] >= 1 and
            len(p['smtp']['last_message_sha256']) == 64 and p['native_mail_fingerprint'] == fingerprint('mail_job',j['id']), 'SMTP/native authority mismatch')
    require(n['category'] == 'support' and n['event_key'] == 'ticket_reply_received' and
            n['dedupe_key'] == 'support:ticket_reply_received:'+m['ticket_id']+':'+m['id'] and
            n['resource_type'] == 'support_ticket' and n['resource_id'] == m['ticket_id'] and
            n['deep_link'] == '/app/support/'+m['ticket_id'] and n['title'] == 'Support reply received' and
            n['summary'] == 'Your support ticket has a new reply.', 'server notification reference/copy mismatch')
    item = p['api_notification']
    require(all(item.get(k) == n[k] for k in ('id','category','event_key','title','summary','deep_link','resource_type','resource_id')) and
            'dedupe_key' not in item, 'recipient notification API differs from durable authority')
    require(p['denials'].keys() == {'anonymous_notification','anonymous_ticket','recipient_admin_mail','recipient_admin_audit'} and
            all(x in (401,403) for x in p['denials'].values()), 'channel authorization matrix incomplete')
    require(p['read_changes'] == [{'state':'read','http_status':200,'durable_read':True},{'state':'unread','http_status':200,'durable_read':False}] and
            p['final_notification'] == {**n,'read':0} and p['after_audit'] == a and
            p['after_mail'] == {k:j[k] for k in ('status','attempt_count')} and p['after_smtp'] == p['smtp'] and p['mail_count'] == 1 and
            p['real_recipient_session'] is True and p['authorized_thread'] is True, 'independent channel state not preserved')
    raw = json.dumps(p).lower()
    require(not any(x in raw for x in ('@example.test','bearer ','__host-gojet_session','password','csrf_token','recipient_value','webhook_secret')), 'private material exposed')
    return {'formal_p20_t038_claim':True,'same_event_channels':3,'authorization_denials':len(p['denials']),
            'collection_sha256':digest((directory/'manifest.json').read_bytes()),'mock_authority':False,
            'secret_material_recorded':False,'audit_mail_authority_independent':True,'next_case_unlocked':False,
            'promotion_requires_same_head_gates':True}


def run_case():
    errors, details = [], {'formal_p20_t038_claim':False,'next_case_unlocked':False}
    try: details = inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError) as error:
        errors.append('T038 cross-channel verification failed: '+ (str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T038','consistency','Notification mail audit cross-channel consistency',errors,details)


if __name__ == '__main__':
    from common import fail_if_errors
    fail_if_errors([run_case()])
