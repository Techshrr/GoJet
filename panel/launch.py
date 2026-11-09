"""Root reads one service's private configuration, then permanently drops privileges."""
import os, pwd, sys
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
os.execve(command[0],command,env)
