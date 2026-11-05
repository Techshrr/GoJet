"""Release-only documentation contract; never execute operator instructions."""
from pathlib import PurePosixPath
import re
import subprocess

RUNBOOKS = ('README.md', 'install.md', 'operations.md', 'upgrade-recovery.md')
PATHS = frozenset('runbooks/' + name for name in RUNBOOKS)


def validate(files):
    actual = {name for name in files if name.startswith('runbooks/')}
    if actual != PATHS:
        raise ValueError('incomplete or unexpected release runbooks')
    shell_blocks = 0
    references = set()
    for name in sorted(PATHS):
        body = files[name].decode('utf-8')
        if not body.strip():
            raise ValueError('empty runbook: ' + name)
        # Runbooks use inline Markdown links and no external/repository links.
        for target in re.findall(r'\]\(([^)]+)\)', body):
            path = PurePosixPath(target)
            if str(path) != target or path.is_absolute() or '..' in path.parts or '\\' in target:
                raise ValueError('unsafe runbook link: ' + target)
            if 'runbooks/' + target not in PATHS:
                raise ValueError('unresolved runbook link: ' + target)
        for target in re.findall(r'(?:installer/bin|deploy/[a-z]+|bin)/[A-Za-z0-9_.-]+', body):
            if target not in files:
                raise ValueError('runbook references absent payload: ' + target)
            references.add(target)
        for language, script in re.findall(r'^```([^\n]*)\n(.*?)^```\s*$', body, re.M | re.S):
            if language in ('sh', 'bash'):
                result = subprocess.run(['bash', '-n'], input=script, text=True, capture_output=True)
                if result.returncode:
                    raise ValueError('invalid shell syntax in ' + name + ': ' + result.stderr)
                shell_blocks += 1
            elif language not in ('json', 'text', ''):
                raise ValueError('unsupported runbook fence: ' + language)
    if not shell_blocks:
        raise ValueError('missing operator command examples')
    return {'files': sorted(PATHS), 'shell_blocks_syntax_checked': shell_blocks,
            'payload_references_checked': sorted(references), 'commands_executed': False}
