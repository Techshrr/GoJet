#!/usr/bin/env python3
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import subprocess
from datetime import datetime, timezone

ROOT = Path('artifacts/v10/P10')
RESULTS = ROOT / 'results'
PRODUCERS = ROOT / 'evidence-producer-manifest.json'
CONTRACT = ROOT / 'contract' / 'contract.json'
INDEX = ROOT / 'evidence-index.json'
T019 = RESULTS / 'P10-T019.json'
REQUIRED_PRODUCERS = ('P10 Text Contract','P10 Real Text Integration','P10 Workspace Text Browser')


def head() -> str:
    return subprocess.check_output(['git','rev-parse','HEAD'], text=True).strip()

def digest(path: Path) -> str:
    h=hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024*1024), b''): h.update(chunk)
    return h.hexdigest()

def load_json(path: Path, errors: list[str]):
    if not path.is_file(): errors.append(f'missing {path}'); return {}
    try: return json.loads(path.read_text(encoding='utf-8'))
    except Exception as exc: errors.append(f'invalid JSON {path}: {exc}'); return {}

def case_path(number: int) -> Path:
    cid=f'P10-T{number:03d}'
    if number <= 5 or number in (12,15): return ROOT/'api'/f'{cid}.json'
    if number <= 14: return ROOT/'headers'/f'{cid}.json'
    return ROOT/'browser'/f'{cid}.json'

def require(condition: bool, message: str, errors: list[str]):
    if not condition: errors.append(message)

def validate_semantics(cases: dict[str,dict], errors: list[str]) -> None:
    obs=lambda n: cases[f'P10-T{n:03d}'].get('observations',{}) if isinstance(cases.get(f'P10-T{n:03d}'),dict) else {}
    require(obs(1).get('same_workspace_total')==1 and obs(1).get('other_workspace_total')==0,'T001 workspace isolation observation mismatch',errors)
    require(obs(3).get('viewer_mutation')==403 and obs(3).get('cross_tenant')==404,'T003 RBAC/tenant observation mismatch',errors)
    require(obs(4).get('stale_status')==409,'T004 stale conflict is not 409',errors)
    require(obs(5).get('public_after_delete')==410 and obs(5).get('stale_update')==410,'T005 delete/removal observation mismatch',errors)
    require(obs(6).get('status')==200 and obs(6).get('script_escaped') is True and obs(6).get('workspace_leaked') is False,'T006 public escaping observation mismatch',errors)
    require(all(obs(7).get(key)==403 for key in ('page','action','download')),'T007 private authority mismatch',errors)
    require(obs(8).get('unauthenticated')==401 and obs(8).get('wrong_password')==403 and obs(8).get('authorized')==200 and obs(8).get('cookie_httponly') is True and obs(8).get('stored_verifier_prefix')=='pbkdf2-sha256','T008 password authority mismatch',errors)
    require(all(obs(9).get(key)==410 for key in ('page','action','download')),'T009 expired lifecycle mismatch',errors)
    require(obs(10).get('concurrent_statuses')==[200,410] and obs(10).get('postconsume_page')==410 and obs(10).get('consumed_at')=='SET','T010 atomic consume mismatch',errors)
    require(obs(11).get('unknown')==404 and obs(11).get('malformed')==404,'T011 unknown/malformed status mismatch',errors)
    denied=obs(12).get('denied',{}); require(obs(12).get('action_status')==200 and obs(12).get('download_status')==200 and denied=={'password':403,'private':403,'expired':410,'consumed':410,'removed':410},'T012 copy/download authority mismatch',errors)
    checks=obs(13).get('checks',[]); require(bool(checks) and all(item.get('x_robots_tag')=='noindex, nofollow' for item in checks) and obs(13).get('unknown_status')==404,'T013 noindex matrix mismatch',errors)
    statuses=obs(14).get('statuses',{}); require(obs(14).get('canonical_present') is False and obs(14).get('hreflang_present') is False and obs(14).get('structured_data_present') is False and obs(14).get('sitemap_text_hits')==[] and statuses=={'available':200,'unknown':404,'expired':410,'consumed':410,'removed':410},'T014 sitemap/status/canonical observation mismatch',errors)
    require(obs(15).get('canonical_abuse_entry')=='/abuse/report' and obs(15).get('p16_completion_claimed') is False,'T015 abuse ownership observation mismatch',errors)
    d16=cases['P10-T016'].get('details',{}); require(all(d16.get(key) is True for key in ('empty','read_only','quota','error')) and '/app/text/' in str(d16.get('created_url','')),'T016 route-backed list/create state evidence mismatch',errors)
    d17=cases['P10-T017'].get('details',{}); require(all(d17.get(key) is True for key in ('read_only','conflict','expired','deleted','error')) and d17.get('public_preview_status')==200,'T017 detail lifecycle state evidence mismatch',errors)
    d18=cases['P10-T018'].get('details',{}); require(len(d18.get('captures',[]))>=11 and bool(d18.get('layouts')) and all(row.get('root_overflow_px')==0 and row.get('body_overflow_px')==0 and row.get('clipped')==[] for row in d18.get('layouts',[])) and bool(d18.get('non_color_state_text')) and d18.get('reduced_motion_usable') is True,'T018 responsive/accessibility evidence mismatch',errors)


def run() -> int:
    errors=[]; exact=head(); RESULTS.mkdir(parents=True, exist_ok=True)
    manifest=load_json(PRODUCERS,errors)
    require(manifest.get('implementation_commit')==exact,'producer manifest exact-head mismatch',errors)
    required=manifest.get('required_workflows',{}) if isinstance(manifest,dict) else {}
    require(set(required)==set(REQUIRED_PRODUCERS),f'producer set mismatch: {sorted(required)}',errors)
    for name in REQUIRED_PRODUCERS:
        entry=required.get(name,{})
        require(entry.get('head_sha')==exact,f'{name} head mismatch',errors)
        require(entry.get('status')=='completed' and entry.get('conclusion')=='success',f'{name} not successful',errors)
        require(isinstance(entry.get('run_id'),int) and entry.get('run_id',0)>0,f'{name} run id missing',errors)
        artifact=entry.get('artifact',{})
        require(isinstance(artifact.get('id'),int) and artifact.get('id',0)>0,f'{name} artifact id missing',errors)
        require(isinstance(artifact.get('digest'),str) and artifact.get('digest','').startswith('sha256:'),f'{name} artifact digest missing',errors)
    contract=load_json(CONTRACT,errors)
    require(contract.get('status')=='PASS' and contract.get('errors')==[],'contract artifact is not PASS',errors)
    require(contract.get('implementation_commit')==exact,'contract artifact exact-head mismatch',errors)
    require(contract.get('case_range')=='P10-T001..P10-T020' and contract.get('case_count')==20,'contract case range/count drift',errors)

    cases={}; entries=[]
    for number in range(1,19):
        cid=f'P10-T{number:03d}'; path=case_path(number); data=load_json(path,errors); cases[cid]=data
        require(data.get('status')=='PASS',f'{cid} status is not PASS',errors)
        require(data.get('errors')==[],f'{cid} errors not empty',errors)
        require(data.get('implementation_commit')==exact,f'{cid} exact-head mismatch',errors)
        if path.is_file(): entries.append({'case_id':cid,'path':str(path),'implementation_commit':data.get('implementation_commit'),'sha256':digest(path)})
    validate_semantics(cases,errors)

    captures=[]
    for path in sorted((ROOT/'captures').glob('P10-T018-*.png')):
        captures.append({'path':str(path),'sha256':digest(path),'size_bytes':path.stat().st_size})
    require(len(captures)>=11,f'expected >=11 T018 captures, got {len(captures)}',errors)

    index={'node':'P10','generated_at':datetime.now(timezone.utc).isoformat(timespec='seconds').replace('+00:00','Z'),'implementation_commit':exact,'input_evidence_count':len(entries),'producer_manifest_sha256':digest(PRODUCERS) if PRODUCERS.is_file() else None,'contract_sha256':digest(CONTRACT) if CONTRACT.is_file() else None,'cases':entries,'captures':captures,'producer_run_ids':{name:required.get(name,{}).get('run_id') for name in REQUIRED_PRODUCERS},'producer_artifacts':{name:required.get(name,{}).get('artifact') for name in REQUIRED_PRODUCERS}}
    INDEX.write_text(json.dumps(index,indent=2,sort_keys=True)+'\n',encoding='utf-8')
    payload={'case':'P10-T019','status':'PASS' if not errors else 'FAIL','implementation_commit':exact,'errors':errors,'observations':{'input_evidence_count':len(entries),'same_exact_head':all(item.get('implementation_commit')==exact for item in entries),'producer_run_ids':index['producer_run_ids'],'producer_artifacts':index['producer_artifacts'],'capture_count':len(captures),'evidence_index_sha256':digest(INDEX),'producer_manifest_sha256':index['producer_manifest_sha256'],'contract_sha256':index['contract_sha256'],'authority':'real MySQL/native Go API + route-backed owner/viewer Workspace/Public browser + raw UGC HTTP evidence; no mock/manual/fixture-only authority'}}
    T019.write_text(json.dumps(payload,indent=2,sort_keys=True)+'\n',encoding='utf-8')
    if errors:
        print('\n'.join(errors)); return 1
    print(f'P10-T019 exact-head coherence PASS on {exact}')
    return 0

if __name__=='__main__': raise SystemExit(run())
