"""Stage an independently checksummed native payload; never chmod an old release.

The distribution ships this as stage-release.py beside release.json. The bundle
checksum must be verified before executing any bundled program.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import shutil
import subprocess
import tarfile


def sha(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def inspect_archive(archive, identity):
    if sha(archive) != identity['archive_sha256']:
        raise ValueError('Archive checksum mismatch')
    with tarfile.open(archive, 'r:gz') as tar:
        rows = tar.getmembers()
        names = set()
        total = 0
        for row in rows:
            path = PurePosixPath(row.name)
            if (not row.isfile() or row.name in names or str(path) != row.name or path.is_absolute()
                    or '..' in path.parts or '\\' in row.name or any(p.startswith('.') for p in path.parts)
                    or row.uid != 0 or row.gid != 0 or row.size > 128 * 1024 * 1024
                    or row.mode != (0o755 if row.name.startswith('bin/') else 0o644)):
                raise ValueError('Unsafe archive member')
            total += row.size
            if total > 1024 * 1024 * 1024:
                raise ValueError('Archive exceeds extraction limit')
            names.add(row.name)
        manifest = tar.extractfile('manifest.json').read()
        if hashlib.sha256(manifest).hexdigest() != identity['manifest_sha256']:
            raise ValueError('Manifest checksum mismatch')
        m = json.loads(manifest)
        if m['source'] != identity['source'] or m['version'] != identity['version']:
            raise ValueError('Release identity mismatch')
        if names != set(m['files']) | {'manifest.json', 'sbom.cdx.json', 'SHA256SUMS'}:
            raise ValueError('Archive inventory mismatch')
        for name, expected in m['files'].items():
            row = tar.getmember(name)
            with tar.extractfile(row) as stream:
                digest = hashlib.file_digest(stream, 'sha256').hexdigest()
            if expected != {'sha256': digest, 'bytes': row.size, 'mode': row.mode}:
                raise ValueError('Archive member integrity mismatch')
    return total


def stage(archive, identity, destination):
    total = inspect_archive(archive, identity)  # No directory changes before validation.
    parent = destination.parent
    if parent.resolve() != parent:
        raise ValueError('Release parent must not contain symbolic links')
    for path in [parent, *parent.parents]:
        st = path.stat()
        if st.st_uid != 0 or stat.S_IMODE(st.st_mode) & 0o022:
            raise ValueError('Release parent must be root owned and not publicly writable')
    if destination.exists() or destination.is_symlink():
        raise ValueError('Release already exists; use verify, never extract over it')
    if shutil.disk_usage(parent).free < total * 2:
        raise ValueError('Insufficient free space for release staging')
    destination.mkdir(mode=0o755)
    destination.chmod(0o755)
    # Explicit umask is essential: GNU tar creates implicit parent directories
    # from the caller's umask even with --same-permissions on regular members.
    subprocess.run(['tar', '--extract', '--gzip', '--file', str(archive),
                    '--directory', str(destination), '--keep-old-files', '--same-permissions',
                    '--no-same-owner'], check=True, umask=0o022)
    verify(destination, identity)
    return destination


def verify(destination, identity):
    subprocess.run(['php8.3', str(destination/'installer/bin/verify-package.php'),
                    str(destination), identity['manifest_sha256'], identity['source']], check=True, capture_output=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['stage', 'verify'])
    args = parser.parse_args()
    if os.geteuid() != 0:
        raise ValueError('Run as root')
    base = Path(__file__).resolve().parent
    identity = json.loads((base/'release.json').read_text())
    if not re.fullmatch(r'10\.0\.0-rc\.[1-9][0-9]*', identity['version']) or not re.fullmatch(r'[a-f0-9]{40}', identity['source']):
        raise ValueError('Invalid release identity')
    archive = base / f"gojet-{identity['version']}-linux-amd64-payload.tar.gz"
    parent = Path('/opt/gojet/releases')
    destination = parent / (identity['version']+'-'+identity['source'][:7])
    if args.action == 'stage':
        inspect_archive(archive, identity)
        # Creation itself must not follow an existing symbolic-link ancestor.
        for path in [Path('/opt'), Path('/opt/gojet'), parent]:
            if path.is_symlink():
                raise ValueError('Symbolic link in release parent')
            if not path.exists():
                path.mkdir(mode=0o755)
                path.chmod(0o755)
            st = path.stat()
            if st.st_uid != 0 or stat.S_IMODE(st.st_mode) & 0o022:
                raise ValueError('Unsafe release parent')
        stage(archive, identity, destination)
    else:
        # Authenticate the verifier bytes from the approved archive before use.
        inspect_archive(archive, identity)
        with tarfile.open(archive, 'r:gz') as tar:
            m = json.load(tar.extractfile('manifest.json'))
        for name, row in m['files'].items():
            if name.startswith('installer/'):
                path = destination/name
                st = path.lstat()
                if path.resolve() != path or not stat.S_ISREG(st.st_mode) or st.st_uid != 0 or st.st_nlink != 1 or stat.S_IMODE(st.st_mode) != row['mode'] or sha(path) != row['sha256']:
                    raise ValueError('Installed verifier integrity mismatch')
        verify(destination, identity)
    print(json.dumps({'release': str(destination), 'staged': True, 'services_switched': False}))


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, tarfile.TarError, subprocess.SubprocessError) as error:
        raise SystemExit('Staging stopped; preserve existing files: '+str(error))
