import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from archive import allowed, digest, write_archive, verify_archive
from runbooks import PATHS, RUNBOOKS, validate

ROOT = Path(__file__).resolve().parents[2]


class RunbookTests(unittest.TestCase):
    def files(self):
        # Resolve documented commands against actual repository payload sources.
        files = {p.relative_to(ROOT).as_posix(): b'' for folder in ('installer/bin', 'deploy')
                 for p in (ROOT/folder).rglob('*') if p.is_file() and allowed(p.relative_to(ROOT).as_posix())}
        files.update({'runbooks/'+name: (ROOT/'docs/release'/name).read_bytes() for name in RUNBOOKS})
        return files

    def test_actual_runbooks_resolve_and_parse_without_execution(self):
        result = validate(self.files())
        self.assertEqual(result['files'], sorted(PATHS))
        self.assertGreater(result['shell_blocks_syntax_checked'], 5)
        self.assertFalse(result['commands_executed'])

    def test_missing_extra_dangling_link_and_command(self):
        for mutation in ('missing', 'extra', 'link', 'escape', 'command', 'syntax'):
            with self.subTest(mutation=mutation):
                files = self.files()
                if mutation == 'missing': del files['runbooks/install.md']
                elif mutation == 'extra': files['runbooks/private.md'] = b'private'
                else:
                    files['runbooks/README.md'] += {
                        'link': b'\n[missing](absent.md)\n', 'escape': b'\n[escape](../manifest.json)\n',
                        'command': b'\ninstaller/bin/fictional-backup.php\n',
                        'syntax': b'\n```bash\nif then\n```\n'}[mutation]
                with self.assertRaises(ValueError): validate(files)

    def test_archive_includes_runbooks_and_rejects_tampering(self):
        self.assertFalse(allowed('runbooks/private.md'))
        self.assertFalse(allowed('runbooks/../README.md'))
        with tempfile.TemporaryDirectory() as directory:
            good, bad = Path(directory)/'good', Path(directory)/'bad'
            files = {name: data for name, data in self.files().items() if name in PATHS}
            write_archive(files, good, 'a'*40, '10.0.0-rc.1', [])
            with tarfile.open(good, 'r:gz') as src, tarfile.open(bad, 'w:gz') as dst:
                for row in src:
                    data = src.extractfile(row).read()
                    if row.name == 'runbooks/install.md':
                        data += b'\nchanged instructions\n'
                        row.size = len(data)
                    dst.addfile(row, io.BytesIO(data))
            with self.assertRaisesRegex(ValueError, 'member integrity mismatch'):
                verify_archive(bad, digest(bad.read_bytes()), 'a'*40)
