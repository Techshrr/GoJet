"""Actual fileworker uncertainty and healthy rescanning of the original T016 file."""
import importlib.util
import json
import os
import socket
import subprocess
import threading
import time

from common import HEAD, ROOT, emit
from t022_case import http, http_json, mysql, predecessor, q, session_cookie
from t037_case import digest, require


def collect():
    previous=ROOT/'artifacts/v10/P20/recovery/P20-T041.json'
    p41=json.loads(previous.read_bytes())
    require(p41['implementation_commit']==HEAD and p41['status']=='PASS' and p41['errors']==[], 'T042 requires same-head T041')
    p23=predecessor('P20-T023')
    p16=predecessor('P20-T016');d=p16['details'];ws,uid,fid=d['workspace_id'],d['user_id'],int(d['file_id'])
    directory=ROOT/'artifacts/v10/P20/runtime/t042';directory.mkdir(parents=True,exist_ok=True)
    suffix=HEAD[:12]
    status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t042-login-'+suffix})
    if status==429:
        time.sleep(61)
        status,headers,body=http_json('POST','/api/auth/login',{'email':f'p20-t009-{suffix}@example.test','password':os.environ['GOJET_P20_T009_LOGIN_FIXTURE'],'correlation_id':'p20-t042-login-retry-'+suffix})
    cookie=session_cookie(headers);require(status==200 and body.get('status')=='authenticated' and cookie,'T042 real owner login failed')
    auth={'Cookie':'__Host-gojet_session='+cookie}
    counter=0
    def action(name):
        nonlocal counter
        counter+=1
        code,_,me=http_json('GET','/api/me',headers=auth)
        require(code==200 and me.get('user',{}).get('id')==uid and me.get('csrf_token'),'T042 session/CSRF binding failed')
        return http_json('POST',f'/api/workspaces/{ws}/files/{fid}/{name}',{'change_reason':'P20 T042 native uncertainty recovery'}, {**auth,'Origin':os.environ['GOJET_AUTH_ALLOWED_ORIGIN'],'X-CSRF-Token':me['csrf_token'],'X-Request-ID':f'p20-t042-{name}-{counter}-{suffix}'})
    def snapshot():
        return json.loads(mysql(f"SELECT JSON_OBJECT('id',f.id,'workspace_id',f.workspace_id,'content_sha256',f.content_sha256,'state',f.scan_state,'published',f.published,'generation',f.scan_generation,'attempt_id',a.id,'attempt_status',a.status,'error_code',COALESCE(a.error_code,''),'engine',COALESCE(a.engine_version,''),'signature',COALESCE(a.signature_version,'')) FROM files f JOIN file_scan_attempts a ON a.file_id=f.id AND a.generation=f.scan_generation WHERE f.id={fid} AND f.workspace_id={q(ws)}"))
    slug=mysql(f'SELECT public_slug FROM files WHERE id={fid} AND workspace_id={q(ws)}')
    def download(public=False):
        path='/api/public/files/'+slug if public else f'/api/workspaces/{ws}/files/{fid}/download'
        code,_,raw=http('GET',path,headers=None if public else auth)
        return {'status':code,'sha256':digest(raw)}
    def denied():
        private,public=download(),download(True);code,_,_=action('publish')
        require(private['status']==409 and public['status']==403 and code==409,'uncertain file gained distribution authority')
        return {'private':private['status'],'public':public['status'],'publish':code}
    spec=importlib.util.spec_from_file_location('p20_clamd_negative_fixture',ROOT/'scripts/p09/clamd_fault_server.py')
    fixture=importlib.util.module_from_spec(spec);spec.loader.exec_module(fixture)
    names=[]
    def worker(name,address,timeout='300ms'):
        env={**os.environ,'GOJET_CLAMAV_ADDRESS':address,'GOJET_CLAMAV_NETWORK':'tcp','GOJET_CLAMAV_DIAL_TIMEOUT':'300ms','GOJET_CLAMAV_SCAN_TIMEOUT':timeout,'GOJET_FILE_WORKER_MAX_JOBS':'1','GOJET_FILE_WORKER_ID':'p20-t042-'+name}
        p=subprocess.run([os.environ['GOJET_P20_FILEWORKER_BIN']],cwd=ROOT,env=env,capture_output=True,timeout=30)
        path=name+'.jsonl';(directory/path).write_bytes(p.stdout);names.append(path)
        require(p.returncode==0 and not p.stderr,'native fileworker failed or emitted unstructured diagnostics')
        rows=[json.loads(line) for line in p.stdout.splitlines() if line]
        completed=[r for r in rows if r.get('msg')=='file scan completed']
        require(len(completed)==1 and completed[0]['file_id']==fid,'worker scanned a different file')
    initial=snapshot();require(p23['details']['file_id']==fid and p23['details']['quarantine_persisted'] is True and initial['state']=='quarantined' and initial['attempt_status']=='queued' and initial['published']==0,'original T023 quarantine not queued/unpublished')
    results=[]
    expected={'missing':{'clamav_unavailable'},'health-indeterminate':{'clamav_unavailable'},'timeout':{'scan_read_failed','scan_write_failed'},'stale':{'signature_stale'},'indeterminate':{'indeterminate_response'}}
    for mode,errors in expected.items():
        if mode!='missing':
            code,_,_=action('rescan');require(code==202,'approved rescan failed')
        queued=snapshot();require(queued['state']=='quarantined' and queued['attempt_status']=='queued' and queued['published']==0,'rescan did not revoke distribution')
        queued_denied=denied()
        server=thread=None;unused=None
        try:
            if mode=='missing':
                # Bound but non-listening socket reserves an endpoint that refuses connections.
                unused=socket.socket();unused.bind(('127.0.0.1',0));address='%s:%s'%unused.getsockname()
            else:
                server=fixture.Server(('127.0.0.1',0),mode,2.0)
                thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
                address='%s:%s'%server.server_address
            worker(mode+'-failed',address)
        finally:
            if unused is not None:unused.close()
            if server is not None:server.shutdown();server.server_close();thread.join(timeout=5)
        failed=snapshot();(directory/'last-scan.json').write_text(json.dumps({'implementation_commit':HEAD,'mode':mode,'state':failed},indent=2)+'\n');require(failed['state']=='scan_error' and failed['attempt_status']=='error' and failed['published']==0 and failed['error_code'] in errors,'uncertainty did not fail closed with native error authority')
        failed_denied=denied()
        code,_,_=action('rescan');require(code==202,'authorized recovery rescan failed')
        recovery_queued=snapshot();require(recovery_queued['generation']==failed['generation']+1 and recovery_queued['state']=='quarantined' and recovery_queued['attempt_status']=='queued','recovery bypassed scan generation')
        worker(mode+'-healthy',os.environ['GOJET_P09_REAL_CLAMD_ADDRESS'],'10s')
        healthy=snapshot();require(healthy['state']=='safe' and healthy['attempt_status']=='clean' and healthy['published']==0 and healthy['engine'] and healthy['signature'] and not healthy['error_code'],'real ClamAV recovery did not produce clean unpublished authority')
        before_publish=download(True);require(before_publish['status']==403,'clean rescan silently republished bytes')
        code,_,_=action('publish');require(code==200,'authorized post-recovery publish failed')
        published=snapshot();private,public=download(),download(True)
        require(published['published']==1 and private['status']==public['status']==200 and private['sha256']==public['sha256']=='sha256:'+initial['content_sha256'],'recovered downloads differ from original content')
        row={'mode':mode,'queued':queued,'queued_denied':queued_denied,'failed':failed,'failed_denied':failed_denied,'recovery_queued':recovery_queued,'healthy':healthy,'before_publish_status':before_publish['status'],'published':published,'private':private,'public':public}
        results.append(row)
        (directory/'progress.json').write_text(json.dumps({'implementation_commit':HEAD,'completed_modes':results},indent=2)+'\n')
    proof={'implementation_commit':HEAD,'initial':initial,'results':results,'negative_clamd_protocol_fixtures':True,'healthy_authority':'actual ClamAV daemon','manual_scan_state_patch':False}
    for name in names:
        raw=(directory/name).read_bytes()
        require(b'@example.test' not in raw and b'Bearer ' not in raw,'private material in fileworker logs')
        for key,value in os.environ.items():
            if key.startswith('GOJET_') and any(x in key for x in ('SECRET','TOKEN','PASSWORD','DSN','LOGIN_FIXTURE')) and len(value)>=8:
                require(value.encode() not in raw,'configured private material in fileworker logs')
    (directory/'observations.json').write_text(json.dumps(proof,indent=2)+'\n')
    (directory/'P20-T041.json').write_bytes(previous.read_bytes());(directory/'P20-T016.json').write_text(json.dumps(p16)+'\n');(directory/'P20-T023.json').write_text(json.dumps(p23)+'\n')
    names+=['observations.json','P20-T041.json','P20-T016.json','P20-T023.json']
    (directory/'manifest.json').write_text(json.dumps({'implementation_commit':HEAD,'files':{n:digest((directory/n).read_bytes()) for n in names}},indent=2)+'\n')
    return directory


def inspect(directory,head):
    m=json.loads((directory/'manifest.json').read_bytes())
    modes=['missing','health-indeterminate','timeout','stale','indeterminate']
    names={mode+'-'+state+'.jsonl' for mode in modes for state in ('failed','healthy')}|{'observations.json','P20-T041.json','P20-T016.json','P20-T023.json'}
    require(m['implementation_commit']==head and set(m['files'])==names,'file recovery inventory/SHA mismatch')
    data={}
    for name,sha in m['files'].items():
        raw=(directory/name).read_bytes();require(digest(raw)==sha,'file recovery digest mismatch');data[name]=raw
    for n in (16,23,41):
        p=json.loads(data[f'P20-T{n:03d}.json']);require(p['implementation_commit']==head and p['status']=='PASS' and p['errors']==[],'file recovery predecessor invalid')
    p=json.loads(data['observations.json']);d=json.loads(data['P20-T016.json'])['details'];initial=p['initial']
    require(p['implementation_commit']==head and initial['id']==int(d['file_id']) and initial['workspace_id']==d['workspace_id'] and initial['state']=='quarantined' and initial['attempt_status']=='queued' and initial['published']==0 and json.loads(data['P20-T023.json'])['details']['file_id']==initial['id'],'original file identity mismatch')
    require([r['mode'] for r in p['results']]==modes and p['negative_clamd_protocol_fixtures'] is True and p['healthy_authority']=='actual ClamAV daemon' and p['manual_scan_state_patch'] is False,'uncertainty coverage/authority mismatch')
    generation=initial['generation']-1
    expected={'missing':{'clamav_unavailable'},'health-indeterminate':{'clamav_unavailable'},'timeout':{'scan_read_failed','scan_write_failed'},'stale':{'signature_stale'},'indeterminate':{'indeterminate_response'}}
    for row in p['results']:
        mode=row['mode'];queued,failed,requeued,healthy,published=[row[k] for k in ('queued','failed','recovery_queued','healthy','published')]
        for state in (queued,failed,requeued,healthy,published):
            require(state['id']==initial['id'] and state['workspace_id']==initial['workspace_id'] and state['content_sha256']==initial['content_sha256'],'file/tenant/content changed across recovery')
        require(queued['generation']==failed['generation']==generation+1 and requeued['generation']==healthy['generation']==published['generation']==generation+2,'scan generation continuity lost');generation+=2
        require(queued['state']==requeued['state']=='quarantined' and queued['attempt_status']==requeued['attempt_status']=='queued' and failed['state']=='scan_error' and failed['attempt_status']=='error' and failed['error_code'] in expected[mode],'uncertain scan state mismatch')
        require(all(s['published']==0 for s in (queued,failed,requeued,healthy)) and row['queued_denied']==row['failed_denied']=={'private':409,'public':403,'publish':409},'uncertain scan distribution not denied')
        require(healthy['state']==published['state']=='safe' and healthy['attempt_status']=='clean' and healthy['engine'] and healthy['signature'] and not healthy['error_code'] and published['published']==1 and row['before_publish_status']==403,'healthy recovery bypassed approved publication')
        for key in ('private','public'):
            require(row[key]['status']==200 and row[key]['sha256']=='sha256:'+initial['content_sha256'],'download content authority mismatch')
        for state,snapshot in (('failed',failed),('healthy',healthy)):
            rows=[json.loads(l) for l in data[mode+'-'+state+'.jsonl'].splitlines() if l]
            completed=[r for r in rows if r.get('msg')=='file scan completed']
            require(len(completed)==1 and completed[0]['file_id']==initial['id'] and completed[0]['attempt_id']==snapshot['attempt_id'] and completed[0]['generation']==snapshot['generation'] and completed[0]['verdict']==('error' if state=='failed' else 'clean'),'native scan log/SQL identity mismatch')
    return {'formal_p20_t042_claim':True,'uncertainty_modes':modes,'real_fileworker_scans':10,'real_clamav_recoveries':5,'negative_clamd_protocol_fixtures':True,'manual_scan_state_patch':False,'collection_sha256':digest((directory/'manifest.json').read_bytes()),'next_case_unlocked':False,'promotion_requires_same_head_gates':True}


def run_case():
    errors,details=[],{'formal_p20_t042_claim':False,'next_case_unlocked':False}
    try:details=inspect(collect(),HEAD)
    except (ValueError,KeyError,TypeError,OSError,RuntimeError,subprocess.SubprocessError) as error:
        errors.append('T042: '+(str(error) if isinstance(error,ValueError) else type(error).__name__))
    return emit('P20-T042','recovery','File worker and ClamAV uncertainty recovery',errors,details)
