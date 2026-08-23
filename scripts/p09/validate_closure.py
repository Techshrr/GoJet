#!/usr/bin/env python3
"""P09-T027 exact-head pre-sign/final signed closure validator."""
from __future__ import annotations
import datetime as dt, hashlib, json, re, subprocess
from pathlib import Path
from typing import Any

ROOT=Path(__file__).resolve().parents[2]; P09=ROOT/'artifacts'/'v10'/'P09'
R=P09/'results'; C=P09/'clamav'; B=P09/'browser'; PLAN=P09/'test-plan.json'; REG=P09/'regression-manifest.json'; COH=P09/'evidence-index.json'; IDX=P09/'closure-evidence-index.json'; REVIEW=P09/'review.md'; CLOSURE=P09/'closure.json'; T027=R/'P09-T027.json'
P08A=P09/'inherited'/'p08-authority.json'; P08=P09/'inherited'/'P08'; P08C=P08/'closure.json'; P08T=P08/'results'/'P08-T016.json'; P08R=P08/'review.md'
WF=(
'P00 Bootstrap and G0 Traceability','P01 Engineering Foundation','P02 Brand Foundation','P03 Design System','P04 Product Shells',
'P05 Links Domain Contract','P05 Real Integration','P05 Workspace Browser','P05 Closure','P06 Custom Domains','P06 Real Integration','P06 Workspace Domains Browser','P06 Closure',
'P07 Analytics Contract','P07 Real Integration','P07 Workspace Analytics Browser','P07 Closure','P08 QR Contract','P08 Real QR Integration','P08 Workspace QR Browser','P08 Evidence Coherence',
'P09 Files Contract','P09 Real Files and ClamAV Integration','P09 Files Health and Installer Preflight','P09 Workspace Files Browser','P09 Evidence Coherence')
CASES=tuple(f'P09-T{i:03d}' for i in range(1,28)); INPUT=CASES[:-1]
PENDING='Status: **PENDING — CONTRACT FROZEN / IMPLEMENTATION NOT YET REVIEWABLE**'; SIGNED='Status: **APPROVED — TECHNICAL REVIEW SIGNED / SAME-REVISION CI REQUIRED**'
P08_SHA='dc055fbd52cde5d0aada0b912373cc182660f105'; P08_RUN=32579404081; P08_ART=9477523498; P08_DIG='sha256:aede44ebad79a78adc872f33a874f8aea1ae24bb430b0e06eaa2da32739c0a2d'

def now(): return dt.datetime.now(dt.timezone.utc).isoformat(timespec='seconds').replace('+00:00','Z')
def git(*a): return subprocess.check_output(['git',*a],cwd=ROOT,text=True).strip()
def load(p:Path)->Any: return json.loads(p.read_text(encoding='utf-8'))
def digest(p:Path)->str:
 h=hashlib.sha256();
 with p.open('rb') as f:
  for x in iter(lambda:f.read(131072),b''): h.update(x)
 return h.hexdigest()
def req(ok:bool,msg:str,e:list[str]):
 if not ok: e.append(msg)
def cp(cid:str)->Path:
 n=int(cid[-3:]); return (C if 5<=n<=10 else B if 21<=n<=25 else R)/f'{cid}.json'

def vplan(e):
 req(PLAN.is_file(),'missing test-plan.json',e)
 if not PLAN.is_file(): return {}
 try:p=load(PLAN)
 except Exception as x:e.append(f'invalid test-plan: {x}'); return {}
 ids=tuple(x.get('id') for x in p.get('cases',[]) if isinstance(x,dict)); req(ids==CASES,'test-plan case range/order drift',e)
 c=p.get('closure_contract',{}); req(isinstance(c,dict),'closure_contract missing',e)
 if isinstance(c,dict):
  req(c.get('same_exact_head_required') is True,'same-exact-head contract drift',e); req(c.get('required_case_range')=='P09-T001..P09-T027','closure case range drift',e); req(c.get('review_required') is True,'review requirement drift',e)
  req((c.get('p0_max'),c.get('p1_max'),c.get('decision_required_max'))==(0,0,0),'closure defect limits drift',e); s=str(c.get('gate_scope','')); req('P09-owned CAP-FILES/CAP-CLAMAV' in s and 'P17/P22' in s and 'release-wide closures remain later-owned' in s,'closure gate boundary drift',e)
 return p

def vreg(head,e):
 req(REG.is_file(),'missing regression-manifest.json',e)
 if not REG.is_file(): return {}
 try:m=load(REG)
 except Exception as x:e.append(f'invalid regression manifest: {x}'); return {}
 req(m.get('implementation_commit')==head,'regression manifest head mismatch',e); w=m.get('required_workflows',{}); req(isinstance(w,dict) and set(w)==set(WF),'regression workflow set mismatch',e)
 req(m.get('missing')==[] and m.get('pending')==[] and m.get('failed')==[],'regression matrix not fully green',e)
 if isinstance(w,dict):
  for n in WF:
   x=w.get(n,{}); req(isinstance(x,dict) and x.get('head_sha')==head and x.get('status')=='completed' and x.get('conclusion')=='success' and isinstance(x.get('run_id'),int) and x['run_id']>0,f'{n} exact-head success record invalid',e)
 return m

def vcases(head,e):
 out=[]
 for cid in INPUT:
  p=cp(cid); req(p.is_file(),f'missing evidence {cid}',e)
  if not p.is_file(): continue
  try:d=load(p)
  except Exception as x:e.append(f'invalid {cid}: {x}'); continue
  req(d.get('case_id',d.get('case'))==cid and d.get('status')=='PASS' and d.get('implementation_commit')==head and d.get('errors')==[],f'{cid} evidence invalid',e)
  if cid=='P09-T026':
   o=d.get('observations',{}); req(o.get('input_evidence_count')==25 and o.get('same_exact_head') is True and o.get('capture_count')==9,'T026 coherence observations drift',e); ids=o.get('producer_run_ids',{}); req(isinstance(ids,dict) and set(ids)=={'P09 Files Contract','P09 Real Files and ClamAV Integration','P09 Files Health and Installer Preflight','P09 Workspace Files Browser'},'T026 producer bindings drift',e)
  out.append({'case_id':cid,'path':str(p.relative_to(ROOT)),'sha256':digest(p),'status':d.get('status'),'implementation_commit':d.get('implementation_commit')})
 req(tuple(x['case_id'] for x in out)==INPUT,'closure evidence set/order mismatch',e); return out

def vp08(e):
 ps=(P08A,P08C,P08T,P08R)
 for p in ps:req(p.is_file(),f'missing inherited P08 authority: {p.name}',e)
 if not all(p.is_file() for p in ps): return {}
 try:a,c,t=load(P08A),load(P08C),load(P08T)
 except Exception as x:e.append(f'invalid inherited P08 JSON: {x}'); return {}
 req(a.get('source_commit')==P08_SHA and a.get('closure_run_id')==P08_RUN and a.get('artifact_id')==P08_ART and a.get('artifact_digest')==P08_DIG,'P08 authority identity drift',e)
 req(a.get('workflow_head_sha')==P08_SHA and a.get('workflow_conclusion')=='success' and a.get('artifact_expired') is False,'P08 authority live metadata invalid',e)
 req(a.get('archive_sha256')==P08_DIG.removeprefix('sha256:'),'P08 artifact archive digest mismatch',e)
 req(c.get('implementation_commit')==P08_SHA and c.get('status')=='PASS' and c.get('phase')=='signed' and c.get('merge_authoritative') is True and c.get('defects')=={'p0':0,'p1':0,'decision_required':0},'P08 signed closure invalid',e)
 req(t.get('implementation_commit')==P08_SHA and t.get('status')=='PASS' and t.get('details',{}).get('closure_phase')=='signed' and t.get('details',{}).get('merge_authoritative') is True,'P08 T016 signed authority invalid',e)
 rv=c.get('review',{}); req(rv.get('review_sha256')==digest(P08R) and c.get('t016',{}).get('sha256')==digest(P08T),'P08 inherited file digest binding invalid',e)
 return {'source_commit':P08_SHA,'closure_run_id':P08_RUN,'artifact_id':P08_ART,'artifact_digest':P08_DIG,'phase':c.get('phase'),'merge_authoritative':c.get('merge_authoritative'),'defects':c.get('defects'),'closure_sha256':digest(P08C),'review_sha256':digest(P08R),'t016_sha256':digest(P08T)}

def vreview(e):
 req(REVIEW.is_file(),'missing P09 review.md',e)
 if not REVIEW.is_file(): return {'phase':'missing','merge_authoritative':False,'defects':None}
 s=REVIEW.read_text(encoding='utf-8'); m=re.search(r'^Status:\s*.+$',s,re.M); line=m.group(0).strip() if m else ''; req(line in (PENDING,SIGNED),'review status invalid',e); req('## 9. Signed-revision rule' in s or '## Signed-revision rule' in s,'signed-revision rule missing',e); req('release-wide G10/G12/G13' in s,'release-wide boundary missing',e)
 if line==PENDING:return {'phase':'pre-sign','merge_authoritative':False,'status':'PENDING','review_sha256':digest(REVIEW),'defects':None}
 parent=git('rev-parse','HEAD^'); sm=re.search(r'Pre-sign exact implementation SHA:\s*`([0-9a-f]{40})`',s); pre=sm.group(1) if sm else None; req(pre==parent,f'pre-sign SHA {pre} != parent {parent}',e)
 im=re.search(r'Accountable reviewer identity:\s*\*\*(.+?)\*\*',s); dm=re.search(r'Review date:\s*\*\*(\d{4}-\d{2}-\d{2})\*\*',s); req(im is not None and dm is not None,'review identity/date missing',e); req('P09-T027: PASS — pre-sign closure / merge-authoritative=false' in s,'pre-sign T027 record missing',e)
 for x in ('Backend Lead','Frontend Lead','QA Lead','Accessibility Reviewer','Security Reviewer','Product/API Reviewer'):req(f'- {x}: APPROVED' in s,f'{x} approval missing',e)
 req('- P0 defects: 0' in s and '- P1 defects: 0' in s and '- `DECISION REQUIRED`: 0' in s,'review defect ledger nonzero/missing',e)
 for x in ('G3 P09','G6 P09','G10 P09','G12/G13 P09'):req(x in s and 'PASS' in s,f'{x} disposition missing',e)
 req('P17/P22' in s and 'signed revision itself must rerun' in s.lower(),'review ownership/rerun boundary missing',e)
 return {'phase':'signed','merge_authoritative':True,'status':'APPROVED','review_sha256':digest(REVIEW),'pre_sign_implementation_sha':pre,'accountable_reviewer_identity':im.group(1).strip() if im else None,'review_date':dm.group(1) if dm else None,'defects':{'p0':0,'p1':0,'decision_required':0}}

def write(head,plan,reg,entries,p08,review,e):
 R.mkdir(parents=True,exist_ok=True); status='PASS' if not e else 'FAIL'; phase=review.get('phase','unknown'); auth=bool(review.get('merge_authoritative')) and not e
 details={'closure_phase':phase,'merge_authoritative':auth,'test_plan_cases':len(plan.get('cases',[])) if isinstance(plan,dict) else 0,'input_evidence_count':len(entries),'required_input_evidence_count':26,'required_regression_workflows':list(WF),'regression_workflow_count':len(reg.get('required_workflows',{})) if isinstance(reg,dict) else 0,'same_exact_head_required':True,'inherited_p08_signed_closure':p08,'review':review,'gate_scope':'P09-owned CAP-FILES/CAP-CLAMAV G3/G6/G10/G12/G13 contribution only; P17/P22 and release-wide closures remain later-owned.'}
 t={'node':'P09','case_id':'P09-T027','name':'same-exact-head-p09-signed-closure-and-affected-regression-matrix','status':status,'generated_at':now(),'implementation_commit':head,'driver':'python3 scripts/p09/validate.py --case P09-T027 --closure','errors':list(e),'details':details}; T027.write_text(json.dumps(t,indent=2,sort_keys=True)+'\n')
 defects=review.get('defects') if phase=='signed' else {'p0':None,'p1':None,'decision_required':None}; gates={'G3':'PASS — P09 CAP-FILES functional/API subset only','G6':'PASS — P09 file-security/mandatory-ClamAV subset only','G10':'PASS — P09 file full-stack subset only','G12_G13':'PASS — P09 ClamAV preflight/runtime contribution only','later_owners':'OPEN — P17/P22 and release-wide closures remain later-owned'}
 c={'node':'P09','status':status,'phase':phase,'merge_authoritative':auth,'generated_at':now(),'implementation_commit':head,'case_range':'P09-T001..P09-T027','input_evidence_count':len(entries),'required_regression_workflow_count':len(WF),'defects':defects,'review':review,'inherited_p08_signed_closure':p08,'gate_scope':gates,'t027':{'path':str(T027.relative_to(ROOT)),'sha256':digest(T027),'status':status,'implementation_commit':head}}; CLOSURE.write_text(json.dumps(c,indent=2,sort_keys=True)+'\n')
 i={'node':'P09','generated_at':now(),'implementation_commit':head,'status':status,'test_plan_sha256':digest(PLAN) if PLAN.is_file() else None,'regression_manifest_sha256':digest(REG) if REG.is_file() else None,'coherence_evidence_index_sha256':digest(COH) if COH.is_file() else None,'review_sha256':digest(REVIEW) if REVIEW.is_file() else None,'input_evidence':entries,'coherence_result':next((x for x in entries if x['case_id']=='P09-T026'),None),'inherited_p08_signed_closure':p08,'closure_result':{'case_id':'P09-T027','path':str(T027.relative_to(ROOT)),'sha256':digest(T027),'status':status,'implementation_commit':head,'phase':phase,'merge_authoritative':auth},'closure_sha256':digest(CLOSURE)}; IDX.write_text(json.dumps(i,indent=2,sort_keys=True)+'\n')

def run_closure(flag:bool)->int:
 if not flag: print('P09-T027: --closure is required'); return 2
 head=git('rev-parse','HEAD'); e=[]; plan=vplan(e); reg=vreg(head,e); entries=vcases(head,e); p08=vp08(e); review=vreview(e); write(head,plan,reg,entries,p08,review,e)
 try:t,c,i=load(T027),load(CLOSURE),load(IDX); req(t.get('implementation_commit')==head and c.get('implementation_commit')==head and i.get('implementation_commit')==head,'written closure head mismatch',e); req(i.get('input_evidence')==entries and i.get('review_sha256')==digest(REVIEW) and i.get('closure_sha256')==digest(CLOSURE),'written closure digest/input binding mismatch',e); req(c.get('phase')==review.get('phase'),'written closure phase mismatch',e)
 except Exception as x:e.append(f'invalid written closure: {x}')
 if e:
  write(head,plan,reg,entries,p08,review,e)
  for x in e: print(f'P09-T027: {x}')
  return 1
 if review.get('phase')=='signed': print(f'P09-T027: PASS — 26/26 evidence, {len(WF)}/{len(WF)} exact-head workflows, inherited P08 signed closure and signed review green for {head}; merge-authoritative=true')
 else: print(f'P09-T027: PASS — pre-sign closure candidate with 26/26 evidence, {len(WF)}/{len(WF)} exact-head workflows and inherited P08 signed closure green for {head}; merge-authoritative=false')
 return 0
