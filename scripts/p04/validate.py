#!/usr/bin/env python3
"""P04 Product Shells validator and evidence emitter."""
from __future__ import annotations
import argparse, hashlib, json, os, platform, re, subprocess, sys
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

ROOT=Path(__file__).resolve().parents[2]
P04=ROOT/'artifacts/v10/P04'; RESULTS=P04/'results'; BROWSER=P04/'browser'; CAPTURES=P04/'captures'
G4=ROOT/'artifacts/v10/gates/G4/browser-responsive'; G9=ROOT/'artifacts/v10/gates/G9/performance'
EXPECTED={
 'website':['normal','menu-open','locale-switch','announcement','maintenance-banner'],
 'auth':['normal','submitting','server-error','rate-limited','provider-error','maintenance'],
 'docs':['article','search-open','nav-drawer','not-found','offline-static'],
 'workspace':['loading-workspace','workspace-empty','read-only-role','workspace-suspended','api-offline','notification-attention'],
 'admin':['admin-auth-required','permission-denied','maintenance','partial-service-degradation','normal'],
 'installer':['session-ready','step-checking','step-pass','hard-failure','retryable-failure','install-running','lock-failed','complete','already-locked'],
}
CASES=[
 ('P04-T001','Six shell contracts and exact IA state sets'),('P04-T002','SPA route transitions and real-link navigation'),
 ('P04-T003','Overlay exclusivity, Escape close and focus return'),('P04-T004','Canonical viewport overflow and clipping'),
 ('P04-T005','IA state applicability without blanket inheritance'),('P04-T006','Website bundle isolation and shell inventory'),
 ('P04-T007','Browser console/pageerror/layout-shift protection'),('P04-T008','Canonical responsive capture completeness'),
 ('P04-T009','Native PHP Installer shell contract'),('P04-T010','G4/G9 P04 subset evidence consistency')]

def now()->str:return datetime.now(timezone.utc).isoformat().replace('+00:00','Z')
def rel(path:Path)->str:return str(path.relative_to(ROOT)).replace('\\','/')
def write_json(path:Path,data:Any)->None:path.parent.mkdir(parents=True,exist_ok=True);path.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
def sha256(path:Path)->str:
 h=hashlib.sha256();h.update(path.read_bytes());return h.hexdigest()
def run(args:list[str],check:bool=True)->subprocess.CompletedProcess[str]:return subprocess.run(args,cwd=ROOT,text=True,capture_output=True,check=check)
def browser_report()->dict[str,Any]:return json.loads((BROWSER/'browser-report.json').read_text(encoding='utf-8'))
def record(case_id:str,ok:bool,errors:list[str],details:dict[str,Any])->tuple[bool,list[str],dict[str,Any]]:
 write_json(RESULTS/f'{case_id}.json',{'case_id':case_id,'name':dict(CASES)[case_id],'status':'PASS' if ok else 'FAIL','errors':errors,'details':details,'recorded_at':now()});return ok,errors,details

def parse_state_registry()->dict[str,list[str]]:
 body=(ROOT/'frontend/packages/utils/src/shell-states.ts').read_text(encoding='utf-8');out={}
 for surface in EXPECTED:
  m=re.search(rf"{re.escape(surface)}:\s*\[(.*?)\]",body,re.S)
  out[surface]=re.findall(r"'([^']+)'",m.group(1)) if m else []
 return out

def t001():
 errors=[];registry=parse_state_registry();files={
  'website':ROOT/'frontend/apps/site/src/shell/SiteShells.tsx','auth':ROOT/'frontend/apps/site/src/shell/SiteShells.tsx',
  'docs':ROOT/'frontend/apps/docs/astro.config.mjs','workspace':ROOT/'frontend/apps/workspace/src/shell/WorkspaceShell.tsx',
  'admin':ROOT/'frontend/apps/admin/src/shell/AdminShell.tsx','installer':ROOT/'installer/src/Shell.php'}
 for surface,path in files.items():
  if not path.exists():errors.append(f'{surface} shell implementation missing: {rel(path)}')
  if registry.get(surface)!=EXPECTED[surface]:errors.append(f'{surface} shell state registry differs from IA contract')
 return record('P04-T001',not errors,errors,{'registry':registry,'files':{k:rel(v) for k,v in files.items()}})

def t002():
 errors=[];report=browser_report();transitions=report.get('route_transitions',[])
 if len(transitions)!=3:errors.append(f'expected 3 SPA transition cases, got {len(transitions)}')
 for row in transitions:
  if not row.get('preserved'):errors.append(f"document reload detected for {row.get('expectedPath')}")
 website=next((x for x in report.get('targets',[]) if x.get('surface')=='website' and x.get('viewport')=='desktop'),{})
 for href in ['/products','/solutions','/developers','/pricing','/login','/register','/docs/en/']:
  if href not in website.get('anchors',[]):errors.append(f'Website primary/task link missing real href: {href}')
 return record('P04-T002',not errors,errors,{'transitions':transitions,'website_anchor_count':len(website.get('anchors',[]))})

def t003():
 errors=[];overlay=browser_report().get('overlay') or {}
 if overlay.get('dialogCount')!=1:errors.append('Command must open exactly one modal dialog')
 if overlay.get('dialogAfterEscape')!=0:errors.append('Escape must close the active overlay')
 if overlay.get('focusReturned') is not True:errors.append('focus did not return to Command trigger after close')
 return record('P04-T003',not errors,errors,overlay)

def t004():
 errors=[];bad=[]
 for row in browser_report().get('targets',[]):
  if row.get('rootOverflow') or row.get('bodyOverflow') or row.get('clippedText'):
   bad.append({'surface':row.get('surface'),'viewport':row.get('viewport'),'rootOverflow':row.get('rootOverflow'),'bodyOverflow':row.get('bodyOverflow'),'clippedText':row.get('clippedText')})
 if bad:errors.append(f'overflow/clipped required shell content found in {len(bad)} canonical captures')
 return record('P04-T004',not errors,errors,{'finding_count':len(bad),'findings':bad})

def t005():
 errors=[];registry=parse_state_registry()
 for surface,expected in EXPECTED.items():
  if registry.get(surface)!=expected:errors.append(f'{surface} state set is not exact')
 prohibited={'website':['loading','empty','permission-denied'],'docs':['quota','billing'],'installer':['quota','stale-success','offline-success']}
 for surface,needles in prohibited.items():
  for needle in needles:
   if needle in registry.get(surface,[]):errors.append(f'{surface} illegally inherits state {needle}')
 return record('P04-T005',not errors,errors,{'registry':registry,'prohibited':prohibited})

def t006():
 errors=[];site_pkg=json.loads((ROOT/'frontend/apps/site/package.json').read_text(encoding='utf-8'));deps=site_pkg.get('dependencies',{})
 for forbidden in ['@gojet/workspace','@gojet/admin']:
  if forbidden in deps:errors.append(f'Website depends on product bundle {forbidden}')
 source='\n'.join(p.read_text(encoding='utf-8') for p in (ROOT/'frontend/apps/site/src').rglob('*') if p.is_file() and p.suffix in {'.ts','.tsx','.js','.jsx'})
 for needle in ['@gojet/workspace','@gojet/admin','frontend/apps/workspace','frontend/apps/admin']:
  if needle in source:errors.append(f'Website source leaks product bundle reference: {needle}')
 dist=ROOT/'frontend/apps/site/dist/assets';bundles=[]
 if dist.exists():bundles=[{'name':p.name,'bytes':p.stat().st_size} for p in sorted(dist.glob('*.js'))]
 if not bundles:errors.append('Website built JavaScript inventory missing')
 summary={'status':'PASS' if not errors else 'FAIL','scope':'P04 shell bundle isolation subset','site_bundles':bundles,'site_total_js_bytes':sum(x['bytes'] for x in bundles),'forbidden_product_dependencies':['@gojet/workspace','@gojet/admin'],'errors':errors}
 write_json(G9/'p04-shell-bundle.json',summary)
 return record('P04-T006',not errors,errors,summary)

def t007():
 errors=[];report=browser_report();console=report.get('console_errors',[]);page_errors=report.get('page_errors',[])
 if console:errors.append(f'console errors: {len(console)}')
 if page_errors:errors.append(f'page errors: {len(page_errors)}')
 shifts=[{'surface':x.get('surface'),'viewport':x.get('viewport'),'layoutShift':x.get('layoutShift',0)} for x in report.get('targets',[]) if float(x.get('layoutShift',0))>0.02]
 route_shifts=[x for x in report.get('route_transitions',[]) if float(x.get('layoutShift',0))>0.02]
 if shifts or route_shifts:errors.append(f'shell layout-shift threshold exceeded in {len(shifts)+len(route_shifts)} cases')
 return record('P04-T007',not errors,errors,{'console_errors':console,'page_errors':page_errors,'viewport_shift_findings':shifts,'route_shift_findings':route_shifts,'threshold':0.02})

def t008():
 errors=[];captures=browser_report().get('captures',[])
 expected_min=len(EXPECTED)*3+1
 if len(captures)<expected_min:errors.append(f'capture matrix incomplete: {len(captures)} < {expected_min}')
 missing=[]
 for row in captures:
  path=ROOT/row['path']
  if not path.exists() or path.stat().st_size<1000:missing.append(row['path'])
 if missing:errors.append(f'missing/empty capture files: {len(missing)}')
 coverage={(row.get('surface'),row.get('viewport')) for row in captures}
 for surface in EXPECTED:
  for viewport in ['desktop','tablet','mobile']:
   if (surface,viewport) not in coverage:errors.append(f'capture missing: {surface}/{viewport}')
 return record('P04-T008',not errors,errors,{'capture_count':len(captures),'missing':missing,'required_base_count':len(EXPECTED)*3})

def t009():
 errors=[];php=(ROOT/'installer/src/Shell.php').read_text(encoding='utf-8');entry=(ROOT/'installer/public/index.php').read_text(encoding='utf-8');css=(ROOT/'installer/public/assets/shell.css').read_text(encoding='utf-8')
 for state in EXPECTED['installer']:
  if state not in php:errors.append(f'Installer state missing from PHP shell: {state}')
 for needle in ['••••••••','X-Robots-Tag: noindex, nofollow','Cache-Control: no-store']:
  if needle not in php+entry:errors.append(f'Installer safety/shell marker missing: {needle}')
 if re.search(r'(?i)docker|compose|node\.js|npm\s',php+entry):errors.append('Installer shell introduces prohibited production Docker/Node choice')
 if re.search(r'#[0-9A-Fa-f]{3,8}\b|\b\d+(?:\.\d+)?(?:px|ms)\b',css):errors.append('Installer shell CSS contains raw visual values instead of canonical tokens')
 lint=[run(['php','-l','installer/src/Shell.php'],False),run(['php','-l','installer/public/index.php'],False)]
 if any(x.returncode for x in lint):errors.append('PHP syntax check failed')
 return record('P04-T009',not errors,errors,{'states':EXPECTED['installer'],'php_lint':[x.stdout.strip() for x in lint]})

def t010():
 errors=[];report=browser_report();g4={'status':'PASS','scope':'P04 browser-responsive subset','full_gate':'OPEN','implementation_commit':run(['git','rev-parse','HEAD']).stdout.strip(),'capture_count':len(report.get('captures',[])),'spa_transition_count':len(report.get('route_transitions',[])),'console_error_count':len(report.get('console_errors',[])),'page_error_count':len(report.get('page_errors',[]))}
 if g4['capture_count']<19 or g4['spa_transition_count']!=3 or g4['console_error_count'] or g4['page_error_count']:g4['status']='FAIL';errors.append('G4 P04 subset summary is not PASS')
 write_json(G4/'p04-subset.json',g4)
 g9_path=G9/'p04-shell-bundle.json'
 if not g9_path.exists():errors.append('G9 P04 shell bundle evidence missing')
 else:
  g9=json.loads(g9_path.read_text(encoding='utf-8'))
  if g9.get('status')!='PASS':errors.append('G9 P04 shell bundle subset is not PASS')
 return record('P04-T010',not errors,errors,{'g4':g4,'g9_path':rel(g9_path)})

FUNCS:dict[str,Callable[[],tuple[bool,list[str],dict[str,Any]]]]={'P04-T001':t001,'P04-T002':t002,'P04-T003':t003,'P04-T004':t004,'P04-T005':t005,'P04-T006':t006,'P04-T007':t007,'P04-T008':t008,'P04-T009':t009,'P04-T010':t010}
def emit_index()->tuple[int,int]:
 commit=run(['git','rev-parse','HEAD']).stdout.strip();rows=[json.loads((RESULTS/f'{case}.json').read_text(encoding='utf-8')) for case,_ in CASES];passed=sum(x['status']=='PASS' for x in rows)
 write_json(P04/'source.json',{'repository':'Techshrr/GoJet','branch':os.environ.get('GITHUB_HEAD_REF') or os.environ.get('GITHUB_REF_NAME') or 'detached','implementation_commit':commit,'specification_ids':['GJ-V10-MP-GREENFIELD-2026-08-20','GJ-V10-DS-GREENFIELD-2026-08-20','GJ-V10-IA-GREENFIELD-2026-08-20']})
 write_json(P04/'environment.json',{'generated_at':now(),'platform':platform.platform(),'python':platform.python_version()})
 candidates=[P04/'source.json',P04/'environment.json',P04/'test-plan.json',P04/'review.md',BROWSER/'browser-report.json',G4/'p04-subset.json',G9/'p04-shell-bundle.json']+[RESULTS/f'{case}.json' for case,_ in CASES]+list(CAPTURES.glob('*.png'))
 files=[{'path':rel(p),'sha256':sha256(p)} for p in sorted(set(candidates)) if p.exists()]
 write_json(P04/'evidence-index.json',{'schema_version':1,'node':'P04','implementation_commit':commit,'generated_at':now(),'results':{'passed':passed,'failed':len(rows)-passed,'total':len(rows)},'files':files})
 return passed,len(rows)
def main()->int:
 parser=argparse.ArgumentParser();parser.add_argument('--case',choices=list(FUNCS));args=parser.parse_args();RESULTS.mkdir(parents=True,exist_ok=True);selected=[args.case] if args.case else [x[0] for x in CASES];failed=0
 for case in selected:
  assert case is not None
  try:ok,errors,_=FUNCS[case]()
  except Exception as exc:ok=False;errors=[f'validator exception: {type(exc).__name__}: {exc}'];record(case,False,errors,{})
  failed+=0 if ok else 1;print(f"{case}: {'PASS' if ok else 'FAIL'}")
  for error in errors:print(f'  - {error}')
 if not args.case:
  passed,total=emit_index();print(f'P04 summary: {passed}/{total} PASS')
 return 1 if failed else 0
if __name__=='__main__':sys.exit(main())
