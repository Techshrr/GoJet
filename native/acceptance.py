"""CI-only systemd acceptance, run on an ephemeral GitHub Ubuntu VM."""
import json,os,subprocess,time
from pathlib import Path

def run(*args,**kw):return subprocess.check_output(args,text=True,**kw).strip()
def inside(*args):
    pid=run('systemctl','show','linketic.service','-p','MainPID','--value')
    return run('sudo','nsenter','--target',pid,'--mount','--root','--wd','--',*args)
def wait(check,seconds=180):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        try:
            if check():return
        except subprocess.CalledProcessError:pass
        time.sleep(2)
    raise RuntimeError('Native startup timed out')
root=Path('/opt/linketic-native/runtime')
package=Path(os.environ['RUNNER_TEMP'])/'linketic-native-delivery/native'
run('docker','stop','linketic-ci')
run('sudo','systemctl','stop','docker.service','docker.socket')
assert subprocess.run(['systemctl','is-active','--quiet','docker.service']).returncode!=0
run('sudo','env','INSTALL_ACCESS_CODE=CiNativeAccessCode1234567890','python3',str(package/'install.py'),'--site-url','https://www.linketic.test:11443')
wait(lambda:'RUNNING' in inside('/usr/bin/supervisorctl','status','wizard'))
run('sudo','cp','-r','panel/tests',str(root/'test'))
inside('/usr/bin/python3','-c',"from pathlib import Path;p=Path('/etc/hosts');p.write_text(p.read_text()+'\\n127.0.0.1 webrisk.googleapis.com challenges.cloudflare.com mail.fixture\\n')")
inside('/usr/bin/cp','/test/cert.pem','/usr/local/share/ca-certificates/panel-ci.crt');inside('/usr/sbin/update-ca-certificates')
# A supervised fixture keeps its lifetime independent of the command shell.
inside('/usr/bin/python3','-c',"import subprocess;subprocess.Popen(['/usr/bin/python3','/test/fixtures.py'],stdin=subprocess.DEVNULL,stdout=open('/tmp/fixtures.log','w'),stderr=subprocess.STDOUT,start_new_session=True)")
wait(lambda:run('curl','--noproxy','*','-kfsS','--resolve','www.linketic.test:11443:127.0.0.1','https://www.linketic.test:11443/install/',stderr=subprocess.DEVNULL))
artifact=Path('artifacts/panel');saved={f.name:f.read_bytes() for f in artifact.glob('*') if f.is_file()}
try:
    for script in ['browser.mjs','customer.mjs']:subprocess.run(['node','panel/tests/'+script],env={**os.environ,'NATIVE_TEST':'1'},check=True)
    run('sudo','systemctl','restart','linketic.service')
    names=['platformapi','redirectengine','analyticsworker','analyticsreconciler','mailworker','fileworker','operationsmonitor','logreceiver','webrisk']
    wait(lambda:all(any(r.startswith(n+' ') and 'RUNNING' in r for r in inside('/usr/bin/supervisorctl','status').splitlines()) for n in names))
    assert subprocess.run(['systemctl','is-active','--quiet','docker.service']).returncode!=0
    assert json.loads(inside('/usr/bin/cat','/var/lib/linketic/private/installed.json'))['schema']=='gojet.panel-install.v1'
    (artifact/'native-acceptance.json').write_text(json.dumps({'passed':True,'host':'Ubuntu 24.04 x86_64','manager':'systemd','docker_service_stopped':True,'browser_installation':True,'customer_flows':True,'restart_persistence':True,'services':names},indent=2)+'\n')
finally:
    for f in list(artifact.glob('*')):
        if f.name in saved and f.read_bytes()!=saved[f.name]:f.rename(artifact/('native-'+f.name))
    for name,data in saved.items():(artifact/name).write_bytes(data)
    run('sudo','systemctl','stop','linketic.service')
    run('sudo','systemctl','start','docker.service')
