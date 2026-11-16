"""Build a Docker-free runtime archive from the exact tested build filesystem.
Docker is a build tool only; the recipient uses systemd and Python 3.
"""
import argparse, hashlib, json, os, shutil, subprocess, tarfile, tempfile, zipfile
from pathlib import Path
VERSION='10.0.0-rc.2-panel.4'
def run(*args):return subprocess.check_output(args,text=True).strip()
def sha(p):
    with p.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def archive_stage(out,stage):
    checksum=stage/'SHA256SUMS'
    checksum.write_text(''.join(sha(f)+'  '+f.name+'\n' for f in sorted(stage.iterdir()) if f!=checksum))
    archive=out/('Linketic-Native-'+VERSION+'-linux-amd64.zip')
    temporary=archive.with_suffix('.zip.tmp')
    with zipfile.ZipFile(temporary,'w',zipfile.ZIP_STORED) as z:
        for f in stage.iterdir():z.write(f,f.name)
    with zipfile.ZipFile(temporary) as z:
        assert z.testzip() is None
        for line in z.read('SHA256SUMS').decode().splitlines():
            expected,name=line.split('  ',1)
            with z.open(name) as stream:assert hashlib.file_digest(stream,'sha256').hexdigest()==expected
    temporary.replace(archive)
    archive.with_suffix('.zip.sha256').write_text(sha(archive)+'  '+archive.name+'\n')
    print(archive)
def main():
    p=argparse.ArgumentParser();p.add_argument('--output',required=True);p.add_argument('--finalize',action='store_true');a=p.parse_args();out=Path(a.output);out.mkdir(parents=True,exist_ok=True)
    if a.finalize:
        stage=out/'native'
        manifest=json.loads((stage/'native-release.json').read_text())
        assert manifest['source']==os.environ['GITHUB_SHA']
        assert sha(stage/'linketic-runtime.tar.gz')==manifest['runtime_sha256']
        evidence=json.loads(Path('artifacts/panel/native-acceptance.json').read_text());assert evidence['passed'] is True
        for name in ['native-acceptance.json','native-browser.json','native-customer-full.json']:
            shutil.copyfile(Path('artifacts/panel')/name,stage/name)
        archive_stage(out,stage);return
    stage=out/'native';stage.mkdir(exist_ok=False)
    container=run('docker','create','linketic-panel:ci')
    try:
        with tempfile.TemporaryDirectory() as tmp:
            exported=Path(tmp)/'rootfs.tar';run('docker','export','-o',str(exported),container)
            with tarfile.open(exported) as source,tarfile.open(stage/'linketic-runtime.tar.gz','w:gz',compresslevel=6) as target:
                for member in source:
                    if member.name.split('/')[0] in ('dev','proc','sys','run','test') or member.isdev():continue
                    target.addfile(member,source.extractfile(member) if member.isfile() else None)
    finally:run('docker','rm',container)
    shutil.copyfile('native/install.py',stage/'install.py');shutil.copyfile('native/INSTALL-ZH.md',stage/'INSTALL-ZH.md')
    (stage/'native-release.json').write_text(json.dumps({'schema':'linketic.native-runtime.v1','version':VERSION,'source':os.environ['GITHUB_SHA'],'runtime_sha256':sha(stage/'linketic-runtime.tar.gz'),'workflow':'https://github.com/Techshrr/GoJet/actions/runs/'+os.environ['GITHUB_RUN_ID'],'requires_docker':False,'service_manager':'systemd','data_directory':'/var/lib/linketic-native'},indent=2)+'\n')
    archive_stage(out,stage)
if __name__=='__main__':main()
