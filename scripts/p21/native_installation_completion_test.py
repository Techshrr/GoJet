"""Shipped final health and root completion authority, disposable native host only."""
import fcntl
import json
import os
from pathlib import Path
import subprocess
import time


def verify_completion(root, release, prepared, on_completed=None, on_verified=None):
    authority = Path('/var/lib/gojet-installer-authority')
    marker = authority/'installed.lock'
    public = Path('/var/lib/gojet-installation-result/result.json')
    assert not public.exists() and not public.is_symlink()
    assert not marker.exists() and not marker.is_symlink()
    target = Path('/etc/nginx/conf.d/gojet.conf')
    expected = target.read_bytes()
    original = {p: (prepared/p).read_bytes() for p in ['data/journal.json', 'services.json', 'site.json']}
    check = ['php8.3', str(release/'installer/bin/check-installation.php'), str(prepared)]
    complete = ['php8.3', str(release/'installer/bin/complete-installation.php'), str(prepared)]

    def call(command, ok=True, nonroot=False):
        r = subprocess.run((['runuser', '-u', 'nobody', '--'] if nonroot else [])+command,
            capture_output=True, timeout=150)
        if ok:
            assert r.returncode == 0 and not r.stderr, 'Native completion health failed'
            return json.loads(r.stdout)
        assert r.returncode == 1 and not r.stdout
        assert r.stderr == b'Installation verification refused; completion not acknowledged.\n'

    def ctl(*args):
        subprocess.run(['systemctl', *args], check=True, capture_output=True, timeout=40)

    try:
        call(complete, False, True)
        with (authority/'service-activation.lock').open('rb') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            call(complete, False)
        marker.symlink_to(authority/'absent-authority')
        try:
            call(complete, False)
            assert marker.is_symlink()
        finally:
            marker.unlink()
        ctl('stop', 'gojet-fileworker.service')
        try:
            call(complete, False)
            assert not marker.exists()
        finally:
            ctl('start', 'gojet-fileworker.service')
        ctl('stop', 'nginx.service')
        try:
            call(complete, False)
            assert not marker.exists()
        finally:
            ctl('start', 'nginx.service')
        # Disk bytes remain approved while the running TLS server has another certificate.
        # This distinguishes a live TLS check from only validating files/nginx -t.
        wrong_cert = root/'wrong-certificate.pem'
        wrong_key = root/'wrong-key.pem'
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
            '-keyout', str(wrong_key), '-out', str(wrong_cert), '-days', '1',
            '-subj', '/CN=localhost', '-addext', 'subjectAltName=DNS:localhost'],
            check=True, capture_output=True, timeout=20)
        wrong_key.chmod(0o600)
        # Derive the actual private key path from the sealed binding, never assume fixture spelling.
        seal = json.loads((prepared/'seal.json').read_text())
        changed = expected
        for path, binding in seal['tls_files'].items():
            changed = changed.replace(path.encode(), str(wrong_key if binding['private'] else wrong_cert).encode())
        assert changed != expected
        target.write_bytes(changed)
        try:
            ctl('reload', 'nginx.service')
            time.sleep(.3)
            target.write_bytes(expected)
            call(complete, False)
            assert not marker.exists()
        finally:
            target.write_bytes(expected)
            ctl('reload', 'nginx.service')
        time.sleep(.3)
        report = call(check)
        assert report['healthy'] is True and report['installation_complete'] is False
        assert not marker.exists()
        first = call(complete)
        assert first['installation_complete'] is True and first['already_applied'] is False
        if on_completed is not None: on_completed()
        public_raw = public.read_bytes()
        public_value = json.loads(public_raw)
        assert set(public_value) == {'schema', 'result_id', 'installation_complete'}
        assert public_value['installation_complete'] is True
        assert public.stat().st_uid == 0 and public.stat().st_mode & 0o777 == 0o644
        stat = marker.stat()
        assert stat.st_uid == 0 and stat.st_mode & 0o777 == 0o600 and stat.st_nlink == 1
        raw = marker.read_bytes()
        record = json.loads(raw)
        assert record == dict(schema='gojet.installation-completion.v1', source=seal['source'],
            manifest_sha256=seal['manifest_sha256'], seal_sha256=__import__('hashlib').sha256((prepared/'seal.json').read_bytes()).hexdigest())
        # Root marker survived but public receipt did not: explicit matching completion repairs it.
        public.unlink()
        public.symlink_to(marker)
        call(complete, False)
        assert public.is_symlink() and marker.read_bytes() == raw
        public.unlink()
        repeat = call(complete)
        assert public.read_bytes() == public_raw
        public.write_bytes(b'{}')
        call(complete, False)
        assert public.read_bytes() == b'{}' and marker.read_bytes() == raw
        public.write_bytes(public_raw)
        repeat = call(complete)
        assert repeat['installation_complete'] is True and repeat['already_applied'] is True
        assert marker.read_bytes() == raw
        for name in ['activate-services', 'recover-services', 'activate-site', 'recover-site']:
            r = subprocess.run(['php8.3', str(release/'installer/bin'/f'{name}.php'), str(prepared)], capture_output=True, timeout=90)
            assert r.returncode == 1 and not r.stdout and marker.read_bytes() == raw
        # The existing HTTP adapter reads this real root marker before authentication/state.
        code = "require $argv[1]; $r=GoJet\\Installer\\HttpAccess::handle(['REQUEST_URI'=>'/install'], [], '', '/missing', '', 'https://localhost', $argv[2]); echo $r[0];"
        r = subprocess.run(['php8.3', '-r', code, str(release/'installer/src/HttpAccess.php'), str(marker)], capture_output=True, timeout=10)
        assert r.returncode == 0 and r.stdout == b'404' and not r.stderr
        for name, raw_before in original.items():
            assert (prepared/name).read_bytes() == raw_before
        if on_verified is not None: on_verified()
    finally:
        if public.exists() or public.is_symlink():
            assert not public.is_symlink() and json.loads(public.read_text())["installation_complete"] is True
            public.unlink()
        # Explicit removal solely to dismantle this isolated CI installation.
        if marker.exists() or marker.is_symlink():
            marker.unlink()
        target.write_bytes(expected)
