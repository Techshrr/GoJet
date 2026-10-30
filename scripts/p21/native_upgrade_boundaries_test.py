"""Actual Nginx maintenance and Redis projection boundary; no systemd simulation."""
import http.client
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
PHP = json.loads(os.environ.get('P21_PHP_COMMAND', '["php8.3"]'))


def free_port():
    with socket.socket() as probe:
        probe.bind(('127.0.0.1', 0))
        return probe.getsockname()[1]


def ready(process, port):
    deadline = time.monotonic()+10
    while time.monotonic()<deadline:
        assert process.poll() is None, 'Native fixture exited before readiness'
        with socket.socket() as probe:
            if probe.connect_ex(('127.0.0.1', port))==0:
                return
        time.sleep(.05)
    raise AssertionError('Native fixture readiness timed out')


def php(code, *args):
    result = subprocess.run(PHP+['-r', code, str(ROOT), *args], capture_output=True, text=True, timeout=30)
    assert result.returncode==0 and not result.stderr, 'PHP upgrade boundary failed: '+result.stderr
    return result.stdout


def main():
    assert os.geteuid()==0
    processes=[]
    with tempfile.TemporaryDirectory(prefix='gojet-upgrade-boundary-') as temporary:
        root=Path(temporary)
        try:
            token=secrets.token_hex(32)
            gate=root/'gate.conf'
            gate.write_text(php('require $argv[1]."/installer/src/UpgradeMaintenance.php"; '
                'echo GoJet\\Installer\\UpgradeMaintenance::render($argv[2]);',token))
            gate.chmod(0o600)
            port=free_port()
            nginx=root/'nginx.conf'
            nginx.write_text(f'user root; daemon off; master_process off; pid {root}/nginx.pid; '
                f'error_log {root}/nginx.log; events {{}} http {{ access_log off; '
                f'client_body_temp_path {root}/body; proxy_temp_path {root}/proxy; '
                f'fastcgi_temp_path {root}/fastcgi; uwsgi_temp_path {root}/uwsgi; scgi_temp_path {root}/scgi; '
                f'server {{ listen 127.0.0.1:{port}; include {gate}; location / {{ return 200 "ready"; }} }} }}')
            binary=os.environ.get('P21_NGINX_BIN','/usr/sbin/nginx')
            check=subprocess.run([binary,'-t','-p',str(root),'-c',str(nginx)],capture_output=True,timeout=10)
            assert check.returncode==0, 'Native Nginx rejected maintenance configuration: '+check.stderr.decode().replace(token,'[probe]')
            process=subprocess.Popen([binary,'-p',str(root),'-c',str(nginx)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            processes.append(process); ready(process,port)
            for headers, expected in [({},503),({'X-Forwarded-For':'127.0.0.1','X-Real-IP':'127.0.0.1'},503),
                ({'X-GoJet-Upgrade-Probe':'0'*64},503),({'X-GoJet-Upgrade-Probe':token},200)]:
                connection=http.client.HTTPConnection('127.0.0.1',port,timeout=3)
                try:
                    connection.request('GET','/',headers=headers)
                    response=connection.getresponse(); response.read()
                    assert response.status==expected, 'Maintenance allowed a forged/missing capability'
                finally:
                    connection.close()

            redis_port=free_port()
            binary=os.environ.get('P21_REDIS_SERVER_BIN','/usr/bin/redis-server')
            process=subprocess.Popen([binary,'--bind','127.0.0.1','--port',str(redis_port),'--save','',
                '--appendonly','no','--dir',str(root),'--logfile',str(root/'redis.log')],
                stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
            processes.append(process); ready(process,redis_port)
            cli=os.environ.get('P21_REDIS_CLI_BIN','/usr/bin/redis-cli')

            def redis(*args):
                return subprocess.check_output([cli,'--raw','-h','127.0.0.1','-p',str(redis_port),*args],text=True,timeout=5).strip()

            keys=['risk:link:123:'+('a'*64),'risk:bio-child:456:'+('b'*64)]
            for key in keys:
                assert redis('SET',key,'stale')=='OK'
            assert redis('SET','unrelated:business','keep')=='OK'
            event=redis('XADD','gojet:analytics:clicks:v1','*','event_id','keep-event')
            code='require $argv[1]."/installer/src/UpgradeRedis.php"; '
            code+='echo GoJet\\Installer\\UpgradeRedis::invalidate(["port"=>(int)$argv[2],"db"=>0,"password"=>""]);'
            assert php(code,str(redis_port))=='2'
            assert php(code,str(redis_port))=='0'
            assert all(redis('EXISTS',key)=='0' for key in keys)
            assert redis('GET','unrelated:business')=='keep'
            assert redis('XLEN','gojet:analytics:clicks:v1')=='1'
            assert event in redis('XRANGE','gojet:analytics:clicks:v1','-','+')
            print(json.dumps(dict(native_nginx=True, native_redis=True, forged_loopback_headers_rejected=True,
                private_probe_accepted=True, stale_risk_keys_removed=True, repeated_invalidation_safe=True,
                unrelated_data_and_analytics_stream_preserved=True, systemd_tested=False)))
        finally:
            for process in reversed(processes):
                if process.poll() is None:
                    process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill(); process.wait(timeout=5)


if __name__=='__main__':
    main()
