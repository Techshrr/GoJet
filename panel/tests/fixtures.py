"""CI-only external protocol fixtures. Never copied into the runtime image."""
import json, socketserver, ssl, threading, email, subprocess, shutil
from pathlib import Path
MAIL_LOCK=threading.Lock()
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit
CTX=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);CTX.load_cert_chain('/test/cert.pem','/test/key.pem')
class External(BaseHTTPRequestHandler):
 def log_message(self,*args): pass
 def answer(self,data,status=200):
  raw=json.dumps(data).encode(); self.send_response(status);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw)
 def do_GET(self):
  if self.headers.get('X-Goog-Api-Key')!='ci-webrisk-valid-key-1234567890': return self.answer({},400)
  if 'malware' in self.path:return self.answer({'threat':{'threatTypes':['MALWARE'],'expireTime':'2099-01-01T00:00:00Z'}})
  self.answer({})
 def do_POST(self):
  data=parse_qs(self.rfile.read(int(self.headers['Content-Length'])).decode())
  token=data.get('response',[''])[0]
  self.answer({'success':data.get('secret')==['ci-turnstile-secret-1234567890'] and (token=='ci-valid-token' or token.startswith('ci-auth-')),'hostname':'www.linketic.test','action':'authentication' if token.startswith('ci-auth-') else 'install'})
class SMTP(socketserver.StreamRequestHandler):
 def handle(self):
  self.wfile.write(b'220 mail.fixture ESMTP\r\n');self.wfile.flush()
  while True:
   line=self.rfile.readline()
   if not line:return
   verb=line.split()[0].upper()
   if verb in (b'EHLO',b'HELO'):self.wfile.write(b'250-mail.fixture\r\n250-STARTTLS\r\n250 AUTH PLAIN\r\n')
   elif verb==b'STARTTLS':
    self.wfile.write(b'220 Ready\r\n');self.wfile.flush();self.connection=CTX.wrap_socket(self.connection,server_side=True);self.rfile=self.connection.makefile('rb');self.wfile=self.connection.makefile('wb')
   elif verb==b'AUTH':self.wfile.write(b'235 Authentication successful\r\n')
   elif verb==b'DATA':
    self.wfile.write(b'354 End with dot\r\n');self.wfile.flush();chunks=[];size=0
    while True:
     part=self.rfile.readline()
     if not part:return
     if part in (b'.\r\n',b'.\n'):break
     size+=len(part)
     if size>1048576:return
     chunks.append(part[1:] if part.startswith(b'..') else part)
    message=email.message_from_bytes(b''.join(chunks))
    text='\n'.join((part.get_payload(decode=True) or b'').decode('utf-8','replace') for part in message.walk() if part.get_content_type()=='text/plain')
    with MAIL_LOCK:
     with Path('/tmp/panel-mailbox.jsonl').open('a') as mailbox:mailbox.write(json.dumps({'to':message['To'],'text':text})+'\n')
    self.wfile.write(b'250 Queued\r\n')
   elif verb==b'QUIT':self.wfile.write(b'221 Bye\r\n');self.wfile.flush();return
   else:self.wfile.write(b'250 OK\r\n')
   self.wfile.flush()
# Isolate this CI TLS proxy from the production Nginx process and cache paths.
upstream=18080 if Path('/opt/linketic/native-mode').exists() else 8088
cache=Path('/tmp/fixture-proxy-cache');cache.mkdir(mode=0o700,exist_ok=True)
shutil.chown(cache,user='www-data',group='www-data')
config=Path('/tmp/fixture-proxy.conf')
config.write_text("""user www-data;
pid /tmp/fixture-proxy.pid;
error_log /tmp/fixture-proxy.log warn;
events { worker_connections 1024; }
http { access_log off; proxy_temp_path /tmp/fixture-proxy-cache; server { listen 11443 ssl; server_name www.linketic.test;
ssl_certificate /test/cert.pem; ssl_certificate_key /test/key.pem;
location / { proxy_pass http://127.0.0.1:UPSTREAM;
proxy_http_version 1.1; proxy_set_header Connection "";
proxy_set_header Host $http_host; proxy_set_header X-Forwarded-Proto https;
proxy_set_header X-Real-IP $remote_addr; }
} }
""".replace('UPSTREAM',str(upstream)))
subprocess.run(['/usr/sbin/nginx','-c',str(config),'-p','/tmp'],check=True)
server=ThreadingHTTPServer(('0.0.0.0',443),External)
server.socket=CTX.wrap_socket(server.socket,server_side=True)
threading.Thread(target=server.serve_forever,daemon=True).start()
socketserver.ThreadingTCPServer(('0.0.0.0',1587),SMTP).serve_forever()
