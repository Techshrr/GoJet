#!/usr/bin/env python3
from __future__ import annotations
import argparse, hashlib, json, os, subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]; SITE=ROOT/'frontend/apps/site'; OUT=ROOT/'artifacts/v10/P19/content'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def sha(path:Path): return hashlib.sha256(path.read_bytes()).hexdigest()
def tree(root:Path):
    result={}
    for path in sorted(root.rglob('*')):
        if path.is_symlink(): result[str(path.relative_to(root))]='SYMLINK'
        elif path.is_file(): result[str(path.relative_to(root)).replace('\\','/')]=sha(path)
    return result
def main():
    parser=argparse.ArgumentParser(); parser.add_argument('--first',required=True); parser.add_argument('--second',default='frontend/apps/site/dist'); args=parser.parse_args()
    first=Path(args.first); second=ROOT/args.second if not Path(args.second).is_absolute() else Path(args.second); errors=[]
    if not first.is_dir() or not second.is_dir(): errors.append('one or both clean build directories are missing')
    first_tree=tree(first) if first.is_dir() else {}; second_tree=tree(second) if second.is_dir() else {}
    if first_tree!=second_tree:
        keys=sorted(set(first_tree)|set(second_tree)); diffs=[{'path':key,'first':first_tree.get(key),'second':second_tree.get(key)} for key in keys if first_tree.get(key)!=second_tree.get(key)]
        errors.append(f'clean Website builds differ at {len(diffs)} paths')
    else: diffs=[]
    content=json.loads((SITE/'src/website/content.json').read_text(encoding='utf-8'))
    if len(content)!=26: errors.append(f'content registry expected 26 route IDs, got {len(content)}')
    route_ids=[p['routeId'] for p in content]; en=[p['path'] for p in content]; zh=[p['zhPath'] for p in content]
    if len(set(route_ids))!=26 or len(set(en))!=26 or len(set(zh))!=26: errors.append('Website route identity/path registry is not unique')
    for page in content:
        expected='/zh-CN/' if page['path']=='/' else f"/zh-CN{page['path']}"
        if page['zhPath']!=expected: errors.append(f"{page['routeId']}: bilingual canonical parity mismatch")
    manifest=json.loads((second/'website-manifest.json').read_text(encoding='utf-8')) if (second/'website-manifest.json').is_file() else {}
    manifest_pages=manifest.get('pages',[])
    if len(manifest_pages)!=52: errors.append(f'publishable manifest expected 52 locale pages, got {len(manifest_pages)}')
    published={(p.get('routeId'),p.get('locale'),p.get('path')) for p in manifest_pages}
    expected={(p['routeId'],'en',p['path']) for p in content}|{(p['routeId'],'zh-CN',p['zhPath']) for p in content}
    if published!=expected: errors.append('publishable manifest does not exactly match bilingual content registry')
    source_paths=[SITE/'src/website/content.json',SITE/'src/website/authority.json',SITE/'src/website/social-cards.json',ROOT/'scripts/p19/postbuild.mjs']
    source_digests={str(path.relative_to(ROOT)).replace('\\','/'):sha(path) for path in source_paths}
    combined=hashlib.sha256(''.join(f'{k}:{second_tree[k]}\n' for k in sorted(second_tree)).encode()).hexdigest() if second_tree else None
    details={'firstFileCount':len(first_tree),'secondFileCount':len(second_tree),'fullDistDigest':combined,'diffs':diffs[:100],'routeIds':route_ids,'localePairs':26,'publishableRecords':len(manifest_pages),'sourceDigests':source_digests}
    data={'node':'P19','case':'P19-T030','name':'Deterministic Website manifest and bilingual parity ledger','status':'PASS' if not errors else 'FAIL','implementation_commit':head(),'errors':errors,'details':details}
    OUT.mkdir(parents=True,exist_ok=True); (OUT/'P19-T030.json').write_text(json.dumps(data,ensure_ascii=False,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print('P19-T030:',data['status']); [print('  -',e) for e in errors]; raise SystemExit(1 if errors else 0)
if __name__=='__main__': main()
