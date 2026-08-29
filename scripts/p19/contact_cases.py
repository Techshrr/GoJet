#!/usr/bin/env python3
import json, subprocess
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
HEAD=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
auth=json.loads((ROOT/'frontend/apps/site/src/website/authority.json').read_text(encoding='utf-8'))
src=(ROOT/'frontend/apps/site/src/contact/ContactPage.tsx').read_text(encoding='utf-8')
errors=[]
for node in auth['contact']['nodes']:
    item=next((x for x in auth['signedIntegrations'] if x['node']==node),None)
    if not item: errors.append(f'missing contact authority node {node}'); continue
    if subprocess.run(['git','merge-base','--is-ancestor',item['integration'],HEAD],cwd=ROOT).returncode!=0: errors.append(f'contact authority {node} not ancestor of HEAD')
for state in auth['contact']['states']:
    if state not in src: errors.append(f'ContactPage missing frozen persistent state {state}')
for token in ["'/api/public/contact'",'turnstile_token','Idempotency-Key','rate_limited','turnstile_rejected','Raw verification material is never shown']:
    if token not in src: errors.append(f'ContactPage missing inherited security behavior: {token}')
if 'ticket_id' not in src: errors.append('contact durable success reference is not persisted in UI state')
data={'case_id':'P19-T016','name':'Contact conversion flow inheritance','status':'PASS' if not errors else 'FAIL','errors':errors,'implementation_commit':HEAD,'details':auth['contact']}
out=ROOT/'artifacts/v10/P19/contact/P19-T016.json'; out.parent.mkdir(parents=True,exist_ok=True); out.write_text(json.dumps(data,indent=2,ensure_ascii=False)+'\n',encoding='utf-8')
print('P19-T016:',data['status'])
for e in errors: print('  -',e)
raise SystemExit(1 if errors else 0)
