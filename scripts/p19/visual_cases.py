#!/usr/bin/env python3
from __future__ import annotations
import hashlib, json, os, re, struct, subprocess
from html import unescape
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; DIST=SITE/'dist'; OUT=ROOT/'artifacts/v10/P19/visual'; BASE='https://gojet.cc'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def file_for(path):
    if path=='/': return DIST/'index.html'
    if path=='/zh-CN/': return DIST/'zh-CN/index.html'
    return DIST/f'{path.lstrip("/")}.html'
def meta(html,key,attr='property'):
    for tag in re.findall(r'<meta\s+[^>]*>',html,re.I):
        attrs=dict((k.lower(),unescape(v)) for k,v in re.findall(r'([\w:-]+)="([^"]*)"',tag))
        if attrs.get(attr)==key: return attrs.get('content')
    return None
def png_size(data):
    if data[:8]!=b'\x89PNG\r\n\x1a\n' or data[12:16]!=b'IHDR': raise ValueError('not a PNG with IHDR')
    return struct.unpack('>II',data[16:24])

def main():
    pages=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    source_manifest=json.loads((SITE/'src/website/social-cards.json').read_text(encoding='utf-8'))
    resolved_path=DIST/'social-cards.json'
    errors=[]; cards={}
    if not resolved_path.is_file():
        errors.append('build-resolved social-card manifest is missing')
        resolved_manifest={'cards':{}}
    else:
        resolved_manifest=json.loads(resolved_path.read_text(encoding='utf-8'))
    if source_manifest.get('integrity',{}).get('algorithm')!='sha256': errors.append('source social-card integrity algorithm is not sha256')
    for locale,source_record in source_manifest['cards'].items():
        record=resolved_manifest.get('cards',{}).get(locale)
        if not record:
            errors.append(f'{locale}: resolved social-card record missing'); continue
        for key in ['path','width','height','mime','alt']:
            if record.get(key)!=source_record.get(key): errors.append(f'{locale}: resolved {key} diverges from source authority')
        digest_authority=record.get('sha256')
        if not isinstance(digest_authority,str) or not re.fullmatch(r'[0-9a-f]{64}',digest_authority): errors.append(f'{locale}: resolved sha256 authority missing or malformed')
        disk=DIST/record['path'].lstrip('/')
        if not disk.is_file(): errors.append(f'{locale}: social card missing from static build: {record["path"]}'); continue
        data=disk.read_bytes(); digest=hashlib.sha256(data).hexdigest()
        try: width,height=png_size(data)
        except Exception as exc: errors.append(f'{locale}: {exc}'); continue
        if digest!=digest_authority: errors.append(f'{locale}: social card digest mismatch {digest}')
        if [width,height]!=[record['width'],record['height']]: errors.append(f'{locale}: social dimensions {width}x{height} do not match authority')
        if record['mime']!='image/png': errors.append(f'{locale}: social MIME is not image/png')
        cards[locale]={'path':record['path'],'sha256':digest,'width':width,'height':height,'alt':record['alt']}
    attribution=DIST/'assets/social/ATTRIBUTION.md'
    if not attribution.is_file(): errors.append('social-card attribution missing from static build')
    else:
        text=attribution.read_text(encoding='utf-8')
        for needle in ['first-party GoJet brand assets','External artwork or stock imagery: none','Third-party logo or customer mark: none','GJ-V10-DS-GREENFIELD-2026-08-20']:
            if needle not in text: errors.append(f'attribution missing authority marker: {needle}')
    metadata_checked=0
    for page in pages:
        for locale,path,copy in [('en',page['path'],page['en']),('zh-CN',page['zhPath'],page['zh'])]:
            html=file_for(path).read_text(encoding='utf-8'); record=resolved_manifest['cards'][locale]; url=BASE+record['path']
            expected={
                ('og:title','property'):copy['title'],('og:description','property'):copy['description'],('og:url','property'):BASE+path,
                ('og:image','property'):url,('og:image:type','property'):record['mime'],('og:image:width','property'):str(record['width']),('og:image:height','property'):str(record['height']),('og:image:alt','property'):record['alt'],
                ('twitter:card','name'):'summary_large_image',('twitter:title','name'):copy['title'],('twitter:description','name'):copy['description'],('twitter:image','name'):url,('twitter:image:alt','name'):record['alt']
            }
            for (key,attr),value in expected.items():
                actual=meta(html,key,attr)
                if actual!=value: errors.append(f'{path}: {key} mismatch: {actual!r} != {value!r}')
            metadata_checked+=1
    data={'node':'P19','case':'P19-T019','name':'Social cards and asset attribution','status':'PASS' if not errors else 'FAIL','implementation_commit':head(),'errors':errors,'details':{'schema':resolved_manifest.get('schema'),'integrity_authority':'build-resolved sha256 bound to deterministic embedded bytes','cards':cards,'metadata_documents_checked':metadata_checked,'attribution':str(attribution.relative_to(ROOT)) if attribution.exists() else None}}
    OUT.mkdir(parents=True,exist_ok=True); (OUT/'P19-T019.json').write_text(json.dumps(data,indent=2,ensure_ascii=False,sort_keys=True)+'\n',encoding='utf-8'); print('P19-T019:',data['status']); [print('  -',e) for e in errors]
    raise SystemExit(1 if errors else 0)
if __name__=='__main__': main()
