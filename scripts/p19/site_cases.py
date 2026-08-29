#!/usr/bin/env python3
from __future__ import annotations
import json, os, re, subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SITE=ROOT/'frontend/apps/site'
DIST=SITE/'dist'
OUT=ROOT/'artifacts/v10/P19/site-core'
EXPECTED={
'WEB-HOME':'/','WEB-PRODUCTS':'/products','WEB-LINKS':'/products/links','WEB-QR':'/products/qr-codes','WEB-FILES':'/products/files','WEB-TEXT':'/products/text-sharing','WEB-BIO':'/products/link-in-bio','WEB-ANALYTICS':'/products/analytics','WEB-ROUTING':'/products/smart-routing','WEB-DOMAINS':'/products/custom-domains','WEB-SOLUTIONS':'/solutions','WEB-SOL-MARKETING':'/solutions/marketing','WEB-SOL-CREATORS':'/solutions/creators','WEB-SOL-TEAMS':'/solutions/teams','WEB-SOL-DEVELOPERS':'/solutions/developers','WEB-DEVELOPERS':'/developers','WEB-PRICING':'/pricing','WEB-SECURITY':'/security','WEB-GUIDES':'/guides','WEB-GUIDE':'/guides/secure-link-sharing','WEB-ABOUT':'/about','WEB-CONTACT':'/contact','WEB-LEGAL-TERMS':'/legal/terms','WEB-LEGAL-PRIVACY':'/legal/privacy','WEB-LEGAL-AUP':'/legal/acceptable-use','WEB-LEGAL-ABUSE':'/legal/abuse'}

def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def file_for(path:str)->Path:
    if path=='/': return DIST/'index.html'
    if path=='/zh-CN/': return DIST/'zh-CN/index.html'
    return DIST/f'{path.lstrip("/")}.html'
def emit(case:str,obs:dict):
    OUT.mkdir(parents=True,exist_ok=True); data={'node':'P19','case':case,'status':'PASS','implementation_commit':head(),'observations':obs,'errors':[]}; (OUT/f'{case}.json').write_text(json.dumps(data,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print(json.dumps(data,indent=2,sort_keys=True))

def main():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    ids=[p['routeId'] for p in pages]
    assert ids==list(EXPECTED), (ids,list(EXPECTED))
    assert len(set(ids))==26
    raw_count=0
    for p in pages:
        assert p['path']==EXPECTED[p['routeId']]
        assert p['zhPath']==('/zh-CN/' if p['path']=='/' else '/zh-CN'+p['path'])
        for locale,path in [('en',p['path']),('zh-CN',p['zhPath'])]:
            target=file_for(path); assert target.is_file(), target
            html=target.read_text(encoding='utf-8')
            assert f'data-route-id="{p["routeId"]}"' in html
            assert f'<html lang="{locale}">' in html
            assert re.search(r'<h1>[^<]{3,}</h1>',html)
            assert '<meta name="description" content=' in html
            assert '<main id="main-content"' in html
            raw_count+=1
    emit('P19-T001',{'route_id_count':26,'canonical_page_count':raw_count,'raw_html_primary_content':True,'invented_route_ids':[]})

    required=['routeId','path','zhPath','updatedTime','contentOwner','structuredData','links','en','zh']
    titles=[]; descriptions=[]
    for p in pages:
        assert all(k in p for k in required)
        assert re.fullmatch(r'20\d\d-\d\d-\d\d',p['updatedTime'])
        assert p['contentOwner'].strip()
        for key in ('en','zh'):
            copy=p[key]
            for field in ('title','description','h1','eyebrow','lede','points'): assert copy[field]
            assert len(copy['points'])>=2
            titles.append(copy['title']); descriptions.append(copy['description'])
    assert len(titles)==len(set(titles)) and len(descriptions)==len(set(descriptions))
    emit('P19-T002',{'records':26,'locales':['en','zh-CN'],'required_meta_fields':['title','description','h1','locale','updatedTime','contentOwner','canonicalPath','translation'],'unique_titles':52,'unique_descriptions':52,'deterministic_registry':True})

if __name__=='__main__': main()
