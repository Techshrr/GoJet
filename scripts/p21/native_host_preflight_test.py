"""Real host-boundary failures in private mount namespaces on the CI runner."""
import copy
import errno
import json
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import sys
import tempfile


def exercise_host_boundaries(plan):
    results = {}
    for case in ('wrong-os', 'wrong-release', 'missing-os', 'wrong-cpu', 'release-space', 'storage-space'):
        result = subprocess.run(['unshare', '--mount', '--propagation', 'private', sys.executable,
                                 str(Path(__file__).resolve()), case],
                                input=json.dumps(plan), capture_output=True, text=True, timeout=120)
        assert result.returncode == 0, 'Native host fixture failed: '+case+' '+result.stderr[-2000:]
        report = json.loads(result.stdout)
        expected = {'host'}
        assert report['ready'] is False and report['installation_complete'] is False
        assert {name for name, passed in report['checks'].items() if not passed} == expected, (case, report)
        results[case] = True
    return results


def fixture(case, original):
    plan = copy.deepcopy(original)
    mounts = []
    with tempfile.TemporaryDirectory(prefix='gojet-host-') as temporary:
        root = Path(temporary)
        root.chmod(0o755)
        command = ['php8.3', str(Path(plan['release'])/'installer/bin/preflight.php')]
        def mount(*args):
            subprocess.run(['mount', *args], check=True, capture_output=True)
            mounts.append(args[-1])
        try:
            if case in ('wrong-os', 'wrong-release', 'missing-os'):
                identity = root/'os-release'
                identity.write_text({'wrong-os': 'ID=debian\nVERSION_ID="24.04"\n',
                    'wrong-release': 'ID=ubuntu\nVERSION_ID="22.04"\n', 'missing-os': ''}[case])
                mount('--bind', str(identity), '/etc/os-release')
            elif case == 'wrong-cpu':
                command = ['setarch', 'linux32', *command]
            elif case == 'release-space':
                original_release = Path(plan['release'])
                # Enough for all real package bytes, but not a second extracted payload.
                allocation = sum(((p.stat().st_size+4095)//4096)*4096 for p in original_release.rglob('*') if p.is_file())
                target = root/'release'
                target.mkdir()
                mount('-t', 'tmpfs', '-o', 'size='+str(allocation+1024*1024), 'tmpfs', str(target))
                shutil.copytree(original_release, target, dirs_exist_ok=True)
                # Keep the original pathname: systemd's separate service namespace
                # still sees the healthy original helper, while this probe sees
                # the identical package bytes on the capacity-limited filesystem.
                mount('--bind', str(target), str(original_release))
            elif case == 'storage-space':
                target = root/'storage'
                target.mkdir()
                mount('-t', 'tmpfs', '-o', 'size=65536', 'tmpfs', str(target))
                account = pwd.getpwnam('gojet')
                for directory in (target, target/'quarantine', target/'published'):
                    directory.mkdir(exist_ok=True)
                    directory.chmod(0o700)
                    os.chown(directory, account.pw_uid, account.pw_gid)
                with (target/'capacity-fixture').open('wb', buffering=0) as stream:
                    try:
                        while True:
                            stream.write(b'x'*4096)
                    except OSError as error:
                        assert error.errno == errno.ENOSPC
                assert os.statvfs(target).f_bavail == 0
                mount('--bind', str(target), plan['storage'])
            else:
                raise AssertionError('Unknown fixture')
            result = subprocess.run(command, input=json.dumps(plan), capture_output=True, text=True, timeout=100)
            assert result.returncode == 2 and not result.stderr, 'Host refusal did not use normal preflight result'
            assert not list(root.rglob('installed.lock'))
            print(result.stdout, end='')
        finally:
            for target in reversed(mounts):
                subprocess.run(['umount', target], check=True, capture_output=True)


if __name__ == '__main__':
    fixture(sys.argv[1], json.load(sys.stdin))
