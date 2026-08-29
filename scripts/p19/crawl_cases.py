#!/usr/bin/env python3
from __future__ import annotations
import argparse, json, os, re, subprocess, xml.etree.ElementTree as ET
from collections import deque
from html import unescape
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import urlparse

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; DIST=SITE/'dist'; OUT=ROOT/'artifacts/v10/P19/crawl'; BASE='https://gojet.cc'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def file_for(path):
    if path=='/': return DIST/'index.html'
    if path=='/zh-CN/': return DIST/'zh-CN/index.html'
    return DIST/f'{path.lstrip("/")}.html'
def emit(case,name,errors,details):
    OUT.mkdir(parents=True,exist_ok=True); data={'node':'P19','case':case,'name':name,'status':'PASS' if not errors else 'FAIL','implementation_commit':head(),'errors':errors,'details':details}; (OUT/f'{case}.json').write_text(json.dumps(data,indent=2,ensure_ascii=False,sort_keys=True)+'\n',encoding='utf-8'); print(f"{case}: {data['status']}"); [print('  -',e) for e in errors]; return 1 if errors else 0

class Scan(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True); self.anchors=[]; self.assets=[]; self.meta=[]; self.links=[]; self.h1=[]; self._h1=False; self._h1buf=[]
    def handle_starttag(self,tag,attrs):
        a=dict(attrs)
        if tag=='a' and a.get('href'): self.anchors.append(a['href'])
        if tag in {'script','img','source'}:
            if a.get('src'): self.assets.append(a['src'])
            if a.get('srcset'): self.assets.extend(part.strip().split(' ')[0] for part in a['srcset'].split(',') if part.strip())
        if tag=='link':
            self.links.append(a)
            if a.get('rel')=='stylesheet' and a.get('href'): self.assets.append(a['href'])
        if tag=='meta': self.meta.append(a)
        if tag=='h1': self._h1=True; self._h1buf=[]
    def handle_data(self,data):
        if self._h1: self._h1buf.append(data)
    def handle_endtag(self,tag):
        if tag=='h1' and self._h1: self.h1.append(''.join(self._h1buf).strip()); self._h1=False

def local_path(value):
    if not value or value.startswith(('#','mailto:','tel:','javascript:')): return None
    parsed=urlparse(value)
    if parsed.scheme or parsed.netloc:
        if parsed.scheme in {'http','https'} and parsed.netloc=='gojet.cc': return parsed.path or '/'
        return None
    return parsed.path or '/'
def page_scan(path):
    html=file_for(path).read_text(encoding='utf-8'); scan=Scan(); scan.feed(html); return html,scan
def jsonld_values(value,key):
    out=[]
    if isinstance(value,dict):
        if key in value and isinstance(value[key],str): out.append(value[key])
        for child in value.values(): out.extend(jsonld_values(child,key))
    elif isinstance(value,list):
        for child in value: out.extend(jsonld_values(child,key))
    return out

def context():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    docs=json.loads((ROOT/'frontend/apps/docs/src/data/content-manifest.json').read_text(encoding='utf-8'))
    en={p['path']:p for p in pages}; zh={p['zhPath']:p for p in pages}; all_web=set(en)|set(zh)
    return {
      'pages':pages,'docs':docs,'en':en,'zh':zh,'all_web':all_web,
      'docs_published':{d['canonicalPath'] for d in docs['documents'] if d['releaseState']=='published'},
      'router':(SITE/'src/router.tsx').read_text(encoding='utf-8'),
      'nginx':(ROOT/'deploy/nginx/site-p19.conf').read_text(encoding='utf-8'),
      'postbuild':(ROOT/'scripts/p19/postbuild.mjs').read_text(encoding='utf-8'),
      'scans':{path:page_scan(path) for path in all_web},
    }

def t017(c):
    errors=[]; graph={}; fake=[]; en=c['en']; zh=c['zh']; docs_published=c['docs_published']
    for path,(html,scan) in c['scans'].items():
        locale='zh-CN' if path.startswith('/zh-CN') else 'en'; same=set(zh) if locale=='zh-CN' else set(en); edges=set()
        for href in scan.anchors:
            target=local_path(href)
            if target in same: edges.add(target)
            if target and target.startswith('/zh-CN') and target not in zh and target not in docs_published: fake.append((path,target))
            if target and not target.startswith('/zh-CN') and target.startswith(('/products','/solutions','/developers','/pricing','/security','/guides','/about','/contact','/legal')) and target not in en: fake.append((path,target))
        graph[path]=sorted(edges)
    reachable={}
    for locale,root,nodes in [('en','/',set(en)),('zh-CN','/zh-CN/',set(zh))]:
        seen=set(); q=deque([root])
        while q:
            cur=q.popleft()
            if cur in seen: continue
            seen.add(cur)
            for nxt in graph.get(cur,[]):
                if nxt in nodes and nxt not in seen: q.append(nxt)
        reachable[locale]=sorted(seen); missing=nodes-seen
        if missing: errors.append(f'{locale} indexable orphan routes: {sorted(missing)}')
        incoming={node:0 for node in nodes}
        for src in nodes:
            for dst in graph.get(src,[]):
                if dst in incoming and src!=dst: incoming[dst]+=1
        orphan_parents=[node for node,count in incoming.items() if node!=root and count==0]
        if orphan_parents: errors.append(f'{locale} routes without crawlable parent edge: {orphan_parents}')
    if fake: errors.append(f'fake/unregistered localized Website targets: {fake[:20]}')
    return emit('P19-T017','Internal-link graph and orphan prevention',errors,{'english_reachable':len(reachable['en']),'zh_cn_reachable':len(reachable['zh-CN']),'expected_per_locale':len(c['pages']),'fake_locale_links':fake,'graph':graph})

def t020(c):
    errors=[]; robots=(DIST/'robots.txt').read_text(encoding='utf-8').splitlines(); rules={line.strip() for line in robots if line.strip()}
    required={'User-agent: *','Allow: /','Disallow: /app/','Disallow: /admin/','Disallow: /preview/','Disallow: /api/',f'Sitemap: {BASE}/sitemap-website.xml'}; missing=required-rules
    if missing: errors.append(f'robots missing required rules: {sorted(missing)}')
    for forbidden in ['Disallow: /docs','Disallow: /products','Disallow: /solutions','Disallow: /guides']:
        if any(line.startswith(forbidden) for line in rules): errors.append(f'robots blocks indexable/public acquisition: {forbidden}')
    ns={'s':'http://www.sitemaps.org/schemas/sitemap/0.9'}; sitemap={n.find('s:loc',ns).text for n in ET.parse(DIST/'sitemap-website.xml').getroot().findall('s:url',ns)}; expected={BASE+p for p in c['all_web']}
    if sitemap!=expected: errors.append(f'sitemap acquisition set mismatch: missing={sorted(expected-sitemap)}, extra={sorted(sitemap-expected)}')
    acquisition=set(sitemap)
    for path,(html,scan) in c['scans'].items():
        for tag in scan.links:
            if tag.get('rel') in {'canonical','alternate'} and tag.get('href'): acquisition.add(tag['href'])
        for payload in re.findall(r'<script\s+type="application/ld\+json">(.*?)</script>',html,re.I|re.S):
            data=json.loads(payload)
            for key in ('url','item'):
                for value in jsonld_values(data,key):
                    if value.startswith(BASE): acquisition.add(value)
    bad=[]
    for url in acquisition:
        if not url.startswith(BASE): continue
        suffix=url[len(BASE):] or '/'
        if suffix not in c['all_web']: bad.append(url)
    if bad: errors.append(f'private/Auth/UGC/error URL entered Website acquisition metadata: {sorted(bad)[:20]}')
    return emit('P19-T020','Robots and crawler policy parity',errors,{'robots':robots,'sitemap_urls':len(sitemap),'acquisition_urls':len(acquisition),'non_website_acquisition':sorted(bad)})

def t021(c):
    errors=[]; checked=[]
    if '$http_user_agent' in c['nginx'] or 'navigator.userAgent' in c['postbuild'] or 'Googlebot' in c['postbuild'] or 'Bingbot' in c['postbuild']: errors.append('crawler-specific rendering branch detected')
    for page in c['pages']:
        for locale,path,copy in [('en',page['path'],page['en']),('zh-CN',page['zhPath'],page['zh'])]:
            html,scan=c['scans'][path]; text=unescape(re.sub(r'<[^>]+>',' ',html)); text=re.sub(r'\s+',' ',text)
            if 'data-p19-static="true"' not in html: errors.append(f'{path}: missing build-time static marker')
            if '<main' not in html or '<article' not in html: errors.append(f'{path}: primary semantic raw HTML missing')
            if len(scan.h1)!=1 or scan.h1[0]!=copy['h1']: errors.append(f'{path}: raw H1 mismatch {scan.h1}')
            if copy['lede'] not in text: errors.append(f'{path}: primary lede absent from raw HTML')
            for point in copy['points']:
                if point not in text: errors.append(f'{path}: substantive point absent from raw HTML: {point}')
            title=re.search(r'<title>(.*?)</title>',html,re.I|re.S)
            if not title or unescape(title.group(1)).strip()!=copy['title']: errors.append(f'{path}: raw title mismatch')
            meta_desc=next((m.get('content') for m in scan.meta if m.get('name')=='description'),None)
            if meta_desc!=copy['description']: errors.append(f'{path}: raw description mismatch')
            if not re.search(r'<script\s+type="application/ld\+json">',html,re.I): errors.append(f'{path}: raw JSON-LD missing')
            checked.append(path)
    return emit('P19-T021','Raw HTML and crawler/render parity',errors,{'documents_checked':len(checked),'raw_primary_content':True,'crawler_specific_branch':False,'paths':sorted(checked)})

def t022(c):
    errors=[]; asset_checked=set(); nav_checked=[]; allowed_auth={'/login','/register'}
    for path,(html,scan) in c['scans'].items():
        for href in scan.anchors:
            target=local_path(href)
            if target is None: continue
            if target in c['all_web']:
                if not file_for(target).is_file(): errors.append(f'{path}: Website navigation target missing build output: {target}')
            elif target in c['docs_published']: pass
            elif target in allowed_auth:
                if f"path: '{target}'" not in c['router'] and f'path: "{target}"' not in c['router']: errors.append(f'{path}: inherited Auth target not registered: {target}')
            elif target.startswith('/docs/'): errors.append(f'{path}: Docs target is not P18 published authority: {target}')
            elif target.startswith('/'): errors.append(f'{path}: unregistered local navigation target: {target}')
            nav_checked.append((path,target))
        for asset in scan.assets:
            target=local_path(asset)
            if target and target.startswith('/assets/'):
                disk=DIST/target.lstrip('/')
                if not disk.is_file(): errors.append(f'{path}: static asset missing: {target}')
                else: asset_checked.add(target)
        for meta in scan.meta:
            key=meta.get('property') or meta.get('name')
            if key in {'og:image','twitter:image'} and meta.get('content'):
                target=local_path(meta['content'])
                if not target or not target.startswith('/assets/social/'): errors.append(f'{path}: social image is not a local controlled asset: {meta.get("content")}')
                elif not (DIST/target.lstrip('/')).is_file(): errors.append(f'{path}: social image missing: {target}')
                else: asset_checked.add(target)
    attribution=DIST/'assets/social/ATTRIBUTION.md'
    if not attribution.is_file(): errors.append('social asset attribution was not copied into static build')
    return emit('P19-T022','Broken-link and static-asset integrity',errors,{'navigation_edges_checked':len(nav_checked),'assets_checked':sorted(asset_checked),'published_docs_targets':sorted(c['docs_published']),'auth_targets':sorted(allowed_auth),'attribution':str(attribution.relative_to(ROOT)) if attribution.exists() else None})

def main():
    parser=argparse.ArgumentParser(); parser.add_argument('--case',required=True,choices=['P19-T017','P19-T020','P19-T021','P19-T022']); args=parser.parse_args(); c=context()
    result={'P19-T017':t017,'P19-T020':t020,'P19-T021':t021,'P19-T022':t022}[args.case](c); raise SystemExit(result)
if __name__=='__main__': main()
