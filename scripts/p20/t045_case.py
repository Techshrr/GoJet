"""Authenticated callback ordering and invalid input after real T040 recovery."""
import concurrent.futures
import hashlib
import hmac
import json
import os
import subprocess
import time

from common import HEAD, ROOT, emit
from t021_case import AMOUNT, signed_post, stripe_event
from t022_case import http, mysql, q
from t037_case import digest, require, fingerprint
from t040_case import inspect as inspect_transaction


def collect():
    previous=ROOT/'artifacts/v10/P20/recovery/P20-T044.json';p44=json.loads(previous.read_bytes())
    require(p44['implementation_commit']==HEAD and p44['status']=='PASS' and p44['errors']==[],'T045 requires same-head T044')
    transaction_root=ROOT/'artifacts/v10/P20/runtime/t040';verified=inspect_transaction(transaction_root,HEAD)
    p40=json.loads((transaction_root/'observations.json').read_bytes());ws,oid=p40['workspace_id'],p40['order_id']
    intent=mysql(f"SELECT provider_reference FROM billing_provider_intents WHERE order_id={q(oid)} AND workspace_id={q(ws)} AND provider='stripe'")
    require(intent and verified['real_mysql_crash_restart'] is True,'original recovery provider intent unavailable')
    def snapshot():
        return json.loads(mysql(f"SELECT JSON_OBJECT('order_status',(SELECT status FROM billing_orders WHERE id={q(oid)}),'invoice_status',(SELECT status FROM billing_invoices WHERE order_id={q(oid)}),'transactions',(SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(oid)}),'transaction_status',(SELECT status FROM billing_transactions WHERE order_id={q(oid)}),'currency',(SELECT currency FROM billing_transactions WHERE order_id={q(oid)}),'amount',(SELECT amount_minor FROM billing_transactions WHERE order_id={q(oid)}),'wrong_tenant',(SELECT COUNT(*) FROM billing_transactions WHERE order_id={q(oid)} AND workspace_id<>{q(ws)}),'payment_notifications',(SELECT COUNT(*) FROM workspace_notifications WHERE resource_type='billing_order' AND resource_id={q(oid)} AND event_key='payment_succeeded'),'failed_notifications',(SELECT COUNT(*) FROM workspace_notifications WHERE resource_type='billing_order' AND resource_id={q(oid)} AND event_key='payment_failed'),'callbacks',(SELECT COUNT(*) FROM payment_callback_events WHERE provider='stripe' AND provider_transaction_id={q(intent)}),'active_subscriptions',(SELECT COUNT(*) FROM workspace_subscriptions WHERE workspace_id={q(ws)} AND status='active'),'active_grants',(SELECT COUNT(*) FROM entitlement_grants WHERE workspace_id={q(ws)} AND source_type='billing' AND revoked_at IS NULL))"))
    suffix=HEAD[:12];initial=snapshot();require(initial['transactions']==initial['payment_notifications']==1 and initial['order_status']==initial['transaction_status']=='paid','original settled authority missing')
    original=stripe_event('evt_p20_t040_'+suffix,'payment_intent.succeeded',intent)
    response=signed_post(original)[0];require(response==200 and snapshot()==initial,'original callback replay changed recovery outcome')
    late=stripe_event('evt_p20_t045_late_'+suffix,'payment_intent.payment_failed',intent)
    late_data=json.loads(late);late_data['data']['object']['status']='requires_payment_method';late=json.dumps(late_data,separators=(',',':')).encode()
    late_status=signed_post(late)[0];require(late_status==409 and snapshot()==initial,'late failure regressed paid settlement')
    tests={
        'amount':stripe_event('evt_p20_t045_amount_'+suffix,'payment_intent.succeeded',intent,AMOUNT+1),
        'unknown_intent':stripe_event('evt_p20_t045_unknown_'+suffix,'payment_intent.succeeded','pi_p20_unbound_'+suffix),
        'malformed':b'{',
    }
    currency=json.loads(stripe_event('evt_p20_t045_currency_'+suffix,'payment_intent.succeeded',intent));currency['data']['object']['currency']='eur';tests['currency']=json.dumps(currency,separators=(',',':')).encode()
    environment=json.loads(stripe_event('evt_p20_t045_live_'+suffix,'payment_intent.succeeded',intent));environment['livemode']=True;tests['livemode']=json.dumps(environment,separators=(',',':')).encode()
    invalid=[]
    for name,raw in tests.items():
        status=signed_post(raw)[0];after=snapshot();require(status==401 and after==initial,'invalid callback gained settlement authority: '+name);invalid.append({'mode':name,'status':status,'after':after})
    stale_ts=int(time.time())-3600;secret=os.environ['GOJET_BILLING_STRIPE_WEBHOOK_SECRET']
    signature=hmac.new(secret.encode(),str(stale_ts).encode()+b'.'+original,hashlib.sha256).hexdigest()
    stale_status=http('POST','/api/payments/callbacks/stripe',original,{'Stripe-Signature':f't={stale_ts},v1={signature}'})[0]
    require(stale_status==401 and snapshot()==initial,'expired signature replay accepted')
    browser=http('GET','/api/payments/callbacks/stripe?payment_intent='+intent+'&redirect_status=succeeded')[0]
    unsigned=http('POST','/api/payments/callbacks/stripe',original)[0]
    require(browser==404 and unsigned==401 and snapshot()==initial,'browser or unsigned return became settlement authority')
    new_event=stripe_event('evt_p20_t045_confirm_'+suffix,'payment_intent.succeeded',intent)
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
        statuses=list(pool.map(lambda _:signed_post(new_event)[0],range(4)))
    expected={**initial,'callbacks':initial['callbacks']+1};final=snapshot()
    require(statuses==[200]*4 and final==expected,'concurrent distinct event repeated settlement instead of only event receipt')
    final_replay=signed_post(original)[0];require(final_replay==200 and snapshot()==final,'reordered original event changed final settlement')
    proof={'implementation_commit':HEAD,'workspace_id':ws,'order_sha256':fingerprint('order',oid),'initial':initial,'original_replay_status':response,'late_failure_status':late_status,'invalid':invalid,'stale_signature_status':stale_status,'browser_status':browser,'unsigned_status':unsigned,'concurrent_statuses':statuses,'final':final,'final_replay_status':final_replay,'t040_collection_sha256':verified['collection_sha256'],'provider_identifiers_recorded':False,'raw_callback_recorded':False,'manual_settlement_patch':False}
    directory=ROOT/'artifacts/v10/P20/runtime/t045';directory.mkdir(parents=True,exist_ok=True)
    files={'observations.json':(json.dumps(proof,indent=2)+'\n').encode(),'P20-T044.json':previous.read_bytes(),'P20-T040.json':(ROOT/'artifacts/v10/P20/recovery/P20-T040.json').read_bytes()}
    for name,data in files.items():
        require(secret.encode() not in data and intent.encode() not in data,'private provider material in callback evidence');(directory/name).write_bytes(data)
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest(raw) for n,raw in files.items()}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes());require(m['implementation_commit']==head and set(m['files'])=={'observations.json','P20-T044.json','P20-T040.json'},'callback evidence inventory/SHA mismatch');data={}
    for name,sha in m['files'].items():
        raw=(directory/name).read_bytes();require(digest(raw)==sha,'callback evidence digest mismatch');data[name]=json.loads(raw)
    for n in (40,44):
        case=data[f'P20-T{n:03d}.json'];require(case['implementation_commit']==head and case['status']=='PASS' and case['errors']==[],'callback predecessor invalid')
    p=data['observations.json'];b=p['initial'];f=p['final'];d=data['P20-T040.json']['details']
    require(p['implementation_commit']==head and p['workspace_id']==d['workspace_id'] and p['t040_collection_sha256']==d['collection_sha256'],'callback recovery correlation mismatch')
    require(b['order_status']==b['invoice_status']==b['transaction_status']=='paid' and b['transactions']==b['payment_notifications']==1 and b['failed_notifications']==b['wrong_tenant']==0 and b['currency']=='USD' and b['amount']==AMOUNT,'initial recovered settlement invalid')
    require({x['mode'] for x in p['invalid']}=={'amount','unknown_intent','malformed','currency','livemode'} and all(x['status']==401 and x['after']==b for x in p['invalid']),'invalid callback matrix incomplete')
    require(p['original_replay_status']==p['final_replay_status']==200 and p['late_failure_status']==409 and p['stale_signature_status']==p['unsigned_status']==401 and p['browser_status']==404,'callback replay/authorization semantics invalid')
    require(p['concurrent_statuses']==[200]*4 and f=={**b,'callbacks':b['callbacks']+1},'concurrent event receipt changed authoritative settlement')
    require(p['provider_identifiers_recorded'] is False and p['raw_callback_recorded'] is False and p['manual_settlement_patch'] is False,'callback evidence boundary misstated')
    return {'formal_p20_t045_claim':True,'real_authenticated_callback_recovery':True,'invalid_modes':sorted(x['mode'] for x in p['invalid']),'late_failure_denied':True,'stale_signature_denied':True,'browser_return_non_authoritative':True,'concurrent_replays':4,'logical_settlements':1,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t045_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T045: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T045','recovery','Payment callback replay ordering and failure recovery',errors,details)
