#!/usr/bin/env python3
import json, re, subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
CONTENT=ROOT/'frontend/apps/site/src/website/content.json'
AUTH=ROOT/'frontend/apps/site/src/website/authority.json'
PAGE=ROOT/'frontend/apps/site/src/website/WebsitePage.tsx'
NGINX=ROOT/'deploy/nginx/site-p19.conf'
DIST=ROOT/'frontend/apps/site/dist'
HEAD=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
pages=json.loads(CONTENT.read_text(encoding='utf-8'))
auth=json.loads(AUTH.read_text(encoding='utf-8'))
page_source=PAGE.read_text(encoding='utf-8')
nginx=NGINX.read_text(encoding='utf-8')
by_id={p['routeId']:p for p in pages}

def emit(case_id,name,errors,details):
    data={'case_id':case_id,'name':name,'status':'PASS' if not errors else 'FAIL','errors':errors,'implementation_commit':HEAD,'details':details}
    out=ROOT/'artifacts/v10/P19/content'/f'{case_id}.json'; out.parent.mkdir(parents=True,exist_ok=True); out.write_text(json.dumps(data,indent=2,ensure_ascii=False)+'\n',encoding='utf-8')
    print(f"{case_id}: {data['status']}")
    for e in errors: print('  -',e)
    return bool(errors)

def ancestor(sha): return subprocess.run(['git','merge-base','--is-ancestor',sha,HEAD],cwd=ROOT).returncode==0
def text_for(route_ids): return '\n'.join(json.dumps(by_id[r],ensure_ascii=False) for r in route_ids)
failed=False

errors=[]
for item in auth['signedIntegrations']:
    if not ancestor(item['integration']): errors.append(f"{item['node']} integration is not an ancestor of HEAD: {item['integration']}")
all_text=CONTENT.read_text(encoding='utf-8')
for pattern in auth['claimPolicy']['prohibitedPositivePatterns']:
    if pattern.lower() in all_text.lower(): errors.append(f'prohibited positive claim present: {pattern}')
for cap in auth['claimPolicy']['deferred']:
    if cap in all_text and cap!='CAP-BIO-OPT-IN-INDEX': errors.append(f'deferred capability promoted in Website copy: {cap}')
if re.search(r'\b(?:[1-9]\d{2,})\s+(?:customers|companies|teams)\b',all_text,re.I): errors.append('fabricated customer/company count pattern found')
if re.search(r'\b[1-5](?:\.\d)?\s*(?:/\s*5|stars?|rating)\b',all_text,re.I): errors.append('fabricated rating pattern found')
failed |= emit('P19-T007','Released-capability claim gating',errors,{'signed_integrations':len(auth['signedIntegrations']),'deferred':auth['claimPolicy']['deferred']})

errors=[]; home=by_id['WEB-HOME']; expected=['default','announcement-partial','pricing-partial','maintenance']
if auth['routeGroups']['home']['states']!=expected: errors.append('home state contract mismatch')
for token in ['announcement-partial','pricing-partial','maintenance','Authoritative plan data','Announcements are temporarily unavailable']:
    if token not in page_source: errors.append(f'WebsitePage missing home partial-state implementation: {token}')
for lang in ['en','zh']:
    if len(home[lang]['points'])<2 or not home[lang]['lede'].strip(): errors.append(f'home {lang} lacks substantive task content')
failed |= emit('P19-T008','Home content and partial-data states',errors,{'states':expected,'route':'/'})

errors=[]; product_ids=auth['routeGroups']['products']['routeIds']
if any(r not in by_id for r in product_ids): errors.append('product route missing from Website content registry')
required_terms={'WEB-FILES':['ClamAV','fail'], 'WEB-DOMAINS':['DNS','HTTPS','risk'], 'WEB-ROUTING':['risk'], 'WEB-BIO':['noindex'], 'WEB-ANALYTICS':['Redis','MySQL']}
for rid,terms in required_terms.items():
    blob=json.dumps(by_id[rid],ensure_ascii=False).lower()
    for term in terms:
        if term.lower() not in blob: errors.append(f'{rid} missing signed-boundary term {term}')
failed |= emit('P19-T009','Product-family factual parity',errors,{'route_ids':product_ids,'authority_nodes':auth['routeGroups']['products']['nodes']})

errors=[]; solution_ids=auth['routeGroups']['solutions']['routeIds']; blob=text_for(solution_ids)
for phrase in ['guaranteed conversion','guaranteed growth','trusted by','customers love','award-winning']:
    if phrase in blob.lower(): errors.append(f'fabricated solution outcome/endorsement: {phrase}')
for rid in solution_ids:
    if len(by_id[rid]['en']['points'])<2 or len(by_id[rid]['zh']['points'])<2: errors.append(f'{rid} lacks workflow substance')
failed |= emit('P19-T010','Solution workflow factual parity',errors,{'route_ids':solution_ids,'authority_nodes':auth['routeGroups']['solutions']['nodes']})

errors=[]; dev=text_for(auth['routeGroups']['developers']['routeIds'])
for token in ['API key','webhook']:
    if token.lower() not in dev.lower(): errors.append(f'developer copy missing released capability: {token}')
for banned in ['/docs/en/api/payments','/docs/en/api/admin','/docs/zh-CN/api/payments','GraphQL']:
    if banned.lower() in dev.lower(): errors.append(f'unreleased/unsupported developer acquisition target: {banned}')
for token in ['/docs/en/','/docs/zh-CN/']:
    if token not in page_source: errors.append(f'Website developer/doc acquisition missing canonical P18 root: {token}')
failed |= emit('P19-T011','Developers and Docs acquisition boundary',errors,{'nodes':auth['routeGroups']['developers']['nodes'],'docs':auth['routeGroups']['developers']['docs']})

errors=[]; pricing=json.dumps(by_id['WEB-PRICING'],ensure_ascii=False)
if re.search(r'(?:\$|€|£|¥|￥)\s*\d|\b\d+(?:\.\d+)?\s*(?:USD|CNY|EUR|GBP)\b',pricing,re.I): errors.append('pricing contains an invented numeric commercial value')
for token in ['data-unavailable','maintenance','Prices and availability are intentionally not estimated','Authoritative plan data is unavailable']:
    if token not in page_source: errors.append(f'pricing state implementation missing: {token}')
failed |= emit('P19-T012','Pricing truth and failure states',errors,{'states':auth['routeGroups']['pricing']['states'],'authority_nodes':auth['routeGroups']['pricing']['nodes']})

errors=[]; security=json.dumps(by_id['WEB-SECURITY'],ensure_ascii=False)
for pattern in [r'100% secure',r'guarantee(?:d|s)? security',r'SOC\s*2\s*(?:certified|compliant)',r'ISO\s*27001\s*(?:certified|compliant)']:
    if re.search(pattern,security,re.I): errors.append(f'unverified security/compliance claim matched: {pattern}')
for term in ['Turnstile','ClamAV','risk','fail closed']:
    if term.lower() not in security.lower(): errors.append(f'security page missing signed control boundary: {term}')
failed |= emit('P19-T013','Security claims evidence boundary',errors,{'authority_nodes':auth['routeGroups']['security']['nodes']})

errors=[]; guide=by_id['WEB-GUIDE']
if guide['path']!='/guides/secure-link-sharing' or guide['zhPath']!='/zh-CN/guides/secure-link-sharing': errors.append('published guide path/translation mismatch')
for path in [guide['path'],guide['zhPath']]:
    out=DIST/(path.lstrip('/')+'.html')
    if not out.exists(): errors.append(f'published guide build missing: {path}')
if 'legacy-deployment' not in nginx or 'return 410' not in nginx: errors.append('withdrawn guide 410 boundary missing from Nginx authority')
if auth['guides']['unknownPolicy']!='404' or auth['guides']['withdrawnPolicy']!='410': errors.append('guide lifecycle policy mismatch')
failed |= emit('P19-T014','Guide publication lifecycle',errors,auth['guides'])

errors=[]
for rid in ['WEB-ABOUT',*auth['legal']['currentRouteIds']]:
    if rid not in by_id: errors.append(f'missing current versioned content record: {rid}'); continue
    rec=by_id[rid]
    if rec['updatedTime']!=auth['legal']['version']: errors.append(f'{rid} version date does not bind reviewed legal/content version')
if auth['legal']['currentPolicy']!='200' or auth['legal']['supersededPolicy']!='308-to-reviewed-current-record' or auth['legal']['withdrawnPolicy']!='410': errors.append('legal lifecycle policy incomplete')
if auth['legal']['noSyntheticAliasUntilRecordExists'] is not True: errors.append('legal alias safety rule missing')
failed |= emit('P19-T015','About and legal versioned content',errors,auth['legal'])

raise SystemExit(1 if failed else 0)
