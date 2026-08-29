#!/usr/bin/env python3
from __future__ import annotations
import json, os, subprocess, urllib.error, urllib.request
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; OUT=ROOT/'artifacts/v10/P19/site-core'; BASE=os.environ.get('P19_SITE_URL','http://127.0.0.1:4189').rstrip('/')
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def request(path):
    req=urllib.request.Request(BASE+path,headers={'User-Agent':'GoJet-P19-T005/1'})
    try:
        with urllib.request.urlopen(req,timeout=10) as r: return r.status,r.read().decode('utf-8','replace'),dict(r.headers)
    except urllib.error.HTTPError as e: return e.code,e.read().decode('utf-8','replace'),dict(e.headers)
def emit(obs):
    OUT.mkdir(parents=True,exist_ok=True); data={'node':'P19','case':'P19-T005','status':'PASS','implementation_commit':head(),'observations':obs,'errors':[]}; (OUT/'P19-T005.json').write_text(json.dumps(data,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print(json.dumps(data,indent=2,sort_keys=True))

def main():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    checked=0
    for p in pages:
      for path in (p['path'],p['zhPath']):
        status,body,_=request(path); assert status==200,(path,status); assert f'data-route-id="{p["routeId"]}"' in body; checked+=1
    for path in ('/not-a-real-page','/guides/unknown-guide','/zh-CN/guides/unknown-guide'):
        status,_,_=request(path); assert status==404,(path,status)
    for path in ('/guides/legacy-deployment','/zh-CN/guides/legacy-deployment'):
        status,_,headers=request(path); assert status==410,(path,status); assert 'noindex' in headers.get('X-Robots-Tag','')
    status,body,_=request('/products?utm_source=p19-test&ref=ignored'); assert status==200; assert '<link rel="canonical" href="https://gojet.cc/products">' in body; assert 'utm_source' not in body
    emit({'canonical_200_pages':checked,'unknown_404':3,'withdrawn_410':2,'soft_404':0,'query_changes_status':False,'query_changes_canonical':False})

if __name__=='__main__': main()
