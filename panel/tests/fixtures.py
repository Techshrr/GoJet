"""CI-only external protocol fixtures. Never copied into the runtime image."""
import http.client, json, smtplib, socketserver, ssl, threading
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
  self.answer({'success':data.get('secret')==['ci-turnstile-secret-1234567890'] and data.get('response')==['ci-valid-token'],'hostname':'www.linketic.test','action':'install'})
class Proxy(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def handle_request(self):
  c=http.client.HTTPConnection('127.0.0.1',8088,timeout=20)
  body=self.rfile.read(int(self.headers.get('Content-Length','0')))
  c.request(self.command,self.path,body,dict(self.headers));r=c.getresponse();raw=r.read();self.send_response(r.status)
  for k,v in r.getheaders():
   if k.lower() not in ('connection','transfer-encoding','content-length','server','date'):self.send_header(k,v)
  self.send_header('Content-Length',str(len(raw)));self.end_headers();self.wfile.write(raw);c.close()
 do_GET=do_POST=handle_request
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
   elif verb==b'QUIT':self.wfile.write(b'221 Bye\r\n');self.wfile.flush();return
   else:self.wfile.write(b'250 OK\r\n')
   self.wfile.flush()
for port,handler in [(443,External),(11443,Proxy)]:
 server=ThreadingHTTPServer(('0.0.0.0',port),handler);server.socket=CTX.wrap_socket(server.socket,server_side=True);threading.Thread(target=server.serve_forever,daemon=True).start()
socketserver.ThreadingTCPServer(('0.0.0.0',1587),SMTP).serve_forever()
