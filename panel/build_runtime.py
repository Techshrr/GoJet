"""Build-time verified native baseline + separately built panel frontend."""
import hashlib, io, json, sys, tarfile, urllib.request, zipfile
from pathlib import Path
ROOT=Path('/opt/gojet/native')
URL='https://github.com/Techshrr/GoJet/releases/download/v10.0.0-rc.2/GoJet-Native-10.0.0-rc.2-ce305a9-Full.zip'
raw=urllib.request.urlopen(URL,timeout=180).read()
assert hashlib.sha256(raw).hexdigest()=='f16c2e6a851b506c6b08bb54ba78942edaf0618f55a22e913e32d6ca65acfb60'
with zipfile.ZipFile(io.BytesIO(raw)) as z:
    prefix=z.namelist()[0].split('/')[0]+'/'
    payload=z.read(prefix+'gojet-10.0.0-rc.2-linux-amd64-payload.tar.gz')
    assert hashlib.sha256(payload).hexdigest()=='49fc5248bd2101db04ee5ba03398c6cb247e14737ada51a2fe0708efae7086b9'
    risk=z.read(prefix+'integrations/GoJet-WebRisk-1.0.0.zip')
    assert hashlib.sha256(risk).hexdigest()=='e6045d32ce89af0d8640a3d069fb849251a0f5acdd414cf224654fdc308d6db0'
with tarfile.open(fileobj=io.BytesIO(payload),mode='r:gz') as t:
    for m in t.getmembers():
        assert m.isfile() and not m.name.startswith('/') and '..' not in Path(m.name).parts
        p=ROOT/m.name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(t.extractfile(m).read());p.chmod(m.mode & 0o755)
with zipfile.ZipFile(io.BytesIO(risk)) as z:
    names=[n for n in z.namelist() if n.endswith('/adapter.py')]
    assert len(names)==1
    Path('/opt/linketic/adapter.py').write_bytes(z.read(names[0]))
