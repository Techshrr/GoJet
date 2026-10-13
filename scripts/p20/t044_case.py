"""Real Link/API navigation under production provider policy and cache recovery."""
from datetime import datetime, timezone
import http.client
import json
import os
import subprocess
import time

from common import HEAD, ROOT, emit
from t022_case import http_json, mysql, predecessor, q, session_cookie
from t037_case import digest, require


def collect():
    previous=ROOT/'artifacts/v10/P20/recovery/P20-T043.json';p43=json.loads(previous.read_bytes())
    require(p43['implementation_commit']==HEAD and p43['status']=='PASS' and p43['errors']==[],'T044 requires same-head T043')
    p13=predecessor('P20-T013');ws,uid=p13['details']['workspace_id'],p13['details']['user_id'];suffix=HEAD[:12]
    directory=ROOT/'artifacts/v10/P20/runtime/t044';directory.mkdir(parents=True,exist_ok=True)
    status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t044-login-'+suffix})
    if status==429:
        time.sleep(61)
        status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t044-login-retry-'+suffix})
    cookie=session_cookie(headers);require(status==200 and body.get('status')=='authenticated' and cookie,'T044 owner login failed')
    auth={'Cookie':'__Host-gojet_session='+cookie}
    def mutation(method,path,payload,label):
        status,_,me=http_json('GET','/api/me',headers=auth)
        require(status==200 and me.get('user',{}).get('id')==uid and me.get('csrf_token'),'T044 session/CSRF binding failed')
        return http_json(method,path,payload,{**auth,'Origin':os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],'X-CSRF-Token':me['csrf_token'],'X-Request-ID':'p20-t044-'+label+'-'+suffix})
    code='p20t044'+suffix
    payload={'hostname':'gojet.cc','domain_kind':'official','code':code,'title':'P20 native risk recovery','primary_destination':'https://example.com/p20-t044/'+suffix,'redirect_status':307,'routing':[],'ab':[],'utm':{},'access':{},'one_time':False,'change_reason':'P20 provider failure recovery verification'}
    status,_,created=mutation('POST',f'/api/workspaces/{ws}/links',payload,'create')
    require(status==201 and created.get('workspace_id')==ws and created.get('id'),'real recovery Link creation failed')
    lid=int(created['id']);original_fp=created['risk_fingerprint'];names=[]
    def snapshot():
        return json.loads(mysql(f"SELECT JSON_OBJECT('workspace_id',workspace_id,'id',id,'fingerprint',risk_fingerprint,'version',version,'clicks',click_count,'outbox',(SELECT COUNT(*) FROM analytics_outbox WHERE link_id={lid}),'events',(SELECT COUNT(*) FROM analytics_events WHERE link_id={lid})) FROM links WHERE id={lid}"))
    def request(label):
        conn=http.client.HTTPConnection('127.0.0.1',18080,timeout=15)
        try:
            conn.request('GET','/'+code,headers={'Host':'gojet.cc','User-Agent':'P20-Risk-Recovery/1.0'})
            response=conn.getresponse();body=response.read()
            if response.status==200:
                name='page-'+label+'.html';(directory/name).write_bytes(body);names.append(name)
            return {'status':response.status,'location_present':bool(response.getheader('Location')),'target_leaked':payload['primary_destination'].encode() in body}
        finally:conn.close()
    def prepare(mode,phase):
        env={**os.environ,'P20_EXACT_HEAD':HEAD,'P20_RISK_WORKSPACE':ws,'P20_RISK_LINK':str(lid),'P20_RISK_MODE':mode,'P20_RISK_PHASE':phase}
        p=subprocess.run(['go','run','./scripts/p20/t044_prepare'],cwd=ROOT,env=env,capture_output=True,timeout=40)
        require(p.returncode==0 and not p.stderr,'native provider/policy preparation failed')
        result=json.loads(p.stdout);name='provider-'+mode+'-'+phase+'.json';(directory/name).write_bytes(p.stdout);names.append(name)
        require(result['implementation_commit']==HEAD and result['workspace_id']==ws and result['link_id']==lid and result['fingerprint']==snapshot()['fingerprint'],'provider observation bound to wrong target')
        return result
    initial=snapshot();require(initial['clicks']==initial['outbox']==initial['events']==0,'new Link has invented click state')
    failures=[]
    for index,mode in enumerate(['pending','unavailable','malformed','partial','timeout','review','block']):
        # The first real queued scan is completed by the unavailable observation;
        # subsequent scenarios enqueue fresh native rescans, never orphan a job.
        phase=str(max(index,1));provider=prepare(mode,phase);response=request(mode);state=snapshot()
        require(provider['state']==('pending' if mode=='pending' else 'block' if mode=='block' else 'review'),'provider input produced an unexpected policy state')
        require(response['status']==200 and not response['location_present'] and not response['target_leaked'] and state==initial,'non-allow provider state enabled navigation/click')
        failures.append({'mode':mode,'provider':provider,'http':response,'state':state})
    short=prepare('allow-short','short');allowed_at=datetime.now(timezone.utc).isoformat();allowed=request('short-allow');require(allowed['status']==307 and allowed['location_present'],'current native allow did not recover navigation')
    after_allow=snapshot();time.sleep(2.2)
    expired_at=datetime.now(timezone.utc).isoformat();expired=request('expired');after_expired=snapshot();require(expired['status']==200 and not expired['location_present'] and after_expired==after_allow,'expired allow retained navigation authority')
    old=prepare('allow','before-mutation')
    current=snapshot();payload.update(primary_destination='https://example.com/p20-t044-new/'+suffix,expected_version=current['version'],status='active')
    status,_,updated=mutation('PATCH',f'/api/workspaces/{ws}/links/{lid}',payload,'destination-change')
    require(status==200 and updated.get('risk_fingerprint')!=original_fp,'real destination mutation did not change fingerprint')
    mutation_state=snapshot();stale=request('changed-fingerprint');require(stale['status']==200 and not stale['location_present'] and not stale['target_leaked'] and snapshot()==mutation_state,'old fingerprint allow authorized changed destination')
    recovered=prepare('allow','current-recovery');final_http=request('recovered');require(final_http['status']==307 and final_http['location_present'],'current fingerprint recovery failed')
    def native(name,command,changes):
        p=subprocess.run(command,cwd=ROOT,env={**os.environ,**changes},capture_output=True,timeout=30)
        require(p.returncode==0 and not p.stderr,'native analytics convergence failed')
        path=name+'.jsonl';(directory/path).write_bytes(p.stdout);names.append(path)
    native('worker',[os.environ['GOJET_P20_ANALYTICS_WORKER_BIN']],{'GOJET_ANALYTICS_WORKER_CONSUMER':'p20-t044-'+suffix,'GOJET_ANALYTICS_WORKER_MAX_MESSAGES':'2'})
    native('reconciler',[os.environ['GOJET_P20_ANALYTICS_RECONCILER_BIN']],{'GOJET_ANALYTICS_RECONCILER_ONCE':'1','GOJET_ANALYTICS_RECONCILE_REPAIR':'1'})
    final=snapshot();require(final['clicks']==final['outbox']==final['events']==2 and recovered['fingerprint']==final['fingerprint']!=old['fingerprint'],'recovered accounting or current authority mismatch')
    proof={'implementation_commit':HEAD,'initial':initial,'failures':failures,'short_allow':short,'allowed':allowed,'allowed_at':allowed_at,'expired_at':expired_at,'after_allow':after_allow,'expired':expired,'after_expired':after_expired,'old_allow':old,'mutation_state':mutation_state,'stale_fingerprint_http':stale,'recovered':recovered,'final_http':final_http,'final':final,'manual_risk_cache_write':False,'manual_link_patch':False,'protocol_fixture':True,'live_provider_claim':False}
    (directory/'observations.json').write_text(json.dumps(proof,indent=2)+'\n');(directory/'P20-T043.json').write_bytes(previous.read_bytes());(directory/'P20-T013.json').write_text(json.dumps(p13)+'\n');names+=['observations.json','P20-T043.json','P20-T013.json']
    for name in names:
        raw=(directory/name).read_bytes();require(cookie.encode() not in raw and b'@example.test' not in raw,'private material in risk evidence')
        for key,value in os.environ.items():
            if key.startswith('GOJET_') and any(x in key for x in ('SECRET','TOKEN','PASSWORD','DSN','LOGIN_FIXTURE')) and len(value)>=8:require(value.encode() not in raw,'configured secret in risk evidence')
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest((directory/n).read_bytes()) for n in names}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    modes=['pending','unavailable','malformed','partial','timeout','review','block']
    names={f'page-{mode}.html' for mode in modes+['expired','changed-fingerprint']}|{f'provider-{mode}-{max(i,1)}.json' for i,mode in enumerate(modes)}|{'provider-allow-short-short.json','provider-allow-before-mutation.json','provider-allow-current-recovery.json','worker.jsonl','reconciler.jsonl','observations.json','P20-T043.json','P20-T013.json'}
    m=json.loads((directory/'manifest.json').read_bytes());require(m['implementation_commit']==head and set(m['files'])==names,'risk source inventory/SHA mismatch');raw={}
    for name,sha in m['files'].items():
        raw[name]=(directory/name).read_bytes();require(digest(raw[name])==sha,'risk source digest mismatch')
    for n in (13,43):
        case=json.loads(raw[f'P20-T{n:03d}.json']);require(case['implementation_commit']==head and case['status']=='PASS' and case['errors']==[],'risk predecessor invalid')
    p=json.loads(raw['observations.json']);initial=p['initial'];final=p['final'];d=json.loads(raw['P20-T013.json'])['details']
    require(p['implementation_commit']==head and initial['workspace_id']==d['workspace_id'] and initial['clicks']==initial['outbox']==initial['events']==0 and [x['mode'] for x in p['failures']]==modes,'risk initial/coverage mismatch')
    for i,row in enumerate(p['failures']):
        provider=row['provider'];require(provider==json.loads(raw[f'provider-{row["mode"]}-{max(i,1)}.json']) and provider['implementation_commit']==head and provider['link_id']==initial['id'] and provider['workspace_id']==initial['workspace_id'] and provider['fingerprint']==initial['fingerprint'],'provider identity mismatch')
        require(provider['state']==('pending' if i==0 else 'block' if row['mode']=='block' else 'review') and row['http']=={'status':200,'location_present':False,'target_leaked':False} and row['state']==initial,'non-allow navigation/accounting mismatch')
    for key,name in [('short_allow','provider-allow-short-short.json'),('old_allow','provider-allow-before-mutation.json'),('recovered','provider-allow-current-recovery.json')]:
        provider=p[key]
        require(provider==json.loads(raw[name]) and provider['implementation_commit']==head and provider['workspace_id']==initial['workspace_id'] and provider['link_id']==initial['id'] and provider['state']==provider['runtime_state']=='allow' and provider['runtime_fingerprint']==provider['fingerprint'] and provider['protocol_fixture'] is True and provider['live_provider_claim'] is False,'allow observation/projection mismatch')
    require(datetime.fromisoformat(p['allowed_at'])<datetime.fromisoformat(p['short_allow']['valid_until'])<=datetime.fromisoformat(p['expired_at']),'expiry proof is not time-bound')
    require(p['allowed']['status']==307 and p['allowed']['location_present'],'initial current allow did not navigate')
    for mode in modes+['expired','changed-fingerprint']:
        page=raw['page-'+mode+'.html']
        expected=b'Link blocked' if mode=='block' else b'Link under review' if mode in modes[1:-1] else b'Link temporarily unavailable'
        require(expected in page and b'https://example.com/' not in page and b'http-equiv="refresh"' not in page.lower(),'rendered safety page contradicts server authority')
    require(p['after_allow']['clicks']==1 and p['after_expired']==p['after_allow'] and p['expired']=={'status':200,'location_present':False,'target_leaked':False},'expired allow failed open')
    require(p['old_allow']['fingerprint']==initial['fingerprint'] and p['mutation_state']['fingerprint']==p['recovered']['fingerprint']==final['fingerprint']!=initial['fingerprint'] and p['stale_fingerprint_http']=={'status':200,'location_present':False,'target_leaked':False},'old fingerprint remained authoritative')
    require(final['workspace_id']==initial['workspace_id'] and final['id']==initial['id'] and final['clicks']==final['outbox']==final['events']==2 and p['final_http']['status']==307 and p['final_http']['location_present'],'current fingerprint recovery/accounting incomplete')
    require(p['manual_risk_cache_write'] is False and p['manual_link_patch'] is False and p['protocol_fixture'] is True and p['live_provider_claim'] is False,'risk fixture boundary misstated')
    workers=[json.loads(l) for l in raw['worker.jsonl'].splitlines() if l];events=[r for r in workers if r.get('msg')=='analytics event processed'];require(len(events)==2 and all(r['link_id']==initial['id'] and r['inserted'] is True for r in events),'native recovery event proof missing')
    cycles=[json.loads(l) for l in raw['reconciler.jsonl'].splitlines() if l];require(any(r.get('msg')=='analytics reconciliation cycle' and r.get('workspace_complete') is True for r in cycles),'native recovery convergence record missing')
    return {'formal_p20_t044_claim':True,'provider_failure_modes':modes,'expired_allow_denied':True,'changed_fingerprint_denied':True,'native_current_recovery':True,'manual_link_patch':False,'manual_risk_cache_write':False,'protocol_fixture':True,'live_provider_claim':False,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t044_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T044: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T044','recovery','Risk-provider failure stale-state and recovery matrix',errors,details)
