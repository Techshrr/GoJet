"""Root reads one service's private configuration, then permanently drops privileges."""
import errno, os, pwd, socket, stat, sys
from common import PRIVATE, SERVICES, read
name=sys.argv[1]
if name not in SERVICES+('webrisk',): raise SystemExit(2)
if name=='webrisk':
    env={'PATH':'/usr/bin:/bin','CREDENTIALS_DIRECTORY':'/var/lib/linketic/webrisk'}
    command=['/usr/bin/python3','/opt/linketic/adapter.py']
    account='webrisk'
else:
    env={'PATH':'/usr/bin:/bin',**read(PRIVATE/'services.json')[name]}
    command=['/opt/gojet/native/bin/'+name]; account='gojet'
u=pwd.getpwnam(account); os.initgroups(account,u.pw_gid); os.setgid(u.pw_gid); os.setuid(u.pw_uid)
if name=='logreceiver':
    path='/run/gojet-log/collector.sock'
    try:
        st=os.lstat(path)
        if not stat.S_ISSOCK(st.st_mode) or st.st_uid!=u.pw_uid: raise SystemExit('Unexpected log socket owner/type')
        probe=socket.socket(socket.AF_UNIX,socket.SOCK_STREAM)
        try:
            probe.connect(path)
            raise SystemExit('Log receiver already running')
        except OSError as e:
            if e.errno!=errno.ECONNREFUSED: raise
            os.unlink(path)
        finally: probe.close()
    except FileNotFoundError: pass
os.execve(command[0],command,env)
