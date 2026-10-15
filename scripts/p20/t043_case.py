"""Native durable mail backoff/restart against an actual local SMTP protocol sink."""
from datetime import datetime
import importlib.util
import hashlib
import json
import os
import subprocess
import threading
import time

from common import HEAD, ROOT, emit
from t022_case import http_json, mysql, predecessor, q, session_cookie
from t037_case import digest, require, fingerprint


def collect():
    previous=ROOT/'artifacts/v10/P20/recovery/P20-T042.json';p42=json.loads(previous.read_bytes())
    require(p42['implementation_commit']==HEAD and p42['status']=='PASS' and p42['errors']==[],'T043 requires same-head T042')
    p20=predecessor('P20-T020');ws,uid=p20['details']['workspace_id'],p20['details']['user_id'];suffix=HEAD[:12]
    directory=ROOT/'artifacts/v10/P20/runtime/t043';directory.mkdir(parents=True,exist_ok=True)
    status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t043-login-'+suffix})
    if status==429:
        time.sleep(61)
        status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t043-login-retry-'+suffix})
    cookie=session_cookie(headers);require(status==200 and body.get('status')=='authenticated' and cookie,'T043 owner login failed')
    auth={'Cookie':'__Host-gojet_session='+cookie}
    def create(label):
        status,_,me=http_json('GET','/api/me',headers=auth)
        require(status==200 and me.get('user',{}).get('id')==uid and me.get('csrf_token'),'T043 session/CSRF binding failed')
        return http_json('POST','/api/support/tickets',{'workspace_id':ws,'category':'general','subject':'P20 T043 mail recovery '+label,'message':'P20 authorized mail retry verification.','turnstile_token':'XXXX.DUMMY.TOKEN.XXXX'}, {**auth,'Origin':os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],'X-CSRF-Token':me['csrf_token'],'X-Request-ID':'p20-t043-'+label+'-'+suffix,'Idempotency-Key':'p20-t043-'+label+'-'+suffix})
    require(mysql("SELECT COUNT(*) FROM mail_jobs WHERE status IN ('queued','retrying','sending')")=='0','unrelated mail queue not drained')
    # Cloudflare's official test token is fixed. Respect the inherited replay
    # expiry; never delete/shorten the security key or invent a fresh token.
    redis_host,redis_port=os.environ['GOJET_REDIS_ADDR'].rsplit(':',1)
    replay_key='support:turnstile:replay:'+hashlib.sha256(b'XXXX.DUMMY.TOKEN.XXXX').hexdigest()
    replay_wait_started=time.monotonic()
    while True:
        ttl=int(subprocess.run(['redis-cli','--raw','-h',redis_host,'-p',redis_port,'TTL',replay_key],capture_output=True,text=True,check=True,timeout=10).stdout.strip())
        require(ttl!=-1 and time.monotonic()-replay_wait_started<620,'inherited test-token replay expiry unavailable')
        if ttl==-2:break
        time.sleep(min(30,max(1,ttl)))
    replay_wait_seconds=round(time.monotonic()-replay_wait_started,3)
    code,_,created=create('recovery');require(code==201 and created.get('created') is True,'real ticket creation failed')
    tid=created['ticket']['id'];require(created['ticket']['workspace_id']==ws,'ticket tenant mismatch')
    def job(ticket=tid):
        return json.loads(mysql(f"SELECT JSON_OBJECT('id',j.id,'resource_id',j.resource_id,'status',j.status,'attempt_count',j.attempt_count,'next_attempt_at',j.next_attempt_at,'updated_at',j.updated_at,'last_error_code',COALESCE(j.last_error_code,''),'workspace_id',t.workspace_id,'requester_user_id',t.requester_user_id,'recipient_matches',j.recipient_value=wm.email) FROM mail_jobs j JOIN support_tickets t ON t.id=j.resource_id JOIN workspace_memberships wm ON wm.workspace_id=t.workspace_id AND wm.user_id=t.requester_user_id WHERE j.resource_type='ticket' AND j.resource_id={q(ticket)} AND j.template_key='support-ticket-created'"))
    initial=job();require(initial['status']=='queued' and initial['attempt_count']==0 and initial['recipient_matches']==1,'logical mail queue binding failed')
    duplicate_code,_,duplicate=create('recovery');require(duplicate_code==400 and job()==initial,'duplicate ticket duplicated logical mail')
    spec=importlib.util.spec_from_file_location('p20_smtp_recovery_sink',ROOT/'scripts/p14/smtp_sink.py');sink=importlib.util.module_from_spec(spec);spec.loader.exec_module(sink)
    mode=directory/'smtp-mode.txt';mode.write_text('transient\n')
    state_path=directory/'smtp-state.json';state=sink.State(state_path,mode);server=sink.Server(('127.0.0.1',0),state)
    thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
    processes=[];handles=[];names=[]
    def start(name):
        path=name+'.jsonl';handle=(directory/path).open('wb');handles.append(handle);names.append(path)
        env={**os.environ,'GOJET_SMTP_ADDR':'%s:%s'%server.server_address,'GOJET_MAILWORKER_INTERVAL':'250ms'}
        p=subprocess.Popen([os.environ['GOJET_TEST_P14_MAILWORKER']],cwd=ROOT,env=env,stdout=handle,stderr=handle);processes.append(p);return p
    def stop(p):
        if p.poll() is None:p.terminate()
        try:require(p.wait(timeout=10)==0,'mailworker graceful stop failed')
        except subprocess.TimeoutExpired:p.kill();p.wait(timeout=5);raise ValueError('mailworker stop timeout')
    def wait(predicate,message,timeout=75):
        deadline=time.monotonic()+timeout
        while not predicate():
            require(time.monotonic()<deadline,message);time.sleep(.2)
    def smtp():return json.loads(state_path.read_bytes())
    try:
        first=start('worker-first');wait(lambda:job()['status']=='retrying','first SMTP failure not retained',20);stop(first)
        retry1=job();smtp1=smtp();require(retry1['attempt_count']==1 and retry1['last_error_code']=='smtp_transient' and smtp1['deliveries']==0 and smtp1['transient_rejections']==1,'first retry state incorrect')
        require((datetime.fromisoformat(retry1['next_attempt_at'])-datetime.fromisoformat(retry1['updated_at'])).total_seconds()==30,'first backoff differs from native 30 seconds')
        second=start('worker-restarted')
        time.sleep(2);before_due=job();require(before_due==retry1 and smtp()==smtp1,'restart bypassed durable backoff')
        wait(lambda:job()['attempt_count']==2 and job()['status']=='retrying','second native retry not observed',45);stop(second)
        retry2=job();smtp2=smtp();require(retry2['last_error_code']=='smtp_transient' and smtp2['transient_rejections']==2 and smtp2['deliveries']==0,'second retry state incorrect')
        require((datetime.fromisoformat(retry2['next_attempt_at'])-datetime.fromisoformat(retry2['updated_at'])).total_seconds()==60,'second backoff differs from native 60 seconds')
        mode.write_text('success\n')
        workers=[start('worker-recovered-a'),start('worker-recovered-b')]
        wait(lambda:job()['status']=='sent','SMTP recovery not delivered',75)
        for p in workers:stop(p)
        settled=job();delivered=smtp();require(settled['attempt_count']==3 and settled['next_attempt_at'] is None and not settled['last_error_code'] and delivered['deliveries']==1,'recovery duplicated or lost authorized mail')
        repeated=start('worker-after-sent');time.sleep(2);stop(repeated)
        after_repeat=job();repeat_smtp=smtp();require(after_repeat==settled and repeat_smtp==delivered,'sent logical mail was delivered again')
        attempts=json.loads(mysql(f"SELECT JSON_ARRAYAGG(JSON_OBJECT('attempt',attempt_number,'status',status,'error_code',COALESCE(error_code,''),'started_at',started_at,'completed_at',completed_at)) FROM mail_attempts WHERE mail_job_id={q(initial['id'])}"))
        proof={'implementation_commit':HEAD,'initial':initial,'duplicate_http_status':duplicate_code,'retry1':retry1,'before_due':before_due,'retry2':retry2,'settled':settled,'after_repeat':after_repeat,'smtp1':smtp1,'smtp2':smtp2,'delivered':delivered,'repeat_smtp':repeat_smtp,'replay_wait_seconds':replay_wait_seconds,'test_token_replay_preserved':True,'attempts':sorted(attempts,key=lambda a:a['attempt']),'actual_backoff_seconds':[30,60],'manual_queue_patch':False,'smtp_authority':'local protocol sink, no external delivery claim'}
    finally:
        for p in processes:
            if p.poll() is None:p.terminate();p.wait(timeout=10)
        for handle in handles:handle.close()
        server.shutdown();server.server_close();thread.join(timeout=5)
    for name in names:
        raw=(directory/name).read_bytes();require(b'@example.test' not in raw and b'Bearer ' not in raw and cookie.encode() not in raw,'private material in mail diagnostics')
        for key,value in os.environ.items():
            if key.startswith('GOJET_') and any(x in key for x in ('SECRET','TOKEN','PASSWORD','DSN','LOGIN_FIXTURE')) and len(value)>=8:
                require(value.encode() not in raw,'configured secret in mail diagnostics')
    (directory/'observations.json').write_text(json.dumps(proof,indent=2)+'\n');(directory/'P20-T042.json').write_bytes(previous.read_bytes());(directory/'P20-T020.json').write_text(json.dumps(p20)+'\n')
    names+=['observations.json','P20-T042.json','P20-T020.json']
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest((directory/n).read_bytes()) for n in names}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes());names={n+'.jsonl' for n in ['worker-first','worker-restarted','worker-recovered-a','worker-recovered-b','worker-after-sent']}|{'observations.json','P20-T042.json','P20-T020.json'}
    require(m['implementation_commit']==head and set(m['files'])==names,'mail recovery inventory/SHA mismatch')
    raw={}
    for name,sha in m['files'].items():
        raw[name]=(directory/name).read_bytes();require(digest(raw[name])==sha,'mail recovery digest mismatch')
    for n in (20,42):
        case=json.loads(raw[f'P20-T{n:03d}.json']);require(case['implementation_commit']==head and case['status']=='PASS' and case['errors']==[],'mail recovery predecessor invalid')
    p=json.loads(raw['observations.json']);d=json.loads(raw['P20-T020.json'])['details'];initial=p['initial']
    require(p['implementation_commit']==head and p['manual_queue_patch'] is False and p['smtp_authority']=='local protocol sink, no external delivery claim','mail source/authority mismatch')
    for name in ['initial','retry1','before_due','retry2','settled','after_repeat']:
        j=p[name];require(j['workspace_id']==d['workspace_id'] and j['requester_user_id']==d['user_id'] and j['recipient_matches']==1,'mail tenant/recipient mismatch')
        require(j['id']==initial['id'] and j['resource_id']==initial['resource_id'],'logical mail changed on restart')
    require(initial['status']=='queued' and initial['attempt_count']==0 and p['duplicate_http_status']==400 and p['test_token_replay_preserved'] is True and p['before_due']==p['retry1'],'initial queue/idempotency/backoff authority mismatch')
    for i,delay in [(1,30),(2,60)]:
        j=p[f'retry{i}'];s=p[f'smtp{i}'];require(j['status']=='retrying' and j['attempt_count']==i and j['last_error_code']=='smtp_transient' and s['transient_rejections']==i and s['deliveries']==0 and (datetime.fromisoformat(j['next_attempt_at'])-datetime.fromisoformat(j['updated_at'])).total_seconds()==delay,'bounded native backoff mismatch')
    j=p['settled'];require(j==p['after_repeat'] and j['status']=='sent' and j['attempt_count']==3 and j['next_attempt_at'] is None and not j['last_error_code'] and p['delivered']==p['repeat_smtp'] and p['delivered']['deliveries']==1,'recovered mail delivered more than once')
    require([a['attempt'] for a in p['attempts']]==[1,2,3] and [a['status'] for a in p['attempts']]==['transient_failure','transient_failure','sent'],'durable mail attempt sequence mismatch')
    for previous,current in zip(p['attempts'],p['attempts'][1:]):
        require((datetime.fromisoformat(current['started_at'])-datetime.fromisoformat(previous['completed_at'])).total_seconds()>=[30,60][previous['attempt']-1],'actual send bypassed durable retry delay')
    rows=[json.loads(line) for name,data in raw.items() if name.endswith('.jsonl') for line in data.splitlines() if line]
    outcomes=[r for r in rows if r.get('msg')=='mail job completion persisted' and r.get('mail_job_fingerprint')==fingerprint('mail_job',initial['id'])]
    require([(r['status'],r['attempt_count']) for r in sorted(outcomes,key=lambda r:r['attempt_count'])]==[('retrying',1),('retrying',2),('sent',3)],'native mail completion records mismatch')
    return {'formal_p20_t043_claim':True,'actual_backoff_seconds':[30,60],'native_restart_preserves_backoff':True,'logical_deliveries':1,'test_token_replay_preserved':True,'manual_queue_patch':False,'external_delivery_claim':False,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t043_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T043: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T043','recovery','Mail worker retry backoff and recovery',errors,details)
