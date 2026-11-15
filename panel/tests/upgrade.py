"""CI-only upgrade acceptance: real released image and the same four data volumes."""
import hashlib, json, os, subprocess, tempfile, time, zipfile
from pathlib import Path
OUT=Path('artifacts/panel')
OLD='Linketic-Panel-10.0.0-rc.2-panel.2-linux-amd64.zip'
SHA='86f4278360610a9f0ff8fe9ac81220ce444641cad55c16ed4f845b005159ad98'
def run(*args, **kw):return subprocess.check_output(args,text=True,**kw).strip()
def wait(check, seconds=180):
    deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        try:
            if check():return
        except subprocess.CalledProcessError:pass
        time.sleep(2)
    raise RuntimeError('CI deployment did not become ready')
def start(image):
    args=['docker','run','-d','--name','linketic-ci','-p','127.0.0.1:18080:8088','-p','127.0.0.1:11443:11443']
    for host in ['webrisk.googleapis.com','challenges.cloudflare.com','mail.fixture']:args+=['--add-host',host+':127.0.0.1']
    args+=['-e','SITE_URL=https://www.linketic.test:11443','-e','INSTALL_ACCESS_CODE=ci-panel-access-code-1234567890','-v',str(Path('panel/tests').resolve())+':/test:ro']
    for name,path in [('app','/var/lib/linketic'),('db','/var/lib/mysql'),('redis','/var/lib/redis'),('av','/var/lib/clamav')]:args+=['-v','upgrade-'+name+':'+path]
    run(*args,image)
    wait(lambda:run('curl','--noproxy','*','-fsS','-H','Host: www.linketic.test','http://127.0.0.1:18080/install/',stderr=subprocess.DEVNULL))
    run('docker','exec','linketic-ci','cp','/test/cert.pem','/usr/local/share/ca-certificates/panel-ci.crt')
    run('docker','exec','linketic-ci','update-ca-certificates')
    run('docker','exec','-d','linketic-ci','sh','-c','python3 /test/fixtures.py > /tmp/fixtures.log 2>&1')
    wait(lambda:run('curl','--noproxy','*','-kfsS','--resolve','www.linketic.test:11443:127.0.0.1','https://www.linketic.test:11443/install/',stderr=subprocess.DEVNULL))
def private_digests():
    return json.loads(run('docker','exec','linketic-ci','python3','-c',"from pathlib import Path;import json,hashlib;p=Path('/var/lib/linketic/private');print(json.dumps({n:hashlib.sha256((p/n).read_bytes()).hexdigest() for n in ['installed.json','services.json','mysql.json']}))"))
def customer(mode):subprocess.run(['node','panel/tests/customer.mjs'],env={**os.environ,'CUSTOMER_TEST_MODE':mode},check=True)
with tempfile.TemporaryDirectory() as tmp:
    run('gh','release','download','v10.0.0-rc.2-panel.2','--repo','Techshrr/GoJet','--pattern',OLD,'--dir',tmp)
    archive=Path(tmp)/OLD
    with archive.open('rb') as f:assert hashlib.file_digest(f,'sha256').hexdigest()==SHA
    with zipfile.ZipFile(archive) as z:z.extract('linketic-image.tar',tmp)
    run('docker','load','--input',str(Path(tmp)/'linketic-image.tar'))
run('docker','stop','linketic-ci');run('docker','rm','linketic-ci')
current_browser=(OUT/'browser.json').read_bytes()
start('linketic-panel:10.0.0-rc.2-panel.2')
subprocess.run(['node','panel/tests/browser.mjs'],check=True)
(OUT/'browser-panel2.json').write_bytes((OUT/'browser.json').read_bytes())
(OUT/'browser.json').write_bytes(current_browser)
customer('before-upgrade')
before=private_digests()
run('docker','stop','linketic-ci');run('docker','rm','linketic-ci')
start('linketic-panel:ci')
names=['platformapi','redirectengine','analyticsworker','analyticsreconciler','mailworker','fileworker','operationsmonitor','logreceiver','webrisk']
wait(lambda:all(any(row.startswith(n+' ') and 'RUNNING' in row for row in run('docker','exec','linketic-ci','supervisorctl','status').splitlines()) for n in names))
assert private_digests()==before,'Upgrade must not replace installation identity, service secrets or database credentials'
customer('after-upgrade')
assert private_digests()==before
identity=json.loads(run('docker','exec','linketic-ci','/opt/gojet/panel/bin/mailworker','--version'))
assert identity['commit']==os.environ['GITHUB_SHA']
(OUT/'upgrade.json').write_text(json.dumps({'passed':True,'from':'10.0.0-rc.2-panel.2','to':'10.0.0-rc.2-panel.3','old_package_sha256':SHA,'same_four_volumes':True,'installation_and_credentials_unchanged':True,'pending_account_recovered':True,'mailworker':identity},indent=2)+'\n')
