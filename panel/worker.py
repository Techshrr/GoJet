"""Fixed-operation root worker, confined to this application's container and volumes."""
import hashlib, json, os, pwd, secrets, smtplib, ssl, subprocess, time, urllib.parse, urllib.request
from pathlib import Path
from common import BASE, PRIVATE, PUBLIC, INBOX, SERVICES, atomic, read, validate
from init import nginx
STEPS=['配置检查','连接外部服务','准备数据库与扫描器','初始化数据与管理员','启动业务服务','健康检查','安装完成']
COMPLETED=[]
class StepError(Exception): pass

def progress(phase,step,error=None):
    atomic(PUBLIC/'progress.json',{'phase':phase,'step':step,'completed':list(COMPLETED),'error':error},0o644)

def command(args,env=None,timeout=120):
    r=subprocess.run(args,env=env,capture_output=True,timeout=timeout)
    if r.returncode: raise StepError('后台步骤执行失败，未宣布安装成功。')
    return r.stdout

def supervisor(action,name): return command(['supervisorctl',action,name],timeout=80)

def wait(predicate,seconds,message):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        try:
            if predicate(): return
        except Exception: pass
        time.sleep(2)
    raise StepError(message)

def upstream(d):
    try:
        with smtplib.SMTP(d['smtp_host'],int(d['smtp_port']),timeout=15) as client:
            client.ehlo(); client.starttls(context=ssl.create_default_context()); client.ehlo()
            if d['smtp_user']: client.login(d['smtp_user'],d['smtp_password'])
    except Exception: raise StepError('SMTP 连接、STARTTLS 或账户验证失败。请检查主机、端口和邮箱密码。') from None
    try:
        body=urllib.parse.urlencode({'secret':d['turnstile_secret'],'response':d['turnstile_token']}).encode()
        req=urllib.request.Request('https://challenges.cloudflare.com/turnstile/v0/siteverify',data=body)
        with urllib.request.urlopen(req,timeout=20) as r: result=json.loads(r.read(8192))
        if not result.get('success') or result.get('hostname')!=urllib.parse.urlsplit(read(PUBLIC/'entry.json')['origin']).hostname or result.get('action')!='install': raise ValueError()
    except Exception: raise StepError('Turnstile 验证失败。请检查域名、Site key 和 Secret key，然后重新完成人机验证。') from None
    try:
        import adapter
        client=adapter.GoogleLookup(d['webrisk_key'])
        status,clean=client.check('https://www.google.com/')
        # Actual API behavior is verified here; no invented fallback allow verdict.
        if status!=200 or clean.get('verdict')!='allow': raise ValueError()
    except Exception: raise StepError('Google Web Risk 查询失败。请检查密钥、API 限制、服务端出口 IP 和账单状态。') from None

def prepare(d):
    if (PRIVATE/'configuration.json').exists(): return
    native={'schema':'gojet.native-configuration.v1','origin':read(PUBLIC/'entry.json')['origin'],
      'database':{'port':3306,'name':'gojet','user':'gojet_app','password':secrets.token_hex(32)},
      'redis':{'port':6379,'db':0,'password':''},
      'secrets':{'key_id':'panel-v1',**{k:secrets.token_hex(32) for k in ('csrf','grant','oauth','admin','file_public','text_public')}},
      'files':{'root':str(BASE/'files'),'clamav_socket':'/run/clamav/clamd.ctl','max_upload_bytes':10485760,'max_files':1000,'max_bytes':1073741824,'types':{'pdf':['application/pdf'],'png':['image/png'],'jpg':['image/jpeg']}},
      'quotas':{'text':1000,'bio':100,'qr':1000},
      'smtp':{'host':d['smtp_host'],'port':int(d['smtp_port']),'from':d['smtp_from'],'username':d['smtp_user'],'password':d['smtp_password']},
      'risk':{'name':'google-web-risk','endpoint':'https://webrisk.internal:9443/v1/check','policy_version':'google-webrisk-v1'},
      'turnstile_secret':d['turnstile_secret'],'auth_rate':{'limit':10,'window_seconds':60}}
    atomic(PRIVATE/'configuration.json',{'native':native,'admin':{'email':d['admin_email'],'display_name':d['admin_name'],'password':d['admin_password']},'site_key':d['turnstile_site_key']})
    atomic(BASE/'webrisk/api-key',(d['webrisk_key']+'\n').encode())
    u=pwd.getpwnam('webrisk'); os.chown(BASE/'webrisk/api-key',u.pw_uid,u.pw_gid)

def database_ready():
    script="<?php $d=json_decode(file_get_contents('/var/lib/linketic/private/mysql.json'),true); new PDO('mysql:unix_socket=/run/mysqld/mysqld.sock','root',$d['password']);"
    return subprocess.run(['php8.3'],input=script.encode(),capture_output=True).returncode==0

def file_ready():
    env={'PATH':'/usr/bin:/bin','GOJET_FILE_STORAGE_ROOT':str(BASE/'files'),'GOJET_CLAMAV_NETWORK':'unix','GOJET_CLAMAV_ADDRESS':'/run/clamav/clamd.ctl'}
    return subprocess.run(['runuser','-u','gojet','--','/opt/gojet/native/bin/filepreflight'],env=env,capture_output=True,timeout=8).returncode==0

def running():
    for name in SERVICES+('webrisk',):
        if b'RUNNING' not in supervisor('status',name): return False
    return True

def health():
    if not running() or not file_ready(): return False
    req=urllib.request.Request('http://127.0.0.1:8081/api/admin/auth/session')
    try: urllib.request.urlopen(req,timeout=5); return False
    except urllib.error.HTTPError as e:
        if e.code!=401: return False
    request=urllib.request.Request('https://webrisk.internal:9443/v1/check',data=json.dumps({'target':'https://www.google.com/'}).encode(),headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(request,timeout=15) as r:
        if json.load(r).get('verdict')!='allow': return False
    return True

def start_services():
    for name in ('webrisk',)+SERVICES:
        state=subprocess.run(['supervisorctl','status',name],capture_output=True).stdout
        if b'RUNNING' not in state and b'STARTING' not in state: supervisor('start',name)

def install():
    config=read(PRIVATE/'configuration.json')
    progress('installing',STEPS[2]); wait(database_ready,120,'数据库尚未就绪。')
    # Freshclam can take several minutes on the first boot; progress stays visible.
    wait(file_ready,600,'病毒库或扫描器未就绪，请检查面板日志中的下载错误。')
    COMPLETED.append(STEPS[2]); progress('installing',STEPS[3])
    command(['php8.3','/opt/linketic/bridge.php','database'],timeout=300)
    command(['php8.3','/opt/linketic/bridge.php','finish'])
    COMPLETED.append(STEPS[3]); progress('installing',STEPS[4]); start_services()
    COMPLETED.append(STEPS[4]); progress('installing',STEPS[5]); wait(health,90,'业务服务健康检查未通过，站点尚未开放。')
    nginx(True,config['site_key']); command(['nginx','-t']); command(['nginx','-s','reload'])
    # Seal after real data/service health, then retire all write endpoints.
    atomic(PRIVATE/'installed.json',{'schema':'gojet.panel-install.v1','site_key':config['site_key'],'backend_source':'ce305a9207fd2fe46396e7cf433137c48ab5aec1','installed_at':int(time.time())})
    atomic(PUBLIC/'complete.json',{'complete':True},0o644)
    # Bootstrap admin password is no longer needed after successful creation.
    (PRIVATE/'configuration.json').unlink()
    if (PRIVATE/'data/request.json').exists(): (PRIVATE/'data/request.json').unlink()
    if (PRIVATE/'accepted.json').exists(): (PRIVATE/'accepted.json').unlink()
    COMPLETED.extend(STEPS[5:]); progress('complete',STEPS[-1])

def main():
    if (PRIVATE/'installed.json').exists():
        wait(database_ready,120,'数据库恢复中。'); start_services()
        atomic(PUBLIC/'complete.json',{'complete':True},0o644)
        progress('complete',STEPS[-1])
    elif (PRIVATE/'configuration.json').exists() and (PRIVATE/'accepted.json').exists():
        # Native journal prevents repeating a partial DDL transaction.
        try: install()
        except Exception: progress('failed','安装中断','安装未完成；已保留数据和原始日志，未重复初始化。')
    while True:
        if (INBOX/'retry.json').exists():
            (INBOX/'retry.json').unlink()
            if not (PRIVATE/'installed.json').exists() and (PRIVATE/'accepted.json').exists():
                try: install()
                except Exception as error: progress('failed','安装未完成',str(error) if isinstance(error,StepError) else '后台步骤未完成，已保留现场。')
        request=INBOX/'request.json'
        if request.exists() and not (PRIVATE/'installed.json').exists():
            phase=read(PUBLIC/'progress.json')['phase']
            if phase not in ('ready','preflight_failed'): request.unlink(); continue
            progress('checking',STEPS[0])
            try:
                fd=os.open(request,os.O_RDONLY|os.O_NOFOLLOW)
                with os.fdopen(fd) as f: d=validate(json.loads(f.read(16385)))
                COMPLETED.clear(); prepare(d)
                command(['php8.3','/opt/linketic/bridge.php','validate'])
                COMPLETED.append(STEPS[0]); progress('checking',STEPS[1]); upstream(d)
                COMPLETED.append(STEPS[1]); atomic(PRIVATE/'accepted.json',{'accepted':True})
            except Exception as error:
                if (PRIVATE/'configuration.json').exists(): (PRIVATE/'configuration.json').unlink()
                request.unlink(missing_ok=True)
                progress('preflight_failed',STEPS[1],str(error) if isinstance(error,(ValueError,StepError)) else '配置检查失败，请核对填写内容。')
                continue
            request.unlink(missing_ok=True)
            try: install()
            except Exception as error: progress('failed','安装未完成',str(error) if isinstance(error,StepError) else '后台安装未完成，已保留现场。')
        time.sleep(2)
if __name__=='__main__': main()
