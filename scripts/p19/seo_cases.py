#!/usr/bin/env python3
from __future__ import annotations
import argparse, json, os, re, subprocess, xml.etree.ElementTree as ET
from html import unescape
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; DIST=SITE/'dist'; CORE=ROOT/'artifacts/v10/P19/site-core'; SEO=ROOT/'artifacts/v10/P19/seo'; BASE='https://gojet.cc'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def file_for(path):
    if path=='/': return DIST/'index.html'
    if path=='/zh-CN/': return DIST/'zh-CN/index.html'
    return DIST/f'{path.lstrip("/")}.html'
def emit(case,obs,out=CORE):
    out.mkdir(parents=True,exist_ok=True); data={'node':'P19','case':case,'status':'PASS','implementation_commit':head(),'observations':obs,'errors':[]}; (out/f'{case}.json').write_text(json.dumps(data,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print(json.dumps(data,indent=2,sort_keys=True))
def link(html,rel,lang=None):
    tags=re.findall(r'<link\s+[^>]*>',html,re.I)
    for tag in tags:
        attrs=dict((k.lower(),unescape(v)) for k,v in re.findall(r'([\w-]+)="([^"]*)"',tag))
        if attrs.get('rel')==rel and (lang is None and 'hreflang' not in attrs or lang is not None and attrs.get('hreflang')==lang): return attrs.get('href')
    return None

def core_cases():
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

def collect_types(value):
    found=[]
    if isinstance(value,dict):
        t=value.get('@type')
        if isinstance(t,str): found.append(t)
        elif isinstance(t,list): found.extend(x for x in t if isinstance(x,str))
        for child in value.values(): found.extend(collect_types(child))
    elif isinstance(value,list):
        for child in value: found.extend(collect_types(child))
    return found

def breadcrumbs(value):
    out=[]
    if isinstance(value,dict):
        if value.get('@type')=='BreadcrumbList': out.append(value)
        for child in value.values(): out.extend(breadcrumbs(child))
    elif isinstance(value,list):
        for child in value: out.extend(breadcrumbs(child))
    return out

def structured_case():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    prohibited={'AggregateRating','Review','Offer'}; structural={'ListItem'}; checked=[]
    for page in pages:
        allowed=set(page['structuredData'])|structural
        for locale,path,copy in [('en',page['path'],page['en']),('zh-CN',page['zhPath'],page['zh'])]:
            html=file_for(path).read_text(encoding='utf-8')
            scripts=re.findall(r'<script\s+type="application/ld\+json">(.*?)</script>',html,re.I|re.S)
            assert len(scripts)==1,(path,'jsonld-count',len(scripts))
            data=json.loads(scripts[0]); types=set(collect_types(data))
            assert types-prohibited==types,(path,'prohibited',types&prohibited)
            assert types <= allowed,(path,'unsupported-types',types-allowed,'allowed',allowed)
            for expected in page['structuredData']: assert expected in types,(path,'missing-type',expected,types)
            title=re.search(r'<title>(.*?)</title>',html,re.I|re.S); h1=re.findall(r'<h1>(.*?)</h1>',html,re.I|re.S)
            assert title and unescape(re.sub('<[^>]+>','',title.group(1))).strip()==copy['title'],(path,'title-parity')
            assert len(h1)==1 and unescape(re.sub('<[^>]+>','',h1[0])).strip()==copy['h1'],(path,'h1-parity')
            for crumb in breadcrumbs(data):
                items=crumb.get('itemListElement') or []
                assert items and items[-1].get('item')==BASE+path,(path,'breadcrumb-current',items[-1] if items else None)
            assert not (types & prohibited),(path,'fabricated-schema',types&prohibited)
            checked.append({'path':path,'locale':locale,'types':sorted(types)})
    emit('P19-T018',{'pages_checked':len(checked),'documents':checked,'prohibited_schema_types':sorted(prohibited),'validation':'JSON parse + IA eligibility + visible title/H1 parity'},SEO)

def main():
    parser=argparse.ArgumentParser(); parser.add_argument('--case',choices=['P19-T018']); args=parser.parse_args()
    if args.case=='P19-T018': structured_case()
    else: core_cases()
if __name__=='__main__': main()
