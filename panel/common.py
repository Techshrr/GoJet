"""Container-only installer contract; never imports host panel configuration."""
import json, os, re, tempfile
from pathlib import Path
from urllib.parse import urlsplit
BASE = Path('/var/lib/linketic')
PRIVATE = BASE / 'private'
INBOX = BASE / 'inbox'
PUBLIC = BASE / 'status'
SERVICES = ('logreceiver','platformapi','redirectengine','analyticsworker','analyticsreconciler','mailworker','fileworker','operationsmonitor')

def atomic(path, value, mode=0o600):
    path = Path(path)
    raw = value if isinstance(value, bytes) else json.dumps(value, ensure_ascii=False).encode()
    fd, name = tempfile.mkstemp(prefix='.new-', dir=path.parent)
    try:
        os.fchmod(fd, mode)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(raw); stream.flush(); os.fsync(stream.fileno())
        os.replace(name, path)
        d = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try: os.fsync(d)
        finally: os.close(d)
    finally:
        if os.path.exists(name): os.unlink(name)

def read(path): return json.loads(Path(path).read_text())

def origin(value):
    u = urlsplit(value)
    if not re.fullmatch(r'https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?', value) or not u.hostname or '.' not in u.hostname:
        raise ValueError('站点地址必须为 HTTPS 域名，不带末尾斜杠。')
    if not 1 <= (u.port or 443) <= 65535: raise ValueError('站点端口无效。')
    return value

def validate(d):
    fields = {'admin_email','admin_name','admin_password','smtp_host','smtp_port','smtp_from','smtp_user','smtp_password','turnstile_site_key','turnstile_secret','turnstile_token','webrisk_key'}
    if not isinstance(d, dict) or set(d) != fields: raise ValueError('安装配置字段不完整。')
    if any(not isinstance(v, str) or len(v.encode()) > 2048 or any(ord(c)<32 or ord(c)==127 for c in v) for v in d.values()): raise ValueError('配置含有无效字符或过长内容。')
    for key in ('admin_email','smtp_from'):
        if not re.fullmatch(r'[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+', d[key]) or len(d[key])>254: raise ValueError('请输入有效邮箱地址。')
    if not 12 <= len(d['admin_password']) <= 128 or not 1 <= len(d['admin_name']) <= 128: raise ValueError('管理员名称必填，密码为 12–128 个字符。')
    if not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?',d['smtp_host']): raise ValueError('SMTP 主机名无效。')
    if not d['smtp_port'].isdigit() or not 1<=int(d['smtp_port'])<=65535: raise ValueError('SMTP 端口无效。')
    if bool(d['smtp_user']) != bool(d['smtp_password']): raise ValueError('SMTP 用户名和密码须同时填写。')
    for key in ('turnstile_site_key','turnstile_secret','webrisk_key'):
        if not re.fullmatch(r'[A-Za-z0-9_-]{20,256}',d[key]): raise ValueError('请检查 Turnstile 与 Web Risk 密钥。')
    if not d['turnstile_token']: raise ValueError('请先完成人机验证。')
    return d
