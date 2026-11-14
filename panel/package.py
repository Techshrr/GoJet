"""Package only the image that passed this workflow's installation and restart checks."""
import argparse, hashlib, json, os, re, shutil, subprocess, zipfile
from pathlib import Path
VERSION='10.0.0-rc.2-panel.1'
IMAGE='linketic-panel:'+VERSION
SERVICES=('platformapi','redirectengine','analyticsworker','analyticsreconciler','mailworker','fileworker','operationsmonitor','logreceiver','webrisk')
def run(*args):return subprocess.check_output(args,text=True).strip()
def digest(path):
    with path.open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()
def main():
    parser=argparse.ArgumentParser();parser.add_argument('--output',required=True);args=parser.parse_args()
    source=os.environ['GITHUB_SHA'];run_id=os.environ['GITHUB_RUN_ID']
    if not re.fullmatch('[a-f0-9]{40}',source) or not run_id.isdigit():raise SystemExit('Invalid build identity')
    browser=json.loads(Path('artifacts/panel/browser.json').read_text());assert browser['passed'] is True
    processes=Path('artifacts/panel/processes-after-restart.txt').read_text().splitlines()
    assert all(any(row.startswith(name+' ') and 'RUNNING' in row for row in processes) for name in SERVICES)
    out=Path(args.output);out.mkdir(parents=True,exist_ok=False);stage=out/'content';stage.mkdir()
    subprocess.run(['docker','tag','linketic-panel:ci',IMAGE],check=True)
    image_id=run('docker','image','inspect','--format','{{.Id}}',IMAGE)
    subprocess.run(['docker','save','--output',str(stage/'linketic-image.tar'),IMAGE],check=True)
    # Verify the exported archive imports back to the exact tested image identity.
    subprocess.run(['docker','load','--input',str(stage/'linketic-image.tar')],check=True,stdout=subprocess.DEVNULL)
    assert run('docker','image','inspect','--format','{{.Id}}',IMAGE)==image_id
    compose=Path('panel/compose.template.yml').read_text()
    old='    build:\n      context: https://github.com/Techshrr/GoJet.git#SOURCE_COMMIT\n      dockerfile: panel/Dockerfile\n'
    assert compose.count(old)==1
    (stage/'docker-compose.yml').write_text(compose.replace(old,'    image: '+IMAGE+'\n    pull_policy: never\n'))
    subprocess.run(['docker','compose','-f',str(stage/'docker-compose.yml'),'config','--quiet'],check=True)
    shutil.copyfile('panel/INSTALL-ZH.md',stage/'INSTALL-ZH.md')
    release={'schema':'linketic.panel-release.v1','version':VERSION,'platform':'linux/amd64','source':source,'backend_source':'ce305a9207fd2fe46396e7cf433137c48ab5aec1','image':IMAGE,'image_id':image_id,'workflow':'https://github.com/Techshrr/GoJet/actions/runs/'+run_id,'browser':browser,'restart_services':list(SERVICES),'distribution_archive_roundtrip':True,'panel_ui_import_verified':False,'onepanel_network_verified':False}
    (stage/'release.json').write_text(json.dumps(release,ensure_ascii=False,indent=2)+'\n')
    (stage/'SHA256SUMS').write_text(''.join(digest(p)+'  '+p.name+'\n' for p in sorted(stage.iterdir())))
    archive=out/('Linketic-Panel-'+VERSION+'-linux-amd64.zip')
    with zipfile.ZipFile(archive,'x',zipfile.ZIP_DEFLATED,compresslevel=6) as z:
        for path in sorted(stage.iterdir()):z.write(path,path.name)
    # ZIP CRCs and independently stored member SHA256 checks must both survive compression.
    with zipfile.ZipFile(archive) as z:
        assert z.testzip() is None
        for line in z.read('SHA256SUMS').decode().splitlines():
            expected,name=line.split('  ',1)
            with z.open(name) as stream:assert hashlib.file_digest(stream,'sha256').hexdigest()==expected
    assert archive.stat().st_size<2_000_000_000
    archive.with_suffix('.zip.sha256').write_text(digest(archive)+'  '+archive.name+'\n')
    shutil.rmtree(stage)
    print(json.dumps({'package':archive.name,'bytes':archive.stat().st_size,'sha256':digest(archive),'image_id':image_id,'source':source}))
if __name__=='__main__':main()
