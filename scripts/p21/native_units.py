"""Validate shipped unit syntax with real built executables, without starting units."""
from pathlib import Path
import subprocess
import tempfile

def verify_units(repository, binaries, services):
    directory=repository/'deploy/systemd'
    expected={'gojet-'+name+'.service' for name in services}
    if {p.name for p in directory.glob('*.service')}!=expected: raise ValueError('incomplete or extra native service unit')
    with tempfile.TemporaryDirectory() as d:
        paths=[]
        for name in sorted(expected):
            original=(directory/name).read_text()
            if 'User=gojet\n' not in original or 'NoNewPrivileges=yes\n' not in original:
                raise ValueError('unit privilege boundary')
            # Relocate only immutable release paths for syntax/exec-existence
            # verification. No system configuration, unit enable or start occurs.
            rendered=original.replace('/opt/gojet/current/bin/',str(binaries)+'/').replace('WorkingDirectory=/opt/gojet/current','WorkingDirectory='+str(binaries))
            path=Path(d)/name; path.write_text(rendered); paths.append(str(path))
        result=subprocess.run(['systemd-analyze','verify','--man=no',*paths],capture_output=True,text=True)
        if result.returncode: raise ValueError('systemd unit verification: '+result.stderr)
    return {'units':sorted(expected),'syntax_verified':True,'started':False,'production_install_claim':False}
