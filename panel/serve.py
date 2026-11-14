"""Unprivileged, same-origin wizard. Only writes the fixed installation mailbox."""
import hashlib, hmac, json, secrets, time, threading
from http.cookies import SimpleCookie
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from common import INBOX, PUBLIC, atomic, read, validate
SETTINGS = read(PUBLIC / 'entry.json')
SESSIONS = {}
ATTEMPTS = []
LOCK = threading.Lock()
WEB = Path('/opt/linketic/web')
class Handler(BaseHTTPRequestHandler):
    server_version = 'Linketic'
    def log_message(self, *args): pass
    def setup(self):
        super().setup(); self.connection.settimeout(15)
    def send(self, status, value, kind='application/json', cookie=None):
        raw = value if isinstance(value, bytes) else json.dumps(value,ensure_ascii=False).encode()
        self.send_response(status)
        for k,v in {'Content-Type':kind,'Content-Length':str(len(raw)), 'Cache-Control':'no-store','X-Content-Type-Options':'nosniff','Referrer-Policy':'no-referrer','Content-Security-Policy':"default-src 'self'; script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; style-src 'self'; connect-src 'self' https://challenges.cloudflare.com; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"}.items(): self.send_header(k,v)
        if cookie: self.send_header('Set-Cookie',cookie)
        self.end_headers(); self.wfile.write(raw)
    def session(self):
        try:
            c=SimpleCookie(self.headers.get('Cookie','')); token=c['linketic_install'].value
            value=SESSIONS.get(token)
            return value if value and value['until']>time.time() else None
        except (KeyError,ValueError): return None
    def closed(self): return (PUBLIC/'complete.json').exists()
    def do_GET(self):
        if self.path=='/install/' and self.closed():
            return self.send(200,'<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>安装已完成</title><link rel="stylesheet" href="/install/style.css"><main><h1>安装已完成</h1><p>安装入口已关闭。</p><a class="button" href="/admin/login">进入管理后台</a></main></html>'.encode(),'text/html; charset=utf-8')
        if self.path == '/install/api/status':
            if not self.session(): return self.send(401,{'error':'请先输入安装访问码。'})
            status=read(PUBLIC/'progress.json')
            return self.send(200,dict(status,origin=SETTINGS['origin']))
        paths={'/install/':('index.html','text/html; charset=utf-8'),'/install/app.js':('app.js','text/javascript'),'/install/style.css':('style.css','text/css')}
        if self.path in paths:
            name,kind=paths[self.path]; return self.send(200,(WEB/name).read_bytes(),kind)
        self.send(404,{'error':'页面不存在。'})
    def do_POST(self):
        if self.closed(): return self.send(410,{'error':'安装已完成，安装入口已关闭。'})
        if self.headers.get('Origin') != SETTINGS['origin']: return self.send(403,{'error':'请从配置的 HTTPS 域名访问。'})
        if self.headers.get('Content-Type') != 'application/json': return self.send(415,{'error':'请求格式无效。'})
        try:
            length=int(self.headers.get('Content-Length','0'))
            if not 0<length<=16384: raise ValueError()
            data=json.loads(self.rfile.read(length))
        except (ValueError,TimeoutError): return self.send(400,{'error':'请求格式无效。'})
        with LOCK:
            if self.path == '/install/api/login':
                now=time.time(); ATTEMPTS[:]=[t for t in ATTEMPTS if t>now-60]
                if len(ATTEMPTS)>=10: return self.send(429,{'error':'尝试过于频繁，请一分钟后重试。'})
                ATTEMPTS.append(now)
                code=data.get('code','') if isinstance(data,dict) else ''
                if not isinstance(code,str) or not hmac.compare_digest(hashlib.sha256(code.encode()).hexdigest(),SETTINGS['code_sha256']): return self.send(401,{'error':'安装访问码不正确。'})
                for k in list(SESSIONS):
                    if SESSIONS[k]['until']<=now: del SESSIONS[k]
                token=secrets.token_hex(32); csrf=secrets.token_hex(32)
                SESSIONS[token]={'until':now+7200,'csrf':csrf}
                return self.send(200,{'csrf':csrf,'origin':SETTINGS['origin']},cookie='linketic_install='+token+'; Path=/install/; HttpOnly; Secure; SameSite=Strict; Max-Age=7200')
            s=self.session()
            if not s or not hmac.compare_digest(self.headers.get('X-CSRF-Token',''),s['csrf']): return self.send(403,{'error':'安装会话已过期，请重新验证。'})
            if self.path == '/install/api/retry':
                if read(PUBLIC/'progress.json')['phase'] != 'failed': return self.send(409,{'error':'当前步骤不需要重试。'})
                atomic(INBOX/'retry.json',{'retry':True})
                return self.send(202,{'queued':True})
            if self.path != '/install/api/configure': return self.send(404,{'error':'页面不存在。'})
            state=read(PUBLIC/'progress.json')
            if state['phase'] not in ('ready','preflight_failed') or (INBOX/'request.json').exists(): return self.send(409,{'error':'安装请求已接收，请查看当前进度。'})
            try: validate(data)
            except ValueError as e: return self.send(400,{'error':str(e)})
            atomic(INBOX/'request.json',data)
            return self.send(202,{'queued':True})
if __name__=='__main__':
    ThreadingHTTPServer(('127.0.0.1',9170),Handler).serve_forever()
