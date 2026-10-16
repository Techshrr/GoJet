"""Run the shipped platformapi local mode against native MySQL/Redis, without HTTP bootstrap."""
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys

binary = sys.argv[1]
password = secrets.token_urlsafe(32)
email = 'initial-admin@p21.test'
request = json.dumps(dict(email=email, display_name='P21 initial administrator', password=password,
                          correlation_id='p21-native-bootstrap')).encode()
keys = ['GOJET_MYSQL_DSN', 'GOJET_REDIS_ADDR', 'GOJET_REDIS_DB', 'GOJET_ADMIN_ACCESS_ENABLED',
        'GOJET_ADMIN_TOTP_KEY_ID', 'GOJET_ADMIN_TOTP_KEY_HEX', 'GOJET_ADMIN_ALLOWED_ORIGIN']
prefix = ['sudo', '--preserve-env='+','.join(keys)]


def call(data, root=True):
    return subprocess.run((prefix if root else [])+[binary, '--bootstrap-admin'],
                          input=data, capture_output=True, timeout=45)


def refused(result):
    assert result.returncode != 0 and result.stdout == b''
    assert result.stderr == b'initial administrator creation refused\n', 'Unredacted diagnostic'


def sql(query):
    return subprocess.check_output(['mysql', '-h127.0.0.1', '-ugojet_test', '-N', '-B',
        'gojet_p21_admin', '-e', query], env=os.environ | {'MYSQL_PWD': 'p21-ci-only'}, text=True).strip()


refused(call(request, root=False))
refused(call(b'{broken'))
refused(call(b'x'*4097))
refused(call(request[:-1]+b',"permissions":["arbitrary"]}'))
# Hold the same database-scoped lock in a separate live MySQL connection.
lock = 'gojet-admin-bootstrap-'+hashlib.sha256(b'gojet_p21_admin').digest()[:16].hex()
holder = subprocess.Popen(['mysql', '-h127.0.0.1', '-ugojet_test', '-N', '-B', '--unbuffered',
    'gojet_p21_admin'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
    env=os.environ | {'MYSQL_PWD': 'p21-ci-only'}, text=True)
try:
    holder.stdin.write("SELECT GET_LOCK('"+lock+"',0);\n")
    holder.stdin.flush()
    assert holder.stdout.readline().strip() == '1'
    refused(call(request))
    assert sql('SELECT COUNT(*) FROM admin_administrators') == '0'
finally:
    holder.stdin.close()
    holder.wait(timeout=5)
    holder.stdout.close()
result = call(request)
assert result.returncode == 0 and result.stdout == b'{"created":true}\n' and not result.stderr
refused(call(request))
assert sql('SELECT COUNT(*) FROM admin_administrators') == '1'
assert sql("SELECT COUNT(*) FROM admin_credentials WHERE password_algorithm='pbkdf2-sha256'") == '1'
assert sql("SELECT COUNT(*) FROM admin_audit_events WHERE actor_kind='system' AND actor_id='installer' AND action='admin.bootstrap' AND result='success'") == '1'
assert sql('SELECT COUNT(*) FROM admin_role_permissions') == '16'
assert sql('SELECT COUNT(*) FROM admin_role_assignments') == '1'
assert sql('SELECT COUNT(*) FROM admin_roles') == '1'
# Validate the inherited PBKDF2 result without exporting its salt/hash or the password.
import base64
stored = sql('SELECT password_hash FROM admin_credentials')
algorithm, rounds, salt, derived = stored.split('$')
decode = lambda value: base64.urlsafe_b64decode(value+'='*((-len(value))%4))
assert algorithm == 'pbkdf2-sha256' and int(rounds) == 600000
assert hashlib.pbkdf2_hmac('sha256', password.encode(), decode(salt), int(rounds)) == decode(derived)
print(json.dumps({'native_admin_bootstrap': True, 'root_only': True, 'stdin_bounded': True,
    'advisory_lock_rejected': True, 'single_creation': True, 'duplicate_refused': True,
    'inherited_password_hash': True, 'inherited_permissions': True, 'inherited_audit': True,
    'credentials_not_emitted': True, 'http_bootstrap_exposed': False,
    'complete_privileged_worker_verified': False}))
