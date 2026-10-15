import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from archive import allowed,digest,write_archive,verify_archive
class ArchiveTests(unittest.TestCase):
    def test_determinism_and_tamper(self):
        with tempfile.TemporaryDirectory() as d:
            a,b=Path(d)/'a.tgz',Path(d)/'b.tgz'; files={'migrations/000001_test.sql':b'SELECT 1;'}
            for p in (a,b): write_archive(files,p,'a'*40,'10.0.0-rc.1',[])
            self.assertEqual(a.read_bytes(),b.read_bytes())
            with self.assertRaises(ValueError): verify_archive(a,'0'*64,'a'*40)
            with self.assertRaises(ValueError): verify_archive(a,digest(a.read_bytes()),'b'*40)
    def test_disallowed_and_secret(self):
        for name in ['../escape','/etc/passwd','static/site/../../escape','static/site/.env','static/site/node_modules/x.js','bin/node','Dockerfile','installer/public/shell.php']:
            self.assertFalse(allowed(name),name)
        with tempfile.TemporaryDirectory() as d:
            with self.assertRaises(ValueError): write_archive({'static/site/key.txt':b'-----BEGIN PRIVATE KEY-----'},Path(d)/'a','a'*40,'1',[])
    def test_links_duplicates_and_payload_change(self):
        with tempfile.TemporaryDirectory() as d:
            good,bad=Path(d)/'good',Path(d)/'bad'
            write_archive({'migrations/000001_test.sql':b'SELECT 1;'},good,'a'*40,'1',[])
            for kind in ['symlink','duplicate','change','unexpected']:
                with tarfile.open(good,'r:gz') as src,tarfile.open(bad,'w:gz') as dst:
                    for row in src:
                        data=src.extractfile(row).read()
                        if kind=='change' and row.name.endswith('.sql'): data=b'SELECT 2;'
                        dst.addfile(row,io.BytesIO(data))
                        if kind=='duplicate' and row.name.endswith('.sql'): dst.addfile(row,io.BytesIO(data))
                    if kind in ['symlink','unexpected']:
                        row=tarfile.TarInfo('static/site/extra.txt')
                        if kind=='symlink': row.type=tarfile.SYMTYPE; row.linkname='/etc/passwd'
                        dst.addfile(row,io.BytesIO(b''))
                with self.assertRaises(ValueError,msg=kind): verify_archive(bad,digest(bad.read_bytes()),'a'*40)
