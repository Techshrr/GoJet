"""Build a Docker-free runtime archive from the exact tested build filesystem.
Docker is a build tool only; the recipient uses systemd and Python 3.
"""
import argparse, hashlib, json, os, shutil, subprocess, tarfile, tempfile, zipfile
from pathlib import Path
VERSION='10.0.0-rc.2-panel.4'
def run(*args):return subprocess.check_output(args,text=True).strip()
def sha(p):
    with p.open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def main():
    p=argparse.ArgumentParser();p.add_argument('--output',required=True);a=p.parse_args();out=Path(a.output);out.mkdir(parents=True,exist_ok=True)
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
    (stage/'native-release.json').write_text(json.dumps({'schema':'linketic.native-runtime.v1','version':VERSION,'source':os.environ['GITHUB_SHA'],'runtime_sha256':sha(stage/'linketic-runtime.tar.gz'),'requires_docker':False,'service_manager':'systemd','data_directory':'/var/lib/linketic-native'},indent=2)+'\n')
    (stage/'SHA256SUMS').write_text(''.join(sha(f)+'  '+f.name+'\n' for f in sorted(stage.iterdir())))
    archive=out/('Linketic-Native-'+VERSION+'-linux-amd64.zip')
    with zipfile.ZipFile(archive,'x',zipfile.ZIP_STORED) as z:
        for f in stage.iterdir():z.write(f,f.name)
    archive.with_suffix('.zip.sha256').write_text(sha(archive)+'  '+archive.name+'\n')
    print(archive)
if __name__=='__main__':main()
