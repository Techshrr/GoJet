#!/usr/bin/env python3
"""Validate the frozen GoJet V10 P09 Files/ClamAV contract."""
from __future__ import annotations
import json, re, subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
PLAN=ROOT/'artifacts/v10/P09/test-plan.json'
REVIEW=ROOT/'artifacts/v10/P09/review.md'
BASE='418277613cf4336273b19f5d0da8a47bc1d403d6'
PENDING='Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'
EXPECTED_CASES=tuple(f'P09-T{n:03d}' for n in range(1,28))
SPEC_BLOBS={
 'specifications/GoJet_V10_MASTER_PLAN_OPTIMIZED.md':'29cb2b4e14076ce71b21747dbf2facc411ccb41a',
 'specifications/GoJet_V10_PAGE_LEVEL_IA_OPTIMIZED.md':'20609139a0265d3f3a40a1c7c07894dc69220290',
 'specifications/GoJet_V10_BRAND_DESIGN_SYSTEM_OPTIMIZED.md':'68ac7c581207570ae849a75132e3e54f03cea651',
 'contracts/traceability/capability-matrix.snapshot.md':'bcc9fef9e666e7b10d5e43ae627ba094d27a8026',
 'contracts/traceability/route-registry.snapshot.md':'35da40a95c1b66ca34741ea0f7996045c4633e72',
}
EXPECTED_APIS=(
 'GET /api/workspaces/{id}/files','POST /api/workspaces/{id}/files',
 'GET /api/workspaces/{id}/files/{fileId}','PATCH /api/workspaces/{id}/files/{fileId}',
 'DELETE /api/workspaces/{id}/files/{fileId}','POST /api/workspaces/{id}/files/{fileId}/publish',
 'POST /api/workspaces/{id}/files/{fileId}/rescan','GET /api/workspaces/{id}/files/{fileId}/download')
EXPECTED_ROUTES=(
 'APP-FILES /app/files','APP-FILE-DETAIL /app/files/{fileId}','PUB-FILE-PAGE /f/{slug}',
 'PUB-FILE-BINARY GET /api/public/files/{slug}','ADMIN-FILES /admin/files[/{fileId}]',
 'ADMIN-STORAGE /admin/platform/storage','INSTALL-ENV /install/environment',
 'INSTALL-SERVICES /install/services','INSTALL-HEALTH /install/health')
MASTER_TESTS=('EICAR','clean','timeout','daemon down','signature stale','indeterminate response','rescan','duplicate claim','service restart','direct quarantine/public path access','Installer/upgrade hard fail')
TRACE={'EICAR':'P09-T006','clean':'P09-T005','timeout':'P09-T008','daemon down':'P09-T007','signature stale':'P09-T009','indeterminate response':'P09-T010','rescan':'P09-T011','duplicate claim':'P09-T012','service restart':'P09-T013','direct quarantine/public path access':'P09-T014','Installer/upgrade hard fail':'P09-T019'}
ALIASES={
 'quarantined':('file-quarantined','PackageLock','File quarantined'),
 'scanning':('file-scanning','LoaderCircle','Security scan in progress'),
 'safe':('file-safe','ShieldCheck','File is safe to publish'),
 'blocked':('file-blocked','ShieldX','File blocked'),
 'scan_error':('file-scan-error','TriangleAlert','Scan unavailable; file remains private')}

def git(*args): return subprocess.check_output(['git',*args],cwd=ROOT,text=True).strip()
def req(ok,msg,errors):
 if not ok: errors.append(msg)
def blob(path): return git('hash-object',path)

def main():
 errors=[]
 req(PLAN.is_file(),'missing P09 test-plan.json',errors); req(REVIEW.is_file(),'missing P09 review.md',errors)
 for path,sha in SPEC_BLOBS.items():
  p=ROOT/path; req(p.is_file(),f'missing frozen authority file: {path}',errors)
  if p.is_file(): req(blob(path)==sha,f'frozen authority blob drift: {path}={blob(path)} expected={sha}',errors)
 try: plan=json.loads(PLAN.read_text(encoding='utf-8')) if PLAN.is_file() else {}
 except Exception as exc: errors.append(f'invalid test-plan JSON: {exc}'); plan={}
 review=REVIEW.read_text(encoding='utf-8') if REVIEW.is_file() else ''
 req(plan.get('node')=='P09','plan node drift',errors)
 req(plan.get('title')=='Files and Mandatory ClamAV','plan title drift',errors)
 req(plan.get('base_integration_commit')==BASE,'base integration SHA drift',errors)
 req('P09 contract-freeze revision' in str(plan.get('case_ids_frozen_by','')),'case-ID authority disclosure missing',errors)
 req(plan.get('specification_ids')==['GJ-V10-MP-GREENFIELD-2026-08-20','GJ-V10-DS-GREENFIELD-2026-08-20','GJ-V10-IA-GREENFIELD-2026-08-20'],'spec IDs drift',errors)
 cap=plan.get('capability_contract',{})
 req(cap.get('capabilities')==[
  {'id':'CAP-FILES','status':'REQUIRED','owners':['P09','P17'],'gates':['G3','G6','G10']},
  {'id':'CAP-CLAMAV-REQUIRED','status':'REQUIRED','owners':['P09','P22'],'gates':['G6','G12','G13']}], 'capability owner/gate drift',errors)
 req(cap.get('p09_dependencies')==['P01','P04'],'P09 predecessor drift',errors)
 req(tuple(cap.get('master_required_tests',[]))==MASTER_TESTS,'Master required-test list drift',errors)
 req(plan.get('master_test_trace')==TRACE,'Master test trace drift',errors)
 route=plan.get('route_contract',{})
 req(tuple(route.get('ia_authoritative_routes',[]))==EXPECTED_ROUTES,'IA route list drift',errors)
 req('does not freeze exact Workspace HTTP method/path families' in str(route.get('workspace_api_authority_note','')),'IA-vs-P09 Workspace API authority distinction missing',errors)
 req(tuple(route.get('p09_workspace_api_family',[]))==EXPECTED_APIS,'P09 implementation API family drift',errors)
 pub=route.get('public_contract',{})
 req(pub.get('binary')=='GET /api/public/files/{slug}','public binary authority drift',errors)
 req((pub.get('unknown_status'),pub.get('blocked_or_not_safe_binary_status'),pub.get('expired_or_removed_status'))==(404,403,410),'public lifecycle HTTP model drift',errors)
 req('HttpOnly' in str(pub.get('page','')) and 'password in a URL' in str(pub.get('page','')),'password page-action privacy contract missing',errors)
 req('PROHIBITED' in str(route.get('legacy_aliases','')),'legacy alias prohibition missing',errors)
 state=plan.get('file_state_contract',{})
 req(state.get('pipeline')==['upload','allowlist/MIME/magic/quota','randomized private name','quarantine','ClamAV','policy','publish'],'security pipeline drift',errors)
 req(state.get('scan_states')==['quarantined','scanning','safe','blocked','scan_error'],'scan state set drift',errors)
 for key,(alias,icon,headline) in ALIASES.items():
  item=state.get('design_aliases',{}).get(key,{})
  req((item.get('alias'),item.get('icon'),item.get('headline'))==(alias,icon,headline),f'DS alias record drift: {key}',errors)
 req('Safe does not auto-publish' in str(state.get('safety_authority','')),'safe-vs-publish boundary missing',errors)
 req('invalidates prior scan authority' in str(state.get('rescan_authority','')),'rescan fail-closed boundary missing',errors)
 storage=plan.get('storage_contract',{})
 req(storage.get('release_target')=='native local filesystem','local filesystem target drift',errors)
 req(str(storage.get('s3','')).startswith('DEFERRED'),'S3 must remain DEFERRED',errors)
 req('outside direct public access' in str(storage.get('direct_access','')),'direct storage access prohibition missing',errors)
 req('server-randomized' in str(storage.get('naming','')),'randomized storage identity missing',errors)
 clam=plan.get('clamav_contract',{})
 req(clam.get('real_dependency_required') is True,'real ClamAV requirement missing',errors)
 req(clam.get('worker_target')=='services/platformapi/cmd/fileworker','native fileworker target drift',errors)
 req(clam.get('client_scan_substitute')=='PROHIBITED','client scan substitute prohibition missing',errors)
 req(set(clam.get('runtime_fail_closed',[]))=={'daemon down','socket failure','timeout','stale signatures','indeterminate/malformed response'},'ClamAV fail-closed set drift',errors)
 env=plan.get('environment_contract',{})
 req(env.get('production_docker_compose_node')=='PROHIBITED','production Docker/Node boundary drift',errors)
 req('Real ClamAV' in str(env.get('clamav','')),'real ClamAV evidence boundary missing',errors)
 req('P22' in str(env.get('installer','')) and 'P17' in str(env.get('admin','')),'later-owner handoff boundary missing',errors)
 cases=plan.get('cases',[])
 ids=tuple(x.get('id') for x in cases if isinstance(x,dict)); req(ids==EXPECTED_CASES,f'case IDs/order drift: {ids}',errors)
 for case in cases:
  if not isinstance(case,dict): errors.append('case entry not object'); continue
  cid=case.get('id'); req(case.get('owner')=='P09',f'{cid} owner drift',errors); req(case.get('expected_exit')==0,f'{cid} expected_exit drift',errors)
  for field in ('name','precondition','driver','oracle','evidence'): req(bool(case.get(field)),f'{cid} missing {field}',errors)
 byid={x.get('id'):x for x in cases if isinstance(x,dict)}
 for n in range(5,11): req('/clamav/' in str(byid.get(f'P09-T{n:03d}',{}).get('evidence','')),f'T{n:03d} must write ClamAV evidence',errors)
 for n in range(21,26): req(str(byid.get(f'P09-T{n:03d}',{}).get('driver','')).startswith('node scripts/p09/browser.mjs'),f'T{n:03d} browser driver drift',errors)
 req(byid.get('P09-T026',{}).get('driver')=='python3 scripts/p09/validate.py --case P09-T026','T026 coherence driver drift',errors)
 req(byid.get('P09-T027',{}).get('driver')=='python3 scripts/p09/validate.py --case P09-T027 --closure','T027 closure driver drift',errors)
 closure=plan.get('closure_contract',{})
 req(closure.get('same_exact_head_required') is True and closure.get('required_case_range')=='P09-T001..P09-T027','closure exact-head/case-range drift',errors)
 req(closure.get('review_required') is True and closure.get('p0_max')==0 and closure.get('p1_max')==0 and closure.get('decision_required_max')==0,'closure review/defect thresholds drift',errors)
 req('P17/P22' in str(closure.get('gate_scope','')),'closure later-owner boundary missing',errors)
 status=[x.strip() for x in review.splitlines() if x.strip().startswith('Status:')]
 req(status==[PENDING],f'review current Status drift: {status}',errors)
 for marker in ('Required P09 case range: **P09-T001..P09-T027**.','does **not** freeze exact Workspace HTTP methods/paths','`CAP-S3-STORAGE` is DEFERRED','No P09 PASS or Exit claim is made in this state.','SAME-REVISION CI REQUIRED'):
  req(marker in review,f'review marker missing: {marker}',errors)
 head=git('rev-parse','HEAD'); req(bool(re.fullmatch(r'[0-9a-f]{40}',head)),f'invalid exact HEAD {head}',errors)
 try: mb=git('merge-base',head,BASE); req(mb==BASE,f'branch base drift: merge-base={mb}',errors)
 except Exception as exc: errors.append(f'cannot verify branch ancestry: {exc}')
 result={'node':'P09','status':'PASS' if not errors else 'FAIL','implementation_commit':head,'base_integration_commit':BASE,'case_range':'P09-T001..P09-T027','case_count':len(cases),'review_status':'PENDING','workspace_api_authority':'P09 implementation contract; IA supplies semantic fileshare/file-delete wording, not exact Workspace HTTP paths','errors':errors}
 print(json.dumps(result,indent=2,sort_keys=True)); return 0 if not errors else 1

if __name__=='__main__': raise SystemExit(main())
