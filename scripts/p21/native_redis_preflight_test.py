"""Real Redis ACL/replica failures; private disposable server, never the host cache."""
import copy
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time


def exercise_redis_access(call, plan):
    with tempfile.TemporaryDirectory(prefix='gojet-redis-access-') as temporary:
        root = Path(temporary)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        password, admin_password = secrets.token_hex(32), secrets.token_hex(32)
        acl = root/'users.acl'
        acl.write_text(f'user default on >{password} ~* +@all\n'
                       f'user fixture-admin on >{admin_password} ~* +@all\n')
        acl.chmod(0o600)
        config = root/'redis.conf'
        config.write_text(f'bind 127.0.0.1\nport {port}\nprotected-mode yes\n'
                          f'save ""\nappendonly no\ndir {root}\naclfile {acl}\n'
                          f'logfile {root}/redis.log\n')
        config.chmod(0o600)
        server = subprocess.Popen(['redis-server', str(config)],
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)

        def cli(*args, db=4):
            result = subprocess.run(['redis-cli', '--raw', '-h', '127.0.0.1', '-p', str(port),
                                     '--user', 'fixture-admin', '-n', str(db), *args],
                                    env=os.environ | {'REDISCLI_AUTH': admin_password},
                                    capture_output=True, text=True, timeout=5)
            if result.returncode:
                raise RuntimeError('Isolated Redis fixture command failed')
            return result.stdout.rstrip('\n')

        try:
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                if server.poll() is not None:
                    raise RuntimeError('Isolated Redis fixture exited')
                try:
                    if cli('PING') == 'PONG':
                        break
                except RuntimeError:
                    pass
                time.sleep(.1)
            else:
                raise RuntimeError('Isolated Redis fixture did not become ready')
            candidate = copy.deepcopy(plan)
            candidate['redis'] = dict(port=port, db=4, password=password)
            sentinel = 'fixture:must-survive'
            assert cli('SET', sentinel, 'selected-database') == 'OK'
            assert cli('SET', sentinel, 'default-database', db=0) == 'OK'

            def check(expected):
                report = call(candidate)
                assert report['checks']['redis'] is expected and report['ready'] is expected
                assert all(value for name, value in report['checks'].items() if name != 'redis')

            def unchanged():
                assert cli('GET', sentinel) == 'selected-database'
                assert cli('GET', sentinel, db=0) == 'default-database'
                assert cli('DBSIZE', db=0) == '1'

            check(True)
            assert cli('DBSIZE') == '1', 'Healthy probe left data behind'
            rejected = []
            for denied in ['set', 'get', 'del']:
                assert cli('ACL', 'SETUSER', 'default', 'reset', 'on', '>'+password,
                           '~*', '+@all', '-'+denied) == 'OK'
                # This is the previously accepted connectivity profile.
                ping = subprocess.run(['redis-cli', '--raw', '-h', '127.0.0.1', '-p', str(port),
                                       '-n', '4', 'PING'], env=os.environ | {'REDISCLI_AUTH': password},
                                      capture_output=True, text=True, timeout=5)
                assert ping.returncode == 0 and ping.stdout.strip() == 'PONG'
                check(False)
                unchanged()
                keys = cli('KEYS', 'gojet:installer:preflight:*').splitlines()
                if denied == 'del':
                    assert len(keys) == 1, 'Failed cleanup must leave only its bounded probe'
                    ttl = int(cli('PTTL', keys[0]))
                    assert 0 < ttl <= 15000
                    deadline = time.monotonic() + 16
                    while cli('EXISTS', keys[0]) == '1' and time.monotonic() < deadline:
                        time.sleep(.1)
                    assert cli('EXISTS', keys[0]) == '0', 'Probe did not expire independently'
                else:
                    assert not keys, 'Denied write/read left a probe behind'
                assert cli('DBSIZE') == '1'
                rejected.append(denied)
                assert cli('ACL', 'SETUSER', 'default', 'reset', 'on', '>'+password, '~*', '+@all') == 'OK'
                check(True)
                assert cli('DBSIZE') == '1'
            # A real replica still answers PING and reads but must not pass installation readiness.
            assert cli('REPLICAOF', '127.0.0.1', '1') == 'OK'
            assert 'role:slave' in cli('INFO', 'replication')
            assert cli('PING') == 'PONG'
            check(False)
            unchanged()
            assert cli('DBSIZE') == '1'
            assert cli('REPLICAOF', 'NO', 'ONE') == 'OK'
            check(True)
            unchanged()
            assert cli('DBSIZE') == '1'
            return dict(real_redis_acl_denials=rejected, read_only_replica_rejected=True,
                        selected_database_read_write_verified=True, probe_cleanup_verified=True,
                        failed_cleanup_expires=True, existing_keys_preserved=True,
                        recovered_readiness_verified=True)
        finally:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)
