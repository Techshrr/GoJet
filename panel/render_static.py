import sys
from pathlib import Path
sys.path.insert(0,'/build/scripts/p21')
from static_routes import render
root=Path('/opt/gojet/current')
files={p.relative_to(root).as_posix():p.read_bytes() for p in (root/'static').rglob('*') if p.is_file()}
(root/'static-apps.conf').write_bytes(render(files,Path('/build/deploy/nginx/site-p19.conf').read_text(),Path('/build/deploy/nginx/docs-p18.conf').read_text()))
