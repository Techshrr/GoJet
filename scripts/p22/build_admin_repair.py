"""Build a separate RC candidate without editing the frozen P21 test plan.

Uses the inherited full native test/build and reproducible archive verifier.
This builds an upgrade candidate; it never operates on an installed host.
"""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import sys
import tarfile

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts/p21'))
import verify_build
import build_payload

VERSION = '10.0.0-rc.2'

def main():
    os.environ['P21_KEEP_BINARIES'] = '1'
    verify_build.main(VERSION)
    build_payload.main(VERSION)
    output = ROOT / 'artifacts/v10/P22/admin-repair-package'
    output.mkdir(parents=True, exist_ok=True)
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    import shutil
    payload = ROOT / f'artifacts/v10/P21/payload/gojet-{VERSION}-linux-amd64-payload.tar.gz'
    shutil.copyfile(payload, output / payload.name)
    verification = json.loads((ROOT / 'artifacts/v10/P21/payload/verification.json').read_text())
    with tarfile.open(payload, 'r:gz') as archive:
        manifest = archive.extractfile('manifest.json').read()
    verification['manifest_sha256'] = hashlib.sha256(manifest).hexdigest()
    verification['version'] = VERSION
    (output/'release.json').write_text(json.dumps({
        'version': VERSION, 'source': head,
        'archive_sha256': hashlib.sha256(payload.read_bytes()).hexdigest(),
        'manifest_sha256': verification['manifest_sha256'],
    }, sort_keys=True, indent=2)+'\n')
    shutil.copyfile(ROOT/'scripts/p22/stage_release.py', output/'stage-release.py')
    (output / 'verification.json').write_text(json.dumps(verification, indent=2) + '\n')
    subprocess.run(['git', 'archive', '--format=tar.gz', f'--prefix=GoJet-source-{head[:7]}/', '-o', str(output / f'gojet-source-{head[:7]}.tar.gz'), 'HEAD'], cwd=ROOT, check=True)
    shutil.copyfile(ROOT / 'docs/p22/admin-repair.md', output / 'START-HERE.md')
    checks = []
    for path in sorted(output.iterdir()):
        if path.is_file() and path.name != 'SHA256SUMS':
            checks.append(hashlib.sha256(path.read_bytes()).hexdigest() + '  ' + path.name)
    (output / 'SHA256SUMS').write_text('\n'.join(checks) + '\n')
    print(json.dumps({'version': VERSION, 'source': head, 'package': str(output), 'host_upgrade_complete': False}))

if __name__ == '__main__':
    main()
