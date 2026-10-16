"""Check the target-host PHP verifier against the actual compiled release payload."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
from archive import verify_archive

repo = Path(__file__).resolve().parents[2]
archive, source = Path(sys.argv[1]).resolve(), sys.argv[2]
assert os.geteuid() == 0
expected_archive = archive.with_name(archive.name+'.sha256').read_text().split()[0]
verified = verify_archive(archive, expected_archive, source)
sha = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
with tempfile.TemporaryDirectory(prefix='gojet-integrity-') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    release = root/'release'
    release.mkdir(mode=0o755)
    with tarfile.open(archive, 'r:gz') as tar:
        tar.extractall(release, filter='data')
    digest = sha(release/'manifest.json')
    # The verifier itself is from the trusted checkout, not executed from an
    # as-yet unverified release. Copy to a traversable root-owned tools directory.
    shutil.copytree(repo/'installer', root/'tools')
    command = ['php8.3', str(root/'tools/bin/verify-package.php')]

    def call(manifest=digest, commit=source, nonroot=False):
        return subprocess.run((['runuser','-u','nobody','--'] if nonroot else []) + command + [str(release),manifest,commit],
            cwd=root, env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin'}, capture_output=True, timeout=60)

    def rejected(result):
        assert result.returncode == 1 and not result.stdout
        assert result.stderr == b'Package integrity verification refused.\n', 'Non-generic verification diagnostic'

    result = call()
    assert result.returncode == 0 and not result.stderr, 'Actual payload verification refused'
    report = json.loads(result.stdout)
    assert report['integrity_verified'] and report['installable'] is False
    assert report['members'] == verified['members'] and report['manifest_sha256'] == digest
    rejected(call(manifest='0'*64))
    rejected(call(commit='0'*40))
    rejected(call(nonroot=True))
    target = release/'deploy/sysusers/gojet.conf'
    original = target.read_bytes()
    target.write_bytes(original+b'\n')
    rejected(call())
    target.write_bytes(original)
    target.chmod(0o666)
    rejected(call())
    target.chmod(0o644)
    os.chown(target, 65534, 65534)
    rejected(call())
    os.chown(target, 0, 0)
    target.unlink()
    rejected(call())
    outside = root/'outside'
    outside.write_bytes(original)
    target.symlink_to(outside)
    rejected(call())
    target.unlink()
    os.link(outside, target)
    rejected(call())
    target.unlink()
    target.write_bytes(original)
    target.chmod(0o644)
    extra = release/'unexpected.env'
    extra.write_text('CI-only unexpected data')
    rejected(call())
    extra.unlink()
    extra.mkdir()
    rejected(call())
    extra.rmdir()
    directory = release/'deploy'
    directory.chmod(0o777)
    rejected(call())
    directory.chmod(0o755)
    sums = release/'SHA256SUMS'
    original_sums = sums.read_bytes()
    sums.write_bytes(original_sums+b'\n')
    rejected(call())
    sums.write_bytes(original_sums)
    manifest = release/'manifest.json'
    original_manifest = manifest.read_bytes()
    manifest.write_bytes(original_manifest+b' ')
    rejected(call())
    manifest.write_bytes(original_manifest)
    assert call().returncode == 0
print(json.dumps(dict(source=source, archive_sha256=expected_archive, manifest_sha256=digest,
    members=report['members'], actual_release_verified=True, root_only=True,
    wrong_authority_rejected=True, missing_tampered_extra_rejected=True,
    symlink_hardlink_rejected=True, owner_modes_rejected=True,
    checksum_inventory_rejected=True, restored_payload_verified=True,
    dependency_preflight_complete=False, installable=False)))
