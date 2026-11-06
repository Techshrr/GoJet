"""Release migration catalog; never invents reversible DDL."""
import hashlib
import re
from pathlib import Path

def catalog(directory):
    rows=[]; names=set()
    for path in sorted(Path(directory).glob('*.sql')):
        m=re.fullmatch(r'(\d{6})_([a-z0-9_]+)\.sql',path.name)
        if not m or path.is_symlink(): raise ValueError('invalid migration: '+path.name)
        number=int(m[1]); raw=path.read_bytes()
        if number != len(rows)+1 or m[2] in names or not raw.strip():
            raise ValueError('duplicate/gap/empty migration: '+path.name)
        names.add(m[2])
        rows.append({'id':number,'path':'migrations/'+path.name,'sha256':hashlib.sha256(raw).hexdigest(),'bytes':len(raw),'rollback':'restore-verified-database-backup; no automatic down migration'})
    if not rows: raise ValueError('empty catalog')
    return {'schema':'gojet.migration-catalog.v1','migrations':rows,'apply_requirement':'global lock; durable started/applied journal; checksum drift rejects; partial DDL requires recovery'}
