"""Deterministic, manifest-bound native payload archive (build host only)."""
import gzip
import hashlib
import io
import json
from pathlib import Path, PurePosixPath
import re
import tarfile

META={'manifest.json','sbom.cdx.json','SHA256SUMS'}
BINS={'redirectengine','analyticsworker','analyticsreconciler','platformapi','mailworker','fileworker','operationsmonitor','logreceiver','filepreflight'}
SECRET=re.compile(rb'-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----|(?:AKIA|ASIA)[A-Z0-9]{16}|gh[pousr]_[A-Za-z0-9]{36,}')
def digest(data): return hashlib.sha256(data).hexdigest()
def encode(value): return (json.dumps(value,sort_keys=True,indent=2)+'\n').encode()
def allowed(name):
    p=PurePosixPath(name)
    if not name or str(p)!=name or p.is_absolute() or '..' in p.parts or '\\' in name: return False
    if any(x.startswith('.') or x.lower() in {'node_modules','docker','compose','dockerfile','node','npm','pnpm'} for x in p.parts): return False
    if name in META: return True
    if name in {'deploy/sysusers/gojet.conf','deploy/tmpfiles/gojet.conf','deploy/logrotate/gojet-nginx','deploy/nginx/static-apps.conf','deploy/nginx/platform-proxy.conf','deploy/nginx/redirect-proxy.conf'}: return True
    if name.startswith('deploy/systemd/'):
        return name in {'deploy/systemd/gojet-'+n+'.service' for n in BINS-{'filepreflight'}}
    if p.parts[0]=='bin': return len(p.parts)==2 and p.name in BINS
    if p.parts[0]=='migrations': return len(p.parts)==2 and bool(re.fullmatch(r'\d{6}_[a-z0-9_]+\.sql',p.name))
    if name=='migration-catalog.json': return True
    if p.parts[0]=='static': return len(p.parts)>2 and p.parts[1] in {'site','docs','workspace','admin'} and p.suffix.lower() not in {'.map','.ts','.tsx','.vue','.php','.env','.pem','.key'}
    if name in {'installer/public/index.php','installer/public/favicon.ico','installer/public/assets/shell.css','installer/public/assets/install.js','installer/src/Shell.php','installer/src/FilePreflight.php','installer/src/Migrations.php','installer/src/DependencyPreflight.php','installer/src/UnprivilegedProbe.php','installer/bin/preflight.php','installer/src/NativeConfiguration.php','installer/bin/configure-services.php','installer/src/ServiceEnvironment.php','installer/bin/create-environment.php','installer/src/PackageIntegrity.php','installer/bin/verify-package.php','installer/src/SiteActivation.php','installer/bin/activate-site.php','installer/bin/recover-site.php','installer/src/ServiceActivation.php','installer/bin/activate-services.php','installer/bin/recover-services.php','installer/src/InstallationPreparation.php','installer/src/WebConfigurationPreparation.php','installer/bin/run-web-preparation.php','installer/bin/recover-web-preparation.php','installer/bin/render-web-preparation-schedule.php','installer/bin/install-web-preparation-schedule.php','installer/src/ConfigurationDraft.php','installer/src/ConfigurationApproval.php','installer/bin/approve-web-configuration.php','installer/bin/prepare-installation.php','installer/bin/prepare-web-configuration.php','installer/bin/prepare-frozen-web-configuration.php','installer/bin/run-prepared-data.php','installer/src/DataBootstrap.php','installer/bin/data-bootstrap.php','installer/src/InstallState.php','installer/src/CompletionLock.php','installer/src/InstallationCompletion.php','installer/src/InstallationApply.php','installer/src/ApplyRequest.php','installer/src/WebInstallationApply.php','installer/src/PreflightSnapshot.php','installer/src/ApplyProgress.php','installer/bin/refresh-web-preflight.php','installer/bin/render-web-preflight-schedule.php','installer/src/PreflightScheduleInstallation.php','installer/bin/install-web-preflight-schedule.php','installer/bin/render-web-apply-schedule.php','installer/bin/install-web-apply-schedule.php','installer/bin/provision-web-apply.php','installer/bin/run-web-apply.php','installer/bin/apply-installation.php','installer/bin/check-installation.php','installer/bin/complete-installation.php','installer/src/HttpAccess.php','installer/src/NativeWebConfig.php','installer/src/ProductWebConfig.php','frontend/packages/tokens/generated/tokens.css'}: return True
    return False

def validate_file(name,data,mode):
    if not allowed(name): raise ValueError('non-allowlisted member: '+name)
    if mode != (0o755 if name.startswith('bin/') else 0o644): raise ValueError('unsafe member mode: '+name)
    if SECRET.search(data): raise ValueError('secret material in payload: '+name)

def write_archive(files,output,source,version,components):
    if not re.fullmatch(r'[0-9a-f]{40}',source): raise ValueError('invalid source')
    if set(files)&META: raise ValueError('metadata supplied as payload')
    entries={}
    for name,data in sorted(files.items()):
        mode=0o755 if name.startswith('bin/') else 0o644
        validate_file(name,data,mode)
        entries[name]={'sha256':digest(data),'bytes':len(data),'mode':mode}
    sbom=encode({'bomFormat':'CycloneDX','specVersion':'1.5','version':1,'metadata':{'component':{'type':'application','name':'GoJet','version':version},'properties':[{'name':'gojet:source','value':source},{'name':'gojet:phase','value':'payload-only-not-installable'}]},'components':components})
    manifest=encode({'schema':'gojet.native-payload.v1','source':source,'version':version,'phase':'payload-only-not-installable','installable':False,'files':entries,'sbom_sha256':digest(sbom)})
    members=dict(files,**{'manifest.json':manifest,'sbom.cdx.json':sbom})
    members['SHA256SUMS']=''.join(digest(data)+'  '+name+'\n' for name,data in sorted(members.items())).encode()
    with Path(output).open('wb') as stream:
        with gzip.GzipFile(filename='',fileobj=stream,mode='wb',mtime=0) as gz:
            with tarfile.open(fileobj=gz,mode='w',format=tarfile.USTAR_FORMAT) as tar:
                for name,data in sorted(members.items()):
                    row=tarfile.TarInfo(name); row.size=len(data); row.mode=0o755 if name.startswith('bin/') else 0o644
                    row.uid=row.gid=row.mtime=0; row.uname=row.gname=''
                    tar.addfile(row,io.BytesIO(data))
    return verify_archive(output,digest(Path(output).read_bytes()),source)

def verify_archive(path,expected_sha,expected_source):
    raw=Path(path).read_bytes()
    if digest(raw)!=expected_sha: raise ValueError('archive checksum mismatch')
    files={}; total=0
    with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as tar:
        for row in tar:
            if not row.isfile() or row.name in files or row.size>128*1024*1024: raise ValueError('invalid/duplicate archive member')
            total+=row.size
            if total>1024*1024*1024: raise ValueError('archive exceeds payload limit')
            data=tar.extractfile(row).read()
            validate_file(row.name,data,row.mode)
            if row.uid or row.gid or row.mtime or row.uname or row.gname: raise ValueError('noncanonical archive metadata')
            files[row.name]=data
    if not META<=files.keys(): raise ValueError('missing package metadata')
    manifest=json.loads(files['manifest.json']); sbom=json.loads(files['sbom.cdx.json'])
    if manifest['source']!=expected_source or manifest['schema']!='gojet.native-payload.v1': raise ValueError('foreign source/schema')
    if manifest['phase']!='payload-only-not-installable' or manifest['installable'] is not False: raise ValueError('premature installable claim')
    if set(manifest['files'])!=set(files)-META: raise ValueError('unaccounted payload')
    for name,row in manifest['files'].items():
        if row!={'sha256':digest(files[name]),'bytes':len(files[name]),'mode':0o755 if name.startswith('bin/') else 0o644}: raise ValueError('member integrity mismatch')
    if digest(files['sbom.cdx.json'])!=manifest['sbom_sha256'] or sbom['bomFormat']!='CycloneDX': raise ValueError('SBOM mismatch')
    sums=''.join(digest(data)+'  '+name+'\n' for name,data in sorted(files.items()) if name!='SHA256SUMS').encode()
    if sums!=files['SHA256SUMS']: raise ValueError('checksum inventory mismatch')
    return {'source':expected_source,'archive_sha256':expected_sha,'members':len(files),'payload_bytes':total,'installable':False}
