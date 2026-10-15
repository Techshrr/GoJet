"""Re-evaluate original dependency-failure HTTP/HTML and durable-state evidence."""
import importlib
import json
from pathlib import Path

from common import HEAD, ROOT, emit
from t037_case import digest, require

CASES=(39,40,41,42,43,44,45)


def inspect(runtime, formal, head):
    admitted={}
    for number in CASES:
        case=f'P20-T{number:03d}'
        packet=runtime/f't{number:03d}'
        result=json.loads((formal/(case+'.json')).read_bytes())
        details=importlib.import_module(f't{number:03d}_case').inspect(packet,head)
        require(result['case']==case and result['implementation_commit']==head and result['status']=='PASS' and result['errors']==[] and result['details']==details,'degradation prerequisite does not reproduce from original evidence: '+case)
        admitted[case]={'formal_sha256':digest((formal/(case+'.json')).read_bytes()),'collection_sha256':details['collection_sha256']}
    raw={n:json.loads((runtime/f't{n:03d}'/'observations.json').read_bytes()) for n in CASES}
    # Bind truthful presentation to the same durable state; do not infer a UI
    # success from a worker exit code or from a producer's PASS label.
    redis=raw[39];sql=raw[40];files=raw[42];mail=raw[43];risk=raw[44];payments=raw[45]
    require(redis['outage']['status']==503 and not redis['outage']['location_present'] and redis['after_outage']==redis['before'],'Redis outage presented a completed redirect')
    require(redis['missing_cache']['pending_page'] and not redis['missing_cache']['location_present'],'missing safety authority presented navigation')
    require(sql['interrupted_http_status'] not in range(200,300) and sql['committed_during_barrier']==sql['after_restart']==sql['before'] and sql['after_restart']['order_status']=='pending','uncommitted payment presented paid success')
    require(all(r['failed']['published']==0 and r['failed_denied']=={'private':409,'public':403,'publish':409} and r['before_publish_status']==403 for r in files['results']),'scanner uncertainty presented published success')
    require(mail['retry1']['status']==mail['retry2']['status']=='retrying' and mail['smtp1']['deliveries']==mail['smtp2']['deliveries']==0 and mail['settled']['status']=='sent' and mail['delivered']['deliveries']==1,'SMTP failure presented completed delivery')
    require(all(r['http']['status']==200 and not r['http']['location_present'] and r['state']==risk['initial'] for r in risk['failures']),'risk UI safety state created a click')
    require(payments['late_failure_status']==409 and all(r['after']==payments['initial'] for r in payments['invalid']) and payments['final']['transactions']==1,'callback error presented false settlement')
    return {'formal_p20_t046_claim':True,'source_evidence':admitted,
        'representative_dependencies':['Redis','MySQL','ClamAV','SMTP','risk provider','payment callback'],
        'truthful_presentations':['503 unavailable without redirect','HTML pending/review/block without destination','uncommitted payment not acknowledged','409/403 unpublished file','durable mail retry before sent','401/409 rejected callback'],
        'original_html_revalidated':9,'original_native_packets_revalidated':len(CASES),
        'live_provider_claim':False,'external_mail_delivery_claim':False,
        'client_browser_matrix_claim':False,'manual_state_patch':False,
        'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    details={'formal_p20_t046_claim':False,'next_case_unlocked':False};errors=[]
    try:details=inspect(ROOT/'artifacts/v10/P20/runtime',ROOT/'artifacts/v10/P20/recovery',HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError) as error:
        errors.append('T046: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T046','recovery','Partial dependency degradation and truthful UI/API states',errors,details)
