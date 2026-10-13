"""Kill native worker after durable insert/before ACK, then restart both workers."""
import http.client
import json
import os
import subprocess
import time

from common import HEAD, ROOT, emit
from t022_case import mysql,q,predecessor
from t037_case import digest,require
from recovery.redis_fault_proxy import RedisFaultProxy


def collect():
    source=ROOT/'artifacts/v10/P20/recovery/P20-T040.json';p40=json.loads(source.read_bytes())
    require(p40['status']=='PASS' and p40['errors']==[] and p40['implementation_commit']==HEAD,'T041 requires same-head T040')
    t13=predecessor('P20-T013');ws,lid=t13['details']['workspace_id'],int(t13['details']['link_id'])
    directory=ROOT/'artifacts/v10/P20/runtime/t041';directory.mkdir(parents=True,exist_ok=True)
    host,port=os.environ['GOJET_REDIS_ADDR'].rsplit(':',1)
    proxy=RedisFaultProxy((host,int(port)))
    env=os.environ.copy();env['GOJET_REDIS_ADDR']='%s:%s'%proxy.server_address
    env.update(GOJET_ANALYTICS_WORKER_CONSUMER='p20-t041-'+HEAD[:12],GOJET_ANALYTICS_WORKER_MAX_MESSAGES='1')
    handles=[];processes=[];names=[]
    def start(name,command,changes=None):
        sink=(directory/(name+'.jsonl')).open('wb');handles.append(sink);names.append(name+'.jsonl')
        p=subprocess.Popen(command,cwd=ROOT,env={**env,**(changes or {})},stdout=sink,stderr=sink);processes.append(p);return p
    def records(name):
        raw=(directory/(name+'.jsonl')).read_bytes()
        if raw and not raw.endswith(b'\n'):raw=raw[:raw.rfind(b'\n')+1]
        return [json.loads(l) for l in raw.splitlines() if l]
    def wait(predicate,message,timeout=20):
        deadline=time.monotonic()+timeout
        while not predicate():
            require(time.monotonic()<deadline,message);time.sleep(.1)
    def run(name,command,changes=None):
        p=start(name,command,changes);require(p.wait(timeout=30)==0,name+' failed');return records(name)
    def redis(*args):
        return subprocess.run(['redis-cli','--raw','-h',host,'-p',port,*args],capture_output=True,text=True,check=True,timeout=10).stdout.strip()
    def snapshot():
        return json.loads(mysql(f"SELECT JSON_OBJECT('clicks',(SELECT click_count FROM links WHERE id={lid}),'outbox',(SELECT COUNT(*) FROM analytics_outbox),'events',(SELECT COUNT(*) FROM analytics_events),'aggregate',(SELECT COALESCE(SUM(clicks),0) FROM analytics_hourly_aggregates),'unpublished',(SELECT COUNT(*) FROM analytics_outbox WHERE published_at IS NULL))"))
    group='gojet-analytics-workers-v1'
    try:
        run('cache-current',['/tmp/gojet-p20-operationsmonitor','--once'],{'GOJET_RISK_PROVIDER_NAME':'p20-controlled-provider','GOJET_RISK_PROVIDER_ENDPOINT':'http://127.0.0.1:9','GOJET_RISK_POLICY_VERSION':'p20-t039-recovery-v1','GOJET_WEBHOOK_DELIVERY_ENABLED':'0'})
        before=snapshot();require(before['outbox']==before['events']==before['aggregate'] and before['unpublished']==0,'initial analytics not drained')
        require(int(redis('XPENDING','gojet:analytics:clicks:v1',group).splitlines()[0])==0,'unrelated pending delivery')
        hostname,code=mysql(f'SELECT hostname,code FROM links WHERE id={lid} AND workspace_id={q(ws)}').split('\t')
        conn=http.client.HTTPConnection('127.0.0.1',18080,timeout=15)
        try:
            conn.request('GET','/'+code,headers={'Host':hostname,'User-Agent':'P20-Worker-Recovery/1.0'})
            response=conn.getresponse();response.read();require(response.status==307 and response.getheader('Location'),'real recovery click failed')
        finally:conn.close()
        event=json.loads(mysql(f"SELECT JSON_OBJECT('event_id',event_id,'workspace_id',workspace_id,'link_id',link_id,'sequence',click_sequence,'stream_id',published_stream_id) FROM analytics_outbox WHERE link_id={lid} ORDER BY click_sequence DESC LIMIT 1"))
        proxy.set_fault(commands=('XACK',))
        worker=start('worker-before-crash',[os.environ['GOJET_P20_ANALYTICS_WORKER_BIN']])
        wait(lambda:proxy.snapshot()['blocked_commands'].get('XACK',0)>0 and snapshot()['events']==before['events']+1,'worker did not persist before actual ACK failure')
        worker.kill();worker.wait(timeout=5);require(worker.returncode==-9,'worker not actually interrupted')
        committed=snapshot();pending_before=int(redis('XPENDING','gojet:analytics:clicks:v1',group).splitlines()[0]);require(pending_before==1,'pre-ACK delivery not pending')
        reconciler=start('reconciler-before-crash',[os.environ['GOJET_P20_ANALYTICS_RECONCILER_BIN']],{'GOJET_ANALYTICS_RECONCILER_ONCE':'0','GOJET_ANALYTICS_RECONCILE_REPAIR':'1','GOJET_ANALYTICS_RECONCILER_INTERVAL_SECONDS':'5'})
        wait(lambda:any(r.get('msg')=='analytics reconciliation cycle' for r in records('reconciler-before-crash')),'native reconciliation did not commit a cycle')
        reconciler.kill();reconciler.wait(timeout=5);require(reconciler.returncode==-9,'reconciler not actually interrupted')
        proxy.set_fault()
        restarted=run('worker-restarted',[os.environ['GOJET_P20_ANALYTICS_WORKER_BIN']])
        matching=[r for r in restarted if r.get('msg')=='analytics event processed' and r.get('event_id')==event['event_id']]
        require(len(matching)==1 and matching[0]['inserted'] is False and matching[0]['stream_id']==event['stream_id'],'restart did not deduplicate the exact pending event')
        require(int(redis('XPENDING','gojet:analytics:clicks:v1',group).splitlines()[0])==0,'pending ACK not drained')
        for name in ('reconciler-restarted','reconciler-repeat'):
            logs=run(name,[os.environ['GOJET_P20_ANALYTICS_RECONCILER_BIN']],{'GOJET_ANALYTICS_RECONCILER_ONCE':'1','GOJET_ANALYTICS_RECONCILE_REPAIR':'1'})
            cycles=[r for r in logs if r['msg']=='analytics reconciliation cycle'];require(len(cycles)==1 and cycles[0]['workspace_complete'] is True and cycles[0]['repaired'] is False,'reconciler restart did not remain converged')
        final=snapshot();require(final==committed and final['clicks']==before['clicks']+1 and final['outbox']==final['events']==final['aggregate']==before['outbox']+1 and final['unpublished']==0,'restart lost or duplicated a click')
        proof={'implementation_commit':HEAD,'workspace_id':ws,'link_id':lid,'event':event,'before':before,'committed_before_crash':committed,'final':final,'worker_exit':worker.returncode,'reconciler_exit':reconciler.returncode,'pending_before_restart':pending_before,'pending_after_restart':0,'faults':proxy.snapshot(),'reconciler_interruption_boundary':'after committed cycle, before next scheduled cycle','manual_data_patch':False}
    finally:
        proxy.set_fault()
        for p in processes:
            if p.poll() is None:p.kill();p.wait(timeout=5)
        for f in handles:f.close()
        proxy.stop()
    for name in names:
        raw=(directory/name).read_bytes()
        require(b'@example.test' not in raw and b'Bearer ' not in raw,'private material in worker recovery logs')
        for key,value in os.environ.items():
            if key.startswith('GOJET_') and any(k in key for k in ('SECRET','TOKEN','PASSWORD','DSN','LOGIN_FIXTURE')) and len(value)>=8:
                require(value.encode() not in raw,'configured private material in recovery logs')
    data=(json.dumps(proof,indent=2)+'\n').encode();(directory/'observations.json').write_bytes(data);(directory/'P20-T040.json').write_bytes(source.read_bytes());names+=['observations.json','P20-T040.json']
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest((directory/n).read_bytes()) for n in names}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes());require(m['implementation_commit']==head and set(m['files'])=={'cache-current.jsonl','worker-before-crash.jsonl','reconciler-before-crash.jsonl','worker-restarted.jsonl','reconciler-restarted.jsonl','reconciler-repeat.jsonl','observations.json','P20-T040.json'},'worker recovery inventory/SHA mismatch')
    data={}
    for name,sha in m['files'].items():
        raw=(directory/name).read_bytes();require(digest(raw)==sha,'worker recovery digest mismatch');data[name]=raw
    p=json.loads(data['observations.json']);pre=json.loads(data['P20-T040.json']);b=p['before'];f=p['final'];event=p['event']
    require(pre['status']=='PASS' and pre['errors']==[] and pre['implementation_commit']==p['implementation_commit']==head,'worker recovery predecessor mismatch')
    require(p['workspace_id']==event['workspace_id']==pre['details']['workspace_id'] and p['link_id']==event['link_id'] and event['sequence']==b['clicks']+1,'worker recovery event binding mismatch')
    require(p['worker_exit']==p['reconciler_exit']==-9 and p['pending_before_restart']==1 and p['pending_after_restart']==0 and p['faults']['blocked_commands']['XACK']>0,'native interruption/ACK boundary unproven')
    require(f==p['committed_before_crash'] and f['clicks']==b['clicks']+1 and f['outbox']==f['events']==f['aggregate']==b['outbox']+1 and f['unpublished']==0 and p['manual_data_patch'] is False,'restart changed authoritative event accounting')
    rows=[json.loads(l) for l in data['worker-restarted.jsonl'].splitlines() if l];matches=[r for r in rows if r.get('msg')=='analytics event processed' and r.get('event_id')==event['event_id']]
    require(len(matches)==1 and matches[0]['inserted'] is False and matches[0]['stream_id']==event['stream_id'],'original duplicate-consumption evidence missing')
    for name in ('reconciler-before-crash.jsonl','reconciler-restarted.jsonl','reconciler-repeat.jsonl'):
        rows=[json.loads(l) for l in data[name].splitlines() if l];cycles=[r for r in rows if r.get('msg')=='analytics reconciliation cycle'];require(cycles and all(r['workspace_complete'] is True and r['repaired'] is False and r['source_event_total']==r['aggregate_after']==f['events'] for r in cycles),'original reconciler convergence evidence missing')
    return {'formal_p20_t041_claim':True,'worker_killed_after_persist_before_ack':True,'reconciler_killed_between_committed_cycles':True,'pending_replayed_once':True,'manual_data_patch':False,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t041_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T041: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T041','recovery','Analytics worker and reconciler restart recovery',errors,details)
