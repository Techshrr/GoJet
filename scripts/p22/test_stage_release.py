import hashlib
import io
import json
from pathlib import Path
import sys
import tarfile
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'p21'))
from archive import write_archive
from stage_release import inspect_archive


class StageArchiveTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root/'release.tar.gz'
        write_archive({'migrations/000001_test.sql': b'SELECT 1;'}, self.archive, 'a'*40, '10.0.0-rc.2', [])
        with tarfile.open(self.archive, 'r:gz') as t:
            manifest = t.extractfile('manifest.json').read()
        self.identity = {'source':'a'*40, 'version':'10.0.0-rc.2',
            'manifest_sha256':hashlib.sha256(manifest).hexdigest(),
            'archive_sha256':hashlib.sha256(self.archive.read_bytes()).hexdigest()}

    def test_valid_and_wrong_binding(self):
        self.assertGreater(inspect_archive(self.archive, self.identity), 0)
        for key, wrong in [('source','b'*40),('version','10.0.0-rc.3'),
                           ('manifest_sha256','0'*64),('archive_sha256','0'*64)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                inspect_archive(self.archive, self.identity | {key:wrong})

    def test_unsafe_members_refused_before_extraction(self):
        for kind in ['symlink','hardlink','traversal','duplicate','tamper','mode','extra']:
            bad = self.root/(kind+'.tar.gz')
            with tarfile.open(self.archive,'r:gz') as src, tarfile.open(bad,'w:gz') as dst:
                for row in src:
                    data=src.extractfile(row).read()
                    if row.name.endswith('.sql'):
                        if kind=='tamper': data=b'SELECT 2;'
                        if kind=='mode': row.mode=0o666
                        if kind=='duplicate': dst.addfile(row,io.BytesIO(data))
                    dst.addfile(row,io.BytesIO(data))
                if kind in ['symlink','hardlink','traversal','extra']:
                    row=tarfile.TarInfo('../escape' if kind=='traversal' else 'unexpected')
                    row.mode=0o644
                    if kind in ['symlink','hardlink']:
                        row.type=tarfile.SYMTYPE if kind=='symlink' else tarfile.LNKTYPE
                        row.linkname='/etc/passwd'
                    dst.addfile(row,io.BytesIO(b''))
            identity=self.identity | {'archive_sha256':hashlib.sha256(bad.read_bytes()).hexdigest()}
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                inspect_archive(bad,identity)


if __name__ == '__main__':
    unittest.main()
