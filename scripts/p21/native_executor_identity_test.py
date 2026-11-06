"""Verify the shipped probe launcher drops identity and keeps NNP in nested systemd units.
The diagnostic executable only reports identity; real health is tested by the payload job.
"""
import json
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import tempfile

assert os.geteuid() == 0
repo = Path(__file__).resolve().parents[2]
subprocess.run(['systemd-sysusers', str(repo/'deploy/sysusers/gojet.conf')], check=True)
account = pwd.getpwnam('gojet')
protections = ['NoNewPrivileges=yes', 'ProtectHome=yes', 'ProtectKernelTunables=yes',
               'ProtectKernelModules=yes', 'ProtectControlGroups=yes', 'RestrictSUIDSGID=yes']
with tempfile.TemporaryDirectory(prefix='gojet-probe-context-', dir='/var/lib') as temporary:
    root = Path(temporary)
    root.chmod(0o755)
    (root/'bin').mkdir(mode=0o755)
    shutil.copyfile(repo/'installer/src/UnprivilegedProbe.php', root/'UnprivilegedProbe.php')
    diagnostic = root/'bin/filepreflight'
    diagnostic.write_text('''#!/usr/bin/python3
import json
from pathlib import Path
fields = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
print(json.dumps({key: fields[key].strip() for key in ['Uid', 'Gid', 'NoNewPrivs', 'CapEff', 'CapBnd']}))
''')
    diagnostic.chmod(0o755)
    runner = root/'probe.php'
    runner.write_text('''<?php
require __DIR__.'/UnprivilegedProbe.php';
$argv = GoJet\\Installer\\UnprivilegedProbe::command(__DIR__, 'filepreflight',
    ['GOJET_FILE_STORAGE_ROOT'=>__DIR__, 'GOJET_CLAMAV_NETWORK'=>'unix', 'GOJET_CLAMAV_ADDRESS'=>'/nonexistent']);
$p = proc_open($argv, [0=>['file','/dev/null','r'], 1=>STDOUT, 2=>STDERR], $pipes, '/');
if (!is_resource($p)) { exit(99); }
exit(proc_close($p));
''')
    command = ['systemd-run', '--quiet', '--wait', '--pipe', '--collect', '--property=User=root', '--property=Group=root']
    for value in protections:
        command += ['--property='+value]
    result = subprocess.run(command+['/usr/bin/php8.3', str(runner)], capture_output=True, text=True, timeout=30)
    assert result.returncode == 0 and not result.stderr, 'Nested unprivileged probe failed'
    report = json.loads(result.stdout)
    assert set(map(int, report['Uid'].split())) == {account.pw_uid}
    assert set(map(int, report['Gid'].split())) == {account.pw_gid}
    assert report['NoNewPrivs'] == '1'
    assert int(report['CapEff'], 16) == 0 and int(report['CapBnd'], 16) == 0
    print(json.dumps(dict(nonroot_identity=True, no_new_privileges=True, no_capabilities=True, nested_systemd=True)))
