"""Actual Redis transport loss, durable outbox replay and native security rebuild."""
from datetime import datetime
import http.client
import json
import os
from pathlib import Path
import subprocess
import time

from common import HEAD, ROOT, emit
from t022_case import mysql, q, predecessor
from t037_case import digest, require
from recovery.redis_fault_proxy import RedisFaultProxy


def collect():
    previous = ROOT/'artifacts/v10/P20/consistency/P20-T038.json'
    p38 = json.loads(previous.read_bytes())
    require(p38['implementation_commit'] == HEAD and p38['status'] == 'PASS' and p38['errors'] == [], 'T039 requires same-head T038')
    t13 = predecessor('P20-T013');ws,lid = t13['details']['workspace_id'],int(t13['details']['link_id'])
    directory = ROOT/'artifacts/v10/P20/runtime/t039';directory.mkdir(parents=True,exist_ok=True)
    env = os.environ.copy();env['P20_EXACT_HEAD'] = HEAD
    prepare = subprocess.run(['go','run','./scripts/p20/t039_prepare'],cwd=ROOT,env=env,capture_output=True,timeout=60)
    require(prepare.returncode == 0, 'inherited P16 controlled risk input preparation failed')
    risk = json.loads(prepare.stdout)
    require(risk['workspace_id'] == ws and risk['link_id'] == lid and risk['state'] == 'allow', 'risk fixture identity mismatch')
    host,code = mysql(f'SELECT hostname,code FROM links WHERE id={lid} AND workspace_id={q(ws)}').split('\t')
    redis_host,redis_port = os.environ['GOJET_REDIS_ADDR'].rsplit(':',1)
    proxy = RedisFaultProxy((redis_host,int(redis_port)))
    env['GOJET_REDIS_ADDR'] = '%s:%s'%proxy.server_address
    env['GOJET_REDIRECTENGINE_ADDR'] = '127.0.0.1:18090'
    env.update(GOJET_ANALYTICS_ENABLED='1',GOJET_TEST_ROUTING_HEADERS='0')
    subprocess.run(['go','build','-o','/tmp/gojet-p20-t039-redirect','./services/redirectengine/cmd/server'],cwd=ROOT,check=True,timeout=60)
    sink = (directory/'redirectengine.jsonl').open('wb')
    redirect = subprocess.Popen(['/tmp/gojet-p20-t039-redirect'],env=env,stdout=sink,stderr=sink)
    files = []
    def native(name,command,changes=None,expected=0):
        runenv = env.copy();runenv.update(changes or {})
        result = subprocess.run(command,cwd=ROOT,env=runenv,capture_output=True,timeout=35)
        require(not result.stderr, name+' emitted unstructured stderr')
        path = name+'.jsonl';(directory/path).write_bytes(result.stdout);files.append(path)
        require(result.returncode == expected, name+' unexpected exit status')
        return [json.loads(x) for x in result.stdout.splitlines() if x]
    def request(path=None):
        conn = http.client.HTTPConnection('127.0.0.1',18090,timeout=15)
        try:
            conn.request('GET',path or '/'+code,headers={'Host':host,'User-Agent':'P20-Recovery/1.0'})
            response = conn.getresponse();body=response.read();location=response.getheader('Location','')
            return {'status':response.status,'location_present':bool(location),'pending_page':b'waiting for a current safety decision' in body.lower()}
        finally:conn.close()
    def snapshot():
        return json.loads(mysql(f"SELECT JSON_OBJECT('clicks',(SELECT click_count FROM links WHERE id={lid}),'outbox',(SELECT COUNT(*) FROM analytics_outbox),'events',(SELECT COUNT(*) FROM analytics_events),'aggregate',(SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates),'unpublished',(SELECT COUNT(*) FROM analytics_outbox WHERE published_at IS NULL),'state',(SELECT status FROM analytics_workspace_state WHERE workspace_id={q(ws)}))"))
    def reconcile(name):
        rows=native(name,[os.environ['GOJET_P20_ANALYTICS_RECONCILER_BIN']],{'GOJET_ANALYTICS_RECONCILER_ONCE':'1','GOJET_ANALYTICS_RECONCILE_REPAIR':'1'})
        cycles=[r for r in rows if r['msg']=='analytics reconciliation cycle'];require(len(cycles)==1,'reconciliation cycle missing');return cycles[0]
    def worker(name):
        return native(name,[os.environ['GOJET_P20_ANALYTICS_WORKER_BIN']],{'GOJET_ANALYTICS_WORKER_MAX_MESSAGES':'1','GOJET_ANALYTICS_WORKER_CONSUMER':'p20-t039-'+HEAD[:12]})
    def redis(*args):
        p=subprocess.run(['redis-cli','--raw','-h',redis_host,'-p',redis_port,*args],capture_output=True,text=True,check=True,timeout=10)
        return p.stdout.strip()
    observations={}
    try:
        deadline=time.monotonic()+15
        while True:
            try:
                if request('/healthz')['status']==200:break
            except OSError:pass
            require(redirect.poll() is None and time.monotonic()<deadline,'native redirect not ready');time.sleep(.1)
        before=snapshot();require(before['outbox']==before['events']==before['aggregate'] and before['unpublished']==0,'initial event authority not drained')
        proxy.set_fault(all_connections=True)
        outage=request();require(outage['status']==503 and not outage['location_present'],'Redis outage allowed navigation')
        native('reconciler-outage',[os.environ['GOJET_P20_ANALYTICS_RECONCILER_BIN']],{'GOJET_ANALYTICS_RECONCILER_ONCE':'1'},1)
        after_outage=snapshot();require(after_outage==before,'unavailable dependency created a click/event')
        proxy.set_fault(commands=('XADD',))
        accepted=request();require(accepted['status']==307 and accepted['location_present'],'accepted redirect did not preserve truthful committed outcome')
        unpublished=snapshot();require(unpublished['clicks']==before['clicks']+1 and unpublished['outbox']==before['outbox']+1 and unpublished['events']==before['events'] and unpublished['unpublished']==1,'publication failure lost durable accepted event')
        event=json.loads(mysql(f"SELECT JSON_OBJECT('event_id',event_id,'workspace_id',workspace_id,'link_id',link_id,'sequence',click_sequence,'published',published_at IS NOT NULL) FROM analytics_outbox WHERE link_id={lid} ORDER BY click_sequence DESC LIMIT 1"))
        blocked=reconcile('reconciler-blocked');partial=snapshot()
        require(blocked['outbox_failed']>=1 and blocked['workspace_complete'] is False and partial['state']=='partial','failed publication claimed complete analytics')
        proxy.set_fault()
        replay=reconcile('reconciler-replay');require(replay['outbox_published']==1,'unpublished event not replayed')
        processed=worker('worker-recovered')
        require(any(r.get('event_id')==event['event_id'] and r.get('inserted') is True for r in processed),'worker did not consume the original accepted event')
        complete=reconcile('reconciler-complete');recovered=snapshot()
        require(recovered['outbox']==recovered['events']==recovered['aggregate']==before['outbox']+1 and recovered['unpublished']==0 and recovered['state']=='complete','recovery did not converge')
        stream_before=int(redis('XLEN','gojet:analytics:clicks:v1'))
        repeat=reconcile('reconciler-repeat');after_repeat=snapshot();stream_after=int(redis('XLEN','gojet:analytics:clicks:v1'))
        require(after_repeat==recovered and stream_after==stream_before and repeat['outbox_published']==0,'replay duplicated accepted events')
        key='risk:link:'+str(lid)+':'+risk['fingerprint']
        require(redis('DEL',key)=='1','security cache fault did not remove exact key')
        missing=request();require(missing['status']==200 and not missing['location_present'] and missing['pending_page'] and snapshot()==recovered,'missing security cache failed open')
        native('operationsmonitor-rebuild',['/tmp/gojet-p20-operationsmonitor','--once'],{'GOJET_RISK_PROVIDER_NAME':'p20-controlled-provider','GOJET_RISK_PROVIDER_ENDPOINT':'http://127.0.0.1:9','GOJET_RISK_POLICY_VERSION':risk['policy_version'],'GOJET_WEBHOOK_DELIVERY_ENABLED':'0'})
        rebuilt=json.loads(redis('GET',key));require(rebuilt['fingerprint']==risk['fingerprint'] and rebuilt['decision']=='allow' and rebuilt['policy_version']==risk['policy_version'],'native security projection did not rebuild exact authority')
        final_request=request();require(final_request['status']==307 and final_request['location_present'],'navigation did not recover after native cache rebuild')
        worker('worker-after-cache-rebuild');reconcile('reconciler-final');final=snapshot()
        require(final['clicks']==before['clicks']+2 and final['outbox']==final['events']==final['aggregate']==before['outbox']+2 and final['state']=='complete' and final['unpublished']==0,'cache recovery click did not converge')
        durable=json.loads(mysql(f"SELECT JSON_OBJECT('id',id,'scan_id',scan_id,'fingerprint',risk_fingerprint,'policy_version',policy_version,'state',state) FROM destination_risk_decisions WHERE id={q(str(risk['decision_id']))}"))
        observations={'implementation_commit':HEAD,'risk_input':risk,'durable_decision':durable,'rebuilt_cache':rebuilt,'before':before,'after_outage':after_outage,'after_repeat':after_repeat,'stream_before_repeat':stream_before,'stream_after_repeat':stream_after,'outage':outage,'accepted':accepted,'unpublished':unpublished,'event':event,'partial':partial,'recovered':recovered,'missing_cache':missing,'final_request':final_request,'final':final,'proxy':proxy.snapshot()}
    finally:
        proxy.set_fault();redirect.terminate()
        try:redirect.wait(timeout=10)
        except subprocess.TimeoutExpired:redirect.kill();redirect.wait(timeout=5)
        sink.close();proxy.stop()
    (directory/'observations.json').write_text(json.dumps(observations,indent=2)+'\n')
    (directory/'P20-T038.json').write_bytes(previous.read_bytes())
    (directory/'P20-T013.json').write_text(json.dumps(t13)+'\n')
    names=files+['redirectengine.jsonl','observations.json','P20-T038.json','P20-T013.json']
    for name in files+['redirectengine.jsonl']:
        raw=(directory/name).read_bytes()
        require(b'@example.test' not in raw and b'Bearer ' not in raw,'private material in recovery logs')
        for key,value in os.environ.items():
            if key.startswith('GOJET_') and any(k in key for k in ('SECRET','TOKEN','PASSWORD','DSN','LOGIN_FIXTURE')) and len(value)>=8:
                require(value.encode() not in raw,'configured private material in recovery logs')
    manifest={'implementation_commit':HEAD,'files':{n:digest((directory/n).read_bytes()) for n in names}}
    (directory/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes());require(m['implementation_commit']==head,'recovery source SHA mismatch')
    names={'redirectengine.jsonl','reconciler-outage.jsonl','reconciler-blocked.jsonl','reconciler-replay.jsonl','worker-recovered.jsonl','reconciler-complete.jsonl','reconciler-repeat.jsonl','operationsmonitor-rebuild.jsonl','worker-after-cache-rebuild.jsonl','reconciler-final.jsonl','observations.json','P20-T038.json','P20-T013.json'}
    require(set(m['files'])==names,'recovery source inventory incomplete')
    raw={}
    for name,sha in m['files'].items():
        raw[name]=(directory/name).read_bytes();require(digest(raw[name])==sha,'recovery source digest mismatch')
    p=json.loads(raw['observations.json']);before=p['before'];final=p['final'];event=p['event'];risk=p['risk_input'];durable=p['durable_decision']
    for n in (13,38):
        case=json.loads(raw[f'P20-T{n:03d}.json']);require(case['status']=='PASS' and case['errors']==[] and case['implementation_commit']==head,'recovery predecessor invalid')
    d13=json.loads(raw['P20-T013.json'])['details']
    require(p['implementation_commit']==head and risk['implementation_commit']==head and risk['workspace_id']==event['workspace_id']==d13['workspace_id'] and risk['link_id']==event['link_id']==int(d13['link_id']),'recovery event/tenant identity mismatch')
    require(event['sequence']==before['clicks']+1 and event['published']==0 and p['unpublished']['unpublished']==1 and p['unpublished']['outbox']==before['outbox']+1,'accepted event not durably pending')
    require(p['after_outage']==before and p['after_repeat']==p['recovered'] and p['stream_before_repeat']==p['stream_after_repeat'],'interruption/repeat changed durable totals')
    require(p['outage']['status']==503 and not p['outage']['location_present'] and p['accepted']['status']==307 and p['accepted']['location_present'],'outage/commit HTTP truth mismatch')
    require(p['partial']['state']=='partial' and p['recovered']['state']=='complete' and p['recovered']['outbox']==p['recovered']['events']==p['recovered']['aggregate']==before['outbox']+1,'replayed event failed to converge')
    require(p['proxy']['faults']>0 and p['proxy']['blocked_commands']['XADD']>0,'real transport fault not observed')
    require(p['missing_cache']['status']==200 and p['missing_cache']['pending_page'] and not p['missing_cache']['location_present'],'missing cache did not fail closed')
    require(durable['id']==risk['decision_id'] and durable['scan_id']==risk['scan_id'] and durable['fingerprint']==risk['fingerprint']==p['rebuilt_cache']['fingerprint'] and durable['state']==p['rebuilt_cache']['decision']=='allow' and durable['policy_version']==p['rebuilt_cache']['policy_version']==risk['policy_version'],'cache rebuilt from wrong durable authority')
    require(datetime.fromisoformat(p['rebuilt_cache']['checked_at']) < datetime.fromisoformat(p['rebuilt_cache']['valid_until']) <= datetime.fromisoformat(risk['valid_until']),'cache extended durable allow validity')
    require(risk['controlled_provider_fixture'] is True and risk['live_provider_claim'] is False,'provider fixture boundary misstated')
    require(final['clicks']==before['clicks']+2 and final['outbox']==final['events']==final['aggregate']==before['outbox']+2 and final['unpublished']==0 and final['state']=='complete' and p['final_request']['status']==307,'final recovery closure mismatch')
    rows={n:[json.loads(l) for l in data.splitlines() if l] for n,data in raw.items() if n.endswith('.jsonl')}
    require(any(r.get('event_id')==event['event_id'] and r.get('inserted') is True for r in rows['worker-recovered.jsonl']),'original worker event record missing')
    for name,expected in [('reconciler-blocked.jsonl',False),('reconciler-complete.jsonl',True),('reconciler-repeat.jsonl',True),('reconciler-final.jsonl',True)]:
        cycles=[r for r in rows[name] if r['msg']=='analytics reconciliation cycle'];require(len(cycles)==1 and cycles[0]['workspace_complete'] is expected,'original reconciliation state mismatch')
    return {'formal_p20_t039_claim':True,'real_redis_transport_interruption':True,'accepted_event_replayed':True,'native_security_cache_rebuilt':True,'controlled_provider_fixture':True,'live_provider_claim':False,'mock_authority':False,'secret_material_recorded':False,'logical_events_added':2,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t039_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T039: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T039','recovery','Redis interruption replay and cache-rebuild recovery',errors,details)


if __name__=='__main__':
    from common import fail_if_errors
    fail_if_errors([run_case()])
