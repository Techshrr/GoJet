"""Crash an actual uncommitted payment transaction, restart MySQL, replay safely."""
from datetime import datetime
import concurrent.futures
import json
import os
import re
import selectors
import subprocess
import time

from common import HEAD, ROOT, emit
from t022_case import http_json, mysql, predecessor, q, session_cookie
from t021_case import AMOUNT, signed_post, stripe_event
from t037_case import digest, require


def collect():
    source=ROOT/'artifacts/v10/P20/recovery/P20-T039.json';p39=json.loads(source.read_bytes())
    require(p39['status']=='PASS' and p39['errors']==[] and p39['implementation_commit']==HEAD,'T040 requires same-head T039')
    container=os.environ.get('GOJET_P20_MYSQL_CONTAINER_ID','')
    require(os.environ.get('GITHUB_ACTIONS')=='true' and re.fullmatch('[a-f0-9]{64}',container),'isolated Actions MySQL container identity required')
    def docker(*args):
        return subprocess.run(['docker',*args,container],capture_output=True,text=True,check=True,timeout=40).stdout.strip()
    require(docker('inspect','--format','{{.Config.Image}}')=='mysql:8.4.11','unexpected container image')
    started_before=docker('inspect','--format','{{.State.StartedAt}}')
    p21=predecessor('P20-T021');ws,uid=p21['details']['workspace_id'],p21['details']['user_id']
    suffix=HEAD[:12]
    code,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t040-login-'+suffix})
    if code==429:
        time.sleep(61)
        code,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t040-login-retry-'+suffix})
    cookie=session_cookie(headers);require(code==200 and body.get('status')=='authenticated' and cookie,'real owner login failed')
    auth={'Cookie':'__Host-gojet_session='+cookie}
    code,_,me=http_json('GET','/api/me',headers=auth);require(code==200 and me.get('user',{}).get('id')==uid and me.get('csrf_token'),'real owner session mismatch')
    plan=int(mysql(f'SELECT plan_id FROM billing_orders WHERE workspace_id={q(ws)} AND status=\'paid\' ORDER BY created_at LIMIT 1'))
    code,_,order=http_json('POST',f'/api/workspaces/{ws}/orders',{'plan_id':plan,'kind':'new'}, {**auth,'Origin':os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],'X-CSRF-Token':me['csrf_token'],'Idempotency-Key':'p20-t040-order-'+suffix})
    require(code==201 and order.get('created') is True,'real recovery order creation failed')
    oid=order['order']['id'];require(order['order']['workspace_id']==ws and order['order']['money']['amount_minor']==AMOUNT,'new order authority mismatch')
    intent,event='pi_p20_t040_'+suffix,'evt_p20_t040_'+suffix
    # Same explicit external-provider-intent fixture boundary as accepted T021.
    mysql("INSERT INTO billing_provider_intents (id,workspace_id,order_id,provider,merchant_reference,provider_reference,settlement_asset_kind,settlement_asset,settlement_amount_units,settlement_scale,status,expires_at) VALUES "+f"({q('pint_p20_t040_'+suffix)},{q(ws)},{q(oid)},'stripe',{q('gj_p20_t040_'+suffix)},{q(intent)},'fiat','USD',{AMOUNT},NULL,'active',DATE_ADD(UTC_TIMESTAMP(6),INTERVAL 1 DAY))")
    raw=stripe_event(event,'payment_intent.succeeded',intent)
    def snapshot(dirty=False):
        sql=f"SELECT JSON_OBJECT('order_status',(SELECT status FROM billing_orders WHERE id={q(oid)}),'invoice_status',(SELECT status FROM billing_invoices WHERE order_id={q(oid)}),'transactions',(SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(oid)}),'events',(SELECT COUNT(*) FROM payment_callback_events WHERE provider='stripe' AND provider_event_id={q(event)}),'notifications',(SELECT COUNT(*) FROM workspace_notifications WHERE resource_type='billing_order' AND resource_id={q(oid)} AND event_key='payment_succeeded'),'wrong_tenant_transactions',(SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(oid)} AND workspace_id<>{q(ws)}),'amount',(SELECT amount_minor FROM billing_orders WHERE id={q(oid)}))"
        return json.loads(mysql(('SET SESSION TRANSACTION ISOLATION LEVEL READ UNCOMMITTED; ' if dirty else '')+sql))
    before=snapshot();require(before['order_status']=='pending' and before['invoice_status']=='open' and before['transactions']==before['events']==before['notifications']==0,'new payment not initially pending')
    env=os.environ.copy();env['MYSQL_PWD']=os.environ.get('GOJET_TEST_MYSQL_PASSWORD','root')
    mysql_args=['mysql','--protocol=tcp','-h',os.environ.get('GOJET_TEST_MYSQL_HOST','127.0.0.1'),'-P',os.environ.get('GOJET_TEST_MYSQL_PORT','3306'),'-u',os.environ.get('GOJET_TEST_MYSQL_USER','root'),'--unbuffered','-N','-B',os.environ.get('GOJET_TEST_MYSQL_DATABASE','gojet_test')]
    blocker=subprocess.Popen(mysql_args+['-e',"LOCK TABLES billing_audit_events WRITE; SELECT 'T040_LOCKED'; DO SLEEP(120); UNLOCK TABLES;"],env=env,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
    interrupted=None;dirty=None;stopped=False
    pool=concurrent.futures.ThreadPoolExecutor(max_workers=4)
    def callback():
        try:return signed_post(raw)[0]
        except (OSError,TimeoutError):return 0
    try:
        selector=selectors.DefaultSelector();selector.register(blocker.stdout,selectors.EVENT_READ)
        require(selector.select(10) and blocker.stdout.readline().strip()==b'T040_LOCKED','transaction barrier not acquired');selector.close()
        inflight=pool.submit(callback)
        deadline=time.monotonic()+10
        while True:
            dirty=snapshot(True)
            if dirty['transactions']==1 and dirty['order_status']=='paid':break
            require(not inflight.done() and time.monotonic()<deadline,'actual uncommitted payment write not observed');time.sleep(.1)
        committed_during_barrier=snapshot();require(committed_during_barrier==before,'uncommitted transaction became visible')
        docker('kill','--signal=KILL');stopped=True
        interrupted=inflight.result(timeout=25);require(interrupted not in range(200,300),'interrupted transaction falsely acknowledged settlement')
        docker('start');stopped=False
        deadline=time.monotonic()+90
        while True:
            try:
                if mysql('SELECT 1')=='1':break
            except RuntimeError:pass
            require(time.monotonic()<deadline,'MySQL restart recovery timeout');time.sleep(1)
        started_after=docker('inspect','--format','{{.State.StartedAt}}')
        require(datetime.fromisoformat(started_after)>datetime.fromisoformat(started_before),'container restart not observed')
        rolled_back=snapshot();require(rolled_back==before,'crash recovery exposed partial settlement')
        success=signed_post(raw)[0];require(success==200,'valid recovery callback not acknowledged')
        settled=snapshot();require(settled['order_status']=='paid' and settled['invoice_status']=='paid' and settled['transactions']==settled['events']==settled['notifications']==1 and settled['wrong_tenant_transactions']==0 and settled['amount']==AMOUNT,'recovered payment was not exactly one atomic settlement')
        duplicates=list(pool.map(lambda _:callback(),range(4)));after_duplicates=snapshot();require(duplicates==[200]*4 and after_duplicates==settled,'concurrent replay duplicated settlement')
        denied,_,_=http_json('POST','/api/payments/callbacks/stripe',json.loads(raw));after_unsigned=snapshot();require(denied==401 and after_unsigned==settled,'unsigned callback gained post-restart authority')
        proof={'implementation_commit':HEAD,'workspace_id':ws,'order_id':oid,'provider_intent_fixture_only':True,'outbound_provider_creation_authority':False,'before':before,'dirty_inflight':dirty,'committed_during_barrier':committed_during_barrier,'interrupted_http_status':interrupted,'after_restart':rolled_back,'recovered':settled,'after_duplicates':after_duplicates,'after_unsigned':after_unsigned,'duplicate_http_statuses':duplicates,'unsigned_http_status':denied,'mysql_restart_observed':True,'mysql_started_before':started_before,'mysql_started_after':started_after}
    finally:
        if stopped:
            docker('start')
        if blocker.poll() is None:blocker.terminate()
        try:blocker.wait(timeout=5)
        except subprocess.TimeoutExpired:blocker.kill();blocker.wait(timeout=5)
        pool.shutdown(wait=True)
    directory=ROOT/'artifacts/v10/P20/runtime/t040';directory.mkdir(parents=True,exist_ok=True)
    files={'observations.json':(json.dumps(proof,indent=2)+'\n').encode(),'P20-T039.json':source.read_bytes(),'P20-T021.json':json.dumps(p21).encode()}
    for name,data in files.items():(directory/name).write_bytes(data)
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest(data) for n,data in files.items()}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes());require(m['implementation_commit']==head and set(m['files'])=={'observations.json','P20-T039.json','P20-T021.json'},'transaction source inventory/SHA mismatch')
    data={}
    for name,sha in m['files'].items():
        raw=(directory/name).read_bytes();require(digest(raw)==sha,'transaction evidence digest mismatch');data[name]=json.loads(raw)
    for n in (21,39):
        p=data[f'P20-T{n:03d}.json'];require(p['implementation_commit']==head and p['status']=='PASS' and p['errors']==[],'transaction predecessor invalid')
    p=data['observations.json'];b=p['before'];r=p['recovered']
    require(p['implementation_commit']==head and p['workspace_id']==data['P20-T021.json']['details']['workspace_id'] and p['order_id'],'transaction source identity mismatch')
    require(b['order_status']=='pending' and b['invoice_status']=='open' and b['transactions']==b['events']==b['notifications']==0,'initial transaction authority mismatch')
    require(p['dirty_inflight']['transactions']==1 and p['dirty_inflight']['order_status']=='paid' and p['committed_during_barrier']==p['after_restart']==b and p['interrupted_http_status'] not in range(200,300),'uncommitted crash rollback not established')
    require(r['order_status']=='paid' and r['invoice_status']=='paid' and r['transactions']==r['events']==r['notifications']==1 and r['wrong_tenant_transactions']==0 and r['amount']==b['amount']==AMOUNT,'recovery settlement mismatch')
    require(datetime.fromisoformat(p['mysql_started_after'])>datetime.fromisoformat(p['mysql_started_before']),'native restart timestamps invalid')
    require(p['after_duplicates']==p['after_unsigned']==r and p['duplicate_http_statuses']==[200]*4 and p['unsigned_http_status']==401 and p['mysql_restart_observed'] is True and p['provider_intent_fixture_only'] is True and p['outbound_provider_creation_authority'] is False,'replay/auth/fixture boundaries invalid')
    return {'formal_p20_t040_claim':True,'workspace_id':p['workspace_id'],'real_mysql_crash_restart':True,'observed_uncommitted_write':True,'atomic_rollback':True,'recovery_settlements':1,'concurrent_duplicate_callbacks':4,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'provider_intent_fixture_only':True,'mock_authority':False,'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t040_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T040: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T040','recovery','MySQL transaction idempotency and restart recovery',errors,details)
