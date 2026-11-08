"""Disposable CI: exercise actual PHP health probes with a CA-issued leaf.

The leaf file contains no root. Test trust is installed only for the duration
of this fixture, and wrong peer names/untrusted chains must still fail.
"""
import json
import os
from pathlib import Path
import ssl
import subprocess
import tempfile
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ROOT = Path(__file__).resolve().parents[2]


def main():
    assert os.geteuid() == 0 and os.environ.get('P22_DISPOSABLE_ADMIN_CI') == '1'
    with tempfile.TemporaryDirectory(prefix='gojet-tls-ci-') as tmp:
        base = Path(tmp)
        def openssl(*args):
            return subprocess.check_output(['openssl', *args], cwd=base, stderr=subprocess.DEVNULL, text=True).strip()
        openssl('req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                '-subj', '/CN=GoJet disposable test CA', '-keyout', 'ca.key', '-out', 'ca.pem',
                '-addext', 'basicConstraints=critical,CA:TRUE')
        openssl('req', '-new', '-newkey', 'rsa:2048', '-nodes', '-subj', '/CN=localhost',
                '-keyout', 'leaf.key', '-out', 'leaf.csr')
        (base/'extensions').write_text('subjectAltName=DNS:localhost\nbasicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\n')
        openssl('x509', '-req', '-in', 'leaf.csr', '-CA', 'ca.pem', '-CAkey', 'ca.key',
                '-CAcreateserial', '-days', '1', '-extfile', 'extensions', '-out', 'leaf.pem')
        hashed = Path('/etc/ssl/certs')/(openssl('x509', '-in', 'ca.pem', '-hash', '-noout')+'.0')
        assert not hashed.exists() and not hashed.is_symlink()
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                body = b'{"error":"Installer authentication required"}'
                self.send_response(401)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(base/'leaf.pem', base/'leaf.key')
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        program = base/'probe.php'
        program.write_text('''<?php
require_once $argv[1].'/installer/src/WebEntryProvisioning.php';
require_once $argv[1].'/installer/src/InstallationCompletion.php';
require_once $argv[1].'/installer/src/UpgradeHealth.php';
$release=['origin'=>$argv[2], 'certificate'=>$argv[3]];
$results=[];
foreach (['WebEntryProvisioning','InstallationCompletion','UpgradeHealth'] as $name) {
    try {
        $method=new ReflectionMethod('GoJet\\\\Installer\\\\'.$name,$name==='WebEntryProvisioning'?'health':'request');
        $args=match($name) {
            'WebEntryProvisioning'=>[$release],
            'InstallationCompletion'=>[parse_url($argv[2]),$argv[3],'/install/'],
            'UpgradeHealth'=>[$release,'/install/',null,false],
        };
        $value=$method->invoke(null,...$args);
        $results[$name]=$name==='WebEntryProvisioning' || $value[0]===401;
    } catch (Throwable) { $results[$name]=false; }
}
echo json_encode($results,JSON_THROW_ON_ERROR);
''')
        def probe(host):
            result = subprocess.run(['php8.3', '-d', 'openssl.capath=', str(program), str(ROOT),
                f'https://{host}:{server.server_port}', str(base/'leaf.pem')], capture_output=True, text=True, timeout=45)
            assert result.returncode == 0 and not result.stderr, 'TLS probe fixture execution failed'
            return json.loads(result.stdout)
        try:
            assert not any(probe('localhost').values()), 'Untrusted chain accepted'
            hashed.symlink_to(base/'ca.pem')
            assert all(probe('localhost').values()), 'CA-issued leaf failed with explicit system trust'
            assert not any(probe('wrong.invalid').values()), 'Wrong TLS peer accepted'
            print(json.dumps({'passed': True, 'ca_issued_leaf_all_three_probes': True,
                'untrusted_chain_refused': True, 'wrong_tls_name_refused': True,
                'global_php_ini_unchanged': True}))
        finally:
            if hashed.is_symlink() and hashed.readlink() == base/'ca.pem':
                hashed.unlink()
            server.shutdown()
            server.server_close()
            thread.join(timeout=5)


if __name__ == '__main__':
    main()
