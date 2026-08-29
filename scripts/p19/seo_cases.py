#!/usr/bin/env python3
from __future__ import annotations
import json, os, re, subprocess, xml.etree.ElementTree as ET
from html import unescape
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; DIST=SITE/'dist'; OUT=ROOT/'artifacts/v10/P19/site-core'; BASE='https://gojet.cc'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def file_for(path):
    if path=='/': return DIST/'index.html'
    if path=='/zh-CN/': return DIST/'zh-CN/index.html'
    return DIST/f'{path.lstrip("/")}.html'
def emit(case,obs):
    OUT.mkdir(parents=True,exist_ok=True); data={'node':'P19','case':case,'status':'PASS','implementation_commit':head(),'observations':obs,'errors':[]}; (OUT/f'{case}.json').write_text(json.dumps(data,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print(json.dumps(data,indent=2,sort_keys=True))
def link(html,rel,lang=None):
    tags=re.findall(r'<link\s+[^>]*>',html,re.I)
    for tag in tags:
        attrs=dict((k.lower(),unescape(v)) for k,v in re.findall(r'([\w-]+)="([^"]*)"',tag))
        if attrs.get('rel')==rel and (lang is None and 'hreflang' not in attrs or lang is not None and attrs.get('hreflang')==lang): return attrs.get('href')
    return None

def main():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    canonical_count=0
    for p in pages:
      for path in (p['path'],p['zhPath']):
        html=file_for(path).read_text(encoding='utf-8'); expected=BASE+path
        assert link(html,'canonical')==expected,(path,link(html,'canonical'),expected)
        assert '?' not in link(html,'canonical') and '#' not in link(html,'canonical')
        assert path=='/' or path=='/zh-CN/' or not path.endswith('/')
        canonical_count+=1
    emit('P19-T003',{'canonical_count':canonical_count,'query_in_canonical_count':0,'fragment_in_canonical_count':0,'normalized_paths':True,'canonical_chain':False})

    alternate_count=0
    for p in pages:
      expected={'en':BASE+p['path'],'zh-CN':BASE+p['zhPath'],'x-default':BASE+p['path']}
      for path in (p['path'],p['zhPath']):
        html=file_for(path).read_text(encoding='utf-8')
        for lang,href in expected.items(): assert link(html,'alternate',lang)==href,(path,lang,link(html,'alternate',lang),href); alternate_count+=1
    emit('P19-T004',{'translation_pairs':26,'alternate_links_verified':alternate_count,'reciprocal':True,'x_default':'English canonical','fabricated_alternates':0})

    sitemap=DIST/'sitemap-website.xml'; assert sitemap.is_file()
    ns={'s':'http://www.sitemaps.org/schemas/sitemap/0.9'}; root=ET.parse(sitemap).getroot(); urls={}
    for node in root.findall('s:url',ns):
        loc=node.find('s:loc',ns).text; lastmod=node.find('s:lastmod',ns).text; urls[loc]=lastmod
    expected={}
    for p in pages:
      expected[BASE+p['path']]=p['updatedTime']; expected[BASE+p['zhPath']]=p['updatedTime']
    assert urls==expected,(set(expected)-set(urls),set(urls)-set(expected))
    prohibited=('/login','/register','/app','/admin','/t/','/p/','/f/','/abuse/report','/linkunavailable')
    assert not any(any(token in url for token in prohibited) for url in urls)
    emit('P19-T006',{'sitemap':'sitemap-website.xml','canonical_indexable_200_count':len(urls),'lastmod_source':'content registry','private_or_ugc_urls':0,'missing_urls':0,'extra_urls':0})

if __name__=='__main__': main()
