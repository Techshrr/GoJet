"""Native binary/catalog evidence only; incomplete release cannot claim G11."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import shutil
from catalog import catalog
ROOT=Path(__file__).resolve().parents[2]
OUT=ROOT/'artifacts/v10/P21/build'
def run(*args,**kwargs):
    return subprocess.check_output(args,cwd=ROOT,text=True,**kwargs).strip()
def main():
    OUT.mkdir(parents=True,exist_ok=True)
    plan=json.loads((ROOT/'artifacts/v10/P21/test-plan.json').read_text())
    head=run('git','rev-parse','HEAD'); version=plan['release_version']
    sources=[str(ROOT/'internal/releaseinfo')]+[str(ROOT/p/'main.go') for p in (plan['services']|plan['helpers']).values()]
    formatting=run('gofmt','-l',*sources)
    if formatting: raise ValueError('gofmt required: '+formatting)
    run('go','test','./internal/releaseinfo')
    baseline=run('go','test','./...')
    (OUT/'go-test.log').write_text(baseline+'\n')
    env=dict(os.environ,CGO_ENABLED='0',GOOS='linux',GOARCH='amd64')
    rows=[]
    with tempfile.TemporaryDirectory() as d:
        for name,package in (plan['services']|plan['helpers']).items():
            binary=Path(d)/name
            run('go','build','-trimpath','-buildvcs=false','-ldflags',f'-buildid= -X github.com/Techshrr/GoJet/internal/releaseinfo.Version={version} -X github.com/Techshrr/GoJet/internal/releaseinfo.Commit={head}','-o',str(binary),'./'+package,env=env)
            raw=binary.read_bytes()
            if raw[:6]!=b'\x7fELF\x02\x01' or int.from_bytes(raw[18:20],'little')!=62: raise ValueError('wrong ELF target')
            program_headers=run('readelf','-l',str(binary))
            if 'INTERP' in program_headers: raise ValueError('dynamic interpreter dependency')
            actual=json.loads(run(str(binary),'--version',env={'PATH':os.environ['PATH']}))
            assert actual=={'service':name,'version':version,'commit':head}
            if os.environ.get('P21_KEEP_BINARIES') == '1':
                (OUT/'bin').mkdir(exist_ok=True)
                shutil.copyfile(binary,OUT/'bin'/name)
            rows.append(dict(actual,sha256=hashlib.sha256(raw).hexdigest(),bytes=len(raw),daemon=name in plan['services']))
    report={'source':head,'version':version,'native_version_checks':rows,'migration_catalog':catalog(ROOT/'migrations'),'package_completion_claim':False,'remaining':['static frontend/package','migration apply','native configuration','Installer/apply','upgrade/rollback','G11/closure']}
    (OUT/'native-build.json').write_text(json.dumps(report,indent=2)+'\n')
    (OUT/'go-modules.json').write_text(run('go','list','-m','-json','all')+'\n')
    print(json.dumps({'native_binaries':8,'helpers':1,'migration_count':len(report['migration_catalog']['migrations']),'source':head,'package_completion_claim':False}))
if __name__=='__main__': main()
