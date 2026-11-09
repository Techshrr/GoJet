"""Container entrypoint. No host mounts, panel API keys, or privileged Docker required."""
import hashlib, os, pwd, secrets, subprocess
from pathlib import Path
from common import BASE, PRIVATE, PUBLIC, INBOX, SERVICES, atomic, read, origin

def mkdir(path, user='root', mode=0o700):
    p=Path(path); p.mkdir(parents=True,exist_ok=True)
    u=pwd.getpwnam(user); os.chown(p,u.pw_uid,u.pw_gid); os.chmod(p,mode)

def run(args): subprocess.run(args,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)

def nginx(installed=False, site_key=''):
    settings=read(PUBLIC/'entry.json'); host=settings['origin'].split('://')[1]
    prefix='''user www-data;
worker_processes auto;
pid /run/nginx.pid;
error_log /dev/stderr warn;
events { worker_connections 1024; }
http {
 include /etc/nginx/mime.types;
 default_type application/octet-stream;
 access_log off;
 server_tokens off;
 client_max_body_size 11m;
 server { listen 127.0.0.1:9443 ssl; server_name webrisk.internal;
 ssl_certificate /var/lib/linketic/private/tls/cert.pem;
 ssl_certificate_key /var/lib/linketic/private/tls/key.pem;
 ssl_protocols TLSv1.2 TLSv1.3;
 location = /v1/check { proxy_pass http://127.0.0.1:9089; }
 location / { return 404; }
 }
 server { listen 8088 default_server; server_name _;
'''
    # Fixed canonical host, not untrusted forwarded host/proto. Installer never available over a public container port.
    prefix+=' if ($host != '+host.split(':')[0]+') { return 421; }\n'
    prefix+=' location ^~ /install/ { proxy_pass http://127.0.0.1:9170; proxy_set_header Host '+host+'; proxy_read_timeout 20s; }\n'
    if installed:
        prefix+=' sub_filter_once on;\n sub_filter "</head>" \'<meta name="gojet-turnstile-site-key" content="'+site_key+'"></head>\';\n'
        prefix+=Path('/opt/gojet/current/static-apps.conf').read_text()
        for name in ('platform-proxy.conf','redirect-proxy.conf'):
            fragment=Path('/opt/gojet/native/deploy/nginx/'+name).read_text()
            fragment=fragment.replace('$scheme','https').replace('$http_host',host).replace('$server_port','443')
            prefix+=fragment
    else: prefix+=' location / { return 302 /install/; }\n'
    atomic('/etc/nginx/nginx.conf',(prefix+'}\n}\n').encode(),0o644)

def supervisor():
    config='''[unix_http_server]
file=/run/supervisor.sock
chmod=0700
[supervisord]
nodaemon=true
user=root
logfile=/dev/null
pidfile=/run/supervisord.pid
[rpcinterface:supervisor]
supervisor.rpcinterface_factory=supervisor.rpcinterface:make_main_rpcinterface
[supervisorctl]
serverurl=unix:///run/supervisor.sock
'''
    programs={
      'mysql':('/usr/sbin/mysqld --user=mysql --bind-address=127.0.0.1 --skip-name-resolve --log-bin-trust-function-creators=ON --init-file=/run/mysqld/init.sql','root',10,True),
      'redis':('/usr/bin/redis-server --bind 127.0.0.1 --protected-mode yes --save "" --appendonly yes --dir /var/lib/redis','redis',10,True),
      'freshclam':('/usr/bin/freshclam --daemon --foreground=true','clamav',10,True),
      'clamav':('/usr/sbin/clamd --foreground=true','clamav',20,True),
      'nginx':('/usr/sbin/nginx -g "daemon off;"','root',30,True),
      'wizard':('/usr/bin/python3 /opt/linketic/serve.py','www-data',30,True),
      'worker':('/usr/bin/python3 /opt/linketic/worker.py','root',40,True)}
    for name in SERVICES+('webrisk',): programs[name]=('/usr/bin/python3 /opt/linketic/launch.py '+name,'root',50,False)
    for name,(cmd,user,priority,auto) in programs.items():
        config+=f'\n[program:{name}]\ncommand={cmd}\nuser={user}\npriority={priority}\nautostart={str(auto).lower()}\nautorestart=true\nstartsecs=2\nstartretries=100\nstopasgroup=true\nkillasgroup=true\nstopwaitsecs=60\nstdout_logfile=/dev/stdout\nstdout_logfile_maxbytes=0\nstderr_logfile=/dev/stderr\nstderr_logfile_maxbytes=0\n'
    atomic('/etc/supervisor/supervisord.conf',config.encode(),0o600)

def main():
    mkdir(BASE,mode=0o755); mkdir(PRIVATE); mkdir(PUBLIC,'www-data',0o750); mkdir(INBOX,'www-data')
    # Public progress is root-owned, read-only to the web service. Mailbox is its only writable path.
    os.chown(PUBLIC,0,pwd.getpwnam('www-data').pw_gid)
    canonical=origin(os.environ['SITE_URL'])
    if (PUBLIC/'entry.json').exists():
        if read(PUBLIC/'entry.json')['origin']!=canonical: raise RuntimeError('SITE_URL differs from persisted site; restore the original value.')
    else:
        code=os.environ.get('INSTALL_ACCESS_CODE','')
        if len(code)<16 or code in ('CHANGE_ME_16_CHARACTERS','replace-with-your-own-install-code'): raise RuntimeError('Set a unique INSTALL_ACCESS_CODE of at least 16 characters in the panel.')
        atomic(PUBLIC/'entry.json',{'origin':canonical,'code_sha256':hashlib.sha256(code.encode()).hexdigest()},0o644)
        atomic(PUBLIC/'progress.json',{'phase':'ready','step':'等待填写安装配置','completed':[]},0o644)
    if not (PRIVATE/'mysql.json').exists(): atomic(PRIVATE/'mysql.json',{'password':secrets.token_hex(32)})
    mkdir('/run/mysqld','mysql',0o750); mkdir('/var/lib/mysql','mysql',0o750)
    if not Path('/var/lib/mysql/mysql').exists(): run(['/usr/sbin/mysqld','--initialize-insecure','--user=mysql','--datadir=/var/lib/mysql'])
    atomic('/run/mysqld/init.sql',("ALTER USER 'root'@'localhost' IDENTIFIED WITH caching_sha2_password BY '"+read(PRIVATE/'mysql.json')['password']+"';\n").encode(),0o640)
    os.chown('/run/mysqld/init.sql',0,pwd.getpwnam('mysql').pw_gid)
    for path,user,mode in [('/var/lib/redis','redis',0o700),('/var/lib/clamav','clamav',0o750),('/run/clamav','clamav',0o755),('/run/gojet-log','gojet',0o700),(BASE/'files','gojet',0o700),(BASE/'webrisk','webrisk',0o700)]: mkdir(path,user,mode)
    mkdir(PRIVATE/'tls')
    if not (PRIVATE/'tls/cert.pem').exists():
        run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','3650','-subj','/CN=webrisk.internal','-addext','subjectAltName=DNS:webrisk.internal','-keyout',str(PRIVATE/'tls/key.pem'),'-out',str(PRIVATE/'tls/cert.pem')])
    atomic('/usr/local/share/ca-certificates/linketic-internal.crt',(PRIVATE/'tls/cert.pem').read_bytes(),0o644); run(['update-ca-certificates'])
    with open('/etc/hosts','a') as f: f.write('\n127.0.0.1 webrisk.internal\n')
    installed=(PRIVATE/'installed.json').exists()
    nginx(installed,read(PRIVATE/'installed.json')['site_key'] if installed else '')
    supervisor()
    # Do not propagate panel deployment secrets to database, web, or business processes.
    os.execve('/usr/bin/supervisord',['supervisord','-c','/etc/supervisor/supervisord.conf'],{'PATH':'/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','LANG':'C.UTF-8'})
if __name__=='__main__': main()
