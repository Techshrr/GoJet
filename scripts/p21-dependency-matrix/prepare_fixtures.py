"""Extract signed Ubuntu packages for disposable negative tests; never install them."""
import hashlib
import json
import os
from pathlib import Path
import pwd
import subprocess
import sys


def run(*argv, cwd=None):
    return subprocess.check_output(argv, cwd=cwd, text=True, timeout=240)


def prepare(root):
    if os.geteuid() != 0 or root.exists():
        raise RuntimeError('A new root-owned fixture directory is required')
    root.mkdir(mode=0o755)
    records = []
    keyring = '/usr/share/keyrings/ubuntu-archive-keyring.gpg'
    # Optional mirror overrides are only for build environments; Ubuntu signature
    # and package hash verification remain mandatory, with no trusted=yes bypass.
    archive = os.environ.get('P21_UBUNTU_ARCHIVE_URL', 'https://archive.ubuntu.com/ubuntu')
    security = os.environ.get('P21_UBUNTU_SECURITY_URL', 'https://security.ubuntu.com/ubuntu')
    for distribution, packages, target in [
        ('jammy', ['php8.1-cli', 'php8.1-fpm', 'php8.1-common', 'php8.1-mysql'], 'php81'),
        ('noble', ['mariadb-server', 'mariadb-server-core', 'liburing2', 'libnuma1'], 'mariadb'),
    ]:
        area = root/distribution
        area.mkdir(mode=0o755)
        lists, downloads = area/'lists', area/'downloads'
        lists.mkdir(); (lists/'partial').mkdir(); downloads.mkdir()
        apt = pwd.getpwnam('_apt')
        os.chown(downloads, apt.pw_uid, apt.pw_gid)
        sources = area/'sources.list'
        sources.write_text(''.join(
            f'deb [arch=amd64 signed-by={keyring}] {uri} {suite} main universe\n'
            for uri, suite in [(archive, distribution), (archive, distribution+'-updates'),
                               (security, distribution+'-security')]))
        options = ['-o', 'Dir::Etc::sourcelist='+str(sources), '-o', 'Dir::Etc::sourceparts=-',
                   '-o', 'Dir::State::lists='+str(lists), '-o', 'Acquire::Retries=2',
                   '-o', 'APT::Get::List-Cleanup=0']
        run('apt-get', *options, 'update', '-qq')
        run('apt-get', *options, 'download', *packages, cwd=downloads)
        dest = root/target
        dest.mkdir(mode=0o755)
        found = set()
        for deb in sorted(downloads.glob('*.deb')):
            name = run('dpkg-deb', '-f', str(deb), 'Package').strip()
            version = run('dpkg-deb', '-f', str(deb), 'Version').strip()
            architecture = run('dpkg-deb', '-f', str(deb), 'Architecture').strip()
            if name not in packages or name in found or architecture not in ('amd64', 'all'):
                raise RuntimeError('Unexpected fixture package')
            found.add(name)
            run('dpkg-deb', '-x', str(deb), str(dest))
            records.append({'package': name, 'version': version, 'distribution': distribution,
                            'architecture': architecture, 'sha256': hashlib.sha256(deb.read_bytes()).hexdigest()})
        if found != set(packages):
            raise RuntimeError('Missing fixture package')
    manifest = {'schema': 'gojet.dependency-version-fixtures.v1', 'signed_ubuntu_apt_metadata': True,
                'debian_maintainer_scripts_executed': False, 'host_packages_replaced': False, 'packages': records}
    (root/'provenance.json').write_text(json.dumps(manifest, indent=2)+'\n')
    return manifest


if __name__ == '__main__':
    print(json.dumps(prepare(Path(sys.argv[1]).resolve())))
