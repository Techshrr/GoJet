"""Isolate identity switching under installer protections on a disposable CI host."""
import json
import subprocess

protections = ['NoNewPrivileges=yes', 'ProtectHome=yes', 'ProtectKernelTunables=yes',
               'ProtectKernelModules=yes', 'ProtectControlGroups=yes', 'RestrictSUIDSGID=yes']
subprocess.run(['sudo', 'useradd', '--system', '--no-create-home', '--shell', '/usr/sbin/nologin', 'gojet-context'], check=True)
results = {}
for omitted in [None, *protections]:
    command = ['sudo', 'systemd-run', '--quiet', '--wait', '--pipe', '--collect', '--property=User=root', '--property=Group=root']
    for value in protections:
        if value != omitted:
            command += ['--property='+value]
    for method, switch in [('runuser', ['/usr/sbin/runuser', '-u', 'gojet-context', '--']),
                           ('setpriv', ['/usr/bin/setpriv', '--reuid=gojet-context', '--regid=gojet-context', '--init-groups', '--'])]:
        result = subprocess.run(command+switch+['/usr/bin/id', '-u'], capture_output=True, text=True, timeout=20)
        # Fixed identity-only command: no installation path, environment or credential is supplied.
        results[(omitted or 'all')+':'+method] = {'exit': result.returncode, 'uid': result.stdout.strip() if result.stdout.strip().isdigit() else None,
                                               'error': result.stderr[:1024]}
print(json.dumps(results, sort_keys=True))
