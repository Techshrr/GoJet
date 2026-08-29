#!/usr/bin/env python3
from __future__ import annotations
import json, os, re, subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SITE=ROOT/'frontend/apps/site'
OUT=ROOT/'artifacts/v10/P19/runtime'
def head(): return os.environ.get('EXACT_HEAD') or subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,text=True).strip()
def main():
    errors=[]
    nginx=(ROOT/'deploy/nginx/site-p19.conf').read_text(encoding='utf-8')
    vite=(SITE/'vite.config.ts').read_text(encoding='utf-8')
    package=json.loads((SITE/'package.json').read_text(encoding='utf-8'))
    postbuild=(ROOT/'scripts/p19/postbuild.mjs').read_text(encoding='utf-8')
    if 'try_files' not in nginx: errors.append('static Nginx try_files boundary missing')
    for token in ['proxy_pass','pm2','node ']:
        if token.lower() in nginx.lower(): errors.append(f'production Website Nginx contains prohibited runtime token: {token}')
    if re.search(r'\b(?:docker|docker-compose|compose\.ya?ml)\b',nginx,re.I): errors.append('production Website Nginx contains Docker/Compose guidance')
    if 'ssr:' in vite or 'adapter' in vite: errors.append('Website Vite config declares SSR/adapter production boundary')
    if package.get('scripts',{}).get('build')!='vite build && node ../../../scripts/p19/postbuild.mjs': errors.append('Website build command no longer binds Vite build to P19 static postbuild')
    if 'createServer(' in postbuild or 'listen(' in postbuild: errors.append('P19 postbuild contains a serving runtime')
    if not (SITE/'dist').is_dir(): errors.append('static Website dist missing')
    else:
        required=['index.html','sitemap-website.xml','robots.txt','website-manifest.json','social-cards.json']
        missing=[name for name in required if not (SITE/'dist'/name).is_file()]
        if missing: errors.append(f'static build artifacts missing: {missing}')
    details={
      'production_runtime':'STATIC_NGINX_ONLY','nginx_try_files':nginx.count('try_files'),
      'proxy_pass': 'proxy_pass' in nginx,'pm2': 'pm2' in nginx.lower(),'docker_compose': bool(re.search(r'\b(?:docker|docker-compose|compose\.ya?ml)\b',nginx,re.I)),
      'vite_ssr_adapter': bool('ssr:' in vite or 'adapter' in vite),'build_command':package.get('scripts',{}).get('build'),
      'postbuild_serves_http': bool('createServer(' in postbuild or 'listen(' in postbuild),
    }
    data={'node':'P19','case':'P19-T029','name':'Static Website production runtime boundary','status':'PASS' if not errors else 'FAIL','implementation_commit':head(),'errors':errors,'details':details}
    OUT.mkdir(parents=True,exist_ok=True); (OUT/'P19-T029.json').write_text(json.dumps(data,indent=2,sort_keys=True)+'\n',encoding='utf-8'); print('P19-T029:',data['status']); [print('  -',e) for e in errors]; raise SystemExit(1 if errors else 0)
if __name__=='__main__': main()
