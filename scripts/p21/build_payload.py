"""Assemble actual compiled output. No native apply/installer completeness claim."""
import json
from pathlib import Path
import subprocess
from urllib.parse import quote
from archive import BINS,digest,encode,write_archive,allowed
from catalog import catalog
ROOT=Path(__file__).resolve().parents[2]
OUT=ROOT/'artifacts/v10/P21/payload'
def command(*args): return subprocess.check_output(args,cwd=ROOT,text=True)
def validate_static(root, app, repository=ROOT):
    if app == 'docs':
        manifest=json.loads((repository/'frontend/apps/docs/src/data/content-manifest.json').read_text())
        required={'404.html','en/search/index.html','zh-CN/search/index.html','pagefind/pagefind.js'}
        for document in manifest['documents']:
            canonical=document['canonicalPath']
            if not canonical.startswith('/docs/') or '..' in canonical.split('/'):
                raise ValueError('invalid Docs route authority')
            required.add(canonical[len('/docs/'):].strip('/')+'/index.html')
    else:
        required={'index.html'}
    missing=sorted(name for name in required if not (root/name).is_file())
    if missing: raise ValueError('missing static app output: '+app+': '+', '.join(missing))

def main():
    head=command('git','rev-parse','HEAD').strip()
    plan=json.loads((ROOT/'artifacts/v10/P21/test-plan.json').read_text())
    report=json.loads((ROOT/'artifacts/v10/P21/build/native-build.json').read_text())
    assert report['source']==head
    files={}
    excluded_build_metadata={}
    def add(target,source):
        if source.is_symlink() or not source.is_file(): raise ValueError('not a regular payload source: '+str(source))
        if target in files: raise ValueError('duplicate payload target')
        files[target]=source.read_bytes()
    for row in report['native_version_checks']:
        name=row['service']; add('bin/'+name,ROOT/'artifacts/v10/P21/build/bin'/name)
        assert digest(files['bin/'+name])==row['sha256']
    assert {n.split('/')[1] for n in files}==BINS
    for app in plan['apps']:
        root=ROOT/'frontend/apps'/app/'dist'
        validate_static(root,app)
        for path in sorted(root.rglob('*')):
            if path.is_symlink(): raise ValueError('static symlink')
            if path.is_file():
                relative=path.relative_to(root).as_posix()
                if relative == '.vite/manifest.json':
                    excluded_build_metadata[app+'/'+relative]=digest(path.read_bytes())
                    continue
                add('static/'+app+'/'+relative,path)
    for source in sorted((ROOT/'deploy/systemd').glob('*.service')):
        add(source.relative_to(ROOT).as_posix(),source)
    for name in ['deploy/sysusers/gojet.conf','deploy/tmpfiles/gojet.conf']:
        add(name,ROOT/name)
    migration_catalog=catalog(ROOT/'migrations')
    files['migration-catalog.json']=encode(migration_catalog)
    for row in migration_catalog['migrations']: add(row['path'],ROOT/row['path'])
    for name in ['installer/public/index.php','installer/public/favicon.ico','installer/public/assets/shell.css','installer/src/Shell.php','installer/src/FilePreflight.php','installer/src/Migrations.php','installer/src/InstallState.php','installer/src/CompletionLock.php','installer/src/HttpAccess.php','installer/src/NativeWebConfig.php','frontend/packages/tokens/generated/tokens.css']:
        add(name,ROOT/name)
    # Dependencies are taken from installed frozen-lock build inputs, not guessed
    # from package.json's direct dependency list. Build-only tools are not shipped.
    modules=command('go','list','-m','-json','all'); decoder=json.JSONDecoder(); rest=modules; components={}
    while rest.strip():
        row,n=decoder.raw_decode(rest.lstrip()); rest=rest.lstrip()[n:]
        version=row.get('Version',plan['release_version']); name=row['Path']; ref='pkg:golang/'+quote(name,safe='/')+'@'+quote(version,safe='')
        components[ref]={'type':'library','name':name,'version':version,'purl':ref,'bom-ref':ref}
    npm=json.loads(command('pnpm','list','-r','--prod','--depth','Infinity','--json'))
    def visit(row):
        for section in ['dependencies','optionalDependencies']:
            for name,dep in row.get(section,{}).items():
                version=dep.get('version','')
                if version and not version.startswith(('link:','file:','workspace:')):
                    ref='pkg:npm/'+quote(name,safe='/@')+'@'+quote(version,safe='')
                    components[ref]={'type':'library','name':name,'version':version,'purl':ref,'bom-ref':ref}
                visit(dep)
    for row in npm: visit(row)
    for name,data in files.items():
        ref='file:'+name
        components[ref]={'type':'file','name':name,'bom-ref':ref,'hashes':[{'alg':'SHA-256','content':digest(data)}]}
    OUT.mkdir(parents=True,exist_ok=True)
    path=OUT/f"gojet-{plan['release_version']}-linux-amd64-payload.tar.gz"
    invalid=sorted(name for name in files if not allowed(name))
    if invalid: raise ValueError('non-allowlisted payload members: '+', '.join(invalid))
    result=write_archive(files,path,head,plan['release_version'],list(components.values()))
    second=OUT/'reproducibility-check.tar.gz'
    other=write_archive(files,second,head,plan['release_version'],list(components.values()))
    assert result['archive_sha256']==other['archive_sha256']; second.unlink()
    (OUT/(path.name+'.sha256')).write_text(result['archive_sha256']+'  '+path.name+'\n')
    (OUT/'verification.json').write_bytes(encode(dict(result,excluded_build_metadata=excluded_build_metadata,reproducible_archive=True,static_apps=plan['apps'],binaries=8,helpers=1,migrations=len(migration_catalog['migrations']),component_count=len(components),pending=['native configuration','migration execution','Installer/apply','upgrade/rollback','G11/closure'])))
    print(json.dumps(result))
if __name__=='__main__': main()
